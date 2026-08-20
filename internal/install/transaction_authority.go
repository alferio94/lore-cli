package install

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultTargetAuthorityWait = 5 * time.Second
	targetAuthorityPoll        = 50 * time.Millisecond
)

const (
	CodeTargetAuthorityBusy    TransactionCode = "target_authority_busy"
	CodeTargetAuthorityTimeout TransactionCode = "target_authority_timeout"
)

var errTargetAuthorityRelease = errors.New("selected target authority release failed")

type targetAuthorityError struct{ code TransactionCode }

func (e *targetAuthorityError) Error() string {
	switch e.code {
	case CodeTargetAuthorityBusy:
		return "selected target is busy"
	case CodeTargetAuthorityTimeout:
		return "selected target wait timed out"
	default:
		return "selected target authority failed"
	}
}

func (e *targetAuthorityError) Code() TransactionCode { return e.code }
func (e *targetAuthorityError) Path() string          { return "selected_target.authority" }
func (e *targetAuthorityError) Is(target error) bool {
	code, ok := target.(TransactionCode)
	return ok && code == e.code
}

func targetAuthorityErrorFor(code TransactionCode) error {
	return &targetAuthorityError{code: code}
}

// targetGuard covers the complete selected-target mutation lifecycle. Build-
// tagged adapters add OS ownership and recovery behind this interface.
type targetGuard interface {
	Recover() error
	Release() error
}

type targetMonotonicWaiter interface {
	Now() time.Time
	Sleep(time.Duration)
}

type systemTargetMonotonicWaiter struct{}

func (systemTargetMonotonicWaiter) Now() time.Time        { return time.Now() }
func (systemTargetMonotonicWaiter) Sleep(d time.Duration) { time.Sleep(d) }

type targetAuthorityDeadline struct {
	zero bool
	at   time.Time
}

func newTargetAuthorityDeadline(start time.Time, wait time.Duration) targetAuthorityDeadline {
	return targetAuthorityDeadline{zero: wait == 0, at: start.Add(wait)}
}

func (d targetAuthorityDeadline) expired(clock targetMonotonicWaiter) bool {
	return !d.zero && !clock.Now().Before(d.at)
}

func (d targetAuthorityDeadline) sleep(clock targetMonotonicWaiter) {
	remaining := d.at.Sub(clock.Now())
	if remaining <= 0 {
		return
	}
	if remaining > targetAuthorityPoll {
		remaining = targetAuthorityPoll
	}
	clock.Sleep(remaining)
}

type targetRootIdentity struct {
	digest   [sha256.Size]byte
	file     *os.File
	info     os.FileInfo
	platform any
}

func (r targetRootIdentity) valid() bool {
	return r.digest != [sha256.Size]byte{} && r.file != nil && r.info != nil && r.info.IsDir()
}

// These seams are replaced exactly once by build-tagged adapters during init.
// The common defaults provide only process-local exclusion; they claim no
// cross-process ownership or owner-death recovery.
var (
	canonicalTargetRoot  = canonicalCommonTargetRoot
	acquireTargetOSGuard = func(targetRootIdentity, targetAuthorityDeadline, targetMonotonicWaiter) (targetGuard, error) {
		return noopTargetGuard{}, nil
	}
)

func canonicalCommonTargetRoot(raw string) (targetRootIdentity, error) {
	if !filepath.IsAbs(raw) || targetPathTraverses(raw) {
		return targetRootIdentity{}, errTransactionFSUnsafePath
	}
	clean := filepath.Clean(raw)
	leaf, err := os.Lstat(clean)
	if err != nil || !leaf.IsDir() || leaf.Mode()&os.ModeSymlink != 0 {
		return targetRootIdentity{}, errTransactionFSUnsafePath
	}
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil || !filepath.IsAbs(resolved) {
		return targetRootIdentity{}, errTransactionFSUnsafePath
	}
	resolved = filepath.Clean(resolved)
	leaf, err = os.Lstat(resolved)
	if err != nil || !leaf.IsDir() || leaf.Mode()&os.ModeSymlink != 0 {
		return targetRootIdentity{}, errTransactionFSUnsafePath
	}
	file, err := os.Open(resolved)
	if err != nil {
		return targetRootIdentity{}, errTransactionFSIO
	}
	pinned, err := file.Stat()
	if err != nil || !pinned.IsDir() || !os.SameFile(leaf, pinned) {
		_ = file.Close()
		return targetRootIdentity{}, errTransactionFSUnsafePath
	}
	return targetRootIdentity{
		digest: sha256.Sum256([]byte(resolved)),
		file:   file,
		info:   pinned,
	}, nil
}

func targetPathTraverses(path string) bool {
	path = strings.TrimPrefix(path, filepath.VolumeName(path))
	for _, part := range strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if part == ".." {
			return true
		}
	}
	return false
}

type localTargetWaiter struct{ root targetRootIdentity }

type localTargetEntry struct {
	identity os.FileInfo
	held     bool
	queue    []*localTargetWaiter
}

type localTargetRegistry struct {
	mu      sync.Mutex
	entries map[[sha256.Size]byte]*localTargetEntry
}

var processTargetRegistry localTargetRegistry

func acquireTargetGuard(root string, wait time.Duration) (targetGuard, error) {
	return acquireTargetGuardWithWaiter(root, wait, systemTargetMonotonicWaiter{})
}

func acquireTargetGuardWithWaiter(root string, wait time.Duration, clock targetMonotonicWaiter) (targetGuard, error) {
	if clock == nil || wait != 0 && wait != defaultTargetAuthorityWait {
		return nil, errTransactionFSUnsafePath
	}
	deadline := newTargetAuthorityDeadline(clock.Now(), wait)
	identity, err := canonicalTargetRoot(root)
	if err != nil {
		return nil, err
	}
	if !identity.valid() {
		return nil, closeTargetRoot(identity, errTransactionFSUnsafePath)
	}
	if deadline.expired(clock) {
		return nil, closeTargetRoot(identity, targetAuthorityErrorFor(CodeTargetAuthorityTimeout))
	}

	waiting := &localTargetWaiter{root: identity}
	entry, acquired := processTargetRegistry.enter(identity, waiting, wait == 0)
	if !acquired && wait == 0 {
		return nil, closeTargetRoot(identity, targetAuthorityErrorFor(CodeTargetAuthorityBusy))
	}
	if !acquired {
		for {
			if deadline.expired(clock) {
				processTargetRegistry.cancel(entry, waiting)
				return nil, closeTargetRoot(identity, targetAuthorityErrorFor(CodeTargetAuthorityTimeout))
			}
			if processTargetRegistry.claim(entry, waiting) {
				if deadline.expired(clock) {
					processTargetRegistry.release(entry)
					return nil, closeTargetRoot(identity, targetAuthorityErrorFor(CodeTargetAuthorityTimeout))
				}
				break
			}
			deadline.sleep(clock)
		}
	}

	local := &localTargetGuard{registry: &processTargetRegistry, entry: entry, root: identity}
	osGuard, err := acquireTargetOSGuard(identity, deadline, clock)
	if err != nil {
		return nil, releaseTargetGuard(local, err)
	}
	if osGuard == nil {
		return nil, releaseTargetGuard(local, errTransactionFSIO)
	}
	combined := &combinedTargetGuard{local: local, os: osGuard}
	if deadline.expired(clock) {
		return nil, releaseTargetGuard(combined, targetAuthorityErrorFor(CodeTargetAuthorityTimeout))
	}
	return combined, nil
}

func closeTargetRoot(root targetRootIdentity, primary error) error {
	if root.file != nil && root.file.Close() != nil {
		return errors.Join(primary, errTargetAuthorityRelease)
	}
	return primary
}

func releaseTargetGuard(guard targetGuard, primary error) error {
	if releaseErr := guard.Release(); releaseErr != nil {
		return errors.Join(primary, releaseErr)
	}
	return primary
}

func (r *localTargetRegistry) enter(identity targetRootIdentity, waiting *localTargetWaiter, zero bool) (*localTargetEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries == nil {
		r.entries = make(map[[sha256.Size]byte]*localTargetEntry)
	}
	entry := r.entries[identity.digest]
	if entry == nil {
		seen := make(map[*localTargetEntry]struct{})
		for _, candidate := range r.entries {
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			if os.SameFile(candidate.identity, identity.info) {
				entry = candidate
				r.entries[identity.digest] = entry
				break
			}
		}
	}
	if entry == nil {
		entry = &localTargetEntry{identity: identity.info, held: true}
		r.entries[identity.digest] = entry
		return entry, true
	}
	if !zero {
		entry.queue = append(entry.queue, waiting)
	}
	return entry, false
}

func (r *localTargetRegistry) claim(entry *localTargetEntry, waiting *localTargetWaiter) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry.held || len(entry.queue) == 0 || entry.queue[0] != waiting {
		return false
	}
	entry.queue = entry.queue[1:]
	entry.held = true
	entry.identity = waiting.root.info
	return true
}

func (r *localTargetRegistry) cancel(entry *localTargetEntry, waiting *localTargetWaiter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, candidate := range entry.queue {
		if candidate == waiting {
			entry.queue = append(entry.queue[:i], entry.queue[i+1:]...)
			break
		}
	}
	if !entry.held && len(entry.queue) == 0 {
		r.remove(entry)
	}
}

func (r *localTargetRegistry) release(entry *localTargetEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry.held = false
	if len(entry.queue) == 0 {
		r.remove(entry)
	}
}

func (r *localTargetRegistry) remove(entry *localTargetEntry) {
	for digest, candidate := range r.entries {
		if candidate == entry {
			delete(r.entries, digest)
		}
	}
}

type localTargetGuard struct {
	registry *localTargetRegistry
	entry    *localTargetEntry
	root     targetRootIdentity
	once     sync.Once
	err      error
}

func (*localTargetGuard) Recover() error { return nil }

func (g *localTargetGuard) Release() error {
	if g == nil {
		return nil
	}
	g.once.Do(func() {
		g.registry.release(g.entry)
		if g.root.file.Close() != nil {
			g.err = errTargetAuthorityRelease
		}
	})
	return g.err
}

type combinedTargetGuard struct {
	local *localTargetGuard
	os    targetGuard
	once  sync.Once
	err   error
}

func (g *combinedTargetGuard) Recover() error { return g.os.Recover() }

func (g *combinedTargetGuard) Release() error {
	if g == nil {
		return nil
	}
	g.once.Do(func() {
		if err := g.os.Release(); err != nil {
			g.err = errTargetAuthorityRelease
		}
		if err := g.local.Release(); err != nil {
			g.err = errors.Join(g.err, err)
		}
	})
	return g.err
}

type noopTargetGuard struct{}

func (noopTargetGuard) Recover() error { return nil }
func (noopTargetGuard) Release() error { return nil }
