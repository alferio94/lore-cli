//go:build windows

package install

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func prepareTransactionTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	secureTransactionTestPath(t, root)
	return root
}

func secureTransactionTestDirectory(t *testing.T, path string) {
	t.Helper()
	secureTransactionTestPath(t, path)
}

func secureTransactionTestPath(t *testing.T, path string) {
	t.Helper()
	if err := windowsProtectFile(path); err != nil {
		t.Fatal(err)
	}
}

func assertTransactionTestProtection(t *testing.T, path string, _ bool) {
	t.Helper()
	name, _ := windows.UTF16PtrFromString(path)
	handle, err := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES|windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if !windowsHandleIsCurrentUserOnlyFile(handle) {
		t.Fatal("path ACL is not current-user-only")
	}
}

func TestW33BWindowsTransactionFSRejectsReparseTraversal(t *testing.T) {
	root := prepareTransactionTestRoot(t)
	outside := prepareTransactionTestRoot(t)
	mustWriteTransactionTestFile(t, filepath.Join(outside, "victim"), "outside")
	link := filepath.Join(root, "link")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
		t.Fatalf("create required junction fixture: %v: %s", err, output)
	}
	_, err := applyTransactionFS(root, []transactionFSWrite{{Path: "link/victim", Data: []byte("changed")}}, nil)
	if err == nil || !errors.Is(err, errTransactionFSUnsafePath) {
		t.Fatalf("reparse apply error = %v", err)
	}
	assertTransactionTestFile(t, filepath.Join(outside, "victim"), "outside")
	assertNoTransactionResidue(t, root)
}

func TestW33BWindowsTransactionFSUsesCurrentUserOnlyACLs(t *testing.T) {
	root := prepareTransactionTestRoot(t)
	journal, err := applyTransactionFS(root, []transactionFSWrite{{Path: "deep/file", Data: []byte("next")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertTransactionTestProtection(t, journal.backupRoot, true)
	assertTransactionTestProtection(t, filepath.Join(root, "deep/file"), false)
	if err := journal.Rollback(); err != nil {
		t.Fatal(err)
	}
}

func TestW33BWindowsTransactionFSRollbackRestoresOriginalACL(t *testing.T) {
	root := prepareTransactionTestRoot(t)
	path := filepath.Join(root, "owned.txt")
	mustWriteTransactionTestFile(t, path, "prior")
	user, err := windows.OpenCurrentProcessToken()
	if err != nil {
		t.Fatal(err)
	}
	defer user.Close()
	tokenUser, err := user.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;GRGW;;;" + tokenUser.User.Sid.String() + ")")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil || windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil) != nil {
		t.Fatal("install original ACL fixture")
	}
	want, err := windowsTransactionACL(path)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := applyTransactionFS(root, []transactionFSWrite{{Path: "owned.txt", Data: []byte("next")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Rollback(); err != nil {
		t.Fatal(err)
	}
	got, err := windowsTransactionACL(path)
	if err != nil || got != want {
		t.Fatalf("restored ACL = %q, %v; want %q", got, err, want)
	}
}

func TestW33C1AWindowsAbandonedMutexRecoversThroughAliasesBeforeMutation(t *testing.T) {
	root := prepareTransactionRecoveryProcessRoot(t)
	runTransactionRecoveryCrashHelper(t, root, "after-write-a")

	holder := startWindowsTargetAuthorityHolder(t, root)
	clock := newTargetAuthorityStepWaiter(time.Date(2055, 2, 3, 4, 5, 6, 0, time.UTC))
	result := make(chan targetAuthorityTestResult, 1)
	alias := windowsTargetVolumeAlias(t, root)
	go acquireTargetAuthorityForTest(8, alias, clock, result)
	assertTargetAuthorityTestSleep(t, clock.slept)
	holder.kill(t)
	clock.advance <- struct{}{}
	acquired := receiveTargetAuthorityTestResult(t, result)
	if acquired.err != nil || acquired.guard == nil {
		t.Fatalf("abandoned-owner recovery acquisition = %#v", acquired)
	}
	released := false
	defer func() {
		if !released {
			_ = acquired.guard.Release()
		}
	}()
	combined, ok := acquired.guard.(*combinedTargetGuard)
	if !ok || combined.local == nil {
		t.Fatalf("combined recovery guard = %T", acquired.guard)
	}
	windowsGuard, ok := combined.os.(*windowsTargetGuard)
	if !ok || !windowsGuard.abandoned {
		t.Fatalf("WAIT_ABANDONED owner-death proof missing: %#v", combined.os)
	}
	if err := acquired.guard.Recover(); err != nil {
		t.Fatal(err)
	}
	digest, ok := transactionFSTargetDigest(acquired.guard)
	if !ok || digest != combined.local.root.digest {
		t.Fatal("abandoned recovery guard did not retain canonical identity")
	}
	platform, err := newTransactionFSPlatform(alias, digest)
	if err != nil {
		t.Fatal(err)
	}
	if err := platform.recover(); err != nil {
		t.Fatalf("recover orphan after abandoned owner: %v", err)
	}
	assertTransactionRecoveryState(t, root, false)
	assertNoTransactionResidue(t, root)
	if marker := runWindowsTargetAuthorityProbe(t, strings.ToUpper(root)); marker != "busy:selected_target.authority:selected target is busy" {
		t.Fatalf("named mutex released before recovery cleanup: %q", marker)
	}

	secondRoot := prepareTransactionRecoveryProcessRoot(t)
	second, err := applyTransactionFS(secondRoot, []transactionFSWrite{{Path: "independent.txt", Data: []byte("independent")}}, nil)
	if err != nil {
		t.Fatalf("unrelated root blocked by recovery mutex: %v", err)
	}
	if err := second.Commit(); err != nil {
		t.Fatal(err)
	}
	assertTransactionTestFile(t, filepath.Join(secondRoot, "independent.txt"), "independent")

	if err := acquired.guard.Release(); err != nil {
		t.Fatal(err)
	}
	released = true
	if marker := runWindowsTargetAuthorityProbe(t, strings.ToUpper(root)); marker != "acquired" {
		t.Fatalf("named mutex unavailable after recovery cleanup: %q", marker)
	}
}

func TestW33C1AWindowsCrashRecoveryRestoresExactACLBytesExistenceAndDirectories(t *testing.T) {
	for _, boundary := range []string{"after-durable-journal", "after-write-a", "after-manifest", "during-rollback-recovery", "restored-marker"} {
		t.Run(boundary, func(t *testing.T) {
			root := prepareTransactionRecoveryProcessRoot(t)
			path := filepath.Join(root, "a.txt")
			installWindowsRecoveryACLFixture(t, path)
			wantACL, err := windowsTransactionACL(path)
			if err != nil {
				t.Fatal(err)
			}
			runTransactionRecoveryCrashHelper(t, root, boundary)
			alias := strings.ToUpper(root)
			if boundary == "after-write-a" {
				alias = windowsTargetVolumeAlias(t, root)
			}
			recoverTransactionBeforeAdmission(t, alias, false)
			assertTransactionRecoveryState(t, root, false)
			gotACL, err := windowsTransactionACL(path)
			if err != nil || gotACL != wantACL {
				t.Fatalf("restored ACL = %q, %v; want exact %q", gotACL, err, wantACL)
			}
			if _, err := os.Lstat(filepath.Join(root, "new")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("created directory survived %s recovery: %v", boundary, err)
			}
			assertNoTransactionResidue(t, root)
		})
	}
}

func TestW33C1AWindowsRecoveryRejectsReparseSwapWithoutPrivilegeSkip(t *testing.T) {
	root := prepareTransactionRecoveryProcessRoot(t)
	outside := prepareTransactionTestRoot(t)
	outsidePath := filepath.Join(outside, "victim")
	mustWriteTransactionTestFile(t, outsidePath, "outside")
	runTransactionRecoveryCrashHelper(t, root, "before-manifest")
	if err := os.RemoveAll(filepath.Join(root, "new")); err != nil {
		t.Fatal(err)
	}
	createWindowsTargetJunction(t, filepath.Join(root, "new"), outside)
	journal := transactionRecoveryExpectedJournal(t, root)

	_, err := applyTransactionFS(strings.ToUpper(root), []transactionFSWrite{{Path: "admission.txt", Data: []byte("must-not-exist")}}, nil)
	assertTransactionResidualRisk(t, err, root, journal, outsidePath, "outside")
	assertTransactionTestFile(t, outsidePath, "outside")
	if _, statErr := os.Lstat(filepath.Join(root, "admission.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("reparse-unsafe recovery admitted mutation: %v", statErr)
	}
	if _, statErr := os.Lstat(journal); statErr != nil {
		t.Fatalf("reparse-unsafe recovery removed evidence: %v", statErr)
	}
}

func TestW33C1AWindowsRecoveryFailsClosedOnDurabilityOrRestorationRisk(t *testing.T) {
	for _, damage := range []string{"marker-durability", "missing-backup"} {
		t.Run(damage, func(t *testing.T) {
			root := prepareTransactionRecoveryProcessRoot(t)
			runTransactionRecoveryCrashHelper(t, root, "after-write-a")
			journal := transactionRecoveryExpectedJournal(t, root)
			switch damage {
			case "marker-durability":
				path := filepath.Join(journal, "restored.prepare")
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
				secureTransactionTestDirectory(t, path)
			case "missing-backup":
				if err := os.Remove(filepath.Join(journal, "backups", "000000")); err != nil {
					t.Fatal(err)
				}
			}

			_, err := applyTransactionFS(root, []transactionFSWrite{{Path: "admission.txt", Data: []byte("must-not-exist")}}, nil)
			assertTransactionResidualRisk(t, err, root, journal, "owner-pid=4321", "bearer-secret")
			if _, statErr := os.Lstat(filepath.Join(root, "admission.txt")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("failed recovery admitted mutation: %v", statErr)
			}
			if _, statErr := os.Lstat(journal); statErr != nil {
				t.Fatalf("failed recovery removed evidence: %v", statErr)
			}
		})
	}
}

func TestW33C1AWindowsFlushWriteThroughAndRecoverySourceOrdering(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate Windows recovery test source")
	}
	dir := filepath.Dir(testFile)
	windowsSource, err := os.ReadFile(filepath.Join(dir, "transaction_fs_windows.go"))
	if err != nil {
		t.Fatal(err)
	}
	commonSource, err := os.ReadFile(filepath.Join(dir, "transaction_fs.go"))
	if err != nil {
		t.Fatal(err)
	}

	assertWindowsRecoverySourceOrder(t, windowsRecoveryFunction(t, string(windowsSource), "func (p *windowsTransactionFS) backup("),
		"file.Write(data)", "file.Sync()")
	assertWindowsRecoverySourceOrder(t, windowsRecoveryFunction(t, string(windowsSource), "func (p *windowsTransactionFS) seal("),
		"writeTransactionWindowsDurableFile", "p.writeMarkerAt", "moveWindowsTransactionDir(p.prepare, p.journal)")
	assertWindowsRecoverySourceOrder(t, windowsRecoveryFunction(t, string(windowsSource), "func (p *windowsTransactionFS) atomicWrite("),
		"tmp.Write(data)", "tmp.Sync()", "moveWindowsFile(name, path)")
	assertWindowsRecoverySourceOrder(t, windowsRecoveryFunction(t, string(windowsSource), "func (p *windowsTransactionFS) writeMarkerAt("),
		"writeTransactionWindowsDurableFile(temp", "moveWindowsFile(temp, path)")
	assertWindowsRecoverySourceOrder(t, windowsRecoveryFunction(t, string(windowsSource), "func (p *windowsTransactionFS) recover("),
		"p.removeResidue(p.cleanupRoot)", "p.removeResidue(p.prepare)", "p.removeMarkerTemps()", "p.readJournalHeader()",
		"p.removeTemps(states)", "p.restore(states[i])", "p.removeCreatedDirs()", "p.markRestored()", "p.cleanup()")
	assertWindowsRecoverySourceOrder(t, windowsRecoveryFunction(t, string(windowsSource), "func (p *windowsTransactionFS) cleanup("),
		"moveWindowsTransactionDir(p.journal, p.cleanupRoot)", "p.removeResidue(p.cleanupRoot)")
	moveDir := windowsRecoveryFunction(t, string(windowsSource), "func moveWindowsTransactionDir(")
	if !strings.Contains(moveDir, "windows.MOVEFILE_WRITE_THROUGH") {
		t.Fatal("journal publication/cleanup lost write-through rename semantics")
	}
	assertWindowsRecoverySourceOrder(t, windowsRecoveryFunction(t, string(commonSource), "func applyTransactionFSWithWait("),
		"acquireTargetGuardWithWaiter", "guard.Recover()", "newTransactionFSPlatform", "platform.recover()", "platform.begin()",
		"platform.backup", "platform.seal", "platform.write")
	assertWindowsRecoverySourceOrder(t, windowsRecoveryFunction(t, string(commonSource), "func (j *transactionFSJournal) Commit("),
		"j.platform.markCommitted()", "j.platform.cleanup()", "j.guard.Release()")
	assertWindowsRecoverySourceOrder(t, windowsRecoveryFunction(t, string(commonSource), "func (j *transactionFSJournal) rollback("),
		"j.platform.removeTemps", "j.platform.restore", "j.platform.removeCreatedDirs", "j.platform.markRestored", "j.platform.cleanup")

	root := prepareTransactionTestRoot(t)
	if err := writeTransactionWindowsDurableFile(filepath.Join(root, "missing", "durable"), []byte("x")); err == nil {
		t.Fatal("durable file helper hid an unavailable parent failure")
	}
	if err := moveWindowsTransactionDir(filepath.Join(root, "missing"), filepath.Join(root, "destination")); err == nil {
		t.Fatal("write-through directory move hid a missing source failure")
	}
}

func installWindowsRecoveryACLFixture(t *testing.T, path string) {
	t.Helper()
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		t.Fatal(err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;GRGW;;;" + user.User.Sid.String() + ")")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		t.Fatalf("fixture DACL: %#v err=%v", dacl, err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatalf("install recovery ACL fixture: %v", err)
	}
}

func windowsRecoveryFunction(t *testing.T, source, signature string) string {
	t.Helper()
	start := strings.Index(source, signature)
	if start < 0 {
		t.Fatalf("missing source function %q", signature)
	}
	rest := source[start:]
	if next := strings.Index(rest[len(signature):], "\nfunc "); next >= 0 {
		rest = rest[:len(signature)+next]
	}
	return rest
}

func assertWindowsRecoverySourceOrder(t *testing.T, source string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		next := strings.Index(source, fragment)
		if next < 0 {
			t.Fatalf("durability/recovery order missing %q", fragment)
		}
		source = source[next+len(fragment):]
	}
}
