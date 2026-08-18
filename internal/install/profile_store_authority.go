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

// withStoreAuthority supplies the common fail-closed lifecycle around
// build-tagged platform adapters. It intentionally does not implement a lock.
func withStoreAuthority(platform storePlatform, rawPath string, options CommitOptions, waiter monotonicWaiter, work func(authority) error) (err error) {
	if platform == nil || waiter == nil || work == nil || !options.valid() {
		return newProfileStoreError(CodeProfileInvalid, "profile_store.authority", false)
	}
	path, canonicalErr := platform.Canonical(rawPath)
	if canonicalErr != nil || !path.valid() {
		return newProfileStoreError(CodeProfileInvalid, "profile_store.path", false)
	}
	owned, acquireErr := platform.Acquire(path, options.WaitBudget, waiter)
	if acquireErr != nil {
		switch {
		case errors.Is(acquireErr, errStoreAuthorityBusy):
			return newProfileStoreError(CodeProfileBusy, "profile_store.authority", false)
		case errors.Is(acquireErr, errStoreAuthorityTimeout):
			return newProfileStoreError(CodeProfileTimeout, "profile_store.authority", false)
		case errors.Is(acquireErr, errStoreAuthorityInvalidPath):
			return newProfileStoreError(CodeProfileInvalid, "profile_store.path", false)
		default:
			return newProfileStoreError(CodeProfileIO, "profile_store.commit", false)
		}
	}
	if owned == nil {
		return newProfileStoreError(CodeProfileIO, "profile_store.commit", false)
	}
	// Release failures remain observable through a fixed sentinel and typed I/O error.
	// Adapter causes stay private because they can carry raw filesystem context.
	// When work already failed, its typed error remains first in the joined chain.
	// This preserves primary Code and Path while retaining deterministic cleanup evidence.
	defer func() {
		if releaseErr := owned.Release(); releaseErr != nil {
			cleanupErr := errors.Join(
				errStoreAuthorityCleanupFailed,
				newProfileStoreError(CodeProfileIO, "profile_store.commit", false),
			)
			if err == nil {
				err = cleanupErr
				return
			}
			err = errors.Join(err, cleanupErr)
		}
	}()
	return work(owned)
}
