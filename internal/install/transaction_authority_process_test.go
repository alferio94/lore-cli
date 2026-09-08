package install

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	transactionRecoveryHelperAction   = "TEST_TRANSACTION_RECOVERY_ACTION"
	transactionRecoveryHelperRoot     = "TEST_TRANSACTION_RECOVERY_ROOT"
	transactionRecoveryHelperBoundary = "TEST_TRANSACTION_RECOVERY_BOUNDARY"
	transactionRecoveryCrashExit      = 86
)

func TestW33C1AProcessCrashBoundariesRecoverBeforeNewMutation(t *testing.T) {
	original := []string{
		"before-backup-a",
		"after-backup-a",
		"after-backup-b",
		"after-backup-new",
		"after-all-backups-before-journal",
		"after-durable-journal",
		"after-write-a",
		"after-write-b",
		"before-manifest",
		"after-manifest",
		"partial-committed-marker",
		"partial-restored-marker",
		"during-rollback-recovery",
		"restored-marker",
		"restored-cleanup",
	}
	committed := []string{"committed-marker", "committed-cleanup"}
	for _, group := range []struct {
		name       string
		boundaries []string
		wantNext   bool
	}{{"restore-original", original, false}, {"preserve-committed", committed, true}} {
		for _, boundary := range group.boundaries {
			t.Run(group.name+"/"+boundary, func(t *testing.T) {
				root := prepareTransactionRecoveryProcessRoot(t)
				runTransactionRecoveryCrashHelper(t, root, boundary)
				assertTransactionRecoveryOneFixedJournal(t, root, boundary)
				recoverTransactionBeforeAdmission(t, root, group.wantNext)
				assertNoTransactionResidue(t, root)
				// A second acquisition proves recovery/completion cleanup is idempotent.
				recoverTransactionBeforeAdmission(t, root, group.wantNext)
				assertNoTransactionResidue(t, root)
			})
		}
	}
}

func TestW33C1ACorruptOrIncompleteJournalFailsClosedAndRetainsEvidence(t *testing.T) {
	for _, damage := range []string{"corrupt-header", "missing-prepared"} {
		t.Run(damage, func(t *testing.T) {
			root := prepareTransactionRecoveryProcessRoot(t)
			runTransactionRecoveryCrashHelper(t, root, "after-durable-journal")
			journal := transactionRecoveryExpectedJournal(t, root)
			switch damage {
			case "corrupt-header":
				path := filepath.Join(journal, "journal.json")
				if err := os.WriteFile(path, []byte("{\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				secureTransactionTestPath(t, path)
			case "missing-prepared":
				if err := os.Remove(filepath.Join(journal, "prepared")); err != nil {
					t.Fatal(err)
				}
			}

			for attempt := 0; attempt < 2; attempt++ {
				_, err := applyTransactionFS(root, []transactionFSWrite{{Path: "admission.txt", Data: []byte("must-not-exist")}}, nil)
				assertTransactionResidualRisk(t, err, root, journal, "owner-pid=4321", "host.example", "bearer-secret", "prior-a")
				if _, statErr := os.Lstat(filepath.Join(root, "admission.txt")); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("failed-closed recovery admitted mutation: %v", statErr)
				}
				if _, statErr := os.Lstat(journal); statErr != nil {
					t.Fatalf("recovery evidence was removed: %v", statErr)
				}
			}
		})
	}
}

func TestW33C1AProcessAuthorityHeldThroughRecoveryCommitRollbackAndCleanup(t *testing.T) {
	for _, finish := range []string{"commit", "rollback"} {
		t.Run(finish, func(t *testing.T) {
			root := prepareTransactionRecoveryProcessRoot(t)
			holder := startTransactionRecoveryHolder(t, root, "hold-finish", finish)
			if got := runTransactionRecoveryProbe(t, root); got != "busy:selected_target.authority:selected target is busy" {
				t.Fatalf("contender during %s = %q", finish, got)
			}
			holder.finish(t)
			if got := runTransactionRecoveryProbe(t, root); got != "acquired" {
				t.Fatalf("contender after %s cleanup = %q", finish, got)
			}
			assertNoTransactionResidue(t, root)
		})
	}

	root := prepareTransactionRecoveryProcessRoot(t)
	runTransactionRecoveryCrashHelper(t, root, "after-write-a")
	holder := startTransactionRecoveryHolder(t, root, "hold-recovery", "")
	if got := runTransactionRecoveryProbe(t, root); got != "busy:selected_target.authority:selected target is busy" {
		t.Fatalf("contender during orphan recovery = %q", got)
	}
	holder.finish(t)
	if got := runTransactionRecoveryProbe(t, root); got != "acquired" {
		t.Fatalf("contender after recovery cleanup = %q", got)
	}
	assertTransactionRecoveryState(t, root, false)
	assertNoTransactionResidue(t, root)
}

func TestW33C1AProcessUnrelatedRootsRemainIndependent(t *testing.T) {
	firstRoot := prepareTransactionRecoveryProcessRoot(t)
	secondRoot := prepareTransactionRecoveryProcessRoot(t)
	holder := startTransactionRecoveryHolder(t, firstRoot, "hold-finish", "commit")
	output, err := runTransactionRecoveryHelper(t, secondRoot, "complete", "")
	if err != nil || !strings.Contains(output, "completed") {
		holder.finish(t)
		t.Fatalf("unrelated-root completion = %v, %q", err, output)
	}
	assertTransactionRecoveryState(t, secondRoot, true)
	assertNoTransactionResidue(t, secondRoot)
	if got := runTransactionRecoveryProbe(t, firstRoot); got != "busy:selected_target.authority:selected target is busy" {
		holder.finish(t)
		t.Fatalf("first root lost independent authority: %q", got)
	}
	holder.finish(t)
}

func TestW33C1ARecoveryHelperProcess(t *testing.T) {
	action := os.Getenv(transactionRecoveryHelperAction)
	if action == "" {
		return
	}
	root := os.Getenv(transactionRecoveryHelperRoot)
	boundary := os.Getenv(transactionRecoveryHelperBoundary)
	switch action {
	case "crash":
		runTransactionRecoveryCrashAction(t, root, boundary)
	case "probe":
		guard, err := acquireTargetGuard(root, 0)
		if err != nil {
			typed, ok := err.(interface{ Path() string })
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
	case "complete":
		journal, err := applyTransactionFS(root, transactionRecoveryProcessWrites(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := journal.Commit(); err != nil {
			t.Fatal(err)
		}
		fmt.Println("completed")
	case "hold-finish":
		journal, err := applyTransactionFS(root, transactionRecoveryProcessWrites(), nil)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println("ready")
		if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
			t.Fatal(err)
		}
		if boundary == "commit" {
			err = journal.Commit()
		} else if boundary == "rollback" {
			err = journal.Rollback()
		} else {
			t.Fatalf("unknown finish %q", boundary)
		}
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println("done")
	case "hold-recovery":
		stop := errors.New("stop after recovery before admission")
		_, err := applyTransactionFS(root, []transactionFSWrite{{Path: "admission.txt", Data: []byte("not-admitted")}}, func(stage, path string) error {
			if stage == "backup" && path == "admission.txt" {
				fmt.Println("ready")
				if _, readErr := bufio.NewReader(os.Stdin).ReadString('\n'); readErr != nil {
					return readErr
				}
				return stop
			}
			return nil
		})
		if !errors.Is(err, stop) {
			t.Fatalf("controlled recovery stop = %v", err)
		}
		fmt.Println("done")
	default:
		t.Fatalf("unknown helper action %q", action)
	}
}

func runTransactionRecoveryCrashAction(t *testing.T, root, boundary string) {
	t.Helper()
	if boundary == "after-all-backups-before-journal" {
		guard, err := acquireTargetGuard(root, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := guard.Recover(); err != nil {
			t.Fatal(err)
		}
		digest, ok := transactionFSTargetDigest(guard)
		if !ok {
			t.Fatal("target authority did not expose the canonical root digest")
		}
		platform, err := newTransactionFSPlatform(root, digest)
		if err != nil {
			t.Fatal(err)
		}
		if err := platform.recover(); err != nil {
			t.Fatal(err)
		}
		if err := platform.begin(); err != nil {
			t.Fatal(err)
		}
		ordered, err := normalizeTransactionWrites(transactionRecoveryProcessWrites())
		if err != nil {
			t.Fatal(err)
		}
		for i, write := range ordered {
			if _, err := platform.backup(write.Path, i); err != nil {
				t.Fatal(err)
			}
		}
		crashTransactionRecoveryHelper(boundary)
	}
	triggers := map[string]string{
		"before-backup-a":       "backup:a.txt",
		"after-backup-a":        "backup:b.txt",
		"after-backup-b":        "backup:new/deep/created.txt",
		"after-backup-new":      "backup:" + provenanceV3Name,
		"after-durable-journal": "write:a.txt",
		"after-write-a":         "write:b.txt",
		"after-write-b":         "write:new/deep/created.txt",
		"before-manifest":       "write:" + provenanceV3Name,
	}
	journal, err := applyTransactionFS(root, transactionRecoveryProcessWrites(), func(stage, path string) error {
		if triggers[boundary] == stage+":"+path {
			crashTransactionRecoveryHelper(boundary)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	switch boundary {
	case "after-manifest":
		crashTransactionRecoveryHelper(boundary)
	case "partial-committed-marker", "partial-restored-marker":
		name := strings.TrimPrefix(strings.TrimSuffix(boundary, "-marker"), "partial-") + ".prepare"
		path := filepath.Join(journal.backupRoot, name)
		if err := os.WriteFile(path, []byte(strings.TrimSuffix(name, ".prepare")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		secureTransactionTestPath(t, path)
		crashTransactionRecoveryHelper(boundary)
	case "during-rollback-recovery":
		if err := journal.platform.removeTemps(journal.states); err != nil {
			t.Fatal(err)
		}
		if err := journal.platform.restore(journal.states[len(journal.states)-1]); err != nil {
			t.Fatal(err)
		}
		crashTransactionRecoveryHelper(boundary)
	case "committed-marker", "committed-cleanup":
		if err := journal.platform.markCommitted(); err != nil {
			t.Fatal(err)
		}
		if boundary == "committed-cleanup" {
			moveTransactionRecoveryJournalToCleanup(t, journal.backupRoot)
		}
		crashTransactionRecoveryHelper(boundary)
	case "restored-marker", "restored-cleanup":
		if err := journal.platform.removeTemps(journal.states); err != nil {
			t.Fatal(err)
		}
		for i := len(journal.states) - 1; i >= 0; i-- {
			if err := journal.platform.restore(journal.states[i]); err != nil {
				t.Fatal(err)
			}
		}
		if err := journal.platform.removeCreatedDirs(); err != nil {
			t.Fatal(err)
		}
		if err := journal.platform.markRestored(); err != nil {
			t.Fatal(err)
		}
		if boundary == "restored-cleanup" {
			moveTransactionRecoveryJournalToCleanup(t, journal.backupRoot)
		}
		crashTransactionRecoveryHelper(boundary)
	default:
		t.Fatalf("boundary %q did not crash", boundary)
	}
}

func crashTransactionRecoveryHelper(boundary string) {
	fmt.Println("crash:" + boundary)
	_ = os.Stdout.Sync()
	os.Exit(transactionRecoveryCrashExit)
}

func moveTransactionRecoveryJournalToCleanup(t *testing.T, journal string) {
	t.Helper()
	if err := os.Rename(journal, journal+".cleanup"); err != nil {
		t.Fatal(err)
	}
}

func transactionRecoveryProcessWrites() []transactionFSWrite {
	return []transactionFSWrite{
		{Path: provenanceV3Name, Data: []byte("next-manifest")},
		{Path: "new/deep/created.txt", Data: []byte("next-created")},
		{Path: "b.txt", Data: []byte("next-b")},
		{Path: "a.txt", Data: []byte("next-a")},
	}
}

func prepareTransactionRecoveryProcessRoot(t *testing.T) string {
	t.Helper()
	root := prepareTransactionTestRoot(t)
	mustWriteTransactionTestFile(t, filepath.Join(root, "a.txt"), "prior-a")
	mustWriteTransactionTestFile(t, filepath.Join(root, "b.txt"), "prior-b")
	mustWriteTransactionTestFile(t, filepath.Join(root, provenanceV3Name), "prior-manifest")
	return root
}

func recoverTransactionBeforeAdmission(t *testing.T, root string, wantNext bool) {
	t.Helper()
	stop := errors.New("stop before new mutation")
	observed := false
	_, err := applyTransactionFS(root, []transactionFSWrite{{Path: "admission.txt", Data: []byte("not-admitted")}}, func(stage, path string) error {
		if stage == "backup" && path == "admission.txt" {
			observed = true
			assertTransactionRecoveryState(t, root, wantNext)
			if _, statErr := os.Lstat(filepath.Join(root, "admission.txt")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("new mutation preceded recovery: %v", statErr)
			}
			return stop
		}
		return nil
	})
	if !observed || !errors.Is(err, stop) {
		t.Fatalf("recovery/admission gate = observed %v, error %v", observed, err)
	}
	assertTransactionRecoveryState(t, root, wantNext)
}

func assertTransactionRecoveryState(t *testing.T, root string, wantNext bool) {
	t.Helper()
	prefix := "prior-"
	created := false
	if wantNext {
		prefix = "next-"
		created = true
	}
	assertTransactionTestFile(t, filepath.Join(root, "a.txt"), prefix+"a")
	assertTransactionTestFile(t, filepath.Join(root, "b.txt"), prefix+"b")
	assertTransactionTestFile(t, filepath.Join(root, provenanceV3Name), prefix+"manifest")
	createdPath := filepath.Join(root, "new", "deep", "created.txt")
	if created {
		assertTransactionTestFile(t, createdPath, "next-created")
		return
	}
	if _, err := os.Lstat(createdPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created resource survived restoration: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "new")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("transaction-created directories survived restoration: %v", err)
	}
}

func assertTransactionRecoveryOneFixedJournal(t *testing.T, root, boundary string) {
	t.Helper()
	base := transactionRecoveryExpectedJournal(t, root)
	matches, err := filepath.Glob(base + "*")
	if err != nil || len(matches) != 1 {
		t.Fatalf("fixed journal matches = %v, %v", matches, err)
	}
	want := base
	if strings.HasPrefix(boundary, "before-backup-") || strings.HasPrefix(boundary, "after-backup-") || boundary == "after-all-backups-before-journal" {
		want += ".prepare"
	}
	if strings.HasSuffix(boundary, "cleanup") {
		want += ".cleanup"
	}
	if matches[0] != want {
		t.Fatalf("journal = %q, want fixed %q", matches[0], want)
	}
	lower := strings.ToLower(filepath.Base(matches[0]))
	for _, prohibited := range []string{"pid", "host", "time", "stamp"} {
		if strings.Contains(lower, prohibited) {
			t.Fatalf("journal name infers staleness through %q: %q", prohibited, matches[0])
		}
	}
}

func transactionRecoveryExpectedJournal(t *testing.T, root string) string {
	t.Helper()
	identity, err := canonicalTargetRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTargetTestIdentity(t, identity)
	return filepath.Join(filepath.Dir(root), fmt.Sprintf(".lore-transaction-v1-%x", identity.digest))
}

func runTransactionRecoveryCrashHelper(t *testing.T, root, boundary string) {
	t.Helper()
	output, err := runTransactionRecoveryHelper(t, root, "crash", boundary)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != transactionRecoveryCrashExit || !strings.Contains(output, "crash:"+boundary) {
		t.Fatalf("crash helper %q = %v, %q", boundary, err, output)
	}
}

func runTransactionRecoveryProbe(t *testing.T, root string) string {
	t.Helper()
	output, err := runTransactionRecoveryHelper(t, root, "probe", "")
	if err != nil {
		t.Fatalf("recovery probe: %v: %s", err, output)
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "acquired" || strings.HasPrefix(line, "busy:") {
			return line
		}
	}
	t.Fatalf("recovery probe returned no marker: %q", output)
	return ""
}

func runTransactionRecoveryHelper(t *testing.T, root, action, boundary string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestW33C1ARecoveryHelperProcess$", "-test.v=false")
	cmd.Env = transactionRecoveryHelperEnv(action, root, boundary)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("helper %q exceeded its safety deadline: %v: %s", action, ctx.Err(), output)
	}
	return string(output), err
}

type transactionRecoveryHolder struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	stderr  *bytes.Buffer
	cancel  context.CancelFunc
	done    chan error
	once    sync.Once
}

func startTransactionRecoveryHolder(t *testing.T, root, action, boundary string) *transactionRecoveryHolder {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestW33C1ARecoveryHelperProcess$", "-test.v=false")
	cmd.Env = transactionRecoveryHelperEnv(action, root, boundary)
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
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	holder := &transactionRecoveryHolder{cmd: cmd, stdin: stdin, scanner: bufio.NewScanner(stdout), stderr: stderr, cancel: cancel, done: make(chan error, 1)}
	go func() { holder.done <- cmd.Wait() }()
	t.Cleanup(func() { holder.stop() })
	if marker := holder.readMarker(t); marker != "ready" {
		holder.stop()
		t.Fatalf("holder readiness = %q; stderr = %q", marker, stderr.String())
	}
	return holder
}

func (h *transactionRecoveryHolder) finish(t *testing.T) {
	t.Helper()
	if _, err := io.WriteString(h.stdin, "continue\n"); err != nil {
		t.Fatal(err)
	}
	if marker := h.readMarker(t); marker != "done" {
		t.Fatalf("holder completion = %q; stderr = %q", marker, h.stderr.String())
	}
	if err := <-h.done; err != nil {
		t.Fatalf("holder exit: %v; stderr = %q", err, h.stderr.String())
	}
	_ = h.stdin.Close()
	h.cancel()
	h.once.Do(func() {})
}

func (h *transactionRecoveryHolder) readMarker(t *testing.T) string {
	t.Helper()
	result := make(chan string, 1)
	go func() {
		for h.scanner.Scan() {
			line := strings.TrimSpace(h.scanner.Text())
			if line == "ready" || line == "done" {
				result <- line
				return
			}
		}
		if err := h.scanner.Err(); err != nil {
			result <- "scanner stopped: " + err.Error()
			return
		}
		result <- "scanner stopped"
	}()
	select {
	case marker := <-result:
		return marker
	case <-time.After(5 * time.Second):
		return "safety deadline exceeded"
	}
}

func (h *transactionRecoveryHolder) stop() {
	h.once.Do(func() {
		_ = h.stdin.Close()
		if h.cmd.Process != nil {
			_ = h.cmd.Process.Kill()
		}
		select {
		case <-h.done:
		case <-time.After(2 * time.Second):
		}
		h.cancel()
	})
}

func transactionRecoveryHelperEnv(action, root, boundary string) []string {
	prefixes := []string{transactionRecoveryHelperAction + "=", transactionRecoveryHelperRoot + "=", transactionRecoveryHelperBoundary + "="}
	env := make([]string, 0, len(os.Environ())+3)
	for _, entry := range os.Environ() {
		skip := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(entry, prefix) {
				skip = true
				break
			}
		}
		if !skip {
			env = append(env, entry)
		}
	}
	return append(env,
		transactionRecoveryHelperAction+"="+action,
		transactionRecoveryHelperRoot+"="+root,
		transactionRecoveryHelperBoundary+"="+boundary,
	)
}
