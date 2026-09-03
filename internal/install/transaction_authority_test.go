package install

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestW33C1ARootCommonAliasesCollapse(t *testing.T) {
	resetTargetAuthorityTestState(t)
	base := t.TempDir()
	root := filepath.Join(base, "target")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}

	canonical, err := canonicalCommonTargetRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	lexical, err := canonicalCommonTargetRoot(root + string(filepath.Separator) + ".")
	if err != nil {
		_ = canonical.file.Close()
		t.Fatal(err)
	}
	if canonical.digest != lexical.digest || !os.SameFile(canonical.info, lexical.info) {
		t.Fatalf("lexical aliases did not converge: %x != %x", canonical.digest, lexical.digest)
	}
	closeTargetTestIdentity(t, canonical)
	closeTargetTestIdentity(t, lexical)

	holder, err := acquireTargetGuard(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = acquireTargetGuard(root+string(filepath.Separator)+".", 0)
	assertTargetAuthorityTestError(t, err, CodeTargetAuthorityBusy, root)
	if err := holder.Release(); err != nil {
		t.Fatal(err)
	}

	aliasParent := filepath.Join(t.TempDir(), "base-alias")
	if err := os.Symlink(base, aliasParent); err != nil {
		t.Logf("existing-ancestor alias unavailable on this host: %v", err)
		return
	}
	aliasRoot := filepath.Join(aliasParent, "target")
	fromRoot, err := canonicalCommonTargetRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	fromAlias, err := canonicalCommonTargetRoot(aliasRoot)
	if err != nil {
		closeTargetTestIdentity(t, fromRoot)
		t.Fatal(err)
	}
	if fromRoot.digest != fromAlias.digest || !os.SameFile(fromRoot.info, fromAlias.info) {
		t.Fatalf("existing-ancestor aliases did not converge: %x != %x", fromRoot.digest, fromAlias.digest)
	}
	closeTargetTestIdentity(t, fromRoot)
	closeTargetTestIdentity(t, fromAlias)

	holder, err = acquireTargetGuard(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = acquireTargetGuard(aliasRoot, 0)
	assertTargetAuthorityTestError(t, err, CodeTargetAuthorityBusy, root, aliasRoot)
	if err := holder.Release(); err != nil {
		t.Fatal(err)
	}

	directLeafAlias := filepath.Join(filepath.Dir(aliasParent), "target-leaf-alias")
	if err := os.Symlink(root, directLeafAlias); err == nil {
		if identity, err := canonicalCommonTargetRoot(directLeafAlias); !errors.Is(err, errTransactionFSUnsafePath) {
			closeTargetTestIdentity(t, identity)
			t.Fatalf("direct symlink leaf error = %v", err)
		}
	}
}

func TestW33C1ALocalPhysicalAliasFallbackCollapsesDifferentDigests(t *testing.T) {
	resetTargetAuthorityTestState(t)
	root := t.TempDir()
	original := canonicalTargetRoot
	canonicalTargetRoot = func(raw string) (targetRootIdentity, error) {
		identity, err := original(root)
		if err != nil {
			return targetRootIdentity{}, err
		}
		identity.digest = targetAuthorityTestDigest(raw)
		return identity, nil
	}

	holder, err := acquireTargetGuard("alias-one", 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = acquireTargetGuard("alias-two", 0)
	assertTargetAuthorityTestError(t, err, CodeTargetAuthorityBusy, root)
	if err := holder.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestW33C1ALocalFIFOExclusion(t *testing.T) {
	resetTargetAuthorityTestState(t)
	root := t.TempDir()
	start := time.Date(2035, 1, 2, 3, 4, 5, 0, time.UTC)
	holder, err := acquireTargetGuardWithWaiter(root, 0, newTargetAuthorityTestWaiter(start))
	if err != nil {
		t.Fatal(err)
	}

	firstClock := newTargetAuthorityStepWaiter(start)
	secondClock := newTargetAuthorityStepWaiter(start)
	results := make(chan targetAuthorityTestResult, 2)
	go acquireTargetAuthorityForTest(1, root, firstClock, results)
	assertTargetAuthorityTestSleep(t, firstClock.slept)
	go acquireTargetAuthorityForTest(2, root, secondClock, results)
	assertTargetAuthorityTestSleep(t, secondClock.slept)

	if err := holder.Release(); err != nil {
		t.Fatal(err)
	}
	firstClock.advance <- struct{}{}
	secondClock.advance <- struct{}{}
	first := receiveTargetAuthorityTestResult(t, results)
	if first.id != 1 || first.err != nil || first.guard == nil {
		t.Fatalf("first acquisition = %#v", first)
	}

	assertTargetAuthorityTestSleep(t, secondClock.slept)
	if err := first.guard.Release(); err != nil {
		t.Fatal(err)
	}
	secondClock.advance <- struct{}{}
	second := receiveTargetAuthorityTestResult(t, results)
	if second.id != 2 || second.err != nil || second.guard == nil {
		t.Fatalf("second acquisition = %#v", second)
	}
	if err := second.guard.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestW33C1ARedactBusyAndMonotonicTimeout(t *testing.T) {
	resetTargetAuthorityTestState(t)
	root := t.TempDir()
	start := time.Date(2040, 6, 7, 8, 9, 10, 0, time.UTC)
	holder, err := acquireTargetGuardWithWaiter(root, 0, newTargetAuthorityTestWaiter(start))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := holder.Release(); err != nil {
			t.Fatal(err)
		}
	}()

	busyClock := newTargetAuthorityTestWaiter(start)
	_, err = acquireTargetGuardWithWaiter(root, 0, busyClock)
	assertTargetAuthorityTestError(t, err, CodeTargetAuthorityBusy, root, "owner-pid=4321", "host.example", "journal payload", "bearer-secret")
	if len(busyClock.Sleeps()) != 0 {
		t.Fatalf("zero-wait attempt slept: %v", busyClock.Sleeps())
	}

	timeoutClock := newTargetAuthorityTestWaiter(start)
	_, err = acquireTargetGuardWithWaiter(root, defaultTargetAuthorityWait, timeoutClock)
	assertTargetAuthorityTestError(t, err, CodeTargetAuthorityTimeout, root, "owner-pid=4321", "host.example", "journal payload", "bearer-secret")
	sleeps := timeoutClock.Sleeps()
	var total time.Duration
	for i, sleep := range sleeps {
		if sleep <= 0 || sleep > targetAuthorityPoll || sleep > 100*time.Millisecond {
			t.Fatalf("sleep[%d] = %s", i, sleep)
		}
		total += sleep
	}
	if total != defaultTargetAuthorityWait {
		t.Fatalf("monotonic wait = %s, want %s", total, defaultTargetAuthorityWait)
	}
	if elapsed := timeoutClock.Now().Sub(start); elapsed != defaultTargetAuthorityWait {
		t.Fatalf("fake monotonic elapsed = %s", elapsed)
	}
}

func TestW33C1ALocalLifecycleHoldsAuthorityThroughCommitAndRollback(t *testing.T) {
	for _, finish := range []string{"commit", "rollback"} {
		t.Run(finish, func(t *testing.T) {
			resetTargetAuthorityTestState(t)
			root := t.TempDir()
			if err := os.Chmod(root, 0o700); err != nil {
				t.Fatal(err)
			}
			start := time.Date(2045, 2, 3, 4, 5, 6, 0, time.UTC)
			clock := newTargetAuthorityTestWaiter(start)
			var events []string
			record := func(event string) { events = append(events, event) }

			acquireTargetOSGuard = func(identity targetRootIdentity, deadline targetAuthorityDeadline, gotClock targetMonotonicWaiter) (targetGuard, error) {
				if !identity.valid() || !os.SameFile(identity.info, mustTargetAuthorityTestStat(t, root)) {
					t.Fatal("OS seam did not receive the pinned selected-root identity")
				}
				if deadline.zero || !deadline.at.Equal(start.Add(defaultTargetAuthorityWait)) || gotClock != clock {
					t.Fatalf("OS seam deadline = %#v, clock match = %v", deadline, gotClock == clock)
				}
				record("os-acquire")
				return &targetAuthorityTestGuard{
					recover: func() error {
						record("recover")
						return nil
					},
					release: func() error {
						_, err := acquireTargetGuardWithWaiter(root, 0, newTargetAuthorityTestWaiter(start))
						if !errors.Is(err, CodeTargetAuthorityBusy) {
							t.Fatalf("local authority released before OS guard: %v", err)
						}
						record("os-release")
						return nil
					},
				}, nil
			}

			journal, err := applyTransactionFSWithWait(root, []transactionFSWrite{{Path: "state.txt", Data: []byte("next")}}, func(stage, path string) error {
				record(stage + ":" + path)
				return nil
			}, defaultTargetAuthorityWait, clock)
			if err != nil {
				t.Fatal(err)
			}
			assertTargetAuthorityTestEventOrder(t, events, "os-acquire", "recover", "backup:state.txt", "write:state.txt")
			_, err = acquireTargetGuardWithWaiter(root, 0, newTargetAuthorityTestWaiter(start))
			assertTargetAuthorityTestError(t, err, CodeTargetAuthorityBusy, root)

			switch finish {
			case "commit":
				err = journal.Commit()
				assertTargetAuthorityTestEventOrder(t, events, "write:state.txt", "cleanup:journal", "os-release")
			case "rollback":
				err = journal.Rollback()
				assertTargetAuthorityTestEventOrder(t, events, "write:state.txt", "rollback:state.txt", "cleanup:journal", "os-release")
			}
			if err != nil {
				t.Fatal(err)
			}

			acquireTargetOSGuard = func(_ targetRootIdentity, deadline targetAuthorityDeadline, gotClock targetMonotonicWaiter) (targetGuard, error) {
				if !deadline.zero || !deadline.at.Equal(start) || gotClock != clock {
					t.Fatalf("zero-wait OS seam deadline = %#v, clock match = %v", deadline, gotClock == clock)
				}
				return noopTargetGuard{}, nil
			}
			guard, err := acquireTargetGuardWithWaiter(root, 0, clock)
			if err != nil {
				t.Fatalf("authority not reusable after %s: %v", finish, err)
			}
			if err := guard.Release(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestW33C1AUnrelatedRootsAcquireConcurrently(t *testing.T) {
	resetTargetAuthorityTestState(t)
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	first, err := acquireTargetGuard(firstRoot, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := acquireTargetGuard(secondRoot, 0)
	if err != nil {
		_ = first.Release()
		t.Fatalf("unrelated root was serialized: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestW33C1ANoProfileLoreOrServerAuthorityCoupling(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate authority test source")
	}
	dir := filepath.Dir(testFile)
	allowedImports := map[string]map[string]bool{
		"transaction_authority.go": {
			"crypto/sha256": true, "errors": true, "os": true, "path/filepath": true, "strings": true, "sync": true, "time": true,
		},
		"transaction_fs.go": {
			"bytes": true, "crypto/sha256": true, "encoding/hex": true, "encoding/json": true,
			"errors": true, "fmt": true, "io": true, "path/filepath": true,
			"sort": true, "strings": true, "time": true,
		},
	}
	for name, allowed := range allowedImports {
		path := filepath.Join(dir, name)
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, spec := range parsed.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil || !allowed[importPath] {
				t.Fatalf("%s has non-authority import %q", name, spec.Path.Value)
			}
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			lower := strings.ToLower(identifier.Name)
			for _, prohibited := range []string{"profile", "lore", "server", "memory", "repository"} {
				if strings.Contains(lower, prohibited) {
					t.Errorf("%s couples target authority through identifier %q", name, identifier.Name)
				}
			}
			return true
		})
	}
}

type targetAuthorityTestWaiter struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

func newTargetAuthorityTestWaiter(now time.Time) *targetAuthorityTestWaiter {
	return &targetAuthorityTestWaiter{now: now}
}

func (w *targetAuthorityTestWaiter) Now() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.now
}

func (w *targetAuthorityTestWaiter) Sleep(d time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sleeps = append(w.sleeps, d)
	w.now = w.now.Add(d)
}

func (w *targetAuthorityTestWaiter) Sleeps() []time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]time.Duration(nil), w.sleeps...)
}

type targetAuthorityStepWaiter struct {
	mu      sync.Mutex
	now     time.Time
	slept   chan time.Duration
	advance chan struct{}
}

func newTargetAuthorityStepWaiter(now time.Time) *targetAuthorityStepWaiter {
	return &targetAuthorityStepWaiter{now: now, slept: make(chan time.Duration), advance: make(chan struct{})}
}

func (w *targetAuthorityStepWaiter) Now() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.now
}

func (w *targetAuthorityStepWaiter) Sleep(d time.Duration) {
	w.slept <- d
	<-w.advance
	w.mu.Lock()
	w.now = w.now.Add(d)
	w.mu.Unlock()
}

type targetAuthorityTestResult struct {
	id    int
	guard targetGuard
	err   error
}

func acquireTargetAuthorityForTest(id int, root string, clock targetMonotonicWaiter, results chan<- targetAuthorityTestResult) {
	guard, err := acquireTargetGuardWithWaiter(root, defaultTargetAuthorityWait, clock)
	results <- targetAuthorityTestResult{id: id, guard: guard, err: err}
}

type targetAuthorityTestGuard struct {
	recover func() error
	release func() error
}

func (g *targetAuthorityTestGuard) Recover() error { return g.recover() }
func (g *targetAuthorityTestGuard) Release() error { return g.release() }

func resetTargetAuthorityTestState(t *testing.T) {
	t.Helper()
	originalCanonical := canonicalTargetRoot
	originalOSGuard := acquireTargetOSGuard
	canonicalTargetRoot = canonicalCommonTargetRoot
	acquireTargetOSGuard = func(targetRootIdentity, targetAuthorityDeadline, targetMonotonicWaiter) (targetGuard, error) {
		return noopTargetGuard{}, nil
	}
	processTargetRegistry = localTargetRegistry{}
	t.Cleanup(func() {
		canonicalTargetRoot = originalCanonical
		acquireTargetOSGuard = originalOSGuard
		processTargetRegistry = localTargetRegistry{}
	})
}

func closeTargetTestIdentity(t *testing.T, identity targetRootIdentity) {
	t.Helper()
	if identity.file != nil {
		if err := identity.file.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func targetAuthorityTestDigest(value string) [32]byte {
	var digest [32]byte
	copy(digest[:], []byte(value))
	if digest == [32]byte{} {
		digest[0] = 1
	}
	return digest
}

func assertTargetAuthorityTestError(t *testing.T, err error, code TransactionCode, forbidden ...string) {
	t.Helper()
	if !errors.Is(err, code) {
		t.Fatalf("error = %v, want %s", err, code)
	}
	typed, ok := err.(interface {
		Code() TransactionCode
		Path() string
	})
	if !ok || typed.Code() != code || typed.Path() != "selected_target.authority" {
		t.Fatalf("typed error = %#v", err)
	}
	wantMessage := "selected target is busy"
	if code == CodeTargetAuthorityTimeout {
		wantMessage = "selected target wait timed out"
	}
	if err.Error() != wantMessage {
		t.Fatalf("error message = %q, want %q", err, wantMessage)
	}
	for _, value := range forbidden {
		if value != "" && strings.Contains(strings.ToLower(err.Error()), strings.ToLower(value)) {
			t.Fatalf("error disclosed %q: %q", value, err)
		}
	}
}

func assertTargetAuthorityTestSleep(t *testing.T, slept <-chan time.Duration) {
	t.Helper()
	select {
	case duration := <-slept:
		if duration <= 0 || duration > targetAuthorityPoll {
			t.Fatalf("poll duration = %s", duration)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not reach a controlled monotonic sleep")
	}
}

func receiveTargetAuthorityTestResult(t *testing.T, results <-chan targetAuthorityTestResult) targetAuthorityTestResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("authority acquisition did not complete")
		return targetAuthorityTestResult{}
	}
}

func mustTargetAuthorityTestStat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func assertTargetAuthorityTestEventOrder(t *testing.T, events []string, ordered ...string) {
	t.Helper()
	position := -1
	for _, want := range ordered {
		found := -1
		for i := position + 1; i < len(events); i++ {
			if events[i] == want {
				found = i
				break
			}
		}
		if found < 0 {
			t.Fatalf("events = %v; missing ordered event %q after index %d", events, want, position)
		}
		position = found
	}
}
