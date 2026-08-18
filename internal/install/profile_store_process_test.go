package install

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alferio94/lore-cli/internal/compiler"
)

const processHelperEnv = "LORE_PROFILE_PROCESS_HELPER"

func TestProfileStoreProcessHelper(t *testing.T) {
	if os.Getenv(processHelperEnv) != "1" {
		return
	}
	path, action := os.Getenv("LORE_STORE_PATH"), os.Getenv("LORE_STORE_ACTION")
	switch action {
	case "complete", "contend", "fail":
		store := NewProfileStore(path)
		prepared, err := store.PrepareProject(os.Getenv("LORE_STORE_ROOT"))
		if err != nil {
			helperDone(false)
		}
		if action == "complete" {
			if !helperTouch(os.Getenv("LORE_STORE_READY")) || !helperWait(os.Getenv("LORE_STORE_GO")) {
				helperDone(false)
			}
		}
		fact := PersistenceFact{}
		if profile := os.Getenv("LORE_STORE_PROFILE"); profile != "" {
			fact = PersistenceFact{Requested: true, ProfileID: profile}
			if os.Getenv("LORE_STORE_SCOPE") == "global" {
				fact.Scope = compiler.ProfileScopeGlobal
			} else {
				fact.Scope, fact.ProjectID = compiler.ProfileScopeProject, prepared.ProjectID()
			}
		}
		if action == "fail" {
			store.commit = func(authority, []byte, []byte) error { return errors.New("injected") }
		}
		budget := defaultProfileStoreWait
		if os.Getenv("LORE_STORE_WAIT") == "0" {
			budget = 0
		}
		err = store.CompleteWithOptions(prepared, fact, ApplyBoundarySuccess, CommitOptions{WaitBudget: budget})
		helperDone(helperResult(err) == os.Getenv("LORE_STORE_EXPECT"))
	case "owner":
		platform := defaultStorePlatform()
		canonical, err := platform.Canonical(path)
		if err != nil {
			helperDone(false)
		}
		owned, err := platform.Acquire(canonical, 0, systemMonotonicWaiter{})
		if err != nil || owned == nil || !helperTouch(os.Getenv("LORE_STORE_READY")) {
			helperDone(false)
		}
		if os.Getenv("LORE_STORE_DEATH") == "crash" {
			if !helperWait(os.Getenv("LORE_STORE_GO")) {
				helperDone(false)
			}
			os.Exit(23)
		}
		for {
			time.Sleep(time.Hour)
		}
	default:
		helperDone(false)
	}
}

func TestProfileStoreProcessGlobalProjectAndAmbiguousRetry(t *testing.T) {
	t.Run("global-project", processGlobalProject)
	t.Run("ambiguous-retry", processAmbiguousRetry)
}

func processGlobalProject(t *testing.T) {
	dir := processPrivateDir(t)
	path, root, goFile := filepath.Join(dir, "profiles.json"), filepath.Join(dir, "project"), filepath.Join(dir, "go")
	global := startCompleteHelper(t, path, root, "balanced", "global", filepath.Join(dir, "ready-global"), goFile, "")
	project := startCompleteHelper(t, path, root, "focused", "project", filepath.Join(dir, "ready-project"), goFile, "")
	waitReady(t, global, filepath.Join(dir, "ready-global"))
	waitReady(t, project, filepath.Join(dir, "ready-project"))
	touch(t, goFile)
	waitHelper(t, global, true)
	waitHelper(t, project, true)
	got, err := NewProfileStore(path).LookupProject(root)
	if err != nil || got.GlobalProfile != "balanced" || got.ProjectProfile != "focused" {
		t.Fatalf("global/project update missing: %+v", got)
	}
	assertNoProcessResidue(t, dir)
}

func processAmbiguousRetry(t *testing.T) {
	dir := processPrivateDir(t)
	path, root := filepath.Join(dir, "profiles.json"), filepath.Join(dir, "project")
	var first []byte
	for attempt := 1; attempt <= 2; attempt++ {
		ready, goFile := filepath.Join(dir, fmt.Sprintf("ready-%d", attempt)), filepath.Join(dir, fmt.Sprintf("go-%d", attempt))
		child := startCompleteHelper(t, path, root, "focused", "project", ready, goFile, "")
		waitReady(t, child, ready)
		touch(t, goFile)
		waitHelper(t, child, true)
		current, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if attempt == 1 {
			first = current // Simulate a durable commit whose response the caller lost.
		} else if !bytes.Equal(current, first) {
			t.Fatal("ambiguous retry caused byte drift")
		}
	}
	got, err := NewProfileStore(path).LookupProject(root)
	if err != nil || got.ProjectProfile != "focused" {
		t.Fatalf("ambiguous retry changed intent: %+v", got)
	}
	assertNoProcessResidue(t, dir)
}

func TestProfileStoreProcessDistinctUpdatesPersist(t *testing.T) {
	dir := processPrivateDir(t)
	path := filepath.Join(dir, "profiles.json")
	alpha, beta := filepath.Join(dir, "alpha"), filepath.Join(dir, "beta")
	goFile := filepath.Join(dir, "go")
	first := startCompleteHelper(t, path, alpha, "alpha-profile", "project", filepath.Join(dir, "ready-a"), goFile, "")
	second := startCompleteHelper(t, path, beta, "beta-profile", "project", filepath.Join(dir, "ready-b"), goFile, "")
	waitReady(t, first, filepath.Join(dir, "ready-a"))
	waitReady(t, second, filepath.Join(dir, "ready-b"))
	touch(t, goFile)
	waitHelper(t, first, true)
	waitHelper(t, second, true)
	store := NewProfileStore(path)
	for root, want := range map[string]string{alpha: "alpha-profile", beta: "beta-profile"} {
		got, err := store.LookupProject(root)
		if err != nil || got.ProjectProfile != want {
			t.Fatalf("distinct update missing: got %q", got.ProjectProfile)
		}
	}
	assertNoProcessResidue(t, dir)
}

func TestProfileStoreProcessSameKeyConflictIsDeterministic(t *testing.T) {
	dir := processPrivateDir(t)
	path, root := filepath.Join(dir, "profiles.json"), filepath.Join(dir, "project")
	secret := "second-secret-profile"
	firstGo, secondGo := filepath.Join(dir, "go-first"), filepath.Join(dir, "go-second")
	first := startCompleteHelper(t, path, root, "first-profile", "project", filepath.Join(dir, "ready-first"), firstGo, "")
	second := startCompleteHelper(t, path, root, secret, "project", filepath.Join(dir, "ready-second"), secondGo, string(CodeProfileConflict)+"@projects[].profile_id")
	waitReady(t, first, filepath.Join(dir, "ready-first"))
	waitReady(t, second, filepath.Join(dir, "ready-second"))
	touch(t, firstGo)
	waitHelper(t, first, true)
	touch(t, secondGo)
	waitHelper(t, second, true)
	if strings.Contains(second.out.String(), secret) || strings.Contains(second.out.String(), path) {
		t.Fatal("conflicting child disclosed sensitive context")
	}
	got, err := NewProfileStore(path).LookupProject(root)
	if err != nil || got.ProjectProfile != "first-profile" {
		t.Fatalf("first committed value was not preserved: %q", got.ProjectProfile)
	}
	assertNoProcessResidue(t, dir)
}

func TestProfileStoreProcessBusyAndTimeoutAreBounded(t *testing.T) {
	dir := processPrivateDir(t)
	path, root := filepath.Join(dir, "profiles.json"), filepath.Join(dir, "project")
	owner := startHelper(t, map[string]string{
		"LORE_STORE_ACTION": "owner", "LORE_STORE_PATH": path,
		"LORE_STORE_READY": filepath.Join(dir, "owner-ready"), "LORE_STORE_DEATH": "kill",
	})
	waitReady(t, owner, filepath.Join(dir, "owner-ready"))
	started := time.Now()
	busy := startHelper(t, map[string]string{
		"LORE_STORE_ACTION": "contend", "LORE_STORE_PATH": path, "LORE_STORE_ROOT": root,
		"LORE_STORE_WAIT": "0", "LORE_STORE_EXPECT": string(CodeProfileBusy) + "@profile_store.authority",
	})
	waitHelper(t, busy, true)
	if time.Since(started) > 2*time.Second {
		t.Fatal("zero-wait contention was not immediate")
	}
	started = time.Now()
	timed := startHelper(t, map[string]string{
		"LORE_STORE_ACTION": "contend", "LORE_STORE_PATH": path, "LORE_STORE_ROOT": root,
		"LORE_STORE_EXPECT": string(CodeProfileTimeout) + "@profile_store.authority",
	})
	waitHelper(t, timed, true)
	if elapsed := time.Since(started); elapsed < 4500*time.Millisecond || elapsed > 8*time.Second {
		t.Fatalf("positive contention elapsed outside bounded window: %s", elapsed)
	}
	killHelper(t, owner)
	runSuccessfulCompletion(t, path, root)
	assertNoProcessResidue(t, dir)
}

func TestProfileStoreProcessCrashAndKillReleaseAuthority(t *testing.T) {
	for _, death := range []string{"crash", "kill"} {
		t.Run(death, func(t *testing.T) {
			dir := processPrivateDir(t)
			path, root := filepath.Join(dir, "profiles.json"), filepath.Join(dir, "project")
			goFile, ready := filepath.Join(dir, "go"), filepath.Join(dir, "ready")
			owner := startHelper(t, map[string]string{
				"LORE_STORE_ACTION": "owner", "LORE_STORE_PATH": path, "LORE_STORE_READY": ready,
				"LORE_STORE_GO": goFile, "LORE_STORE_DEATH": death,
			})
			waitReady(t, owner, ready)
			if death == "crash" {
				touch(t, goFile)
				waitHelper(t, owner, false)
			} else {
				killHelper(t, owner)
			}
			runSuccessfulCompletion(t, path, root)
			if _, err := NewProfileStore(path).LookupProject(root); err != nil {
				t.Fatal("contender did not progress after process death")
			}
			assertNoProcessResidue(t, dir)
		})
	}
}

func TestProfileStoreProcessFailurePreservesStateModeAndSecrets(t *testing.T) {
	dir := processPrivateDir(t)
	path, root := filepath.Join(dir, "profiles.json"), filepath.Join(dir, "project")
	runSuccessfulCompletion(t, path, root)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeMode := filePerm(t, path)
	secret := "bearer-secret-profile"
	child := startHelper(t, map[string]string{
		"LORE_STORE_ACTION": "fail", "LORE_STORE_PATH": path, "LORE_STORE_ROOT": root,
		"LORE_STORE_SCOPE": "global", "LORE_STORE_PROFILE": secret,
		"LORE_STORE_EXPECT": string(CodeProfileIO) + "@profile_store.commit",
	})
	waitHelper(t, child, true)
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, before) || filePerm(t, path) != beforeMode {
		t.Fatal("injected failure changed prior bytes or mode")
	}
	if strings.Contains(child.out.String(), secret) || strings.Contains(child.out.String(), path) {
		t.Fatal("failure child disclosed sensitive context")
	}
	assertNoProcessResidue(t, dir)
}

func TestProfileStoreProcessAbsentFailureCleansAuthorityResidue(t *testing.T) {
	dir := processPrivateDir(t)
	path, root := filepath.Join(dir, "profiles.json"), filepath.Join(dir, "project")
	child := startHelper(t, map[string]string{
		"LORE_STORE_ACTION": "fail",
		"LORE_STORE_PATH":   path, "LORE_STORE_ROOT": root,
		"LORE_STORE_EXPECT": string(CodeProfileIO) + "@profile_store.commit",
	})
	waitHelper(t, child, true)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed absent commit created state: %v", err)
	}
	assertNoProcessResidue(t, dir)
}
func TestProfileStoreProcessCanonicalAliasesConverge(t *testing.T) {
	dir := processPrivateDir(t)
	physical := filepath.Join(dir, "profiles.json")
	alias := physical
	if runtime.GOOS == "windows" {
		alias = filepath.Join(dir, strings.ToUpper(filepath.Base(physical)))
	} else {
		aliasRoot := processPrivateDir(t)
		link := filepath.Join(aliasRoot, "state-link")
		if err := os.Symlink(dir, link); err != nil {
			t.Fatalf("supported host could not create ancestor symlink: %v", err)
		}
		alias = filepath.Join(link, filepath.Base(physical))
	}
	alpha, beta := filepath.Join(dir, "alpha"), filepath.Join(dir, "beta")
	goFile := filepath.Join(dir, "go")
	first := startCompleteHelper(t, physical, alpha, "alpha-profile", "project", filepath.Join(dir, "ready-a"), goFile, "")
	second := startCompleteHelper(t, alias, beta, "beta-profile", "project", filepath.Join(dir, "ready-b"), goFile, "")
	waitReady(t, first, filepath.Join(dir, "ready-a"))
	waitReady(t, second, filepath.Join(dir, "ready-b"))
	touch(t, goFile)
	waitHelper(t, first, true)
	waitHelper(t, second, true)
	store := NewProfileStore(physical)
	for _, root := range []string{alpha, beta} {
		if _, err := store.LookupProject(root); err != nil {
			t.Fatal("canonical alias update was lost")
		}
	}
	assertNoProcessResidue(t, dir)
}

type processChild struct {
	cmd  *exec.Cmd
	out  bytes.Buffer
	done chan struct{}
	err  error
}

func startCompleteHelper(t *testing.T, path, root, profile, scope, ready, goFile, expect string) *processChild {
	t.Helper()
	return startHelper(t, map[string]string{
		"LORE_STORE_ACTION": "complete", "LORE_STORE_PATH": path, "LORE_STORE_ROOT": root,
		"LORE_STORE_PROFILE": profile, "LORE_STORE_SCOPE": scope, "LORE_STORE_READY": ready,
		"LORE_STORE_GO": goFile, "LORE_STORE_EXPECT": expect,
	})
}
func startHelper(t *testing.T, values map[string]string) *processChild {
	t.Helper()
	child := &processChild{}
	child.cmd = exec.Command(os.Args[0], "-test.run=^TestProfileStoreProcessHelper$")
	child.cmd.Env = append(os.Environ(), processHelperEnv+"=1")
	for key, value := range values {
		child.cmd.Env = append(child.cmd.Env, key+"="+value)
	}
	child.cmd.Stdout, child.cmd.Stderr = &child.out, &child.out
	if err := child.cmd.Start(); err != nil {
		t.Fatal("could not start process helper")
	}
	child.done = make(chan struct{})
	go func() {
		child.err = child.cmd.Wait()
		close(child.done)
	}()
	return child
}

func waitReady(t *testing.T, child *processChild, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case <-child.done:
			t.Fatalf("process helper exited before barrier: %v", child.err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = child.cmd.Process.Kill()
	<-child.done
	t.Fatal("process helper barrier timed out")
}

func waitHelper(t *testing.T, child *processChild, success bool) {
	t.Helper()
	<-child.done
	err := child.err
	if success && (err != nil || child.out.String() != "ok\n") {
		t.Fatal("process helper failed without disclosing child context")
	}
	if !success && err == nil {
		t.Fatal("process helper unexpectedly survived forced death")
	}
}

func killHelper(t *testing.T, child *processChild) {
	t.Helper()
	if err := child.cmd.Process.Kill(); err != nil {
		t.Fatal("could not kill process helper")
	}
	waitHelper(t, child, false)
}

func runSuccessfulCompletion(t *testing.T, path, root string) {
	t.Helper()
	dir := filepath.Dir(path)
	ready, goFile := filepath.Join(dir, "complete-ready"), filepath.Join(dir, "complete-go")
	_ = os.Remove(ready)
	_ = os.Remove(goFile)
	child := startCompleteHelper(t, path, root, "", "", ready, goFile, "")
	waitReady(t, child, ready)
	touch(t, goFile)
	waitHelper(t, child, true)
}

func helperResult(err error) string {
	if err == nil {
		return ""
	}
	var typed *ProfileStoreError
	if !errors.As(err, &typed) {
		return "unknown"
	}
	return string(typed.Code()) + "@" + typed.Path()
}

func helperTouch(path string) bool {
	return path != "" && os.WriteFile(path, []byte("ready"), 0o600) == nil
}

func helperWait(path string) bool {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func helperDone(ok bool) {
	if ok {
		fmt.Println("ok")
		os.Exit(0)
	}
	fmt.Println("helper failed")
	os.Exit(2)
}

func processPrivateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := protectStoreDirectory(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func filePerm(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func assertNoProcessResidue(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".profiles-") || strings.HasSuffix(entry.Name(), ".lock") {
			t.Fatalf("profile-store residue remains: %s", entry.Name())
		}
	}
}
