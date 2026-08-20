//go:build darwin || linux

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

	"golang.org/x/sys/unix"
)

const (
	unixAuthorityHelperAction = "TEST_UNIX_AUTHORITY_ACTION"
	unixAuthorityHelperRoot   = "TEST_UNIX_AUTHORITY_ROOT"
)

func TestW33C1AUnixRootOpenIdentityAndAliases(t *testing.T) {
	t.Parallel()
	root := newUnixAuthorityTestRoot(t)
	identity, err := canonicalUnixTargetRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTargetTestIdentity(t, identity)

	platform, ok := identity.platform.(unixTargetRootIdentity)
	if !ok {
		t.Fatalf("platform identity = %#v", identity.platform)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(identity.file.Fd()), &stat); err != nil {
		t.Fatal(err)
	}
	if platform.device != uint64(stat.Dev) || platform.inode != uint64(stat.Ino) {
		t.Fatalf("identity = (%d,%d), pinned = (%d,%d)", platform.device, platform.inode, stat.Dev, stat.Ino)
	}

	lexical, err := canonicalUnixTargetRoot(root + string(filepath.Separator) + ".")
	if err != nil {
		t.Fatal(err)
	}
	defer closeTargetTestIdentity(t, lexical)
	aliasParent := filepath.Join(t.TempDir(), "parent-alias")
	if err := os.Symlink(filepath.Dir(root), aliasParent); err != nil {
		t.Fatal(err)
	}
	alias, err := canonicalUnixTargetRoot(filepath.Join(aliasParent, filepath.Base(root)))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTargetTestIdentity(t, alias)
	for name, candidate := range map[string]targetRootIdentity{"lexical": lexical, "ancestor symlink": alias} {
		got := candidate.platform.(unixTargetRootIdentity)
		if candidate.digest != identity.digest || got != platform || !os.SameFile(candidate.info, identity.info) {
			t.Fatalf("%s alias did not converge: digest %x/%x identity %#v/%#v", name, candidate.digest, identity.digest, got, platform)
		}
	}

	directLeaf := filepath.Join(t.TempDir(), "target-leaf-alias")
	if err := os.Symlink(root, directLeaf); err != nil {
		t.Fatal(err)
	}
	if fd, err := unix.Open(directLeaf, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0); err == nil {
		_ = unix.Close(fd)
		t.Fatal("kernel followed a direct symlink despite O_NOFOLLOW")
	}
	if unsafe, err := canonicalUnixTargetRoot(directLeaf); !errors.Is(err, errTransactionFSUnsafePath) {
		closeTargetTestIdentity(t, unsafe)
		t.Fatalf("direct symlink error = %v", err)
	}
	traversal := root + string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Base(root)
	if unsafe, err := canonicalUnixTargetRoot(traversal); !errors.Is(err, errTransactionFSUnsafePath) {
		closeTargetTestIdentity(t, unsafe)
		t.Fatalf("traversal error = %v", err)
	}
}

func TestW33C1AUnixCrossProcessBusyTimeoutAndRedaction(t *testing.T) {
	t.Parallel()
	root := newUnixAuthorityTestRoot(t)
	aliasParent := filepath.Join(t.TempDir(), "holder-parent-alias")
	if err := os.Symlink(filepath.Dir(root), aliasParent); err != nil {
		t.Fatal(err)
	}
	holder := startUnixAuthorityHolder(t, filepath.Join(aliasParent, filepath.Base(root)))

	_, err := acquireTargetGuard(root, 0)
	assertTargetAuthorityTestError(t, err, CodeTargetAuthorityBusy, root, "owner-pid=4321", "host.example", "journal payload", "bearer-secret")

	start := time.Date(2050, 1, 2, 3, 4, 5, 0, time.UTC)
	clock := newTargetAuthorityTestWaiter(start)
	_, err = acquireTargetGuardWithWaiter(root, defaultTargetAuthorityWait, clock)
	assertTargetAuthorityTestError(t, err, CodeTargetAuthorityTimeout, root, "owner-pid=4321", "host.example", "journal payload", "bearer-secret")
	var total time.Duration
	for i, sleep := range clock.Sleeps() {
		if sleep <= 0 || sleep > targetAuthorityPoll || sleep > 100*time.Millisecond {
			t.Fatalf("sleep[%d] = %s", i, sleep)
		}
		total += sleep
	}
	if total != defaultTargetAuthorityWait || !clock.Now().Equal(start.Add(defaultTargetAuthorityWait)) {
		t.Fatalf("bounded wait = %s at %s", total, clock.Now())
	}

	holder.release(t)
	guard, err := acquireTargetGuard(root, 0)
	if err != nil {
		t.Fatalf("authority unavailable after release: %v", err)
	}
	if err := guard.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestW33C1AUnixOwnerDeathReleasesKernelAuthority(t *testing.T) {
	t.Parallel()
	root := newUnixAuthorityTestRoot(t)
	holder := startUnixAuthorityHolder(t, root)
	holder.kill(t)

	guard, err := acquireTargetGuard(root, 0)
	if err != nil {
		t.Fatalf("OS-proven owner death did not release authority: %v", err)
	}
	if err := guard.Release(); err != nil {
		t.Fatal(err)
	}

	// This test proves only kernel owner-death release. Durable orphan journal
	// restoration and fail-closed residual-risk behavior belong to task 3.8 and
	// are intentionally not asserted by this task-3.6 Unix authority file.
}

func TestW33C1AUnixFlockHeldThroughTransactionRelease(t *testing.T) {
	for _, finish := range []string{"commit", "rollback"} {
		t.Run(finish, func(t *testing.T) {
			root := newUnixAuthorityTestRoot(t)
			journal, err := applyTransactionFS(root, []transactionFSWrite{{Path: "state.txt", Data: []byte("next")}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if marker := runUnixAuthorityProbe(t, root); marker != "busy:selected_target.authority:selected target is busy" {
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
			if marker := runUnixAuthorityProbe(t, root); marker != "acquired" {
				t.Fatalf("contender after %s = %q", finish, marker)
			}
		})
	}
}

func TestW33C1AUnixUnrelatedRootsRemainIndependent(t *testing.T) {
	t.Parallel()
	firstRoot := newUnixAuthorityTestRoot(t)
	secondRoot := newUnixAuthorityTestRoot(t)
	holder := startUnixAuthorityHolder(t, firstRoot)

	guard, err := acquireTargetGuard(secondRoot, 0)
	if err != nil {
		t.Fatalf("unrelated root was serialized: %v", err)
	}
	if err := guard.Release(); err != nil {
		t.Fatal(err)
	}
	holder.release(t)
}

func TestW33C1AUnixAuthoritySurfaceAndCoupling(t *testing.T) {
	t.Parallel()
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate Unix authority test source")
	}
	path := filepath.Join(filepath.Dir(testFile), "transaction_authority_unix.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if !strings.HasPrefix(text, "//go:build darwin || linux\n") {
		t.Fatal("Unix production build tag drifted")
	}
	if !strings.Contains(text, "unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW") {
		t.Fatal("Unix root open does not retain the complete no-follow directory flag set")
	}
	if !strings.Contains(text, "unix.LOCK_EX|unix.LOCK_NB") {
		t.Fatal("Unix authority is not a nonblocking exclusive flock")
	}

	parsed, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"crypto/sha256": true, "encoding/binary": true, "errors": true, "os": true,
		"path/filepath": true, "strings": true, "sync": true, "golang.org/x/sys/unix": true,
	}
	for _, spec := range parsed.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil || !allowed[importPath] {
			t.Fatalf("Unix authority has prohibited import %q", spec.Path.Value)
		}
	}
	ast.Inspect(parsed, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		lower := strings.ToLower(identifier.Name)
		for _, prohibited := range []string{"profile", "lore", "server", "memory", "repository", "journal"} {
			if strings.Contains(lower, prohibited) {
				t.Errorf("Unix target authority couples through identifier %q", identifier.Name)
			}
		}
		return true
	})
}

func TestW33C1AUnixHelperProcess(t *testing.T) {
	action := os.Getenv(unixAuthorityHelperAction)
	if action == "" {
		return
	}
	root := os.Getenv(unixAuthorityHelperRoot)
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

type unixAuthorityHolder struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	cancel context.CancelFunc
	done   chan error
	once   sync.Once
}

func startUnixAuthorityHolder(t *testing.T, root string) *unixAuthorityHolder {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestW33C1AUnixHelperProcess$", "-test.v=false")
	cmd.Env = append(os.Environ(), unixAuthorityHelperAction+"=hold", unixAuthorityHelperRoot+"="+root)
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
	holder := &unixAuthorityHolder{cmd: cmd, stdin: stdin, cancel: cancel, done: done}
	t.Cleanup(func() { holder.stop(true) })
	select {
	case got := <-ready:
		if got != "ready" {
			holder.stop(true)
			t.Fatalf("helper readiness = %q; stderr = %q", got, stderr.String())
		}
	case err := <-done:
		cancel()
		t.Fatalf("helper exited before readiness: %v; stderr = %q", err, stderr.String())
	case <-ctx.Done():
		holder.stop(true)
		t.Fatalf("helper readiness timed out: %v; stderr = %q", ctx.Err(), stderr.String())
	}
	return holder
}

func (h *unixAuthorityHolder) release(t *testing.T) {
	t.Helper()
	if err := h.stop(false); err != nil {
		t.Fatalf("release helper: %v", err)
	}
}

func (h *unixAuthorityHolder) kill(t *testing.T) {
	t.Helper()
	if err := h.stop(true); err != nil {
		t.Fatalf("kill helper: %v", err)
	}
}

func (h *unixAuthorityHolder) stop(kill bool) (result error) {
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
		case <-time.After(2 * time.Second):
			_ = h.cmd.Process.Kill()
			result = errors.New("helper shutdown timed out")
		}
		h.cancel()
	})
	return result
}

func runUnixAuthorityProbe(t *testing.T, root string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestW33C1AUnixHelperProcess$", "-test.v=false")
	cmd.Env = append(os.Environ(), unixAuthorityHelperAction+"=probe", unixAuthorityHelperRoot+"="+root)
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

func newUnixAuthorityTestRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "target")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}
