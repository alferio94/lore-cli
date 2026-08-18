//go:build windows

package install

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const windowsAuthorityPoll = 100 * time.Millisecond

type windowsStorePlatform struct{ fail func(string) error }

type windowsStorePath struct {
	parent    windows.Handle
	rawParent string
	finalPath string
	leaf      string
	volume    uint32
	index     uint64
}

type windowsStoreAuthority struct {
	handle    windows.Handle
	parent    windows.Handle
	path      *windowsStorePath
	name      string
	abandoned bool
	released  bool
}

func newWindowsStorePlatform() windowsStorePlatform { return windowsStorePlatform{} }
func defaultStorePlatform() storePlatform           { return newWindowsStorePlatform() }

func (windowsStorePlatform) Canonical(raw string) (storePath, error) {
	if raw == "" || !filepath.IsAbs(raw) || strings.IndexByte(raw, 0) >= 0 {
		return storePath{}, errStoreAuthorityInvalidPath
	}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == '\\' || r == '/' }) {
		if part == ".." {
			return storePath{}, errStoreAuthorityInvalidPath
		}
	}
	clean := filepath.Clean(raw)
	leaf := filepath.Base(clean)
	if !safeWindowsLeaf(leaf) {
		return storePath{}, errStoreAuthorityInvalidPath
	}
	parent, err := openWindowsDirectory(filepath.Dir(clean))
	if err != nil {
		return storePath{}, errStoreAuthorityInvalidPath
	}
	info, final, err := windowsHandleIdentity(parent)
	if err != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || !windowsHandleIsCurrentUserOnlyFile(parent) {
		windows.CloseHandle(parent)
		return storePath{}, errStoreAuthorityInvalidPath
	}
	path := &windowsStorePath{parent: parent, rawParent: filepath.Dir(clean), finalPath: final, leaf: strings.ToUpper(leaf), volume: info.VolumeSerialNumber, index: uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)}
	if path.validateLeaf() != nil {
		windows.CloseHandle(parent)
		return storePath{}, errStoreAuthorityInvalidPath
	}
	identity := fmt.Sprintf("windows:%08x:%016x:%s", path.volume, path.index, path.leaf)
	return canonicalStorePath(identity, path)
}

func safeWindowsLeaf(leaf string) bool {
	if leaf == "." || leaf == `\` || len(leaf) > 240 || strings.HasSuffix(leaf, ".") || strings.HasSuffix(leaf, " ") || strings.ContainsAny(leaf, `<>:"|?*`) {
		return false
	}
	base := strings.ToUpper(strings.SplitN(leaf, ".", 2)[0])
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" {
		return false
	}
	return !(len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9')
}

func openWindowsDirectory(path string) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES|windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
}

func windowsHandleIdentity(handle windows.Handle) (windows.ByHandleFileInformation, string, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return info, "", err
	}
	size, err := windows.GetFinalPathNameByHandle(handle, nil, 0, 0)
	if err != nil || size == 0 {
		return info, "", err
	}
	buf := make([]uint16, size)
	n, err := windows.GetFinalPathNameByHandle(handle, &buf[0], uint32(len(buf)), 0)
	if err != nil || n == 0 || n >= uint32(len(buf)) {
		return info, "", errStoreAuthorityInvalidPath
	}
	return info, strings.ToUpper(windows.UTF16ToString(buf[:n])), nil
}

func (p *windowsStorePath) revalidate() error {
	other, err := openWindowsDirectory(p.rawParent)
	if err != nil {
		return errStoreAuthorityInvalidPath
	}
	defer windows.CloseHandle(other)
	info, final, err := windowsHandleIdentity(other)
	if err != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.VolumeSerialNumber != p.volume || uint64(info.FileIndexHigh)<<32|uint64(info.FileIndexLow) != p.index || !strings.EqualFold(final, p.finalPath) || !windowsHandleIsCurrentUserOnlyFile(other) {
		return errStoreAuthorityInvalidPath
	}
	return p.validateLeaf()
}

func (p *windowsStorePath) validateLeaf() error {
	path := filepath.Join(p.finalPath, p.leaf)
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return errStoreAuthorityInvalidPath
	}
	handle, err := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES|windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return errStoreAuthorityInvalidPath
	}
	defer windows.CloseHandle(handle)
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(handle, &info) != nil || info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 || info.NumberOfLinks != 1 || !windowsHandleIsCurrentUserOnlyFile(handle) {
		return errStoreAuthorityInvalidPath
	}
	return nil
}

func (windowsStorePlatform) Acquire(path storePath, budget time.Duration, waiter monotonicWaiter) (authority, error) {
	p, ok := path.platform.(*windowsStorePath)
	if !ok || p == nil || p.parent == 0 || waiter == nil || (budget != 0 && budget != defaultProfileStoreWait) || p.revalidate() != nil {
		if ok && p != nil && p.parent != 0 {
			windows.CloseHandle(p.parent)
		}
		return nil, errStoreAuthorityInvalidPath
	}
	name := `Global\LoreProfileState-` + hex.EncodeToString(path.identity[:])
	namePtr, _ := windows.UTF16PtrFromString(name)
	_, attrs, err := windowsCurrentUserSecurity()
	if err != nil {
		windows.CloseHandle(p.parent)
		return nil, err
	}
	handle, err := windows.CreateMutex(attrs, false, namePtr)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		windows.CloseHandle(p.parent)
		return nil, err
	}
	if handle == 0 || !windowsHandleIsCurrentUserOnly(handle) {
		windows.CloseHandle(handle)
		windows.CloseHandle(p.parent)
		return nil, errStoreAuthorityInvalidPath
	}
	deadline := waiter.Now().Add(budget)
	runtime.LockOSThread()
	for {
		result, waitErr := windows.WaitForSingleObject(handle, 0)
		if waitErr != nil {
			windows.CloseHandle(handle)
			windows.CloseHandle(p.parent)
			runtime.UnlockOSThread()
			return nil, waitErr
		}
		if windowsWaitOwns(result) {
			if p.revalidate() == nil {
				return &windowsStoreAuthority{handle: handle, parent: p.parent, path: p, name: name, abandoned: result == uint32(windows.WAIT_ABANDONED)}, nil
			}
			windows.ReleaseMutex(handle)
			windows.CloseHandle(handle)
			windows.CloseHandle(p.parent)
			runtime.UnlockOSThread()
			return nil, errStoreAuthorityInvalidPath
		}
		if result != uint32(windows.WAIT_TIMEOUT) {
			windows.CloseHandle(handle)
			windows.CloseHandle(p.parent)
			runtime.UnlockOSThread()
			return nil, errStoreAuthorityInvalidPath
		}
		if budget == 0 {
			windows.CloseHandle(handle)
			windows.CloseHandle(p.parent)
			runtime.UnlockOSThread()
			return nil, errStoreAuthorityBusy
		}
		now := waiter.Now()
		if !now.Before(deadline) {
			windows.CloseHandle(handle)
			windows.CloseHandle(p.parent)
			runtime.UnlockOSThread()
			return nil, errStoreAuthorityTimeout
		}
		sleep := deadline.Sub(now)
		if sleep > windowsAuthorityPoll {
			sleep = windowsAuthorityPoll
		}
		waiter.Sleep(sleep)
	}
}

func (windowsStorePlatform) Read(owned authority) ([]byte, bool, error) {
	a, ok := owned.(*windowsStoreAuthority)
	if !ok || a == nil || a.released || a.path.revalidate() != nil {
		return nil, false, errStoreAuthorityInvalidPath
	}
	raw, err := os.ReadFile(filepath.Join(a.path.finalPath, a.path.leaf))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil || a.path.revalidate() != nil {
		return nil, false, errStoreAuthorityInvalidPath
	}
	return raw, true, nil
}

func (p windowsStorePlatform) Commit(owned authority, prior, next []byte) error {
	a, ok := owned.(*windowsStoreAuthority)
	if !ok || a == nil || a.released || a.path.revalidate() != nil {
		return errStoreAuthorityInvalidPath
	}
	tmp, err := os.CreateTemp(a.path.finalPath, ".profiles-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err = windowsProtectFile(tmpPath); err == nil {
		err = p.inject("write")
	}
	if err == nil {
		_, err = tmp.Write(next)
	}
	if err == nil {
		err = p.inject("sync")
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = p.inject("replace")
	}
	if err == nil && a.path.revalidate() == nil {
		err = moveWindowsFile(tmpPath, filepath.Join(a.path.finalPath, a.path.leaf))
	}
	if err != nil {
		return err
	}
	if err = p.inject("durability"); err == nil {
		return nil
	}
	if restoreErr := p.restore(a, prior); restoreErr != nil {
		return errors.Join(errStoreRollbackFailed, restoreErr)
	}
	return err
}

func (p windowsStorePlatform) restore(a *windowsStoreAuthority, prior []byte) error {
	if err := p.inject("restore"); err != nil {
		return err
	}
	destination := filepath.Join(a.path.finalPath, a.path.leaf)
	if len(prior) == 0 {
		return os.Remove(destination)
	}
	tmp, err := os.CreateTemp(a.path.finalPath, ".profiles-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = windowsProtectFile(name); err == nil {
		_, err = tmp.Write(prior)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = moveWindowsFile(name, destination)
	}
	return err
}

func moveWindowsFile(from, to string) error {
	source, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	destination, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(source, destination, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func windowsProtectFile(path string) error {
	sd, _, err := windowsCurrentUserSecurity()
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

func (p windowsStorePlatform) inject(stage string) error {
	if p.fail != nil {
		return p.fail(stage)
	}
	return nil
}

func windowsWaitOwns(result uint32) bool {
	return result == uint32(windows.WAIT_OBJECT_0) || result == uint32(windows.WAIT_ABANDONED)
}

func windowsCurrentUserSecurity() (*windows.SECURITY_DESCRIPTOR, *windows.SecurityAttributes, error) {
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
	return sd, &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}, nil
}

func windowsHandleIsCurrentUserOnlyFile(handle windows.Handle) bool {
	return windowsHandleACLIsCurrentUserOnly(handle, windows.SE_FILE_OBJECT)
}
func windowsHandleIsCurrentUserOnly(handle windows.Handle) bool {
	return windowsHandleACLIsCurrentUserOnly(handle, windows.SE_KERNEL_OBJECT)
}
func windowsHandleACLIsCurrentUserOnly(handle windows.Handle, objectType windows.SE_OBJECT_TYPE) bool {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return false
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	sd, sdErr := windows.GetSecurityInfo(handle, objectType, windows.DACL_SECURITY_INFORMATION)
	if err != nil || sdErr != nil {
		return false
	}
	control, _, err := sd.Control()
	dacl, _, daclErr := sd.DACL()
	if err != nil || daclErr != nil || control&windows.SE_DACL_PROTECTED == 0 || dacl == nil || dacl.AceCount != 1 {
		return false
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if windows.GetAce(dacl, 0, &ace) != nil || ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
		return false
	}
	return (*windows.SID)(unsafe.Pointer(&ace.SidStart)).Equals(user.User.Sid)
}

func (a *windowsStoreAuthority) Release() error {
	if a == nil || a.released {
		return nil
	}
	a.released = true
	err := errors.Join(windows.ReleaseMutex(a.handle), windows.CloseHandle(a.handle), windows.CloseHandle(a.parent))
	runtime.UnlockOSThread()
	return err
}
