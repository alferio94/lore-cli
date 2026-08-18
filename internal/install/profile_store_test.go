package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alferio94/lore-cli/internal/compiler"
)

func TestProfileStoreProjectIDLifecycleAndSeparation(t *testing.T) {
	store := NewProfileStore(filepath.Join(t.TempDir(), "state", "profiles.json"))
	alpha := filepath.Join(t.TempDir(), "alpha")
	beta := filepath.Join(t.TempDir(), "beta")

	first, err := store.PrepareProject(alpha)
	if err != nil || first.ProjectID() == "" {
		t.Fatalf("PrepareProject(alpha) = %q, %v", first.ProjectID(), err)
	}
	if _, err := os.Stat(store.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("PrepareProject persisted state: %v", err)
	}
	second, err := store.PrepareProject(alpha)
	if err != nil || second.ProjectID() != first.ProjectID() {
		t.Fatalf("second ProjectID = %q, %v; want %q", second.ProjectID(), err, first.ProjectID())
	}
	other, err := store.PrepareProject(beta)
	if err != nil || other.ProjectID() == first.ProjectID() {
		t.Fatalf("beta ProjectID = %q, %v; want distinct", other.ProjectID(), err)
	}
	if err := store.Complete(first, PersistenceFact{}, ApplyBoundarySuccess); err != nil {
		t.Fatalf("Complete(alpha) error = %v", err)
	}
	got, err := store.LookupProject(alpha)
	if err != nil || got.ProjectID != first.ProjectID() {
		t.Fatalf("LookupProject(alpha) = %+v, %v", got, err)
	}
	rerun, err := store.PrepareProject(alpha)
	if err != nil || rerun.ProjectID() != first.ProjectID() {
		t.Fatalf("rerun ProjectID = %q, %v; want %q", rerun.ProjectID(), err, first.ProjectID())
	}
	if _, err := store.LookupProject(beta); !errors.Is(err, CodeProfileNotFound) {
		t.Fatalf("LookupProject(beta) error = %v, want not found", err)
	}
}

func TestProfileStoreRequestedScope(t *testing.T) {
	store := NewProfileStore(filepath.Join(t.TempDir(), "profiles.json"))
	alphaRoot, betaRoot := filepath.Join(t.TempDir(), "alpha"), filepath.Join(t.TempDir(), "beta")
	alpha, _ := store.PrepareProject(alphaRoot)
	if err := store.Complete(alpha, PersistenceFact{Requested: true, Scope: compiler.ProfileScopeGlobal, ProfileID: "balanced"}, ApplyBoundarySuccess); err != nil {
		t.Fatalf("Complete(global) error = %v", err)
	}
	beta, _ := store.PrepareProject(betaRoot)
	if err := store.Complete(beta, PersistenceFact{Requested: true, Scope: compiler.ProfileScopeProject, ProjectID: beta.ProjectID(), ProfileID: "focused"}, ApplyBoundarySuccess); err != nil {
		t.Fatalf("Complete(project) error = %v", err)
	}

	alphaSelection, err := store.LookupProject(alphaRoot)
	if err != nil || alphaSelection.GlobalProfile != "balanced" || alphaSelection.ProjectProfile != "" || alphaSelection.EffectiveProfile() != "balanced" {
		t.Fatalf("alpha selection = %+v, %v", alphaSelection, err)
	}
	betaSelection, err := store.LookupProject(betaRoot)
	if err != nil || betaSelection.GlobalProfile != "balanced" || betaSelection.ProjectProfile != "focused" || betaSelection.EffectiveProfile() != "focused" {
		t.Fatalf("beta selection = %+v, %v", betaSelection, err)
	}
}

func TestProfileStoreOnlyPersistsAtSuccessfulBoundary(t *testing.T) {
	boundaries := []ApplyBoundary{
		ApplyBoundaryDryRun,
		ApplyBoundaryReconcileRejected,
		ApplyBoundaryApplyFailed,
		ApplyBoundaryFinalizationFailed,
		ApplyBoundaryManifestFailed,
	}
	for _, boundary := range boundaries {
		t.Run(string(boundary), func(t *testing.T) {
			store := NewProfileStore(filepath.Join(t.TempDir(), "profiles.json"))
			prepared, err := store.PrepareProject(filepath.Join(t.TempDir(), "project"))
			if err != nil {
				t.Fatal(err)
			}
			fact := PersistenceFact{Requested: true, Scope: compiler.ProfileScopeProject, ProjectID: prepared.ProjectID(), ProfileID: "focused"}
			if err := store.Complete(prepared, fact, boundary); err != nil {
				t.Fatalf("Complete(%s) error = %v", boundary, err)
			}
			if _, err := os.Stat(store.Path()); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("%s persisted ProjectID/profile: %v", boundary, err)
			}
		})
	}
}

func TestProfileStoreRejectsInvalidAndCorruptStateWithoutSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	store := NewProfileStore(path)
	prepared, _ := store.PrepareProject(filepath.Join(t.TempDir(), "project"))
	secret := "Bearer top-secret-value"
	fact := PersistenceFact{Requested: true, Scope: compiler.ProfileScopeGlobal, ProfileID: secret}
	if err := store.Complete(prepared, fact, ApplyBoundarySuccess); !errors.Is(err, CodeProfileInvalid) || strings.Contains(err.Error(), secret) {
		t.Fatalf("secret profile error = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid profile persisted: %v", err)
	}

	if err := os.WriteFile(path, []byte(`{"version":1,"global_profile":"","projects":[`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PrepareProject(filepath.Join(t.TempDir(), "other")); !errors.Is(err, CodeProfileCorrupt) {
		t.Fatalf("corrupt state error = %v", err)
	}
	if _, err := store.LookupProject("relative/path"); !errors.Is(err, CodeProfileInvalid) {
		t.Fatalf("relative project error = %v", err)
	}
}

func TestProfileStoreRestrictiveAtomicWriteAndRollback(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	path := filepath.Join(dir, "profiles.json")
	store := NewProfileStore(path)
	root := filepath.Join(t.TempDir(), "project")
	prepared, _ := store.PrepareProject(root)
	if err := store.Complete(prepared, PersistenceFact{}, ApplyBoundarySuccess); err != nil {
		t.Fatal(err)
	}
	assertStorePermissions(t, dir, path)
	before, _ := os.ReadFile(path)

	update, _ := store.PrepareProject(root)
	store.commit = func(authority, []byte, []byte) error { return errors.New("injected commit failure") }
	err := store.Complete(update, PersistenceFact{Requested: true, Scope: compiler.ProfileScopeGlobal, ProfileID: "balanced"}, ApplyBoundarySuccess)
	if !errors.Is(err, CodeProfileIO) || strings.Contains(err.Error(), "balanced") {
		t.Fatalf("rename error = %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Fatalf("failed atomic replacement changed prior state\nbefore=%s\nafter=%s", before, after)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, ".profiles-*.json"))
	if len(matches) != 0 {
		t.Fatalf("temporary files remain: %v", matches)
	}
}

func TestProfileStoreRejectsStaleAndOverPermissiveState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	store := NewProfileStore(path)
	root := filepath.Join(t.TempDir(), "project")
	stale, _ := store.PrepareProject(root)
	fresh, _ := store.PrepareProject(root)
	if err := store.Complete(fresh, PersistenceFact{Requested: true, Scope: compiler.ProfileScopeGlobal, ProfileID: "first"}, ApplyBoundarySuccess); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if err := store.Complete(stale, PersistenceFact{Requested: true, Scope: compiler.ProfileScopeGlobal, ProfileID: "balanced"}, ApplyBoundarySuccess); !errors.Is(err, CodeProfileConflict) {
		t.Fatalf("same-key stale Complete() error = %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Fatal("stale completion changed persisted state")
	}

	makeStoreFileInsecure(t, path)
	if _, err := store.LookupProject(root); !errors.Is(err, CodeProfileCorrupt) {
		t.Fatalf("over-permissive state error = %v", err)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode()
}

func TestProfileStoreAuthorityTypedErrorsAreStableAndRedacted(t *testing.T) {
	cases := []struct {
		name     string
		code     ProfileStoreCode
		path     string
		residual bool
	}{
		{"busy", CodeProfileBusy, "profile_store.authority", false},
		{"timeout", CodeProfileTimeout, "profile_store.authority", false},
		{"global conflict", CodeProfileConflict, "global_profile", false},
		{"project identity conflict", CodeProfileConflict, "projects[].project_id", false},
		{"project profile conflict", CodeProfileConflict, "projects[].profile_id", false},
		{"unsafe path", CodeProfileInvalid, "profile_store.path", false},
		{"commit", CodeProfileIO, "profile_store.commit", false},
		{"rollback", CodeProfileIO, "profile_store.rollback", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := newProfileStoreError(tc.code, tc.path, tc.residual)
			if err.Code() != tc.code || err.Path() != tc.path || err.ResidualRisk() != tc.residual {
				t.Fatalf("typed error = (%q, %q, %t)", err.Code(), err.Path(), err.ResidualRisk())
			}
			if err.Error() != "profile state operation failed" || strings.Contains(err.Error(), "Bearer secret") {
				t.Fatalf("error text = %q, want fixed redacted text", err.Error())
			}
			if !errors.Is(err, tc.code) {
				t.Fatalf("errors.Is(%q) = false", tc.code)
			}
		})
	}
}

func TestProfileStoreAuthorityCanonicalIdentityAndLifecycle(t *testing.T) {
	canonical, err := canonicalStorePath("physical-parent:42/leaf", nil)
	if err != nil {
		t.Fatal(err)
	}
	platform := &fakeStorePlatform{canonical: canonical, auth: &fakeStoreAuthority{}}
	waiter := &fakeMonotonicWaiter{now: time.Unix(10, 0)}
	for i, alias := range []string{"/alias/one", "/alias/two"} {
		err := withStoreAuthority(platform, alias, CommitOptions{WaitBudget: 5 * time.Second}, waiter, func(auth authority) error {
			if !platform.auth.held || platform.auth.releases != i {
				t.Fatal("authority was not held throughout work")
			}
			return platform.Commit(auth, []byte("prior"), []byte("next"))
		})
		if err != nil {
			t.Fatalf("withStoreAuthority(%q) error = %v", alias, err)
		}
	}
	if len(platform.acquired) != 2 || platform.acquired[0].identity != platform.acquired[1].identity {
		t.Fatalf("canonical aliases acquired different identities: %+v", platform.acquired)
	}
	if platform.auth.releases != 2 || platform.commits != 2 {
		t.Fatalf("releases/commits = %d/%d, want 2/2", platform.auth.releases, platform.commits)
	}
	if platform.budget != 5*time.Second || platform.waiter != waiter {
		t.Fatalf("acquisition controls = %s/%T", platform.budget, platform.waiter)
	}
}

func TestProfileStoreAuthorityBusyTimeoutAndFailClosedValidation(t *testing.T) {
	canonical, err := canonicalStorePath("physical-parent:42/leaf", nil)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		budget time.Duration
		cause  error
		code   ProfileStoreCode
	}{
		{"busy", 0, errStoreAuthorityBusy, CodeProfileBusy},
		{"timeout", 5 * time.Second, errStoreAuthorityTimeout, CodeProfileTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			platform := &fakeStorePlatform{canonical: canonical, acquireErr: tc.cause}
			err := withStoreAuthority(platform, "/private/Bearer-secret", CommitOptions{WaitBudget: tc.budget}, &fakeMonotonicWaiter{}, func(authority) error {
				t.Fatal("busy acquisition entered critical section")
				return nil
			})
			assertProfileStoreError(t, err, tc.code, "profile_store.authority")
			if strings.Contains(err.Error(), "Bearer-secret") {
				t.Fatalf("authority error disclosed input: %v", err)
			}
		})
	}

	platform := &fakeStorePlatform{canonicalErr: errors.New("symlink at /private/Bearer-secret")}
	err = withStoreAuthority(platform, "/private/Bearer-secret", CommitOptions{}, &fakeMonotonicWaiter{}, func(authority) error { return nil })
	assertProfileStoreError(t, err, CodeProfileInvalid, "profile_store.path")
	if platform.acquireCalls != 0 || strings.Contains(err.Error(), "Bearer-secret") {
		t.Fatalf("unsafe path reached acquisition or leaked: calls=%d err=%v", platform.acquireCalls, err)
	}

	platform = &fakeStorePlatform{canonical: canonical, acquireErr: errStoreAuthorityInvalidPath}
	err = withStoreAuthority(platform, "/private/Bearer-secret", CommitOptions{}, &fakeMonotonicWaiter{}, func(authority) error { return nil })
	assertProfileStoreError(t, err, CodeProfileInvalid, "profile_store.path")
	if strings.Contains(err.Error(), "Bearer-secret") {
		t.Fatalf("acquisition path error disclosed input: %v", err)
	}

	platform = &fakeStorePlatform{canonical: canonical}
	err = withStoreAuthority(platform, "/safe", CommitOptions{WaitBudget: time.Second}, &fakeMonotonicWaiter{}, func(authority) error { return nil })
	assertProfileStoreError(t, err, CodeProfileInvalid, "profile_store.authority")
	if platform.canonicalCalls != 0 || platform.acquireCalls != 0 {
		t.Fatal("invalid wait budget reached path or authority platform")
	}
}

func TestProfileStoreAuthorityReleasesAfterWorkFailure(t *testing.T) {
	canonical, _ := canonicalStorePath("physical-parent:42/leaf", nil)
	platform := &fakeStorePlatform{canonical: canonical, auth: &fakeStoreAuthority{}}
	workErr := newProfileStoreError(CodeProfileConflict, "projects[].profile_id", false)
	err := withStoreAuthority(platform, "/safe", CommitOptions{}, &fakeMonotonicWaiter{}, func(authority) error { return workErr })
	if !errors.Is(err, CodeProfileConflict) || platform.auth.releases != 1 || platform.auth.held {
		t.Fatalf("work failure lifecycle = err:%v releases:%d held:%t", err, platform.auth.releases, platform.auth.held)
	}
}

func TestProfileStoreCompleteRebasesDistinctConcurrentRecords(t *testing.T) {
	store := NewProfileStore(filepath.Join(t.TempDir(), "profiles.json"))
	alphaRoot, betaRoot := filepath.Join(t.TempDir(), "alpha"), filepath.Join(t.TempDir(), "beta")
	alpha, err := store.PrepareProject(alphaRoot)
	if err != nil {
		t.Fatal(err)
	}
	beta, err := store.PrepareProject(betaRoot)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	complete := func(prepared PreparedProject, profile string) {
		ready.Done()
		<-start
		errs <- store.CompleteWithOptions(prepared, PersistenceFact{Requested: true, Scope: compiler.ProfileScopeProject, ProjectID: prepared.ProjectID(), ProfileID: profile}, ApplyBoundarySuccess, CommitOptions{WaitBudget: defaultProfileStoreWait})
	}
	go complete(alpha, "alpha-profile")
	go complete(beta, "beta-profile")
	ready.Wait()
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent Complete() error = %v", err)
		}
	}
	for root, want := range map[string]string{alphaRoot: "alpha-profile", betaRoot: "beta-profile"} {
		selection, err := store.LookupProject(root)
		if err != nil || selection.ProjectProfile != want {
			t.Fatalf("LookupProject(%q) = %+v, %v; want %q", root, selection, err, want)
		}
	}
}

func TestProfileStoreCompleteRebasesGlobalAndProjectAndConflictsSameKey(t *testing.T) {
	store := NewProfileStore(filepath.Join(t.TempDir(), "profiles.json"))
	root := filepath.Join(t.TempDir(), "project")
	global, _ := store.PrepareProject(root)
	project, _ := store.PrepareProject(root)
	if err := store.Complete(global, PersistenceFact{Requested: true, Scope: compiler.ProfileScopeGlobal, ProfileID: "balanced"}, ApplyBoundarySuccess); err != nil {
		t.Fatal(err)
	}
	if err := store.Complete(project, PersistenceFact{Requested: true, Scope: compiler.ProfileScopeProject, ProjectID: project.ProjectID(), ProfileID: "focused"}, ApplyBoundarySuccess); err != nil {
		t.Fatalf("unrelated stale project update = %v", err)
	}
	selection, err := store.LookupProject(root)
	if err != nil || selection.GlobalProfile != "balanced" || selection.ProjectProfile != "focused" {
		t.Fatalf("rebased selection = %+v, %v", selection, err)
	}

	first, _ := store.PrepareProject(root)
	stale, _ := store.PrepareProject(root)
	if err := store.Complete(first, PersistenceFact{Requested: true, Scope: compiler.ProfileScopeProject, ProjectID: first.ProjectID(), ProfileID: "first"}, ApplyBoundarySuccess); err != nil {
		t.Fatal(err)
	}
	err = store.Complete(stale, PersistenceFact{Requested: true, Scope: compiler.ProfileScopeProject, ProjectID: stale.ProjectID(), ProfileID: "second"}, ApplyBoundarySuccess)
	assertProfileStoreError(t, err, CodeProfileConflict, "projects[].profile_id")
	selection, _ = store.LookupProject(root)
	if selection.ProjectProfile != "first" {
		t.Fatalf("conflict replaced first value: %+v", selection)
	}
}

func TestProfileStoreCompleteIdempotentRetryKeepsBytes(t *testing.T) {
	store := NewProfileStore(filepath.Join(t.TempDir(), "profiles.json"))
	root := filepath.Join(t.TempDir(), "project")
	first, _ := store.PrepareProject(root)
	retry, _ := store.PrepareProject(root)
	fact := PersistenceFact{Requested: true, Scope: compiler.ProfileScopeGlobal, ProfileID: "balanced"}
	if err := store.Complete(first, fact, ApplyBoundarySuccess); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(store.Path())
	if err := store.Complete(retry, fact, ApplyBoundarySuccess); err != nil {
		t.Fatalf("idempotent stale retry = %v", err)
	}
	after, _ := os.ReadFile(store.Path())
	if string(after) != string(before) {
		t.Fatal("idempotent retry changed canonical bytes")
	}
}

func TestProfileStoreCompleteValidatesBeforeAuthorityAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	canonical, _ := canonicalStorePath("test-path", nil)
	platform := &fakeStorePlatform{canonical: canonical, auth: &fakeStoreAuthority{}}
	store := ProfileStore{path: path, platform: platform, waiter: &fakeMonotonicWaiter{}}
	prepared := PreparedProject{path: path, root: filepath.Join(t.TempDir(), "project"), state: profileState{Version: profileStateVersion, Projects: []profileProject{}}}
	prepared.id = stableProjectID(prepared.root)

	bad := PersistenceFact{Requested: true, Scope: compiler.ProfileScopeGlobal, ProfileID: "Bearer secret"}
	if err := store.CompleteWithOptions(prepared, bad, ApplyBoundarySuccess, CommitOptions{}); !errors.Is(err, CodeProfileInvalid) {
		t.Fatalf("invalid completion = %v", err)
	}
	if platform.canonicalCalls != 0 || platform.acquireCalls != 0 {
		t.Fatal("invalid success input acquired authority")
	}
	for _, boundary := range []ApplyBoundary{ApplyBoundaryDryRun, ApplyBoundaryReconcileRejected, ApplyBoundaryApplyFailed, ApplyBoundaryFinalizationFailed, ApplyBoundaryManifestFailed} {
		if err := store.CompleteWithOptions(prepared, PersistenceFact{}, boundary, CommitOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if platform.acquireCalls != 0 {
		t.Fatal("non-success boundary acquired authority")
	}

	platform.onAcquire = func() { platform.readRaw, platform.readExists = []byte("malformed"), true }
	err := store.CompleteWithOptions(prepared, PersistenceFact{}, ApplyBoundarySuccess, CommitOptions{})
	if !errors.Is(err, CodeProfileCorrupt) || platform.auth.releases != 1 || platform.auth.held {
		t.Fatalf("authoritative reread/release = %v, releases=%d held=%t", err, platform.auth.releases, platform.auth.held)
	}
}

func TestProfileStoreAuthorityReleaseIsPanicSafe(t *testing.T) {
	canonical, _ := canonicalStorePath("physical-parent:42/leaf", nil)
	platform := &fakeStorePlatform{canonical: canonical, auth: &fakeStoreAuthority{}}
	func() {
		defer func() { _ = recover() }()
		_ = withStoreAuthority(platform, "/safe", CommitOptions{}, &fakeMonotonicWaiter{}, func(authority) error {
			panic("injected panic")
		})
	}()
	if platform.auth.releases != 1 || platform.auth.held {
		t.Fatalf("panic release = %d held=%t", platform.auth.releases, platform.auth.held)
	}
}

func TestProfileStoreCompleteMapsCommitAndRollbackFailuresAndReleases(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		cause      error
		residual   bool
	}{
		{"commit", "profile_store.commit", errors.New("injected commit"), false},
		{"rollback", "profile_store.rollback", errStoreRollbackFailed, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "profiles.json")
			canonical, _ := canonicalStorePath("test-path", nil)
			platform := &fakeStorePlatform{canonical: canonical, auth: &fakeStoreAuthority{}, commitErr: tc.cause}
			store := ProfileStore{path: path, platform: platform, waiter: &fakeMonotonicWaiter{}}
			root := filepath.Join(t.TempDir(), "project")
			prepared := PreparedProject{path: path, root: root, id: stableProjectID(root), state: profileState{Version: profileStateVersion, Projects: []profileProject{}}}
			err := store.CompleteWithOptions(prepared, PersistenceFact{}, ApplyBoundarySuccess, CommitOptions{})
			assertProfileStoreError(t, err, CodeProfileIO, tc.path)
			var typed *ProfileStoreError
			if !errors.As(err, &typed) || typed.ResidualRisk() != tc.residual || platform.auth.releases != 1 || platform.auth.held {
				t.Fatalf("failure lifecycle = %v residual=%t releases=%d held=%t", err, typed.ResidualRisk(), platform.auth.releases, platform.auth.held)
			}
		})
	}
}

func assertProfileStoreError(t *testing.T, err error, code ProfileStoreCode, path string) {
	t.Helper()
	var typed *ProfileStoreError
	if !errors.As(err, &typed) || typed.Code() != code || typed.Path() != path {
		t.Fatalf("error = %v, want %s at %s", err, code, path)
	}
}

type fakeStorePlatform struct {
	canonical      storePath
	canonicalErr   error
	auth           *fakeStoreAuthority
	acquireErr     error
	canonicalCalls int
	acquireCalls   int
	acquired       []storePath
	budget         time.Duration
	waiter         monotonicWaiter
	commits        int
	onAcquire      func()
	readRaw        []byte
	readExists     bool
	readErr        error
	commitErr      error
}

func (f *fakeStorePlatform) Canonical(string) (storePath, error) {
	f.canonicalCalls++
	return f.canonical, f.canonicalErr
}
func (f *fakeStorePlatform) Acquire(path storePath, budget time.Duration, waiter monotonicWaiter) (authority, error) {
	f.acquireCalls++
	f.acquired = append(f.acquired, path)
	f.budget, f.waiter = budget, waiter
	if f.onAcquire != nil {
		f.onAcquire()
	}
	if f.acquireErr != nil {
		return nil, f.acquireErr
	}
	if f.auth == nil {
		f.auth = &fakeStoreAuthority{}
	}
	f.auth.held = true
	return f.auth, nil
}
func (f *fakeStorePlatform) Read(auth authority) ([]byte, bool, error) {
	if auth != f.auth || !f.auth.held {
		return nil, false, errors.New("read without authority")
	}
	return append([]byte(nil), f.readRaw...), f.readExists, f.readErr
}
func (f *fakeStorePlatform) Commit(auth authority, _, _ []byte) error {
	if auth != f.auth || !f.auth.held {
		return errors.New("commit without authority")
	}
	f.commits++
	return f.commitErr
}

type fakeStoreAuthority struct {
	held     bool
	releases int
}

func (a *fakeStoreAuthority) Release() error {
	a.releases++
	a.held = false
	return nil
}

type fakeMonotonicWaiter struct{ now time.Time }

func (w *fakeMonotonicWaiter) Now() time.Time        { return w.now }
func (w *fakeMonotonicWaiter) Sleep(d time.Duration) { w.now = w.now.Add(d) }
