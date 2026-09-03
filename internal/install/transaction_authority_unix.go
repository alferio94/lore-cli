//go:build darwin || linux

package install

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

const unixTargetRootIdentityDomain = "lore-target-root-unix-v1\x00"

type unixTargetRootIdentity struct {
	device uint64
	inode  uint64
}

type unixTargetGuard struct {
	root *os.File
	once sync.Once
	err  error
}

func init() {
	canonicalTargetRoot = canonicalUnixTargetRoot
	acquireTargetOSGuard = acquireUnixTargetGuard
}

func canonicalUnixTargetRoot(raw string) (targetRootIdentity, error) {
	if raw == "" || strings.IndexByte(raw, 0) >= 0 || !filepath.IsAbs(raw) || targetPathTraverses(raw) {
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
	fd, err := unix.Open(resolved, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			return targetRootIdentity{}, errTransactionFSUnsafePath
		}
		return targetRootIdentity{}, errTransactionFSIO
	}
	root := os.NewFile(uintptr(fd), "selected-target-root")
	if root == nil {
		_ = unix.Close(fd)
		return targetRootIdentity{}, errTransactionFSIO
	}
	failed := true
	defer func() {
		if failed {
			_ = root.Close()
		}
	}()

	var stat unix.Stat_t
	pinned, err := root.Stat()
	if err != nil || unix.Fstat(fd, &stat) != nil || !safeUnixTargetRoot(stat) || !pinned.IsDir() {
		return targetRootIdentity{}, errTransactionFSUnsafePath
	}
	currentLeaf, err := os.Lstat(clean)
	if err != nil || !currentLeaf.IsDir() || currentLeaf.Mode()&os.ModeSymlink != 0 {
		return targetRootIdentity{}, errTransactionFSUnsafePath
	}
	current, err := os.Stat(clean)
	if err != nil || !os.SameFile(pinned, current) {
		return targetRootIdentity{}, errTransactionFSUnsafePath
	}
	resolvedCurrent, err := os.Stat(resolved)
	if err != nil || !os.SameFile(pinned, resolvedCurrent) {
		return targetRootIdentity{}, errTransactionFSUnsafePath
	}

	platform := unixTargetRootIdentity{device: uint64(stat.Dev), inode: uint64(stat.Ino)}
	identityBytes := make([]byte, len(unixTargetRootIdentityDomain)+16)
	copy(identityBytes, unixTargetRootIdentityDomain)
	binary.LittleEndian.PutUint64(identityBytes[len(unixTargetRootIdentityDomain):], platform.device)
	binary.LittleEndian.PutUint64(identityBytes[len(unixTargetRootIdentityDomain)+8:], platform.inode)
	failed = false
	return targetRootIdentity{
		digest:   sha256.Sum256(identityBytes),
		file:     root,
		info:     pinned,
		platform: platform,
	}, nil
}

func safeUnixTargetRoot(stat unix.Stat_t) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFDIR && stat.Mode&0o777 == 0o700
}

func acquireUnixTargetGuard(root targetRootIdentity, deadline targetAuthorityDeadline, clock targetMonotonicWaiter) (targetGuard, error) {
	platform, ok := root.platform.(unixTargetRootIdentity)
	if !ok || !root.valid() || clock == nil {
		return nil, errTransactionFSUnsafePath
	}
	if !unixTargetRootStillPinned(root, platform) {
		return nil, errTransactionFSUnsafePath
	}
	for {
		err := unix.Flock(int(root.file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			if !unixTargetRootStillPinned(root, platform) {
				_ = unix.Flock(int(root.file.Fd()), unix.LOCK_UN)
				return nil, errTransactionFSUnsafePath
			}
			return &unixTargetGuard{root: root.file}, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return nil, errTransactionFSIO
		}
		if deadline.zero {
			return nil, targetAuthorityErrorFor(CodeTargetAuthorityBusy)
		}
		if deadline.expired(clock) {
			return nil, targetAuthorityErrorFor(CodeTargetAuthorityTimeout)
		}
		deadline.sleep(clock)
	}
}

func unixTargetRootStillPinned(root targetRootIdentity, want unixTargetRootIdentity) bool {
	var stat unix.Stat_t
	return root.file != nil && unix.Fstat(int(root.file.Fd()), &stat) == nil &&
		safeUnixTargetRoot(stat) && uint64(stat.Dev) == want.device && uint64(stat.Ino) == want.inode
}

// Flock ownership is attached to the pinned open file description. Process
// death or descriptor close releases it in the kernel; no timestamp, PID, host
// metadata, or removable lock artifact participates. Durable journal recovery
// remains the transaction filesystem layer's separate responsibility.
func (*unixTargetGuard) Recover() error { return nil }

func (g *unixTargetGuard) Release() error {
	if g == nil {
		return nil
	}
	g.once.Do(func() {
		if g.root == nil || unix.Flock(int(g.root.Fd()), unix.LOCK_UN) != nil {
			g.err = errTargetAuthorityRelease
		}
	})
	return g.err
}
