//go:build darwin || linux

package install

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const unixAuthorityPoll = 100 * time.Millisecond

type unixStorePlatform struct{ fail func(string) error }

type unixStorePath struct {
	parent         *os.File
	rawParent      string
	resolvedParent string
	leaf           string
	lockLeaf       string
	device         uint64
	inode          uint64
}

type unixStoreAuthority struct {
	parent      *os.File
	path        *unixStorePath
	state       *os.File
	sidecar     *os.File
	locks       []*os.File
	ownsSidecar bool
	released    bool
}

type unixHeldProfileState struct {
	authority *unixStoreAuthority
	present   bool
	raw       []byte
	mode      uint32
}

func (*unixHeldProfileState) heldProfileState() {}

func newUnixStorePlatform() unixStorePlatform { return unixStorePlatform{} }
func defaultStorePlatform() storePlatform     { return newUnixStorePlatform() }

func protectStoreDirectory(path string) error { return os.Chmod(path, 0o700) }
func storeFilePermissionsValid(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().Perm()&0o077 == 0
}

func (unixStorePlatform) Canonical(raw string) (result storePath, err error) {
	parent, leaf, err := splitUnixStorePath(raw)
	if err != nil {
		return storePath{}, errStoreAuthorityInvalidPath
	}
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil || !filepath.IsAbs(resolved) {
		return storePath{}, errStoreAuthorityInvalidPath
	}
	fd, err := unix.Open(resolved, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return storePath{}, errStoreAuthorityInvalidPath
	}
	parentFile := os.NewFile(uintptr(fd), "profile-store-parent")
	if parentFile == nil {
		_ = unix.Close(fd)
		return storePath{}, errStoreAuthorityInvalidPath
	}
	defer func() {
		if err != nil {
			_ = parentFile.Close()
		}
	}()
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || !safeUnixDirectory(stat) {
		return storePath{}, errStoreAuthorityInvalidPath
	}
	platform := &unixStorePath{
		parent:         parentFile,
		rawParent:      parent,
		resolvedParent: resolved,
		leaf:           leaf,
		lockLeaf:       "." + leaf + ".lock",
		device:         uint64(stat.Dev),
		inode:          uint64(stat.Ino),
	}
	if len(platform.lockLeaf) > 255 || platform.validateLeaves() != nil {
		return storePath{}, errStoreAuthorityInvalidPath
	}
	identity := fmt.Sprintf("unix:%d:%d:%s", platform.device, platform.inode, leaf)
	result, err = canonicalStorePath(identity, platform)
	return result, err
}

func splitUnixStorePath(raw string) (string, string, error) {
	if raw == "" || !filepath.IsAbs(raw) || strings.IndexByte(raw, 0) >= 0 {
		return "", "", errStoreAuthorityInvalidPath
	}
	for _, component := range strings.Split(raw, string(os.PathSeparator)) {
		if component == ".." {
			return "", "", errStoreAuthorityInvalidPath
		}
	}
	clean := filepath.Clean(raw)
	leaf := filepath.Base(clean)
	if clean == string(os.PathSeparator) || leaf == "." || leaf == string(os.PathSeparator) || len(leaf) > 240 {
		return "", "", errStoreAuthorityInvalidPath
	}
	return filepath.Dir(clean), leaf, nil
}

func (p *unixStorePath) validateLeaves() error {
	if err := validateUnixLeaf(int(p.parent.Fd()), p.leaf, false); err != nil {
		return err
	}
	return validateUnixLeaf(int(p.parent.Fd()), p.lockLeaf, true)
}

func (p *unixStorePath) revalidate() error {
	var held unix.Stat_t
	if unix.Fstat(int(p.parent.Fd()), &held) != nil || !safeUnixDirectory(held) || uint64(held.Dev) != p.device || uint64(held.Ino) != p.inode {
		return errStoreAuthorityInvalidPath
	}
	resolved, err := filepath.EvalSymlinks(p.rawParent)
	if err != nil || filepath.Clean(resolved) != filepath.Clean(p.resolvedParent) {
		return errStoreAuthorityInvalidPath
	}
	var current unix.Stat_t
	if unix.Stat(resolved, &current) != nil || uint64(current.Dev) != p.device || uint64(current.Ino) != p.inode {
		return errStoreAuthorityInvalidPath
	}
	return p.validateLeaves()
}

func validateUnixLeaf(parentFD int, leaf string, authorityLeaf bool) error {
	var stat unix.Stat_t
	err := unix.Fstatat(parentFD, leaf, &stat, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil || !safeUnixFile(stat) || (authorityLeaf && stat.Size != 0) {
		return errStoreAuthorityInvalidPath
	}
	return nil
}

func safeUnixDirectory(stat unix.Stat_t) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFDIR && stat.Mode&0o777 == 0o700
}

func safeUnixFile(stat unix.Stat_t) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFREG && stat.Mode&0o777 == 0o600 && stat.Nlink == 1
}

func (unixStorePlatform) Acquire(path storePath, budget time.Duration, waiter monotonicWaiter) (authority, error) {
	platform, ok := path.platform.(*unixStorePath)
	if !ok || platform == nil || platform.parent == nil || waiter == nil || (budget != 0 && budget != defaultProfileStoreWait) {
		return nil, errStoreAuthorityInvalidPath
	}
	if err := platform.revalidate(); err != nil {
		_ = platform.parent.Close()
		return nil, errStoreAuthorityInvalidPath
	}
	deadline := waiter.Now().Add(budget)
	for {
		owned, contended, err := tryUnixAuthority(platform)
		if err != nil {
			_ = platform.parent.Close()
			return nil, err
		}
		if !contended {
			return owned, nil
		}
		if budget == 0 {
			_ = platform.parent.Close()
			return nil, errStoreAuthorityBusy
		}
		now := waiter.Now()
		if !now.Before(deadline) {
			_ = platform.parent.Close()
			return nil, errStoreAuthorityTimeout
		}
		sleep := deadline.Sub(now)
		if sleep > unixAuthorityPoll {
			sleep = unixAuthorityPoll
		}
		waiter.Sleep(sleep)
	}
}

func tryUnixAuthority(path *unixStorePath) (*unixStoreAuthority, bool, error) {
	state, missing, err := openUnixLeaf(path, path.leaf, false)
	if err != nil {
		return nil, false, err
	}
	if !missing {
		locked, err := lockUnixFile(state)
		if err != nil {
			_ = state.Close()
			return nil, false, err
		}
		if !locked {
			_ = state.Close()
			return nil, true, nil
		}
		if path.revalidate() != nil || !unixFileMatchesLeaf(path, path.leaf, state, false) {
			_ = unix.Flock(int(state.Fd()), unix.LOCK_UN)
			_ = state.Close()
			return nil, false, errStoreAuthorityInvalidPath
		}
		return &unixStoreAuthority{parent: path.parent, path: path, state: state, locks: []*os.File{state}}, false, nil
	}

	sidecar, created, err := openUnixAuthorityLeaf(path)
	if err != nil {
		return nil, false, err
	}
	locked, err := lockUnixFile(sidecar)
	if err != nil {
		_ = sidecar.Close()
		return nil, false, err
	}
	if !locked {
		_ = sidecar.Close()
		return nil, true, nil
	}
	if err := path.revalidate(); err != nil || !unixFileMatchesLeaf(path, path.lockLeaf, sidecar, true) {
		_ = unix.Flock(int(sidecar.Fd()), unix.LOCK_UN)
		_ = sidecar.Close()
		return nil, false, errStoreAuthorityInvalidPath
	}
	state, missing, err = openUnixLeaf(path, path.leaf, false)
	if err != nil {
		_ = unix.Flock(int(sidecar.Fd()), unix.LOCK_UN)
		_ = sidecar.Close()
		return nil, false, err
	}
	locks := []*os.File{sidecar}
	if !missing {
		stateLocked, lockErr := lockUnixFile(state)
		if lockErr != nil || !stateLocked {
			_ = state.Close()
			_ = unix.Flock(int(sidecar.Fd()), unix.LOCK_UN)
			_ = sidecar.Close()
			if lockErr != nil {
				return nil, false, lockErr
			}
			return nil, true, nil
		}
		if !unixFileMatchesLeaf(path, path.leaf, state, false) {
			_ = unix.Flock(int(state.Fd()), unix.LOCK_UN)
			_ = state.Close()
			_ = unix.Flock(int(sidecar.Fd()), unix.LOCK_UN)
			_ = sidecar.Close()
			return nil, false, errStoreAuthorityInvalidPath
		}
		locks = append(locks, state)
	}
	return &unixStoreAuthority{
		parent: path.parent, path: path, state: state, sidecar: sidecar,
		locks: locks, ownsSidecar: created,
	}, false, nil
}

func (unixStorePlatform) Read(owned authority) ([]byte, bool, error) {
	a, ok := owned.(*unixStoreAuthority)
	if !ok || a == nil || a.released || a.path.revalidate() != nil {
		return nil, false, errStoreAuthorityInvalidPath
	}
	if a.state == nil {
		return nil, false, nil
	}
	if !unixFileMatchesLeaf(a.path, a.path.leaf, a.state, false) {
		return nil, false, errStoreAuthorityInvalidPath
	}
	if _, err := a.state.Seek(0, io.SeekStart); err != nil {
		return nil, false, err
	}
	raw, err := io.ReadAll(a.state)
	return raw, true, err
}

func (p unixStorePlatform) snapshotHeld(owned authority) ([]byte, bool, heldProfileState, error) {
	a, ok := owned.(*unixStoreAuthority)
	if !ok || a == nil || a.released || a.path.revalidate() != nil {
		return nil, false, nil, errStoreAuthorityInvalidPath
	}
	raw, present, err := p.Read(owned)
	if err != nil {
		return nil, false, nil, err
	}
	state := &unixHeldProfileState{authority: a, present: present, raw: append([]byte(nil), raw...)}
	if present {
		var stat unix.Stat_t
		if a.state == nil || unix.Fstat(int(a.state.Fd()), &stat) != nil || !safeUnixFile(stat) || !unixFileMatchesLeaf(a.path, a.path.leaf, a.state, false) {
			return nil, false, nil, errStoreAuthorityInvalidPath
		}
		state.mode = uint32(stat.Mode & 0o777)
	}
	return append([]byte(nil), raw...), present, state, nil
}

func (p unixStorePlatform) restoreHeld(owned authority, prior heldProfileState) error {
	a, ok := owned.(*unixStoreAuthority)
	state, stateOK := prior.(*unixHeldProfileState)
	if !ok || !stateOK || a == nil || state == nil || state.authority != a || a.released || a.path.revalidate() != nil {
		return errStoreAuthorityInvalidPath
	}
	if err := p.inject("held-restore"); err != nil {
		return err
	}
	if !state.present {
		if a.sidecar == nil || !unixFileMatchesLeaf(a.path, a.path.lockLeaf, a.sidecar, true) {
			sidecar, created, err := openUnixAuthorityLeaf(a.path)
			if err != nil || !created {
				if sidecar != nil {
					_ = sidecar.Close()
				}
				return errStoreAuthorityInvalidPath
			}
			locked, err := lockUnixFile(sidecar)
			if err != nil || !locked {
				_ = sidecar.Close()
				return errStoreAuthorityInvalidPath
			}
			a.sidecar, a.ownsSidecar = sidecar, true
			a.locks = append(a.locks, sidecar)
		}
		if a.state != nil && !unixFileMatchesLeaf(a.path, a.path.leaf, a.state, false) {
			return errStoreAuthorityInvalidPath
		}
		if err := unix.Unlinkat(int(a.parent.Fd()), a.path.leaf, 0); err != nil && !errors.Is(err, unix.ENOENT) {
			return err
		}
		a.state = nil
		if err := syncUnixDirectory(a.parent); err != nil {
			return err
		}
		return p.inject("held-restore-cleanup")
	}
	if state.mode != 0o600 {
		return errStoreAuthorityInvalidPath
	}
	name, file, err := p.writeTemp(a, state.raw)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Unlinkat(int(a.parent.Fd()), name, 0) }()
	if err = unix.Fchmod(int(file.Fd()), state.mode); err == nil {
		err = unix.Renameat(int(a.parent.Fd()), name, int(a.parent.Fd()), a.path.leaf)
	}
	if err != nil {
		_ = file.Close()
		return err
	}
	name = ""
	a.state, a.locks = file, append(a.locks, file)
	if err = syncUnixDirectory(a.parent); err != nil {
		return err
	}
	return p.inject("held-restore-cleanup")
}

func (p unixStorePlatform) Commit(owned authority, prior, next []byte) error {
	a, ok := owned.(*unixStoreAuthority)
	if !ok || a == nil || a.released || a.path.revalidate() != nil {
		return errStoreAuthorityInvalidPath
	}
	name, file, err := p.writeTemp(a, next)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Unlinkat(int(a.parent.Fd()), name, 0) }()
	if err = p.inject("replace"); err == nil {
		err = unix.Renameat(int(a.parent.Fd()), name, int(a.parent.Fd()), a.path.leaf)
	}
	if err != nil {
		_ = file.Close()
		return err
	}
	name = ""
	a.state, a.locks = file, append(a.locks, file)
	if err = p.inject("dirsync"); err == nil {
		err = syncUnixDirectory(a.parent)
	}
	if err == nil {
		err = p.inject("cleanup")
	}
	if err == nil && len(prior) == 0 {
		err = a.removeSidecar(false)
	} else if err == nil {
		err = syncUnixDirectory(a.parent)
	}
	if err == nil {
		return nil
	}
	if restoreErr := p.restore(a, prior); restoreErr != nil {
		return errors.Join(errStoreRollbackFailed, restoreErr)
	}
	return err
}

func (p unixStorePlatform) writeTemp(a *unixStoreAuthority, data []byte) (string, *os.File, error) {
	var token [8]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", nil, err
	}
	name := fmt.Sprintf(".profiles-%x.tmp", token)
	fd, err := unix.Openat(int(a.parent.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return "", nil, err
	}
	file := os.NewFile(uintptr(fd), "profile-store-temp")
	if file == nil {
		_ = unix.Close(fd)
		return name, nil, unix.EBADF
	}
	if err = p.inject("write"); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = p.inject("sync")
	}
	if err == nil {
		err = file.Sync()
	}
	if err == nil {
		_, err = lockUnixFile(file)
	}
	if err != nil {
		_ = file.Close()
		_ = unix.Unlinkat(int(a.parent.Fd()), name, 0)
		return "", nil, err
	}
	return name, file, nil
}

func (p unixStorePlatform) restore(a *unixStoreAuthority, prior []byte) error {
	if err := p.inject("restore"); err != nil {
		return err
	}
	if len(prior) == 0 {
		if err := unix.Unlinkat(int(a.parent.Fd()), a.path.leaf, 0); err != nil && !errors.Is(err, unix.ENOENT) {
			return err
		}
		return syncUnixDirectory(a.parent)
	}
	name, file, err := p.writeTemp(a, prior)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Unlinkat(int(a.parent.Fd()), name, 0) }()
	if err = unix.Renameat(int(a.parent.Fd()), name, int(a.parent.Fd()), a.path.leaf); err != nil {
		_ = file.Close()
		return err
	}
	a.state, a.locks = file, append(a.locks, file)
	return syncUnixDirectory(a.parent)
}

func (p unixStorePlatform) inject(stage string) error {
	if p.fail != nil {
		return p.fail(stage)
	}
	return nil
}

func syncUnixDirectory(parent *os.File) error {
	err := unix.Fsync(int(parent.Fd()))
	if errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOTSUP) {
		return nil
	}
	return err
}

// appendCompletionBackup extends the already sealed selected-target journal
// with D's target-scoped v3 state. The durable header is replaced before the
// manifest write, so owner death still restores the complete boundary.
func (p *unixTransactionFS) appendCompletionBackup(rel string, index int, states []transactionFSState) (transactionFSState, error) {
	if p == nil || transactionPathBase(rel) != provenanceV3Name || index != len(states) || !validTransactionRelativePath(rel) {
		return transactionFSState{}, errTransactionFSUnsafePath
	}
	target, err := transactionNativePath(p.root, rel)
	if err != nil {
		return transactionFSState{}, err
	}
	info, err := p.validate(target, true)
	state := transactionFSState{path: rel}
	createdBackup, complete := "", false
	defer func() {
		if !complete && createdBackup != "" {
			_ = os.Remove(createdBackup)
		}
	}()
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	} else if err != nil || info.IsDir() {
		return transactionFSState{}, errTransactionFSUnsafePath
	} else {
		fd, openErr := unix.Open(target, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if openErr != nil {
			return transactionFSState{}, errTransactionFSIO
		}
		source := os.NewFile(uintptr(fd), "completion-manifest-source")
		if source == nil {
			_ = unix.Close(fd)
			return transactionFSState{}, errTransactionFSIO
		}
		defer source.Close()
		var stat unix.Stat_t
		if unix.Fstat(fd, &stat) != nil || !safeTransactionUnixFile(stat) {
			return transactionFSState{}, errTransactionFSUnsafePath
		}
		backup := fmt.Sprintf("backups/%06d", index)
		backupPath, pathErr := transactionNativePath(p.journal, backup)
		if pathErr != nil {
			return transactionFSState{}, pathErr
		}
		out, createErr := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if createErr == nil {
			_, createErr = out.ReadFrom(source)
		}
		if createErr == nil {
			createErr = out.Sync()
		}
		if out != nil {
			if closeErr := out.Close(); createErr == nil {
				createErr = closeErr
			}
		}
		if createErr != nil || syncTransactionUnixDir(filepath.Dir(backupPath)) != nil {
			_ = os.Remove(backupPath)
			return transactionFSState{}, errTransactionFSIO
		}
		createdBackup = backupPath
		state = transactionFSState{path: rel, backup: backup, exists: true, mode: uint32(info.Mode().Perm())}
	}
	next := append(append([]transactionFSState(nil), states...), state)
	data, err := encodeTransactionFSDurableJournal(p.identity, next, p.createdDirs)
	if err != nil {
		return transactionFSState{}, err
	}
	temp := filepath.Join(p.journal, "journal.json.completion")
	if _, err := os.Lstat(temp); !errors.Is(err, os.ErrNotExist) {
		return transactionFSState{}, errTransactionFSIO
	}
	if err := writeTransactionUnixDurableFile(temp, data); err != nil {
		return transactionFSState{}, err
	}
	if err := os.Rename(temp, filepath.Join(p.journal, "journal.json")); err != nil || syncTransactionUnixDir(p.journal) != nil {
		_ = os.Remove(temp)
		return transactionFSState{}, errTransactionFSIO
	}
	complete = true
	return state, nil
}

func (p *unixTransactionFS) publishCompletionManifest(rel string, data []byte, fail transactionFSFailpoint) (err error) {
	if transactionPathBase(rel) != provenanceV3Name || len(data) == 0 {
		return errTransactionFSUnsafePath
	}
	target, err := transactionNativePath(p.root, rel)
	if err != nil {
		return err
	}
	if err := p.ensureParents(filepath.Dir(target)); err != nil {
		return err
	}
	if _, err := p.validate(target, true); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errTransactionFSUnsafePath
	}
	name, err := p.transactionTempPath(target)
	if err != nil {
		return err
	}
	if err := injectTransactionFS(fail, "manifest-temp", rel); err != nil {
		return err
	}
	tmp, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errTransactionFSIO
	}
	closed := false
	defer func() {
		if !closed {
			if closeErr := tmp.Close(); err == nil && closeErr != nil {
				err = errTransactionFSIO
			}
		}
		if removeErr := os.Remove(name); err == nil && removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errTransactionFSIO
		}
	}()
	if err = injectTransactionFS(fail, "manifest-write", rel); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = injectTransactionFS(fail, "manifest-sync", rel)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	closed = true
	if err == nil {
		err = injectTransactionFS(fail, "manifest-replace", rel)
	}
	if err == nil {
		err = os.Rename(name, target)
	}
	if err == nil {
		err = injectTransactionFS(fail, "manifest-dirsync", rel)
	}
	if err == nil {
		err = syncTransactionUnixDir(filepath.Dir(target))
	}
	if err == nil {
		err = injectTransactionFS(fail, "manifest-cleanup", rel)
	}
	if err != nil {
		return err
	}
	return nil
}

func unixFileMatchesLeaf(path *unixStorePath, leaf string, file *os.File, authorityLeaf bool) bool {
	var opened, current unix.Stat_t
	if unix.Fstat(int(file.Fd()), &opened) != nil || unix.Fstatat(int(path.parent.Fd()), leaf, &current, unix.AT_SYMLINK_NOFOLLOW) != nil {
		return false
	}
	return safeUnixFile(opened) && safeUnixFile(current) && opened.Dev == current.Dev && opened.Ino == current.Ino && (!authorityLeaf || opened.Size == 0)
}

func openUnixLeaf(path *unixStorePath, leaf string, authorityLeaf bool) (*os.File, bool, error) {
	fd, err := unix.Openat(int(path.parent.Fd()), leaf, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, true, nil
	}
	if err != nil {
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			return nil, false, errStoreAuthorityInvalidPath
		}
		return nil, false, err
	}
	file := os.NewFile(uintptr(fd), "profile-store-authority")
	if file == nil {
		_ = unix.Close(fd)
		return nil, false, unix.EBADF
	}
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || !safeUnixFile(stat) || (authorityLeaf && stat.Size != 0) {
		_ = file.Close()
		return nil, false, errStoreAuthorityInvalidPath
	}
	return file, false, nil
}

func openUnixAuthorityLeaf(path *unixStorePath) (*os.File, bool, error) {
	flags := unix.O_RDWR | unix.O_CLOEXEC | unix.O_NOFOLLOW
	for attempt := 0; attempt < 2; attempt++ {
		fd, err := unix.Openat(int(path.parent.Fd()), path.lockLeaf, flags|unix.O_CREAT|unix.O_EXCL, 0o600)
		if errors.Is(err, unix.EEXIST) {
			file, missing, openErr := openUnixLeaf(path, path.lockLeaf, true)
			if openErr != nil || !missing {
				return file, false, openErr
			}
			continue
		}
		if err != nil {
			if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
				return nil, false, errStoreAuthorityInvalidPath
			}
			return nil, false, err
		}
		file := os.NewFile(uintptr(fd), "profile-store-authority")
		if file == nil {
			_ = unix.Close(fd)
			return nil, false, unix.EBADF
		}
		if unix.Fchmod(fd, 0o600) != nil {
			_ = file.Close()
			return nil, false, unix.EPERM
		}
		var stat unix.Stat_t
		if unix.Fstat(fd, &stat) != nil || !safeUnixFile(stat) || stat.Size != 0 {
			_ = file.Close()
			return nil, false, errStoreAuthorityInvalidPath
		}
		return file, true, nil
	}
	return nil, false, errStoreAuthorityInvalidPath
}

func lockUnixFile(file *os.File) (bool, error) {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return false, nil
	}
	return err == nil, err
}

// removeSidecar runs before unlock and unlinks only the still-locked matching inode.
// Release requires creator provenance; successful state-inode handoff may clean dead residue.
func (a *unixStoreAuthority) removeSidecar(creatorOnly bool) error {
	if (creatorOnly && !a.ownsSidecar) || a.sidecar == nil || !unixFileMatchesLeaf(a.path, a.path.lockLeaf, a.sidecar, true) {
		return nil
	}
	if err := unix.Unlinkat(int(a.parent.Fd()), a.path.lockLeaf, 0); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return err
	}
	a.ownsSidecar = false
	return syncUnixDirectory(a.parent)
}

func (a *unixStoreAuthority) Release() error { return a.close(true) }
func (a *unixStoreAuthority) closeWithoutUnlock() error {
	return a.close(false)
}

func (a *unixStoreAuthority) close(unlock bool) error {
	if a == nil || a.released {
		return nil
	}
	a.released = true
	var result error
	if unlock {
		result = errors.Join(result, a.removeSidecar(true))
	}
	for i := len(a.locks) - 1; i >= 0; i-- {
		if unlock {
			result = errors.Join(result, unix.Flock(int(a.locks[i].Fd()), unix.LOCK_UN))
		}
		result = errors.Join(result, a.locks[i].Close())
	}
	if a.parent != nil {
		result = errors.Join(result, a.parent.Close())
	}
	return result
}
