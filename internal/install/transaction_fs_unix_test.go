//go:build darwin || linux

package install

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func prepareTransactionTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

func secureTransactionTestDirectory(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func secureTransactionTestPath(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertTransactionTestProtection(t *testing.T, path string, directory bool) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	want := os.FileMode(0o600)
	if directory {
		want = 0o700
	}
	if info.Mode().Perm() != want {
		t.Fatalf("mode = %o, want %o", info.Mode().Perm(), want)
	}
}

func TestW33BUnixTransactionFSBackupFailureDoesNotReplaceTargets(t *testing.T) {
	root := prepareTransactionTestRoot(t)
	path := filepath.Join(root, "a.txt")
	mustWriteTransactionTestFile(t, path, "prior")
	mustWriteTransactionTestFile(t, filepath.Join(root, "b.txt"), "prior")
	before := fileIdentity(t, path)
	_, _ = applyTransactionFS(root, []transactionFSWrite{{Path: "a.txt", Data: []byte("next")}, {Path: "b.txt", Data: []byte("next")}}, func(stage, path string) error {
		if stage == "backup" && path == "b.txt" {
			return errors.New("injected")
		}
		return nil
	})
	if after := fileIdentity(t, path); after != before {
		t.Fatal("backup failure replaced an unmutated target")
	}
}

func TestW33BUnixTransactionFSRejectsSymlinkTraversal(t *testing.T) {
	root := prepareTransactionTestRoot(t)
	outside := prepareTransactionTestRoot(t)
	mustWriteTransactionTestFile(t, filepath.Join(outside, "victim"), "outside")
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	_, err := applyTransactionFS(root, []transactionFSWrite{{Path: "link/victim", Data: []byte("changed")}}, nil)
	if err == nil || !errors.Is(err, errTransactionFSUnsafePath) {
		t.Fatalf("symlink apply error = %v", err)
	}
	assertTransactionTestFile(t, filepath.Join(outside, "victim"), "outside")
	assertNoTransactionResidue(t, root)
}

func TestW33BUnixTransactionFSUsesRestrictiveJournalAndFiles(t *testing.T) {
	root := prepareTransactionTestRoot(t)
	journal, err := applyTransactionFS(root, []transactionFSWrite{{Path: "deep/file", Data: []byte("next")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertTransactionTestProtection(t, journal.backupRoot, true)
	assertTransactionTestProtection(t, filepath.Join(root, "deep"), true)
	assertTransactionTestProtection(t, filepath.Join(root, "deep/file"), false)
	if err := journal.Rollback(); err != nil {
		t.Fatal(err)
	}
}

func TestW33C1AUnixOwnerDeathRecoveryPinsIdentityAndFlockThroughCleanup(t *testing.T) {
	root := prepareTransactionRecoveryProcessRoot(t)
	runTransactionRecoveryCrashHelper(t, root, "after-write-a")

	aliasParent := filepath.Join(t.TempDir(), "recovery-parent-alias")
	if err := os.Symlink(filepath.Dir(root), aliasParent); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(aliasParent, filepath.Base(root))
	guard, err := acquireTargetGuard(alias, 0)
	if err != nil {
		t.Fatalf("owner-death handoff: %v", err)
	}
	released := false
	defer func() {
		if !released {
			_ = guard.Release()
		}
	}()

	combined, ok := guard.(*combinedTargetGuard)
	if !ok || combined.local == nil {
		t.Fatalf("combined recovery guard = %T", guard)
	}
	identity, ok := combined.local.root.platform.(unixTargetRootIdentity)
	if !ok {
		t.Fatalf("recovery identity = %#v", combined.local.root.platform)
	}
	var pinned unix.Stat_t
	if unix.Fstat(int(combined.local.root.file.Fd()), &pinned) != nil ||
		uint64(pinned.Dev) != identity.device || uint64(pinned.Ino) != identity.inode ||
		!unixTargetRootStillPinned(combined.local.root, identity) {
		t.Fatalf("recovery root was not pinned to device/inode (%d,%d): %#v", identity.device, identity.inode, pinned)
	}

	contenderFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(contenderFD, unix.LOCK_EX|unix.LOCK_NB); !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
		_ = unix.Close(contenderFD)
		t.Fatalf("independent descriptor bypassed recovery flock: %v", err)
	}
	_ = unix.Close(contenderFD)

	digest, ok := transactionFSTargetDigest(guard)
	if !ok || digest != combined.local.root.digest {
		t.Fatal("recovery guard did not retain the canonical root digest")
	}
	platform, err := newTransactionFSPlatform(alias, digest)
	if err != nil {
		t.Fatal(err)
	}
	if err := platform.recover(); err != nil {
		t.Fatalf("recover orphan while authoritative: %v", err)
	}
	assertTransactionRecoveryState(t, alias, false)
	assertNoTransactionResidue(t, alias)
	if marker := runUnixAuthorityProbe(t, root); marker != "busy:selected_target.authority:selected target is busy" {
		t.Fatalf("flock released before recovery cleanup: %q", marker)
	}

	secondRoot := prepareTransactionRecoveryProcessRoot(t)
	second, err := applyTransactionFS(secondRoot, []transactionFSWrite{{Path: "independent.txt", Data: []byte("independent")}}, nil)
	if err != nil {
		t.Fatalf("unrelated root blocked by recovery: %v", err)
	}
	if err := second.Commit(); err != nil {
		t.Fatal(err)
	}
	assertTransactionTestFile(t, filepath.Join(secondRoot, "independent.txt"), "independent")

	if err := guard.Release(); err != nil {
		t.Fatal(err)
	}
	released = true
	if marker := runUnixAuthorityProbe(t, alias); marker != "acquired" {
		t.Fatalf("flock unavailable after recovery cleanup: %q", marker)
	}
}

func TestW33C1AUnixCrashRecoveryRestoresExactModesExistenceAndDirectories(t *testing.T) {
	for _, boundary := range []string{"after-durable-journal", "after-manifest", "during-rollback-recovery", "restored-marker"} {
		t.Run(boundary, func(t *testing.T) {
			root := prepareTransactionRecoveryProcessRoot(t)
			runTransactionRecoveryCrashHelper(t, root, boundary)
			journal := transactionRecoveryExpectedJournal(t, root)
			if boundary != "restored-marker" {
				if err := filepath.Walk(journal, func(path string, info os.FileInfo, err error) error {
					if err != nil {
						return err
					}
					want := os.FileMode(0o600)
					if info.IsDir() {
						want = 0o700
					}
					if info.Mode().Perm() != want {
						t.Fatalf("recovery evidence mode for %s = %o, want %o", filepath.Base(path), info.Mode().Perm(), want)
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			recoverTransactionBeforeAdmission(t, root, false)
			assertTransactionRecoveryState(t, root, false)
			for _, name := range []string{"a.txt", "b.txt", provenanceV3Name} {
				assertTransactionTestProtection(t, filepath.Join(root, name), false)
			}
			if _, err := os.Lstat(filepath.Join(root, "new")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("created directory survived %s recovery: %v", boundary, err)
			}
			assertNoTransactionResidue(t, root)
		})
	}
}

func TestW33C1AUnixRecoveryFailsClosedOnDurabilityOrSymlinkRestorationRisk(t *testing.T) {
	for _, damage := range []string{"marker-durability", "resource-symlink"} {
		t.Run(damage, func(t *testing.T) {
			root := prepareTransactionRecoveryProcessRoot(t)
			runTransactionRecoveryCrashHelper(t, root, "after-write-a")
			journal := transactionRecoveryExpectedJournal(t, root)
			outside := prepareTransactionTestRoot(t)
			outsidePath := filepath.Join(outside, "victim")
			mustWriteTransactionTestFile(t, outsidePath, "outside")
			switch damage {
			case "marker-durability":
				path := filepath.Join(journal, "restored.prepare")
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
				secureTransactionTestDirectory(t, path)
			case "resource-symlink":
				if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outsidePath, filepath.Join(root, "a.txt")); err != nil {
					t.Fatal(err)
				}
			}

			_, err := applyTransactionFS(root, []transactionFSWrite{{Path: "admission.txt", Data: []byte("must-not-exist")}}, nil)
			assertTransactionResidualRisk(t, err, root, journal, outsidePath, "outside")
			assertTransactionTestFile(t, outsidePath, "outside")
			if _, statErr := os.Lstat(filepath.Join(root, "admission.txt")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("failed recovery admitted mutation: %v", statErr)
			}
			if _, statErr := os.Lstat(journal); statErr != nil {
				t.Fatalf("failed recovery removed evidence: %v", statErr)
			}
		})
	}
}

func TestW33C1AUnixDurabilityAndRecoverySourceOrdering(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate Unix recovery test source")
	}
	dir := filepath.Dir(testFile)
	unixSource, err := os.ReadFile(filepath.Join(dir, "transaction_fs_unix.go"))
	if err != nil {
		t.Fatal(err)
	}
	commonSource, err := os.ReadFile(filepath.Join(dir, "transaction_fs.go"))
	if err != nil {
		t.Fatal(err)
	}

	assertUnixRecoverySourceOrder(t, unixRecoveryFunction(t, string(unixSource), "func (p *unixTransactionFS) backup("),
		"out.ReadFrom(file)", "out.Sync()", "syncTransactionUnixDir(filepath.Dir(backupPath))")
	assertUnixRecoverySourceOrder(t, unixRecoveryFunction(t, string(unixSource), "func (p *unixTransactionFS) seal("),
		"writeTransactionUnixDurableFile", "p.writeMarkerAt", "os.Rename(p.prepare, p.journal)", "syncTransactionUnixDir(filepath.Dir(p.root))")
	assertUnixRecoverySourceOrder(t, unixRecoveryFunction(t, string(unixSource), "func (p *unixTransactionFS) atomicWrite("),
		"tmp.Write(data)", "tmp.Sync()", "os.Rename(name, path)", "syncTransactionUnixDir(filepath.Dir(path))")
	assertUnixRecoverySourceOrder(t, unixRecoveryFunction(t, string(unixSource), "func (p *unixTransactionFS) writeMarkerAt("),
		"writeTransactionUnixDurableFile(temp", "os.Rename(temp, path)", "syncTransactionUnixDir(root)")
	assertUnixRecoverySourceOrder(t, unixRecoveryFunction(t, string(unixSource), "func (p *unixTransactionFS) recover("),
		"p.removeResidue(p.cleanupRoot)", "p.removeResidue(p.prepare)", "p.removeMarkerTemps()", "p.readJournalHeader()",
		"p.removeTemps(states)", "p.restore(states[i])", "p.removeCreatedDirs()", "p.markRestored()", "p.cleanup()")
	assertUnixRecoverySourceOrder(t, unixRecoveryFunction(t, string(unixSource), "func (p *unixTransactionFS) cleanup("),
		"os.Rename(p.journal, p.cleanupRoot)", "syncTransactionUnixDir(filepath.Dir(p.root))", "p.removeResidue(p.cleanupRoot)")
	assertUnixRecoverySourceOrder(t, unixRecoveryFunction(t, string(commonSource), "func applyTransactionFSWithWait("),
		"acquireTargetGuardWithWaiter", "guard.Recover()", "newTransactionFSPlatform", "platform.recover()", "platform.begin()",
		"platform.backup", "platform.seal", "platform.write")
	assertUnixRecoverySourceOrder(t, unixRecoveryFunction(t, string(commonSource), "func (j *transactionFSJournal) Commit("),
		"j.platform.markCommitted()", "j.platform.cleanup()", "j.guard.Release()")
	assertUnixRecoverySourceOrder(t, unixRecoveryFunction(t, string(commonSource), "func (j *transactionFSJournal) rollback("),
		"j.platform.removeTemps", "j.platform.restore", "j.platform.removeCreatedDirs", "j.platform.markRestored", "j.platform.cleanup")

	root := prepareTransactionTestRoot(t)
	if err := writeTransactionUnixDurableFile(filepath.Join(root, "missing", "durable"), []byte("x")); err == nil {
		t.Fatal("durable file helper hid an unavailable parent failure")
	}
	if err := syncTransactionUnixDir(filepath.Join(root, "missing")); err == nil {
		t.Fatal("directory durability helper hid an unavailable directory failure")
	}
}

func unixRecoveryFunction(t *testing.T, source, signature string) string {
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

func assertUnixRecoverySourceOrder(t *testing.T, source string, fragments ...string) {
	t.Helper()
	position := -1
	for _, fragment := range fragments {
		next := strings.Index(source, fragment)
		if next < 0 || next <= position {
			t.Fatalf("durability/recovery order missing %q after offset %d", fragment, position)
		}
		position = next
		source = source[next+len(fragment):]
		position = -1
	}
}
