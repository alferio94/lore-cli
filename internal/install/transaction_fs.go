package install

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	transactionFSJournalVersion  = 1
	transactionFSJournalMaxBytes = 4 << 20
	transactionFSJournalMaxItems = 10000
)

const (
	CodeTransactionIO           TransactionCode = "transaction_io"
	CodeTransactionResidualRisk TransactionCode = "transaction_residual_risk"
)

var (
	errTransactionFSUnsafePath = errors.New("unsafe transaction filesystem path")
	errTransactionFSIO         = errors.New("transaction filesystem I/O failure")
)

type transactionFSWrite struct {
	Path string
	Data []byte
}

type transactionFSFailpoint func(stage, path string) error

type transactionFSState struct {
	path, backup string
	exists       bool
	mode         uint32
	windowsACL   string
}

type transactionFSDurableEntry struct {
	Path       string `json:"path"`
	Backup     string `json:"backup"`
	Exists     bool   `json:"exists"`
	Mode       uint32 `json:"mode"`
	WindowsACL string `json:"windows_acl"`
}

type transactionFSDurableJournal struct {
	Version     int                         `json:"version"`
	Root        string                      `json:"root"`
	Entries     []transactionFSDurableEntry `json:"entries"`
	CreatedDirs []string                    `json:"created_dirs"`
}

type transactionFSDurableEnvelope struct {
	Payload  json.RawMessage `json:"payload"`
	Checksum string          `json:"sha256"`
}

type transactionFSPlatform interface {
	recover() error
	begin() error
	backup(string, int) (transactionFSState, error)
	seal([]transactionFSState) error
	write(string, []byte) error
	removeTemps([]transactionFSState) error
	restore(transactionFSState) error
	removeCreatedDirs() error
	markCommitted() error
	markRestored() error
	cleanup() error
	journalRoot() string
}

type transactionFSJournal struct {
	platform   transactionFSPlatform
	guard      targetGuard
	states     []transactionFSState
	fail       transactionFSFailpoint
	backupRoot string
	mutated    int
	sealed     bool
	active     bool
}

type transactionFSApplyError struct {
	message string
	causes  []error
}

func (e *transactionFSApplyError) Error() string   { return e.message }
func (e *transactionFSApplyError) Unwrap() []error { return append([]error(nil), e.causes...) }

type transactionFSOutcomeError struct {
	code    TransactionCode
	path    string
	message string
	causes  []error
}

func (e *transactionFSOutcomeError) Error() string         { return e.message }
func (e *transactionFSOutcomeError) Code() TransactionCode { return e.code }
func (e *transactionFSOutcomeError) Path() string          { return e.path }
func (e *transactionFSOutcomeError) Unwrap() []error       { return append([]error(nil), e.causes...) }
func (e *transactionFSOutcomeError) Is(target error) bool {
	code, ok := target.(TransactionCode)
	return ok && code == e.code
}

func transactionFSIOError(path string, causes ...error) error {
	return &transactionFSOutcomeError{
		code:    CodeTransactionIO,
		path:    path,
		message: "transaction filesystem I/O failed",
		causes:  append([]error(nil), causes...),
	}
}

func transactionFSResidualRiskError(causes ...error) error {
	return &transactionFSOutcomeError{
		code:    CodeTransactionResidualRisk,
		path:    "transaction.rollback",
		message: "transaction restoration left residual risk",
		causes:  append([]error(nil), causes...),
	}
}

func applyTransactionFS(root string, writes []transactionFSWrite, fail transactionFSFailpoint) (*transactionFSJournal, error) {
	return applyTransactionFSWithWait(root, writes, fail, defaultTargetAuthorityWait, systemTargetMonotonicWaiter{})
}

func applyTransactionFSWithWait(root string, writes []transactionFSWrite, fail transactionFSFailpoint, wait time.Duration, clock targetMonotonicWaiter) (*transactionFSJournal, error) {
	ordered, err := normalizeTransactionWrites(writes)
	if err != nil {
		return nil, err
	}
	guard, err := acquireTargetGuardWithWaiter(root, wait, clock)
	if err != nil {
		return nil, err
	}
	if err := guard.Recover(); err != nil {
		return nil, releaseTransactionGuard(guard, err)
	}
	digest, ok := transactionFSTargetDigest(guard)
	if !ok {
		return nil, releaseTransactionGuard(guard, errTransactionFSUnsafePath)
	}
	platform, err := newTransactionFSPlatform(root, digest)
	if err != nil {
		return nil, releaseTransactionGuard(guard, err)
	}
	if err := platform.recover(); err != nil {
		return nil, releaseTransactionGuard(guard, transactionFSResidualRiskError(err))
	}
	if err := platform.begin(); err != nil {
		cleanupErr := platform.cleanup()
		causes := []error{err}
		if cleanupErr != nil {
			causes = append(causes, cleanupErr)
		}
		return nil, releaseTransactionGuard(guard, transactionFSIOError("transaction.journal", causes...))
	}
	journal := &transactionFSJournal{platform: platform, guard: guard, fail: fail, backupRoot: platform.journalRoot(), active: true}
	for i, write := range ordered {
		if err := injectTransactionFS(fail, "backup", write.Path); err != nil {
			return nil, journal.abort(err)
		}
		state, backupErr := platform.backup(write.Path, i)
		if backupErr != nil {
			return nil, journal.abort(backupErr)
		}
		journal.states = append(journal.states, state)
	}
	if err := platform.seal(journal.states); err != nil {
		return nil, journal.abort(transactionFSIOError("transaction.journal", err))
	}
	journal.sealed = true
	for i, write := range ordered {
		if err := injectTransactionFS(fail, "write", write.Path); err != nil {
			return nil, journal.abort(err)
		}
		if err := platform.write(write.Path, write.Data); err != nil {
			journal.mutated = i + 1
			return nil, journal.abort(err)
		}
		journal.mutated = i + 1
	}
	return journal, nil
}

func transactionFSTargetDigest(guard targetGuard) ([sha256.Size]byte, bool) {
	combined, ok := guard.(*combinedTargetGuard)
	if !ok || combined.local == nil || !combined.local.root.valid() {
		return [sha256.Size]byte{}, false
	}
	return combined.local.root.digest, true
}

func normalizeTransactionWrites(in []transactionFSWrite) ([]transactionFSWrite, error) {
	out := make([]transactionFSWrite, len(in))
	for i, write := range in {
		if !validTransactionRelativePath(write.Path) || !validTransactionNativeRelativePath(write.Path) {
			return nil, errTransactionFSUnsafePath
		}
		out[i] = transactionFSWrite{Path: write.Path, Data: append([]byte(nil), write.Data...)}
	}
	sort.Slice(out, func(i, j int) bool { return compareTransactionResources(out[i].Path, out[j].Path) < 0 })
	seen := make(map[string]struct{}, len(out))
	for _, write := range out {
		key := transactionPathKey(write.Path)
		if _, exists := seen[key]; exists {
			return nil, errTransactionFSUnsafePath
		}
		seen[key] = struct{}{}
	}
	return out, nil
}

func validTransactionRelativePath(name string) bool {
	if name == "" || len(name) > 4096 || strings.IndexByte(name, 0) >= 0 || strings.Contains(name, `\`) {
		return false
	}
	if len(name) >= 2 && (name[0] >= 'a' && name[0] <= 'z' || name[0] >= 'A' && name[0] <= 'Z') && name[1] == ':' {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func validTransactionNativeRelativePath(logical string) bool {
	native := filepath.FromSlash(logical)
	if filepath.IsAbs(native) || filepath.VolumeName(native) != "" || !filepath.IsLocal(native) || filepath.ToSlash(filepath.Clean(native)) != logical {
		return false
	}
	if filepath.Separator == '\\' {
		for _, part := range strings.Split(logical, "/") {
			if part == "" || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || strings.ContainsAny(part, `<>:"|?*`) {
				return false
			}
		}
	}
	return true
}

func transactionNativePath(root, logical string) (string, error) {
	if !validTransactionRelativePath(logical) || !validTransactionNativeRelativePath(logical) {
		return "", errTransactionFSUnsafePath
	}
	native := filepath.FromSlash(logical)
	joined := filepath.Join(root, native)
	rel, err := filepath.Rel(filepath.Clean(root), joined)
	if err != nil || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" || filepath.ToSlash(filepath.Clean(rel)) != logical {
		return "", errTransactionFSUnsafePath
	}
	return joined, nil
}

func transactionLogicalPathFromNative(rel string) (string, error) {
	if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" || !filepath.IsLocal(rel) {
		return "", errTransactionFSUnsafePath
	}
	logical := filepath.ToSlash(filepath.Clean(rel))
	if !validTransactionRelativePath(logical) || !validTransactionNativeRelativePath(logical) || filepath.Clean(filepath.FromSlash(logical)) != filepath.Clean(rel) {
		return "", errTransactionFSUnsafePath
	}
	return logical, nil
}

func transactionPathKey(name string) string {
	if filepath.Separator == '\\' {
		return strings.ToUpper(filepath.FromSlash(name))
	}
	return name
}

func transactionPathBase(name string) string {
	if index := strings.LastIndexByte(name, '/'); index >= 0 {
		return name[index+1:]
	}
	return name
}

func compareTransactionResources(a, b string) int {
	aManifest := transactionPathBase(a) == provenanceV3Name
	bManifest := transactionPathBase(b) == provenanceV3Name
	if aManifest != bManifest {
		if aManifest {
			return 1
		}
		return -1
	}
	return strings.Compare(a, b)
}

func (j *transactionFSJournal) Commit() error {
	if j == nil || !j.active {
		return nil
	}
	var causes []error
	if err := injectTransactionFS(j.fail, "cleanup", "journal"); err != nil {
		causes = append(causes, err)
	}
	committed := false
	if err := j.platform.markCommitted(); err != nil {
		causes = append(causes, transactionFSIOError("transaction.journal", err))
	} else {
		committed = true
	}
	if committed {
		if err := j.platform.cleanup(); err != nil {
			causes = append(causes, transactionFSIOError("transaction.journal", err))
		}
	}
	if err := j.guard.Release(); err != nil {
		causes = append(causes, err)
	}
	j.active = false
	if len(causes) != 0 {
		return &transactionFSApplyError{message: "transaction filesystem cleanup failed", causes: causes}
	}
	return nil
}

func (j *transactionFSJournal) Rollback() error {
	if j == nil || !j.active {
		return nil
	}
	causes, residual := j.rollback()
	if residual {
		causes = append(causes, transactionFSResidualRiskError(causes...))
	}
	if err := j.guard.Release(); err != nil {
		causes = append(causes, err)
	}
	j.active = false
	if len(causes) != 0 {
		return &transactionFSApplyError{message: "transaction filesystem rollback failed", causes: causes}
	}
	return nil
}

func (j *transactionFSJournal) abort(primary error) error {
	causes := []error{primary}
	rollbackCauses, residual := j.rollback()
	causes = append(causes, rollbackCauses...)
	if residual {
		causes = append(causes, transactionFSResidualRiskError(rollbackCauses...))
	}
	if err := j.guard.Release(); err != nil {
		causes = append(causes, err)
	}
	j.active = false
	return &transactionFSApplyError{message: "transaction filesystem apply failed", causes: causes}
}

func releaseTransactionGuard(guard targetGuard, primary error) error {
	if releaseErr := guard.Release(); releaseErr != nil {
		return &transactionFSApplyError{message: "transaction filesystem authority failed", causes: []error{primary, releaseErr}}
	}
	return primary
}

func (j *transactionFSJournal) rollback() ([]error, bool) {
	var causes []error
	residual := false
	if j.sealed {
		if err := j.platform.removeTemps(j.states); err != nil {
			causes = append(causes, err)
			residual = true
		}
		for i := len(j.states) - 1; i >= 0; i-- {
			state := j.states[i]
			if err := injectTransactionFS(j.fail, "rollback", state.path); err != nil {
				causes = append(causes, err)
			}
			if err := j.platform.restore(state); err != nil {
				causes = append(causes, err)
				residual = true
			}
		}
		if err := j.platform.removeCreatedDirs(); err != nil {
			causes = append(causes, err)
			residual = true
		}
		if !residual {
			if err := j.platform.markRestored(); err != nil {
				causes = append(causes, err)
				residual = true
			}
		}
	}
	if err := injectTransactionFS(j.fail, "cleanup", "journal"); err != nil {
		causes = append(causes, err)
	}
	if !residual {
		if err := j.platform.cleanup(); err != nil {
			causes = append(causes, transactionFSIOError("transaction.journal", err))
		}
	}
	return causes, residual
}

func encodeTransactionFSDurableJournal(root [sha256.Size]byte, states []transactionFSState, createdDirs []string) ([]byte, error) {
	record := transactionFSDurableJournal{
		Version:     transactionFSJournalVersion,
		Root:        hex.EncodeToString(root[:]),
		Entries:     make([]transactionFSDurableEntry, len(states)),
		CreatedDirs: append([]string(nil), createdDirs...),
	}
	for i, state := range states {
		record.Entries[i] = transactionFSDurableEntry{
			Path:       state.path,
			Backup:     state.backup,
			Exists:     state.exists,
			Mode:       state.mode,
			WindowsACL: state.windowsACL,
		}
	}
	if err := validateTransactionFSDurableJournal(record, root); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return nil, errTransactionFSIO
	}
	sum := sha256.Sum256(payload)
	envelope, err := json.Marshal(transactionFSDurableEnvelope{Payload: payload, Checksum: hex.EncodeToString(sum[:])})
	if err != nil || len(envelope) > transactionFSJournalMaxBytes {
		return nil, errTransactionFSIO
	}
	return append(envelope, '\n'), nil
}

func decodeTransactionFSDurableJournal(data []byte, root [sha256.Size]byte) (transactionFSDurableJournal, error) {
	if len(data) == 0 || len(data) > transactionFSJournalMaxBytes {
		return transactionFSDurableJournal{}, errTransactionFSIO
	}
	var envelope transactionFSDurableEnvelope
	if err := decodeTransactionFSJSON(data, &envelope); err != nil || len(envelope.Payload) == 0 {
		return transactionFSDurableJournal{}, errTransactionFSIO
	}
	canonicalEnvelope, err := json.Marshal(envelope)
	if err != nil || !bytes.Equal(bytes.TrimSpace(data), canonicalEnvelope) {
		return transactionFSDurableJournal{}, errTransactionFSIO
	}
	sum := sha256.Sum256(envelope.Payload)
	if envelope.Checksum != hex.EncodeToString(sum[:]) {
		return transactionFSDurableJournal{}, errTransactionFSIO
	}
	var record transactionFSDurableJournal
	if err := decodeTransactionFSJSON(envelope.Payload, &record); err != nil {
		return transactionFSDurableJournal{}, errTransactionFSIO
	}
	canonicalPayload, err := json.Marshal(record)
	if err != nil || !bytes.Equal(envelope.Payload, canonicalPayload) {
		return transactionFSDurableJournal{}, errTransactionFSIO
	}
	if err := normalizeTransactionFSLegacyJournal(&record); err != nil {
		return transactionFSDurableJournal{}, err
	}
	if err := validateTransactionFSDurableJournal(record, root); err != nil {
		return transactionFSDurableJournal{}, err
	}
	return record, nil
}

func decodeTransactionFSJSON(data []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errTransactionFSIO
	}
	return nil
}

func normalizeTransactionFSLegacyJournal(record *transactionFSDurableJournal) error {
	if record == nil {
		return errTransactionFSIO
	}
	var slash, backslash bool
	observe := func(name string) error {
		hasSlash, hasBackslash := strings.Contains(name, "/"), strings.Contains(name, `\`)
		if hasSlash && hasBackslash {
			return errTransactionFSIO
		}
		slash, backslash = slash || hasSlash, backslash || hasBackslash
		return nil
	}
	for _, entry := range record.Entries {
		if err := observe(entry.Path); err != nil {
			return err
		}
		if entry.Backup != "" {
			if err := observe(entry.Backup); err != nil {
				return err
			}
		}
	}
	for _, dir := range record.CreatedDirs {
		if err := observe(dir); err != nil {
			return err
		}
	}
	if slash && backslash {
		return errTransactionFSIO
	}
	if backslash {
		for i := range record.Entries {
			record.Entries[i].Path = strings.ReplaceAll(record.Entries[i].Path, `\`, "/")
			record.Entries[i].Backup = strings.ReplaceAll(record.Entries[i].Backup, `\`, "/")
		}
		for i := range record.CreatedDirs {
			record.CreatedDirs[i] = strings.ReplaceAll(record.CreatedDirs[i], `\`, "/")
		}
	}
	return nil
}

func validateTransactionFSDurableJournal(record transactionFSDurableJournal, root [sha256.Size]byte) error {
	if record.Version != transactionFSJournalVersion || record.Root != hex.EncodeToString(root[:]) || len(record.Entries) > transactionFSJournalMaxItems || len(record.CreatedDirs) > transactionFSJournalMaxItems {
		return errTransactionFSIO
	}
	entryPaths := make(map[string]struct{}, len(record.Entries))
	for i, entry := range record.Entries {
		if !validTransactionRelativePath(entry.Path) || !validTransactionNativeRelativePath(entry.Path) || strings.IndexByte(entry.WindowsACL, 0) >= 0 || len(entry.WindowsACL) > 1<<20 {
			return errTransactionFSIO
		}
		key := transactionPathKey(entry.Path)
		if _, exists := entryPaths[key]; exists || i > 0 && compareTransactionResources(record.Entries[i-1].Path, entry.Path) >= 0 {
			return errTransactionFSIO
		}
		entryPaths[key] = struct{}{}
		if entry.Exists {
			if entry.Backup != fmt.Sprintf("backups/%06d", i) {
				return errTransactionFSIO
			}
		} else if entry.Backup != "" || entry.Mode != 0 || entry.WindowsACL != "" {
			return errTransactionFSIO
		}
	}
	dirs := make(map[string]struct{}, len(record.CreatedDirs))
	for i, dir := range record.CreatedDirs {
		if !validTransactionRelativePath(dir) || !validTransactionNativeRelativePath(dir) {
			return errTransactionFSIO
		}
		key := transactionPathKey(dir)
		if _, exists := dirs[key]; exists || i > 0 && compareTransactionDirs(record.CreatedDirs[i-1], dir) >= 0 {
			return errTransactionFSIO
		}
		dirs[key] = struct{}{}
	}
	return nil
}

func compareTransactionDirs(a, b string) int {
	depthA, depthB := transactionPathDepth(a), transactionPathDepth(b)
	if depthA < depthB {
		return -1
	}
	if depthA > depthB {
		return 1
	}
	return strings.Compare(a, b)
}

func transactionPathDepth(name string) int {
	if !validTransactionRelativePath(name) {
		return 0
	}
	return strings.Count(name, "/") + 1
}

func durableTransactionStates(record transactionFSDurableJournal) []transactionFSState {
	states := make([]transactionFSState, len(record.Entries))
	for i, entry := range record.Entries {
		states[i] = transactionFSState{path: entry.Path, backup: entry.Backup, exists: entry.Exists, mode: entry.Mode, windowsACL: entry.WindowsACL}
	}
	return states
}

func injectTransactionFS(fail transactionFSFailpoint, stage, path string) error {
	if fail == nil {
		return nil
	}
	return fail(stage, path)
}
