package install

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/alferio94/lore-cli/internal/compiler"
)

const profileStateVersion = 1

type ProfileStoreCode string

const (
	CodeProfileInvalid  ProfileStoreCode = "invalid_profile_state"
	CodeProfileCorrupt  ProfileStoreCode = "corrupt_profile_state"
	CodeProfileNotFound ProfileStoreCode = "profile_state_not_found"
	CodeProfileBusy     ProfileStoreCode = "profile_state_busy"
	CodeProfileTimeout  ProfileStoreCode = "profile_state_timeout"
	CodeProfileConflict ProfileStoreCode = "profile_state_conflict"
	CodeProfileIO       ProfileStoreCode = "profile_state_io"
)

func (c ProfileStoreCode) Error() string { return string(c) }

type ProfileStoreError struct {
	code         ProfileStoreCode
	path         string
	residualRisk bool
}

func (e *ProfileStoreError) Error() string          { return "profile state operation failed" }
func (e *ProfileStoreError) Code() ProfileStoreCode { return e.code }
func (e *ProfileStoreError) Path() string           { return e.path }
func (e *ProfileStoreError) ResidualRisk() bool     { return e.residualRisk }
func (e *ProfileStoreError) Is(target error) bool {
	code, ok := target.(ProfileStoreCode)
	return ok && code == e.code
}
func newProfileStoreError(code ProfileStoreCode, path string, residualRisk bool) *ProfileStoreError {
	return &ProfileStoreError{code: code, path: path, residualRisk: residualRisk}
}
func profileStoreError(code ProfileStoreCode) error { return newProfileStoreError(code, "", false) }

type ApplyBoundary string

const (
	ApplyBoundaryDryRun             ApplyBoundary = "dry_run"
	ApplyBoundaryReconcileRejected  ApplyBoundary = "reconcile_rejected"
	ApplyBoundaryApplyFailed        ApplyBoundary = "apply_failed"
	ApplyBoundaryFinalizationFailed ApplyBoundary = "finalization_failed"
	ApplyBoundaryManifestFailed     ApplyBoundary = "manifest_failed"
	ApplyBoundarySuccess            ApplyBoundary = "success"
)

type profileProject struct {
	Root      string             `json:"root"`
	ProjectID compiler.ProjectID `json:"project_id"`
	ProfileID string             `json:"profile_id,omitempty"`
}

type profileState struct {
	Version       int              `json:"version"`
	GlobalProfile string           `json:"global_profile,omitempty"`
	Projects      []profileProject `json:"projects"`
}

type ProfileStore struct {
	path     string
	platform storePlatform
	waiter   monotonicWaiter
	commit   func(authority, []byte, []byte) error
}

func NewProfileStore(path string) ProfileStore {
	return ProfileStore{path: filepath.Clean(path), platform: defaultStorePlatform(), waiter: systemMonotonicWaiter{}}
}
func (s ProfileStore) Path() string { return s.path }

type PreparedProject struct {
	path, root string
	id         compiler.ProjectID
	state      profileState
	baseline   []byte
	existed    bool
}

func (p PreparedProject) ProjectID() compiler.ProjectID { return p.id }

type ProjectSelection struct {
	ProjectID      compiler.ProjectID
	GlobalProfile  string
	ProjectProfile string
}

func (s ProjectSelection) EffectiveProfile() string {
	if s.ProjectProfile != "" {
		return s.ProjectProfile
	}
	return s.GlobalProfile
}

func (s ProfileStore) PrepareProject(root string) (PreparedProject, error) {
	root, err := validProjectRoot(root)
	if err != nil || !filepath.IsAbs(s.path) {
		return PreparedProject{}, profileStoreError(CodeProfileInvalid)
	}
	state, raw, existed, err := s.load()
	if err != nil {
		return PreparedProject{}, err
	}
	id := stableProjectID(root)
	for _, project := range state.Projects {
		if project.Root == root {
			id = project.ProjectID
			break
		}
	}
	return PreparedProject{path: s.path, root: root, id: id, state: state, baseline: raw, existed: existed}, nil
}

func (s ProfileStore) LookupProject(root string) (ProjectSelection, error) {
	root, err := validProjectRoot(root)
	if err != nil {
		return ProjectSelection{}, profileStoreError(CodeProfileInvalid)
	}
	state, _, _, err := s.load()
	if err != nil {
		return ProjectSelection{}, err
	}
	for _, project := range state.Projects {
		if project.Root == root {
			return ProjectSelection{ProjectID: project.ProjectID, GlobalProfile: state.GlobalProfile, ProjectProfile: project.ProfileID}, nil
		}
	}
	return ProjectSelection{}, profileStoreError(CodeProfileNotFound)
}

// Complete is the success-only profile-state handoff. Other recognized apply
// outcomes return before path canonicalization or authority acquisition.
func (s ProfileStore) Complete(prepared PreparedProject, fact PersistenceFact, boundary ApplyBoundary) error {
	return s.CompleteWithOptions(prepared, fact, boundary, CommitOptions{WaitBudget: defaultProfileStoreWait})
}

func (s ProfileStore) CompleteWithOptions(prepared PreparedProject, fact PersistenceFact, boundary ApplyBoundary, options CommitOptions) error {
	switch boundary {
	case ApplyBoundaryDryRun, ApplyBoundaryReconcileRejected, ApplyBoundaryApplyFailed, ApplyBoundaryFinalizationFailed, ApplyBoundaryManifestFailed:
		return nil
	case ApplyBoundarySuccess:
	default:
		return profileStoreError(CodeProfileInvalid)
	}
	if err := s.validateCompletion(prepared, fact, options); err != nil {
		return err
	}
	if err := s.protectCompletionDirectory(); err != nil {
		return err
	}
	return withStoreAuthority(s.platform, s.path, options, s.waiter, func(owned authority) error {
		raw, exists, err := s.platform.Read(owned)
		if err != nil {
			return mapProfileReadError(err)
		}
		data, changed, err := profileCompletionBytes(prepared, fact, raw, exists)
		if err != nil || !changed {
			return err
		}
		return s.commitProfileBytes(owned, raw, data)
	})
}

// profileCompletionLease keeps the W3.2 authority live across D's provisional
// profile publication, canonical v3 publication, and final selected-target
// completion. It is internal and cannot be used as a general restore API.
type profileCompletionLease struct {
	held     *heldStoreAuthority
	prior    heldProfileState
	changed  bool
	restored bool
	closed   bool
}

func (s ProfileStore) beginHeldProfileCompletion(prepared PreparedProject, fact PersistenceFact, options CommitOptions) (*profileCompletionLease, error) {
	if err := s.validateCompletion(prepared, fact, options); err != nil {
		return nil, err
	}
	if err := s.protectCompletionDirectory(); err != nil {
		return nil, err
	}
	held, err := acquireStoreAuthority(s.platform, s.path, options, s.waiter)
	if err != nil {
		return nil, err
	}
	if held.restore == nil {
		return nil, profileReleaseResult(newProfileStoreError(CodeProfileIO, "profile_store.commit", false), held, false)
	}
	raw, exists, prior, err := held.restore.snapshotHeld(held.owned)
	if err != nil || prior == nil {
		if errors.Is(err, errStoreAuthorityInvalidPath) {
			err = newProfileStoreError(CodeProfileInvalid, "profile_store.path", false)
		} else {
			err = newProfileStoreError(CodeProfileIO, "profile_store.commit", false)
		}
		return nil, profileReleaseResult(err, held, false)
	}
	data, changed, err := profileCompletionBytes(prepared, fact, raw, exists)
	if err != nil {
		return nil, profileReleaseResult(err, held, false)
	}
	lease := &profileCompletionLease{held: held, prior: prior, changed: changed}
	if !changed {
		return lease, nil
	}
	if err := s.commitProfileBytes(held.owned, raw, data); err != nil {
		var typed *ProfileStoreError
		residual := errors.As(err, &typed) && typed.ResidualRisk()
		return nil, profileReleaseResult(err, held, residual)
	}
	return lease, nil
}

func (s ProfileStore) validateCompletion(prepared PreparedProject, fact PersistenceFact, options CommitOptions) error {
	if !options.valid() || s.platform == nil || s.waiter == nil || !filepath.IsAbs(s.path) || prepared.path != s.path || prepared.root == "" || prepared.root != filepath.Clean(prepared.root) || prepared.id != stableProjectID(prepared.root) || validateProfileState(prepared.state) != nil || !validPersistenceFact(prepared, fact) {
		return profileStoreError(CodeProfileInvalid)
	}
	return nil
}

func (s ProfileStore) protectCompletionDirectory() error {
	dir := filepath.Dir(s.path)
	if os.MkdirAll(dir, 0o700) != nil || protectStoreDirectory(dir) != nil {
		return newProfileStoreError(CodeProfileIO, "profile_store.commit", false)
	}
	return nil
}

func profileCompletionBytes(prepared PreparedProject, fact PersistenceFact, raw []byte, exists bool) ([]byte, bool, error) {
	current, err := decodeProfileState(raw, exists)
	if err != nil {
		return nil, false, err
	}
	next, err := rebaseProfileState(prepared, fact, current)
	if err != nil {
		return nil, false, err
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return nil, false, profileStoreError(CodeProfileInvalid)
	}
	data = append(data, '\n')
	return data, !exists || !bytes.Equal(raw, data), nil
}

func (s ProfileStore) commitProfileBytes(owned authority, prior, next []byte) error {
	commit := s.commit
	if commit == nil {
		commit = s.platform.Commit
	}
	if err := commit(owned, prior, next); err != nil {
		if errors.Is(err, errStoreRollbackFailed) {
			return newProfileStoreError(CodeProfileIO, "profile_store.rollback", true)
		}
		return newProfileStoreError(CodeProfileIO, "profile_store.commit", false)
	}
	return nil
}

func mapProfileReadError(err error) error {
	if errors.Is(err, errStoreAuthorityInvalidPath) {
		return newProfileStoreError(CodeProfileInvalid, "profile_store.path", false)
	}
	return newProfileStoreError(CodeProfileIO, "profile_store.commit", false)
}

func (l *profileCompletionLease) commit() error {
	if l == nil || l.closed || l.held == nil {
		return newProfileStoreError(CodeProfileIO, "profile_store.commit", false)
	}
	l.closed = true
	return profileReleaseResult(nil, l.held, false)
}

func (l *profileCompletionLease) restore() error {
	if l == nil || l.closed || l.restored || l.held == nil || l.prior == nil {
		return newProfileStoreError(CodeProfileIO, "profile_store.rollback", true)
	}
	l.restored = true
	if l.changed {
		if restoreErr := l.held.restore.restoreHeld(l.held.owned, l.prior); restoreErr != nil {
			return newProfileStoreError(CodeProfileIO, "profile_store.rollback", true)
		}
	}
	return nil
}

func (l *profileCompletionLease) releaseRollback() error {
	if l == nil || l.closed || !l.restored || l.held == nil {
		return newProfileStoreError(CodeProfileIO, "profile_store.rollback", true)
	}
	l.closed = true
	return profileReleaseResult(nil, l.held, true)
}

func (l *profileCompletionLease) rollback() error {
	restoreErr := l.restore()
	releaseErr := l.releaseRollback()
	return errors.Join(restoreErr, releaseErr)
}

func validPersistenceFact(prepared PreparedProject, fact PersistenceFact) bool {
	if !fact.Requested {
		return fact.Scope == "" && fact.ProjectID == "" && fact.ProfileID == ""
	}
	if !validProfileID(fact.ProfileID) {
		return false
	}
	return (fact.Scope == compiler.ProfileScopeGlobal && fact.ProjectID == "") || (fact.Scope == compiler.ProfileScopeProject && fact.ProjectID == prepared.id)
}

func rebaseProfileState(prepared PreparedProject, fact PersistenceFact, current profileState) (profileState, error) {
	baseline, baselineFound := findProfileProject(prepared.state, prepared.root)
	index := -1
	for i := range current.Projects {
		if current.Projects[i].Root == prepared.root {
			index = i
			break
		}
	}
	if index < 0 {
		if baselineFound {
			return profileState{}, newProfileStoreError(CodeProfileConflict, "projects[].project_id", false)
		}
		current.Projects = append(current.Projects, profileProject{Root: prepared.root, ProjectID: prepared.id})
		index = len(current.Projects) - 1
	} else if current.Projects[index].ProjectID != prepared.id {
		return profileState{}, newProfileStoreError(CodeProfileConflict, "projects[].project_id", false)
	}
	if fact.Requested && fact.Scope == compiler.ProfileScopeGlobal {
		if current.GlobalProfile != fact.ProfileID && current.GlobalProfile != prepared.state.GlobalProfile {
			return profileState{}, newProfileStoreError(CodeProfileConflict, "global_profile", false)
		}
		current.GlobalProfile = fact.ProfileID
	}
	if fact.Requested && fact.Scope == compiler.ProfileScopeProject {
		baselineProfile := ""
		if baselineFound {
			baselineProfile = baseline.ProfileID
		}
		if current.Projects[index].ProfileID != fact.ProfileID && current.Projects[index].ProfileID != baselineProfile {
			return profileState{}, newProfileStoreError(CodeProfileConflict, "projects[].profile_id", false)
		}
		current.Projects[index].ProfileID = fact.ProfileID
	}
	sort.Slice(current.Projects, func(i, j int) bool { return current.Projects[i].Root < current.Projects[j].Root })
	if validateProfileState(current) != nil {
		return profileState{}, profileStoreError(CodeProfileCorrupt)
	}
	return current, nil
}

func findProfileProject(state profileState, root string) (profileProject, bool) {
	for _, project := range state.Projects {
		if project.Root == root {
			return project, true
		}
	}
	return profileProject{}, false
}

func decodeProfileState(raw []byte, exists bool) (profileState, error) {
	if !exists {
		return profileState{Version: profileStateVersion, Projects: []profileProject{}}, nil
	}
	var state profileState
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&state) != nil || decoder.Decode(&struct{}{}) != io.EOF || validateProfileState(state) != nil {
		return profileState{}, profileStoreError(CodeProfileCorrupt)
	}
	return state, nil
}

var profileIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

func validProfileID(id string) bool { return profileIDPattern.MatchString(id) }
func validProjectRoot(root string) (string, error) {
	if root == "" || !filepath.IsAbs(root) {
		return "", profileStoreError(CodeProfileInvalid)
	}
	return filepath.Clean(root), nil
}
func stableProjectID(root string) compiler.ProjectID {
	sum := sha256.Sum256([]byte("lore.project.v1\x00" + root))
	return compiler.ProjectID("project:" + hex.EncodeToString(sum[:12]))
}

func (s ProfileStore) load() (profileState, []byte, bool, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return profileState{Version: profileStateVersion, Projects: []profileProject{}}, nil, false, nil
	}
	if err != nil {
		return profileState{}, nil, false, profileStoreError(CodeProfileIO)
	}
	if !storeFilePermissionsValid(s.path) {
		return profileState{}, nil, false, profileStoreError(CodeProfileCorrupt)
	}
	var state profileState
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&state) != nil || decoder.Decode(&struct{}{}) != io.EOF || validateProfileState(state) != nil {
		return profileState{}, nil, false, profileStoreError(CodeProfileCorrupt)
	}
	return state, append([]byte(nil), raw...), true, nil
}

func validateProfileState(state profileState) error {
	if state.Version != profileStateVersion || (state.GlobalProfile != "" && !validProfileID(state.GlobalProfile)) {
		return errors.New("invalid")
	}
	seen := map[string]bool{}
	for _, project := range state.Projects {
		root, err := validProjectRoot(project.Root)
		if err != nil || root != project.Root || seen[root] || project.ProjectID != stableProjectID(root) || (project.ProfileID != "" && !validProfileID(project.ProfileID)) {
			return errors.New("invalid")
		}
		seen[root] = true
	}
	return nil
}
