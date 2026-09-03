package install

import (
	"crypto/sha256"
	"errors"
	"time"
)

const defaultProfileStoreWait = 5 * time.Second

var (
	errStoreAuthorityBusy          = errors.New("profile store authority busy")
	errStoreAuthorityTimeout       = errors.New("profile store authority timeout")
	errStoreAuthorityCleanupFailed = errors.New("profile store authority cleanup failed")
	errStoreRollbackFailed         = errors.New("profile store rollback failed")
)

// CommitOptions bounds profile-store authority acquisition. The CLI default is
// five seconds; zero is reserved for a single nonblocking attempt.
type CommitOptions struct{ WaitBudget time.Duration }

func (o CommitOptions) valid() bool {
	return o.WaitBudget == 0 || o.WaitBudget == defaultProfileStoreWait
}

// storePath carries a non-reversible canonical identity plus adapter-private
// state. Platform adapters validate paths and own the private value.
type storePath struct {
	identity [sha256.Size]byte
	platform any
}

func canonicalStorePath(identity string, platform any) (storePath, error) {
	if identity == "" {
		return storePath{}, errStoreAuthorityInvalidPath
	}
	return storePath{identity: sha256.Sum256([]byte(identity)), platform: platform}, nil
}

func (p storePath) valid() bool { return p.identity != [sha256.Size]byte{} }

var errStoreAuthorityInvalidPath = errors.New("invalid profile store path")

// authority is OS-owned writer authority. Release must relinquish the kernel
// resource; callers never infer staleness from metadata.
type authority interface{ Release() error }

type monotonicWaiter interface {
	Now() time.Time
	Sleep(time.Duration)
}

type systemMonotonicWaiter struct{}

func (systemMonotonicWaiter) Now() time.Time        { return time.Now() }
func (systemMonotonicWaiter) Sleep(d time.Duration) { time.Sleep(d) }

type storePlatform interface {
	Canonical(string) (storePath, error)
	Acquire(storePath, time.Duration, monotonicWaiter) (authority, error)
	Read(authority) ([]byte, bool, error)
	Commit(authority, []byte, []byte) error
}

// heldProfileState is adapter-private restoration evidence. It binds one exact
// canonical state leaf to the authority object that captured it.
type heldProfileState interface{ heldProfileState() }

type heldRestorePlatform interface {
	snapshotHeld(authority) ([]byte, bool, heldProfileState, error)
	restoreHeld(authority, heldProfileState) error
}

// heldStoreAuthority is the single internal lifecycle used by both public
// profile completion and D's reversible completion lease.
type heldStoreAuthority struct {
	platform storePlatform
	restore  heldRestorePlatform
	owned    authority
	released bool
}

func acquireStoreAuthority(platform storePlatform, rawPath string, options CommitOptions, waiter monotonicWaiter) (*heldStoreAuthority, error) {
	if platform == nil || waiter == nil || !options.valid() {
		return nil, newProfileStoreError(CodeProfileInvalid, "profile_store.authority", false)
	}
	path, canonicalErr := platform.Canonical(rawPath)
	if canonicalErr != nil || !path.valid() {
		return nil, newProfileStoreError(CodeProfileInvalid, "profile_store.path", false)
	}
	owned, acquireErr := platform.Acquire(path, options.WaitBudget, waiter)
	if acquireErr != nil {
		switch {
		case errors.Is(acquireErr, errStoreAuthorityBusy):
			return nil, newProfileStoreError(CodeProfileBusy, "profile_store.authority", false)
		case errors.Is(acquireErr, errStoreAuthorityTimeout):
			return nil, newProfileStoreError(CodeProfileTimeout, "profile_store.authority", false)
		case errors.Is(acquireErr, errStoreAuthorityInvalidPath):
			return nil, newProfileStoreError(CodeProfileInvalid, "profile_store.path", false)
		default:
			return nil, newProfileStoreError(CodeProfileIO, "profile_store.commit", false)
		}
	}
	if owned == nil {
		return nil, newProfileStoreError(CodeProfileIO, "profile_store.commit", false)
	}
	restore, _ := platform.(heldRestorePlatform)
	return &heldStoreAuthority{platform: platform, restore: restore, owned: owned}, nil
}

func (h *heldStoreAuthority) release() error {
	if h == nil || h.released {
		return nil
	}
	h.released = true
	if h.owned == nil || h.owned.Release() != nil {
		return errStoreAuthorityCleanupFailed
	}
	return nil
}

func profileReleaseResult(primary error, held *heldStoreAuthority, rollback bool) error {
	releaseErr := held.release()
	if releaseErr == nil {
		return primary
	}
	path := "profile_store.commit"
	if rollback {
		path = "profile_store.rollback"
	}
	cleanup := errors.Join(releaseErr, newProfileStoreError(CodeProfileIO, path, rollback))
	if primary == nil {
		return cleanup
	}
	return errors.Join(primary, cleanup)
}

// withStoreAuthority supplies the common fail-closed lifecycle around
// build-tagged platform adapters. Public Complete* and the D lease therefore
// cannot diverge in canonical identity, acquisition order, or release handling.
func withStoreAuthority(platform storePlatform, rawPath string, options CommitOptions, waiter monotonicWaiter, work func(authority) error) (err error) {
	if work == nil {
		return newProfileStoreError(CodeProfileInvalid, "profile_store.authority", false)
	}
	held, err := acquireStoreAuthority(platform, rawPath, options, waiter)
	if err != nil {
		return err
	}
	defer func() { err = profileReleaseResult(err, held, false) }()
	return work(held.owned)
}
