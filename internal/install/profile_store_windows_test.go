//go:build windows

package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsProfileStoreLoadAcceptsCurrentUserOnlyState(t *testing.T) {
	dir := windowsPrivateDir(t)
	store := NewProfileStore(filepath.Join(dir, "profiles.json"))
	root := filepath.Join(t.TempDir(), "project")
	prepared, err := store.PrepareProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Complete(prepared, PersistenceFact{}, ApplyBoundarySuccess); err != nil {
		t.Fatal(err)
	}
	assertStorePermissions(t, dir, store.Path())
	// Windows FileMode bits do not represent this verified DACL.
	if _, err := store.LookupProject(root); err != nil {
		t.Fatalf("LookupProject() rejected current-user-only state: %v", err)
	}
}

func TestWindowsProfileStoreCompleteHardensStoreDirectory(t *testing.T) {
	dir := windowsPrivateDir(t)
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	store := NewProfileStore(filepath.Join(dir, "profiles.json"))
	prepared, err := store.PrepareProject(filepath.Join(t.TempDir(), "project"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Complete(prepared, PersistenceFact{}, ApplyBoundarySuccess); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	handle, err := openWindowsDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if !windowsHandleIsCurrentUserOnlyFile(handle) {
		t.Fatal("store directory DACL is not current-user-only")
	}
}

func TestWindowsProfileStoreCanonicalIdentityUsesVolumeParentAndFoldedLeaf(t *testing.T) {
	parent := windowsPrivateDir(t)
	platform := newWindowsStorePlatform()
	first, err := platform.Canonical(filepath.Join(parent, "Profiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := platform.Canonical(filepath.Join(parent, ".", "PROFILES.JSON"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeWindowsPath(t, first)
	defer closeWindowsPath(t, second)
	if first.identity != second.identity {
		t.Fatal("case aliases produced different authority identities")
	}
}

func TestWindowsProfileStoreCanonicalRejectsTraversalLinksAndBroadACL(t *testing.T) {
	parent := windowsPrivateDir(t)
	state := filepath.Join(parent, "profiles.json")
	if err := os.WriteFile(state, []byte("state"), 0o600); err != nil {
		t.Fatal(err)
	}
	windowsSecurePath(t, state)
	alias := filepath.Join(parent, "alias.json")
	if err := os.Link(state, alias); err != nil {
		t.Fatal(err)
	}
	cases := []string{
		"relative\\profiles.json",
		parent + `\child\..\profiles.json`,
		filepath.Join(parent, "profiles.json."),
		filepath.Join(parent, "profiles.json:stream"),
		filepath.Join(parent, "CON.json"),
		alias,
	}
	for _, path := range cases {
		if got, err := newWindowsStorePlatform().Canonical(path); err == nil {
			closeWindowsPath(t, got)
			t.Fatalf("Canonical(%q) accepted unsafe path", path)
		} else if !errors.Is(err, errStoreAuthorityInvalidPath) || strings.Contains(err.Error(), path) {
			t.Fatalf("Canonical() error = %v", err)
		}
	}

	broad := windowsPrivateDir(t)
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil || windows.SetNamedSecurityInfo(broad, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil) != nil {
		t.Fatal("could not install broad test ACL")
	}
	if got, err := newWindowsStorePlatform().Canonical(filepath.Join(broad, "profiles.json")); err == nil {
		closeWindowsPath(t, got)
		t.Fatal("Canonical accepted a world-accessible parent")
	}
}

func TestWindowsProfileStoreRejectsReparseLeafAndParentSwap(t *testing.T) {
	parent := windowsPrivateDir(t)
	target := filepath.Join(parent, "target")
	if err := os.WriteFile(target, []byte("state"), 0o600); err != nil {
		t.Fatal(err)
	}
	windowsSecurePath(t, target)
	state := filepath.Join(parent, "profiles.json")
	platform := newWindowsStorePlatform()
	canonical, err := platform.Canonical(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, state); err != nil {
		closeWindowsPath(t, canonical)
		t.Skipf("symlink privilege unavailable: %v", err)
	}
	if _, err := platform.Acquire(canonical, 0, &windowsRecordingWaiter{}); !errors.Is(err, errStoreAuthorityInvalidPath) {
		t.Fatalf("Acquire after reparse swap = %v, want invalid path", err)
	}
}

func TestWindowsProfileStoreMutexIsExclusiveBoundedAndReusable(t *testing.T) {
	path := filepath.Join(windowsPrivateDir(t), "profiles.json")
	platform := newWindowsStorePlatform()
	firstPath, _ := platform.Canonical(path)
	first, err := platform.Acquire(firstPath, 0, &windowsRecordingWaiter{})
	if err != nil {
		t.Fatal(err)
	}
	busyPath, _ := platform.Canonical(path)
	busyResult := make(chan error, 1)
	go func() {
		_, err := platform.Acquire(busyPath, 0, &windowsRecordingWaiter{})
		busyResult <- err
	}()
	if err := <-busyResult; !errors.Is(err, errStoreAuthorityBusy) {
		t.Fatalf("zero-wait Acquire = %v, want busy", err)
	}
	timedPath, _ := platform.Canonical(path)
	waiter := &windowsRecordingWaiter{now: time.Unix(100, 0)}
	timeoutResult := make(chan error, 1)
	go func() {
		_, err := platform.Acquire(timedPath, defaultProfileStoreWait, waiter)
		timeoutResult <- err
	}()
	if err := <-timeoutResult; !errors.Is(err, errStoreAuthorityTimeout) {
		t.Fatalf("bounded Acquire = %v, want timeout", err)
	}
	if waiter.now.Sub(time.Unix(100, 0)) != defaultProfileStoreWait {
		t.Fatalf("elapsed = %s", waiter.now.Sub(time.Unix(100, 0)))
	}
	for _, sleep := range waiter.sleeps {
		if sleep <= 0 || sleep > 100*time.Millisecond {
			t.Fatalf("poll = %s", sleep)
		}
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	retryPath, _ := platform.Canonical(path)
	retry, err := platform.Acquire(retryPath, 0, &windowsRecordingWaiter{})
	if err != nil {
		t.Fatal(err)
	}
	if err := retry.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsProfileStoreOwnerThreadExitYieldsAbandonedOSProof(t *testing.T) {
	path := filepath.Join(windowsPrivateDir(t), "profiles.json")
	platform := newWindowsStorePlatform()
	ownerResult := make(chan *windowsStoreAuthority, 1)
	ownerDone := make(chan struct{})
	go func() {
		canonical, _ := platform.Canonical(path)
		owned, err := platform.Acquire(canonical, 0, &windowsRecordingWaiter{})
		if err != nil {
			ownerResult <- nil
			close(ownerDone)
			return
		}
		ownerResult <- owned.(*windowsStoreAuthority)
		close(ownerDone)
		// Returning while LockOSThread is active terminates this OS thread;
		// Windows then marks its owned mutex abandoned.
	}()
	deadOwner := <-ownerResult
	<-ownerDone
	if deadOwner == nil {
		t.Fatal("owner acquisition failed")
	}
	canonical, _ := platform.Canonical(path)
	contender, err := platform.Acquire(canonical, defaultProfileStoreWait, wallClockWindowsWaiter{})
	if err != nil {
		t.Fatalf("Acquire after owner-thread exit = %v", err)
	}
	if !contender.(*windowsStoreAuthority).abandoned {
		t.Fatal("owner-thread exit was not reported as abandoned OS proof")
	}
	windows.CloseHandle(deadOwner.handle)
	windows.CloseHandle(deadOwner.parent)
	if err := contender.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsProfileStoreMutexDACLAndAbandonedProofAreRestrictive(t *testing.T) {
	path := filepath.Join(windowsPrivateDir(t), "Bearer-secret-profiles.json")
	platform := newWindowsStorePlatform()
	canonical, _ := platform.Canonical(path)
	owned, err := platform.Acquire(canonical, 0, &windowsRecordingWaiter{})
	if err != nil {
		t.Fatal(err)
	}
	winOwned := owned.(*windowsStoreAuthority)
	if !windowsHandleIsCurrentUserOnly(winOwned.handle) {
		t.Fatal("mutex DACL is not current-user-only")
	}
	if strings.Contains(winOwned.name, path) || strings.Contains(winOwned.name, "Bearer-secret") {
		t.Fatalf("mutex name disclosed path: %q", winOwned.name)
	}
	if !windowsWaitOwns(uint32(windows.WAIT_OBJECT_0)) || !windowsWaitOwns(uint32(windows.WAIT_ABANDONED)) || windowsWaitOwns(uint32(windows.WAIT_TIMEOUT)) {
		t.Fatal("wait result did not treat normal/abandoned ownership as OS proof")
	}
	if err := owned.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsProfileStoreCommitPublishesProtectedFile(t *testing.T) {
	path := filepath.Join(windowsPrivateDir(t), "profiles.json")
	platform := newWindowsStorePlatform()
	canonical, _ := platform.Canonical(path)
	owned, err := platform.Acquire(canonical, 0, &windowsRecordingWaiter{})
	if err != nil {
		t.Fatal(err)
	}
	if err := platform.Commit(owned, nil, []byte("next-state")); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "next-state" {
		t.Fatalf("state = %q, %v", got, err)
	}
	handle, err := windows.Open(path, windows.O_RDONLY, 0)
	if err != nil || !windowsHandleIsCurrentUserOnlyFile(handle) {
		t.Fatal("committed state ACL is not current-user-only")
	}
	windows.CloseHandle(handle)
	_ = owned.Release()
}

func TestWindowsProfileStoreCommitFailureRestoresPriorBytes(t *testing.T) {
	path := filepath.Join(windowsPrivateDir(t), "profiles.json")
	prior := []byte("prior-state")
	if err := os.WriteFile(path, prior, 0o600); err != nil {
		t.Fatal(err)
	}
	windowsSecurePath(t, path)
	platform := windowsStorePlatform{fail: func(stage string) error {
		if stage == "durability" {
			return errors.New("injected durability failure")
		}
		return nil
	}}
	canonical, _ := platform.Canonical(path)
	owned, err := platform.Acquire(canonical, 0, &windowsRecordingWaiter{})
	if err != nil {
		t.Fatal(err)
	}
	if err := platform.Commit(owned, prior, []byte("next-state")); err == nil {
		t.Fatal("Commit unexpectedly succeeded")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(prior) {
		t.Fatalf("restored state = %q, %v", got, err)
	}
	if matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".profiles-*.tmp")); len(matches) != 0 {
		t.Fatalf("temporary residue = %v", matches)
	}
	_ = owned.Release()
}

type wallClockWindowsWaiter struct{}

func (wallClockWindowsWaiter) Now() time.Time        { return time.Now() }
func (wallClockWindowsWaiter) Sleep(d time.Duration) { time.Sleep(d) }

type windowsRecordingWaiter struct {
	now    time.Time
	sleeps []time.Duration
}

func (w *windowsRecordingWaiter) Now() time.Time { return w.now }
func (w *windowsRecordingWaiter) Sleep(d time.Duration) {
	w.sleeps = append(w.sleeps, d)
	w.now = w.now.Add(d)
}

func assertStorePermissions(t *testing.T, dir, path string) {
	t.Helper()
	dirHandle, err := openWindowsDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(dirHandle)
	fileHandle, err := windows.Open(path, windows.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(fileHandle)
	if !windowsHandleIsCurrentUserOnlyFile(dirHandle) || !windowsHandleIsCurrentUserOnlyFile(fileHandle) {
		t.Fatal("store directory or file DACL is not current-user-only")
	}
}
func makeStoreFileInsecure(t *testing.T, path string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, _ := sd.DACL()
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
}

func windowsPrivateDir(t *testing.T) string {
	t.Helper()
	path := t.TempDir()
	windowsSecurePath(t, path)
	return path
}

func windowsSecurePath(t *testing.T, path string) {
	t.Helper()
	sd, _, err := windowsCurrentUserSecurity()
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil || windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil) != nil {
		t.Fatal("could not install private ACL")
	}
}

func closeWindowsPath(t *testing.T, path storePath) {
	t.Helper()
	winPath, ok := path.platform.(*windowsStorePath)
	if !ok {
		t.Fatalf("platform path type = %T", path.platform)
	}
	if err := windows.CloseHandle(winPath.parent); err != nil {
		t.Fatal(err)
	}
}
