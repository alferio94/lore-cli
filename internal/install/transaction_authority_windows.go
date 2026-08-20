//go:build windows

package install

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	windowsTargetRootIdentityDomain = "lore-target-root-windows-v1\x00"
	windowsTargetMutexPrefix        = `Global\LoreTargetTransaction-`
	windowsVolumeNameGUID           = 0x1
)

type windowsTargetFileIDInfo struct {
	VolumeSerialNumber uint64
	FileID             [16]byte
}

type windowsTargetRootIdentity struct {
	rawPath   string
	finalPath string
	volume    uint64
	fileID    [16]byte
}

type windowsTargetGuard struct {
	root      targetRootIdentity
	abandoned bool
	release   chan struct{}
	done      chan error
	once      sync.Once
	err       error
}

func init() {
	canonicalTargetRoot = canonicalWindowsTargetRoot
	acquireTargetOSGuard = acquireWindowsTargetGuard
}

func canonicalWindowsTargetRoot(raw string) (targetRootIdentity, error) {
	if raw == "" || strings.IndexByte(raw, 0) >= 0 || !filepath.IsAbs(raw) || targetPathTraverses(raw) {
		return targetRootIdentity{}, errTransactionFSUnsafePath
	}
	clean := filepath.Clean(raw)
	handle, err := openWindowsTargetRoot(clean)
	if err != nil {
		return targetRootIdentity{}, classifyWindowsTargetRootOpen(err)
	}
	failed := true
	defer func() {
		if failed {
			_ = windows.CloseHandle(handle)
		}
	}()

	platform, info, err := windowsTargetRootIdentityForHandle(handle, clean)
	if err != nil || !safeWindowsTargetRoot(handle, info) {
		return targetRootIdentity{}, errTransactionFSUnsafePath
	}
	root := os.NewFile(uintptr(handle), "selected-target-root")
	if root == nil {
		return targetRootIdentity{}, errTransactionFSIO
	}
	pinned, err := root.Stat()
	if err != nil || !pinned.IsDir() {
		_ = root.Close()
		failed = false
		return targetRootIdentity{}, errTransactionFSUnsafePath
	}

	identityBytes := make([]byte, len(windowsTargetRootIdentityDomain)+8+len(platform.fileID)+len(platform.finalPath))
	copy(identityBytes, windowsTargetRootIdentityDomain)
	offset := len(windowsTargetRootIdentityDomain)
	binary.LittleEndian.PutUint64(identityBytes[offset:], platform.volume)
	offset += 8
	copy(identityBytes[offset:], platform.fileID[:])
	offset += len(platform.fileID)
	copy(identityBytes[offset:], platform.finalPath)

	identity := targetRootIdentity{
		digest:   sha256.Sum256(identityBytes),
		file:     root,
		info:     pinned,
		platform: platform,
	}
	if !windowsTargetRootStillPinned(identity, platform) {
		_ = root.Close()
		failed = false
		return targetRootIdentity{}, errTransactionFSUnsafePath
	}
	failed = false
	return identity, nil
}

func openWindowsTargetRoot(path string) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(
		name,
		windows.FILE_READ_ATTRIBUTES|windows.READ_CONTROL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
}

func classifyWindowsTargetRootOpen(err error) error {
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) ||
		errors.Is(err, windows.ERROR_PATH_NOT_FOUND) ||
		errors.Is(err, windows.ERROR_INVALID_NAME) ||
		errors.Is(err, windows.ERROR_DIRECTORY) ||
		errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windows.ERROR_CANT_ACCESS_FILE) ||
		errors.Is(err, windows.ERROR_REPARSE_TAG_INVALID) ||
		errors.Is(err, windows.ERROR_STOPPED_ON_SYMLINK) {
		return errTransactionFSUnsafePath
	}
	return errTransactionFSIO
}

func windowsTargetRootIdentityForHandle(handle windows.Handle, rawPath string) (windowsTargetRootIdentity, windows.ByHandleFileInformation, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return windowsTargetRootIdentity{}, info, err
	}
	var fileID windowsTargetFileIDInfo
	if err := windows.GetFileInformationByHandleEx(
		handle,
		windows.FileIdInfo,
		(*byte)(unsafe.Pointer(&fileID)),
		uint32(unsafe.Sizeof(fileID)),
	); err != nil || fileID.FileID == [16]byte{} {
		return windowsTargetRootIdentity{}, info, errTransactionFSUnsafePath
	}
	finalPath, err := windowsTargetFinalPath(handle)
	if err != nil {
		return windowsTargetRootIdentity{}, info, err
	}
	return windowsTargetRootIdentity{
		rawPath:   rawPath,
		finalPath: finalPath,
		volume:    fileID.VolumeSerialNumber,
		fileID:    fileID.FileID,
	}, info, nil
}

func windowsTargetFinalPath(handle windows.Handle) (string, error) {
	size, err := windows.GetFinalPathNameByHandle(handle, nil, 0, windowsVolumeNameGUID)
	if err != nil || size == 0 {
		return "", errTransactionFSUnsafePath
	}
	for attempts := 0; attempts < 2; attempts++ {
		buf := make([]uint16, size+1)
		n, err := windows.GetFinalPathNameByHandle(handle, &buf[0], uint32(len(buf)), windowsVolumeNameGUID)
		if err != nil || n == 0 {
			return "", errTransactionFSUnsafePath
		}
		if n < uint32(len(buf)) {
			path := windows.UTF16ToString(buf[:n])
			path = filepath.Clean(strings.ReplaceAll(path, "/", `\`))
			path = strings.ToUpper(path)
			if path == "" || !filepath.IsAbs(path) {
				return "", errTransactionFSUnsafePath
			}
			return path, nil
		}
		size = n
	}
	return "", errTransactionFSUnsafePath
}

func safeWindowsTargetRoot(handle windows.Handle, info windows.ByHandleFileInformation) bool {
	return info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 &&
		info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 &&
		windowsTargetHandleIsCurrentUserOnly(handle, windows.SE_FILE_OBJECT)
}

func windowsTargetRootStillPinned(root targetRootIdentity, want windowsTargetRootIdentity) bool {
	if !root.valid() || root.file == nil {
		return false
	}
	pinnedHandle := windows.Handle(root.file.Fd())
	pinned, info, err := windowsTargetRootIdentityForHandle(pinnedHandle, want.rawPath)
	if err != nil || !safeWindowsTargetRoot(pinnedHandle, info) || !sameWindowsTargetRoot(pinned, want) {
		return false
	}
	currentHandle, err := openWindowsTargetRoot(want.rawPath)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(currentHandle)
	current, currentInfo, err := windowsTargetRootIdentityForHandle(currentHandle, want.rawPath)
	return err == nil && safeWindowsTargetRoot(currentHandle, currentInfo) && sameWindowsTargetRoot(current, want)
}

func sameWindowsTargetRoot(a, b windowsTargetRootIdentity) bool {
	return a.volume == b.volume && a.fileID == b.fileID && a.finalPath == b.finalPath
}

func acquireWindowsTargetGuard(root targetRootIdentity, deadline targetAuthorityDeadline, clock targetMonotonicWaiter) (targetGuard, error) {
	platform, ok := root.platform.(windowsTargetRootIdentity)
	if !ok || !root.valid() || clock == nil || !windowsTargetRootStillPinned(root, platform) {
		return nil, errTransactionFSUnsafePath
	}

	name := windowsTargetMutexPrefix + hex.EncodeToString(root.digest[:])
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, errTransactionFSIO
	}
	securityDescriptor, attrs, err := windowsTargetCurrentUserSecurity()
	if err != nil {
		return nil, errTransactionFSIO
	}
	handle, createErr := windows.CreateMutex(attrs, false, namePtr)
	runtime.KeepAlive(securityDescriptor)
	if createErr != nil && !errors.Is(createErr, windows.ERROR_ALREADY_EXISTS) {
		if handle != 0 {
			_ = windows.CloseHandle(handle)
		}
		return nil, errTransactionFSIO
	}
	if handle == 0 || !windowsTargetHandleIsCurrentUserOnly(handle, windows.SE_KERNEL_OBJECT) {
		if handle != 0 {
			_ = windows.CloseHandle(handle)
		}
		return nil, errTransactionFSUnsafePath
	}

	guard := &windowsTargetGuard{
		root:    root,
		release: make(chan struct{}),
		done:    make(chan error, 1),
	}
	acquired := make(chan error, 1)
	go guard.ownMutex(handle, platform, deadline, clock, acquired)
	if err := <-acquired; err != nil {
		return nil, err
	}
	return guard, nil
}

// A Windows mutex is owned by the thread that successfully waits on it. Keep
// one private locked OS thread for the complete guard lifecycle so Commit or
// Rollback may release the guard from any goroutine without transferring mutex
// ownership to a different thread.
func (g *windowsTargetGuard) ownMutex(handle windows.Handle, platform windowsTargetRootIdentity, deadline targetAuthorityDeadline, clock targetMonotonicWaiter, acquired chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for {
		result, waitErr := windows.WaitForSingleObject(handle, 0)
		if waitErr != nil {
			acquired <- closeWindowsTargetMutex(handle, errTransactionFSIO)
			return
		}
		switch result {
		case uint32(windows.WAIT_OBJECT_0), uint32(windows.WAIT_ABANDONED):
			if !windowsTargetRootStillPinned(g.root, platform) {
				_ = windows.ReleaseMutex(handle)
				acquired <- closeWindowsTargetMutex(handle, errTransactionFSUnsafePath)
				return
			}
			g.abandoned = result == uint32(windows.WAIT_ABANDONED)
			acquired <- nil
			<-g.release
			releaseErr := windows.ReleaseMutex(handle)
			closeErr := windows.CloseHandle(handle)
			if releaseErr != nil || closeErr != nil {
				g.done <- errTargetAuthorityRelease
				return
			}
			g.done <- nil
			return
		case uint32(windows.WAIT_TIMEOUT):
			if deadline.zero {
				acquired <- closeWindowsTargetMutex(handle, targetAuthorityErrorFor(CodeTargetAuthorityBusy))
				return
			}
			if deadline.expired(clock) {
				acquired <- closeWindowsTargetMutex(handle, targetAuthorityErrorFor(CodeTargetAuthorityTimeout))
				return
			}
			deadline.sleep(clock)
		default:
			acquired <- closeWindowsTargetMutex(handle, errTransactionFSIO)
			return
		}
	}
}

func closeWindowsTargetMutex(handle windows.Handle, primary error) error {
	if closeErr := windows.CloseHandle(handle); closeErr != nil {
		return errors.Join(primary, errTargetAuthorityRelease)
	}
	return primary
}

func windowsTargetCurrentUserSecurity() (*windows.SECURITY_DESCRIPTOR, *windows.SecurityAttributes, error) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return nil, nil, err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, nil, err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return nil, nil, err
	}
	attrs := &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: sd,
	}
	return sd, attrs, nil
}

func windowsTargetHandleIsCurrentUserOnly(handle windows.Handle, objectType windows.SE_OBJECT_TYPE) bool {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return false
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return false
	}
	sd, err := windows.GetSecurityInfo(handle, objectType, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return false
	}
	dacl, present, err := sd.DACL()
	if err != nil || !present || dacl == nil || dacl.AceCount != 1 {
		return false
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if windows.GetAce(dacl, 0, &ace) != nil || ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
		return false
	}
	return (*windows.SID)(unsafe.Pointer(&ace.SidStart)).Equals(user.User.Sid)
}

// WAIT_ABANDONED is retained as OS proof that the previous mutex owner died.
// Recover deliberately performs no journal I/O in task 3.7: task 3.8 must
// inspect and restore the durable orphan journal on every acquisition while
// this mutex remains owned, using abandoned only as additional owner-death
// evidence rather than as a substitute for durable journal state.
func (g *windowsTargetGuard) Recover() error {
	if g == nil || g.release == nil || g.done == nil {
		return errTransactionFSUnsafePath
	}
	return nil
}

func (g *windowsTargetGuard) Release() error {
	if g == nil {
		return nil
	}
	g.once.Do(func() {
		if g.release == nil || g.done == nil {
			g.err = errTargetAuthorityRelease
			return
		}
		close(g.release)
		g.err = <-g.done
	})
	return g.err
}
