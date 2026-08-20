//go:build windows

package install

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	windowsAuthorityHelperAction = "TEST_WINDOWS_TARGET_AUTHORITY_ACTION"
	windowsAuthorityHelperRoot   = "TEST_WINDOWS_TARGET_AUTHORITY_ROOT"
)

func TestW33C1AWindowsRootIdentityAliasesAndReparseSafety(t *testing.T) {
	root := newWindowsTargetAuthorityRoot(t)
	aliasParent := filepath.Join(t.TempDir(), "parent-junction")
	createWindowsTargetJunction(t, aliasParent, filepath.Dir(root))
	ancestorAlias := filepath.Join(aliasParent, filepath.Base(root))
	volumeAlias := windowsTargetVolumeAlias(t, root)

	paths := []struct{ name, path string }{
		{"direct", root},
		{"case", strings.ToUpper(root)},
		{"ancestor reparse", ancestorAlias},
		{"volume GUID alias", volumeAlias},
	}
	identities := make(map[string]targetRootIdentity, len(paths))
	for _, candidate := range paths {
		identity, err := canonicalWindowsTargetRoot(candidate.path)
		if err != nil {
			t.Fatalf("canonical %s alias: %v", candidate.name, err)
		}
		identities[candidate.name] = identity
		defer closeTargetTestIdentity(t, identity)
	}

	want := identities["direct"]
	wantPlatform, ok := want.platform.(windowsTargetRootIdentity)
	if !ok {
		t.Fatalf("platform identity = %T", want.platform)
	}
	if wantPlatform.volume == 0 || wantPlatform.fileID == [16]byte{} {
		t.Fatalf("unstable Windows identity = %#v", wantPlatform)
	}
	if wantPlatform.finalPath != strings.ToUpper(filepath.Clean(wantPlatform.finalPath)) ||
		!strings.HasPrefix(wantPlatform.finalPath, `\\?\VOLUME{`) {
		t.Fatalf("final handle path is not normalized volume-GUID form: %q", wantPlatform.finalPath)
	}
	for _, candidate := range paths {
		identity := identities[candidate.name]
		platform := identity.platform.(windowsTargetRootIdentity)
		if identity.digest != want.digest || platform.volume != wantPlatform.volume ||
			platform.fileID != wantPlatform.fileID || platform.finalPath != wantPlatform.finalPath {
			t.Fatalf("%s alias did not converge: digest=%x platform=%#v; want digest=%x platform=%#v", candidate.name, identity.digest, platform, want.digest, wantPlatform)
		}
	}

	reparseLeaf := filepath.Join(t.TempDir(), "target-junction")
	createWindowsTargetJunction(t, reparseLeaf, root)
	if unsafeRoot, err := canonicalWindowsTargetRoot(reparseLeaf); !errors.Is(err, errTransactionFSUnsafePath) {
		closeTargetTestIdentity(t, unsafeRoot)
		t.Fatalf("direct reparse root error = %v, want unsafe path", err)
	}
	traversal := root + `\..\` + filepath.Base(root)
	if unsafeRoot, err := canonicalWindowsTargetRoot(traversal); !errors.Is(err, errTransactionFSUnsafePath) {
		closeTargetTestIdentity(t, unsafeRoot)
		t.Fatalf("traversal root error = %v, want unsafe path", err)
	}
}

func TestW33C1AWindowsCrossProcessBusyTimeoutAndRedaction(t *testing.T) {
	root := newWindowsTargetAuthorityRoot(t)
	aliasParent := filepath.Join(t.TempDir(), "holder-parent-junction")
	createWindowsTargetJunction(t, aliasParent, filepath.Dir(root))
	holder := startWindowsTargetAuthorityHolder(t, filepath.Join(aliasParent, filepath.Base(root)))
	defer holder.release(t)

	busyClock := newTargetAuthorityTestWaiter(time.Date(2050, 1, 2, 3, 4, 5, 0, time.UTC))
	_, err := acquireTargetGuardWithWaiter(windowsTargetVolumeAlias(t, root), 0, busyClock)
	assertTargetAuthorityTestError(t, err, CodeTargetAuthorityBusy, root, holder.cmd.String(), "owner-pid=4321", "journal payload", "bearer-secret")
	if sleeps := busyClock.Sleeps(); len(sleeps) != 0 {
		t.Fatalf("zero-wait contender slept: %v", sleeps)
	}

	start := time.Date(2050, 1, 2, 3, 4, 5, 0, time.UTC)
	timeoutClock := newTargetAuthorityTestWaiter(start)
	_, err = acquireTargetGuardWithWaiter(strings.ToUpper(root), defaultTargetAuthorityWait, timeoutClock)
	assertTargetAuthorityTestError(t, err, CodeTargetAuthorityTimeout, root, holder.cmd.String(), "owner-pid=4321", "journal payload", "bearer-secret")
	var total time.Duration
	for i, sleep := range timeoutClock.Sleeps() {
		if sleep <= 0 || sleep > targetAuthorityPoll || sleep > 100*time.Millisecond {
			t.Fatalf("sleep[%d] = %s", i, sleep)
		}
		total += sleep
	}
	if total != defaultTargetAuthorityWait || !timeoutClock.Now().Equal(start.Add(defaultTargetAuthorityWait)) {
		t.Fatalf("bounded monotonic wait = %s at %s", total, timeoutClock.Now())
	}
}

func TestW33C1AWindowsMutexSecurityDescriptorAndCleanup(t *testing.T) {
	root := newWindowsTargetAuthorityRoot(t)
	identity, err := canonicalWindowsTargetRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	name := windowsTargetMutexPrefix + fmt.Sprintf("%x", identity.digest)
	closeTargetTestIdentity(t, identity)
	if strings.Contains(strings.ToUpper(name), strings.ToUpper(root)) || len(strings.TrimPrefix(name, windowsTargetMutexPrefix)) != 64 {
		t.Fatalf("mutex name is not digest-only: %q", name)
	}

	sd, attrs, err := windowsTargetCurrentUserSecurity()
	if err != nil {
		t.Fatal(err)
	}
	if attrs == nil || attrs.SecurityDescriptor != sd || attrs.Length != uint32(unsafe.Sizeof(windows.SecurityAttributes{})) {
		t.Fatalf("security attributes = %#v", attrs)
	}
	assertWindowsTargetCurrentUserOnlyDescriptor(t, sd)

	for attempt := 0; attempt < 3; attempt++ {
		guard, err := acquireTargetGuard(root, 0)
		if err != nil {
			t.Fatalf("acquire attempt %d: %v", attempt, err)
		}
		combined, ok := guard.(*combinedTargetGuard)
		if !ok {
			t.Fatalf("guard type = %T", guard)
		}
		windowsGuard, ok := combined.os.(*windowsTargetGuard)
		if !ok || windowsGuard.abandoned {
			t.Fatalf("Windows guard = %#v", combined.os)
		}
		rootHandle := combined.local.root.file
		mutex := openWindowsTargetMutexForACL(t, name)
		if !windowsTargetHandleIsCurrentUserOnly(mutex, windows.SE_KERNEL_OBJECT) {
			_ = windows.CloseHandle(mutex)
			t.Fatal("named mutex ACL is not protected current-user-only")
		}
		mutexSD, err := windows.GetSecurityInfo(mutex, windows.SE_KERNEL_OBJECT, windows.DACL_SECURITY_INFORMATION)
		if closeErr := windows.CloseHandle(mutex); err != nil || closeErr != nil {
			t.Fatalf("read/close mutex security: %v / %v", err, closeErr)
		}
		assertWindowsTargetCurrentUserOnlyDescriptor(t, mutexSD)
		if err := guard.Release(); err != nil {
			t.Fatal(err)
		}
		if _, err := rootHandle.Stat(); err == nil {
			t.Fatal("pinned root handle remained usable after authority release")
		}
		if err := guard.Release(); err != nil {
			t.Fatalf("idempotent release: %v", err)
		}
		assertWindowsTargetMutexAbsent(t, name)
	}

	processTargetRegistry.mu.Lock()
	entries := len(processTargetRegistry.entries)
	processTargetRegistry.mu.Unlock()
	if entries != 0 {
		t.Fatalf("process-local authority residue = %d entries", entries)
	}
}

func TestW33C1AWindowsWaitAbandonedOwnerDeathProof(t *testing.T) {
	root := newWindowsTargetAuthorityRoot(t)
	holder := startWindowsTargetAuthorityHolder(t, root)

	start := time.Date(2055, 2, 3, 4, 5, 6, 0, time.UTC)
	clock := newTargetAuthorityStepWaiter(start)
	result := make(chan targetAuthorityTestResult, 1)
	go acquireTargetAuthorityForTest(1, windowsTargetVolumeAlias(t, root), clock, result)
	assertTargetAuthorityTestSleep(t, clock.slept)

	holder.kill(t)
	clock.advance <- struct{}{}
	acquired := receiveTargetAuthorityTestResult(t, result)
	if acquired.err != nil || acquired.guard == nil {
		t.Fatalf("acquire after owner death = %#v", acquired)
	}
	combined, ok := acquired.guard.(*combinedTargetGuard)
	if !ok {
		t.Fatalf("guard type = %T", acquired.guard)
	}
	windowsGuard, ok := combined.os.(*windowsTargetGuard)
	if !ok || !windowsGuard.abandoned {
		t.Fatalf("WAIT_ABANDONED proof missing: %#v", combined.os)
	}
	if err := acquired.guard.Recover(); err != nil {
		t.Fatalf("task-3.7 recovery seam: %v", err)
	}
	if err := acquired.guard.Release(); err != nil {
		t.Fatal(err)
	}

	// This proves only OS-owned mutex abandonment. Durable orphan-journal
	// discovery, restoration, durability, and residue cleanup belong to task 3.8.
}

func TestW33C1AWindowsMutexHeldThroughTransactionRelease(t *testing.T) {
	for _, finish := range []string{"commit", "rollback"} {
		t.Run(finish, func(t *testing.T) {
			root := newWindowsTargetAuthorityRoot(t)
			journal, err := applyTransactionFS(root, []transactionFSWrite{{Path: "state.txt", Data: []byte("next")}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if marker := runWindowsTargetAuthorityProbe(t, windowsTargetVolumeAlias(t, root)); marker != "busy:selected_target.authority:selected target is busy" {
				t.Fatalf("contender while transaction active = %q", marker)
			}
			if finish == "commit" {
				err = journal.Commit()
			} else {
				err = journal.Rollback()
			}
			if err != nil {
				t.Fatal(err)
			}
			if marker := runWindowsTargetAuthorityProbe(t, strings.ToUpper(root)); marker != "acquired" {
				t.Fatalf("contender after %s = %q", finish, marker)
			}
		})
	}
}

func TestW33C1AWindowsUnrelatedRootsRemainIndependent(t *testing.T) {
	firstRoot := newWindowsTargetAuthorityRoot(t)
	secondRoot := newWindowsTargetAuthorityRoot(t)
	holder := startWindowsTargetAuthorityHolder(t, firstRoot)
	defer holder.release(t)

	guard, err := acquireTargetGuard(windowsTargetVolumeAlias(t, secondRoot), 0)
	if err != nil {
		t.Fatalf("unrelated root was serialized: %v", err)
	}
	if err := guard.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestW33C1AWindowsAuthoritySurfaceAndCoupling(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate Windows authority test source")
	}
	path := filepath.Join(filepath.Dir(testFile), "transaction_authority_windows.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if !strings.HasPrefix(text, "//go:build windows\n") {
		t.Fatal("Windows production build tag drifted")
	}
	if !strings.Contains(text, "windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT") {
		t.Fatal("Windows root open does not retain backup-semantics plus no-follow flags")
	}
	if !strings.Contains(text, "windows.WAIT_ABANDONED") || !strings.Contains(text, "runtime.LockOSThread()") {
		t.Fatal("Windows owner-death or thread-affine mutex lifecycle drifted")
	}

	parsed, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"crypto/sha256": true, "encoding/binary": true, "encoding/hex": true, "errors": true,
		"os": true, "path/filepath": true, "runtime": true, "strings": true, "sync": true,
		"unsafe": true, "golang.org/x/sys/windows": true,
	}
	for _, spec := range parsed.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil || !allowed[importPath] {
			t.Fatalf("Windows authority has prohibited import %q", spec.Path.Value)
		}
	}
	ast.Inspect(parsed, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		lower := strings.ToLower(identifier.Name)
		for _, prohibited := range []string{"profile", "store", "lore", "server", "memory", "repository", "journal"} {
			if strings.Contains(lower, prohibited) {
				t.Errorf("Windows target authority couples through identifier %q", identifier.Name)
			}
		}
		return true
	})
}

func TestW33C1AWindowsHelperProcess(t *testing.T) {
	action := os.Getenv(windowsAuthorityHelperAction)
	if action == "" {
		return
	}
	root := os.Getenv(windowsAuthorityHelperRoot)
	guard, err := acquireTargetGuard(root, 0)
	if action == "probe" {
		if err != nil {
			typed, ok := err.(interface {
				Code() TransactionCode
				Path() string
			})
			if !ok {
				t.Fatalf("untyped probe error: %v", err)
			}
			fmt.Printf("busy:%s:%s\n", typed.Path(), err.Error())
			return
		}
		fmt.Println("acquired")
		if err := guard.Release(); err != nil {
			t.Fatal(err)
		}
		return
	}
	if action != "hold" || err != nil {
		t.Fatalf("helper action %q acquire: %v", action, err)
	}
	fmt.Println("ready")
	_, _ = io.Copy(io.Discard, os.Stdin)
	if err := guard.Release(); err != nil {
		t.Fatal(err)
	}
}

func newWindowsTargetAuthorityRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "target")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	protectWindowsTargetAuthorityPath(t, root)
	return root
}

func protectWindowsTargetAuthorityPath(t *testing.T, path string) {
	t.Helper()
	sd, _, err := windowsTargetCurrentUserSecurity()
	if err != nil {
		t.Fatal(err)
	}
	dacl, present, err := sd.DACL()
	if err != nil || !present || dacl == nil {
		t.Fatalf("current-user DACL: present=%v err=%v", present, err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatalf("protect target root: %v", err)
	}
}

func createWindowsTargetJunction(t *testing.T, link, target string) {
	t.Helper()
	output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Fatalf("create required non-privileged junction fixture: %v: %s", err, output)
	}
}

func windowsTargetVolumeAlias(t *testing.T, path string) string {
	t.Helper()
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	if len(volume) != 2 || volume[1] != ':' {
		t.Fatalf("test root is not on a drive-letter volume: %q", clean)
	}
	mount := volume + `\`
	mountPtr, err := windows.UTF16PtrFromString(mount)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]uint16, 1024)
	if err := windows.GetVolumeNameForVolumeMountPoint(mountPtr, &buffer[0], uint32(len(buffer))); err != nil {
		t.Fatalf("resolve volume GUID alias: %v", err)
	}
	guid := windows.UTF16ToString(buffer)
	if !strings.HasPrefix(strings.ToUpper(guid), `\\?\VOLUME{`) || !strings.HasSuffix(guid, `\`) {
		t.Fatalf("volume GUID path = %q", guid)
	}
	relative := strings.TrimPrefix(clean, mount)
	if relative == clean {
		t.Fatalf("path %q is outside mount %q", clean, mount)
	}
	return guid + relative
}

func assertWindowsTargetCurrentUserOnlyDescriptor(t *testing.T, sd *windows.SECURITY_DESCRIPTOR) {
	t.Helper()
	if sd == nil {
		t.Fatal("nil security descriptor")
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("DACL is not protected: control=%#x err=%v", control, err)
	}
	dacl, present, err := sd.DACL()
	if err != nil || !present || dacl == nil || dacl.AceCount != 1 {
		t.Fatalf("DACL = %#v present=%v err=%v", dacl, present, err)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil || ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
		t.Fatalf("sole allow ACE = %#v err=%v", ace, err)
	}
	mask := uint32(ace.Mask)
	if mask != windows.GENERIC_ALL && mask&windows.MUTEX_ALL_ACCESS != windows.MUTEX_ALL_ACCESS {
		t.Fatalf("sole current-user ACE mask = %#x, want generic/mutex all access", mask)
	}
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		t.Fatal(err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	if !(*windows.SID)(unsafe.Pointer(&ace.SidStart)).Equals(user.User.Sid) {
		t.Fatal("sole mutex ACE does not belong to the current user")
	}
}

func openWindowsTargetMutexForACL(t *testing.T, name string) windows.Handle {
	t.Helper()
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.OpenMutex(windows.READ_CONTROL, false, namePtr)
	if err != nil {
		t.Fatalf("open named mutex for ACL inspection: %v", err)
	}
	return handle
}

func assertWindowsTargetMutexAbsent(t *testing.T, name string) {
	t.Helper()
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.OpenMutex(windows.READ_CONTROL, false, namePtr)
	if err == nil {
		_ = windows.CloseHandle(handle)
		t.Fatal("named mutex handle remained after authority release")
	}
	if !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		t.Fatalf("open released mutex error = %v", err)
	}
}

type windowsTargetAuthorityHolder struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	cancel context.CancelFunc
	done   chan error
	once   sync.Once
}

func startWindowsTargetAuthorityHolder(t *testing.T, root string) *windowsTargetAuthorityHolder {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestW33C1AWindowsHelperProcess$", "-test.v=false")
	cmd.Env = append(os.Environ(), windowsAuthorityHelperAction+"=hold", windowsAuthorityHelperRoot+"="+root)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ready := make(chan string, 1)
	go func() {
		line, readErr := bufio.NewReader(stdout).ReadString('\n')
		if readErr != nil {
			ready <- "read helper readiness: " + readErr.Error()
			return
		}
		ready <- strings.TrimSpace(line)
	}()
	holder := &windowsTargetAuthorityHolder{cmd: cmd, stdin: stdin, cancel: cancel, done: done}
	t.Cleanup(func() { _ = holder.stop(true) })
	select {
	case got := <-ready:
		if got != "ready" {
			_ = holder.stop(true)
			t.Fatalf("helper readiness = %q; stderr = %q", got, stderr.String())
		}
	case err := <-done:
		cancel()
		t.Fatalf("helper exited before readiness: %v; stderr = %q", err, stderr.String())
	case <-ctx.Done():
		_ = holder.stop(true)
		t.Fatalf("helper readiness timed out: %v; stderr = %q", ctx.Err(), stderr.String())
	}
	return holder
}

func (h *windowsTargetAuthorityHolder) release(t *testing.T) {
	t.Helper()
	if err := h.stop(false); err != nil {
		t.Fatalf("release helper: %v", err)
	}
}

func (h *windowsTargetAuthorityHolder) kill(t *testing.T) {
	t.Helper()
	if err := h.stop(true); err != nil {
		t.Fatalf("kill helper: %v", err)
	}
}

func (h *windowsTargetAuthorityHolder) stop(kill bool) (result error) {
	h.once.Do(func() {
		if kill {
			if err := h.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				result = err
			}
		} else if err := h.stdin.Close(); err != nil {
			result = err
		}
		select {
		case err := <-h.done:
			if !kill && result == nil {
				result = err
			}
		case <-time.After(3 * time.Second):
			_ = h.cmd.Process.Kill()
			result = errors.New("helper shutdown timed out")
		}
		h.cancel()
	})
	return result
}

func runWindowsTargetAuthorityProbe(t *testing.T, root string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestW33C1AWindowsHelperProcess$", "-test.v=false")
	cmd.Env = append(os.Environ(), windowsAuthorityHelperAction+"=probe", windowsAuthorityHelperRoot+"="+root)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("authority probe: %v: %s", err, output)
	}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "acquired" || strings.HasPrefix(line, "busy:") {
			return line
		}
	}
	t.Fatalf("authority probe returned no marker: %q", output)
	return ""
}
