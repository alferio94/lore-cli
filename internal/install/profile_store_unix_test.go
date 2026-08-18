//go:build darwin || linux

package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestUnixProfileStoreCanonicalAliasesSharePhysicalIdentity(t *testing.T) {
	parent := privateDir(t)
	aliasRoot := t.TempDir()
	alias := filepath.Join(aliasRoot, "state-link")
	if err := os.Symlink(parent, alias); err != nil {
		t.Fatal(err)
	}
	platform := newUnixStorePlatform()
	paths := []string{
		filepath.Join(parent, "profiles.json"),
		filepath.Join(alias, ".", "profiles.json"),
	}
	canonical := make([]storePath, 0, len(paths))
	for _, path := range paths {
		got, err := platform.Canonical(path)
		if err != nil {
			t.Fatalf("Canonical(alias) error = %v", err)
		}
		canonical = append(canonical, got)
	}
	defer closeUnixPath(t, canonical[0])
	defer closeUnixPath(t, canonical[1])
	if canonical[0].identity != canonical[1].identity {
		t.Fatal("symlinked existing parent aliases produced different authority identities")
	}
}

func TestUnixProfileStoreCanonicalRejectsUnsafePaths(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*testing.T) string
	}{
		{"relative", func(t *testing.T) string { return "relative/profiles.json" }},
		{"traversal", func(t *testing.T) string {
			parent := privateDir(t)
			return parent + string(os.PathSeparator) + "child" + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "profiles.json"
		}},
		{"missing parent", func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing", "profiles.json") }},
		{"symlink state leaf", func(t *testing.T) string {
			parent := privateDir(t)
			target := filepath.Join(parent, "target")
			writePrivateFile(t, target)
			path := filepath.Join(parent, "profiles.json")
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
			return path
		}},
		{"hard linked state leaf", func(t *testing.T) string {
			parent := privateDir(t)
			target := filepath.Join(parent, "target")
			writePrivateFile(t, target)
			path := filepath.Join(parent, "profiles.json")
			if err := os.Link(target, path); err != nil {
				t.Fatal(err)
			}
			return path
		}},
		{"directory state leaf", func(t *testing.T) string {
			parent := privateDir(t)
			path := filepath.Join(parent, "profiles.json")
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			return path
		}},
		{"permissive parent", func(t *testing.T) string {
			parent := privateDir(t)
			if err := os.Chmod(parent, 0o755); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(parent, "profiles.json")
		}},
		{"permissive state", func(t *testing.T) string {
			parent := privateDir(t)
			path := filepath.Join(parent, "profiles.json")
			if err := os.WriteFile(path, []byte("state"), 0o644); err != nil {
				t.Fatal(err)
			}
			return path
		}},
		{"symlink authority leaf", func(t *testing.T) string {
			parent := privateDir(t)
			target := filepath.Join(parent, "target")
			writePrivateFile(t, target)
			path := filepath.Join(parent, "profiles.json")
			if err := os.Symlink(target, filepath.Join(parent, ".profiles.json.lock")); err != nil {
				t.Fatal(err)
			}
			return path
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := tc.setup(t)
			got, err := newUnixStorePlatform().Canonical(raw)
			if err == nil {
				closeUnixPath(t, got)
				t.Fatal("Canonical() accepted an unsafe path")
			}
			if !errors.Is(err, errStoreAuthorityInvalidPath) || strings.Contains(err.Error(), raw) {
				t.Fatalf("Canonical() error = %v, want fixed redacted invalid-path error", err)
			}
		})
	}
}

func TestUnixProfileStoreRejectsPathSwapsBeforeAuthority(t *testing.T) {
	t.Run("authority leaf swap", func(t *testing.T) {
		parent := privateDir(t)
		path := filepath.Join(parent, "profiles.json")
		platform := newUnixStorePlatform()
		canonical, err := platform.Canonical(path)
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(parent, "target")
		writePrivateFile(t, target)
		if err := os.Symlink(target, filepath.Join(parent, ".profiles.json.lock")); err != nil {
			t.Fatal(err)
		}
		if _, err := platform.Acquire(canonical, 0, &recordingWaiter{}); !errors.Is(err, errStoreAuthorityInvalidPath) {
			t.Fatalf("Acquire() after authority swap error = %v, want invalid path", err)
		}
	})

	t.Run("ancestor alias swap", func(t *testing.T) {
		first, second := privateDir(t), privateDir(t)
		aliasRoot := privateDir(t)
		alias := filepath.Join(aliasRoot, "state-link")
		if err := os.Symlink(first, alias); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(alias, "profiles.json")
		platform := newUnixStorePlatform()
		canonical, err := platform.Canonical(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(alias); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(second, alias); err != nil {
			t.Fatal(err)
		}
		if _, err := platform.Acquire(canonical, 0, &recordingWaiter{}); !errors.Is(err, errStoreAuthorityInvalidPath) {
			t.Fatalf("Acquire() after ancestor swap error = %v, want invalid path", err)
		}
	})
}

func TestUnixProfileStoreRejectsAncestorSwapWhileWaiting(t *testing.T) {
	firstParent, secondParent := privateDir(t), privateDir(t)
	writePrivateFile(t, filepath.Join(firstParent, "profiles.json"))
	aliasRoot := privateDir(t)
	alias := filepath.Join(aliasRoot, "state-link")
	if err := os.Symlink(firstParent, alias); err != nil {
		t.Fatal(err)
	}
	platform := newUnixStorePlatform()
	path := filepath.Join(alias, "profiles.json")
	ownerPath, _ := platform.Canonical(path)
	owner, err := platform.Acquire(ownerPath, 0, &recordingWaiter{})
	if err != nil {
		t.Fatal(err)
	}
	contenderPath, _ := platform.Canonical(path)
	waiter := &recordingWaiter{now: time.Unix(100, 0)}
	waiter.onSleep = func() {
		if err := owner.Release(); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(alias); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(secondParent, alias); err != nil {
			t.Fatal(err)
		}
		waiter.onSleep = nil
	}
	if _, err := platform.Acquire(contenderPath, defaultProfileStoreWait, waiter); !errors.Is(err, errStoreAuthorityInvalidPath) {
		t.Fatalf("Acquire() after wait-time ancestor swap error = %v, want invalid path", err)
	}
}

func TestUnixProfileStoreKernelAuthorityIsExclusiveBoundedAndReusable(t *testing.T) {
	path := filepath.Join(privateDir(t), "profiles.json")
	platform := newUnixStorePlatform()
	firstPath, err := platform.Canonical(path)
	if err != nil {
		t.Fatal(err)
	}
	first, err := platform.Acquire(firstPath, 0, &recordingWaiter{})
	if err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}

	busyPath, _ := platform.Canonical(path)
	if _, err := platform.Acquire(busyPath, 0, &recordingWaiter{}); !errors.Is(err, errStoreAuthorityBusy) {
		t.Fatalf("zero-wait Acquire() error = %v, want busy", err)
	}

	timedPath, _ := platform.Canonical(path)
	waiter := &recordingWaiter{now: time.Unix(100, 0)}
	if _, err := platform.Acquire(timedPath, defaultProfileStoreWait, waiter); !errors.Is(err, errStoreAuthorityTimeout) {
		t.Fatalf("bounded Acquire() error = %v, want timeout", err)
	}
	if elapsed := waiter.now.Sub(time.Unix(100, 0)); elapsed != defaultProfileStoreWait {
		t.Fatalf("timeout elapsed = %s, want %s", elapsed, defaultProfileStoreWait)
	}
	if len(waiter.sleeps) == 0 {
		t.Fatal("positive wait did not poll")
	}
	for _, sleep := range waiter.sleeps {
		if sleep <= 0 || sleep > 100*time.Millisecond {
			t.Fatalf("poll sleep = %s, want (0,100ms]", sleep)
		}
	}

	if err := first.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	reacquirePath, _ := platform.Canonical(path)
	reacquired, err := platform.Acquire(reacquirePath, 0, &recordingWaiter{})
	if err != nil {
		t.Fatalf("Acquire() after release error = %v", err)
	}
	if err := reacquired.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestUnixProfileStoreAuthorityUsesRestrictiveEmptyArtifactAndProtectsLiveOwner(t *testing.T) {
	parent := privateDir(t)
	path := filepath.Join(parent, "Bearer-secret-profiles.json")
	lockPath := filepath.Join(parent, ".Bearer-secret-profiles.json.lock")
	platform := newUnixStorePlatform()
	canonical, _ := platform.Canonical(path)
	owned, err := platform.Acquire(canonical, 0, &recordingWaiter{})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 || info.Size() != 0 || !info.Mode().IsRegular() {
		t.Fatalf("authority artifact = mode:%o size:%d type:%s", info.Mode().Perm(), info.Size(), info.Mode())
	}
	before := fileIdentity(t, lockPath)
	content, err := os.ReadFile(lockPath)
	if err != nil || len(content) != 0 {
		t.Fatalf("authority metadata = %q, %v; want empty", content, err)
	}
	contenderPath, _ := platform.Canonical(path)
	if _, err := platform.Acquire(contenderPath, 0, &recordingWaiter{}); !errors.Is(err, errStoreAuthorityBusy) {
		t.Fatalf("contender error = %v, want busy", err)
	}
	if after := fileIdentity(t, lockPath); after != before {
		t.Fatal("contender deleted or replaced a live authority artifact")
	}
	if err := owned.Release(); err != nil {
		t.Fatal(err)
	}

	// An absent-state sidecar is retained rather than unsafely unlinked while a
	// contender may hold an open descriptor. Kernel ownership, not residue age,
	// controls reuse.
	residue := fileIdentity(t, lockPath)
	reusePath, _ := platform.Canonical(path)
	reused, err := platform.Acquire(reusePath, 0, &recordingWaiter{})
	if err != nil {
		t.Fatalf("Acquire(residue) error = %v", err)
	}
	if fileIdentity(t, lockPath) != residue {
		t.Fatal("safe residue reuse replaced the authority inode")
	}
	if err := reused.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestUnixProfileStoreAuthorityCloseReleasesKernelOwnership(t *testing.T) {
	path := filepath.Join(privateDir(t), "profiles.json")
	platform := newUnixStorePlatform()
	canonical, _ := platform.Canonical(path)
	owned, err := platform.Acquire(canonical, 0, &recordingWaiter{})
	if err != nil {
		t.Fatal(err)
	}
	unixOwned, ok := owned.(*unixStoreAuthority)
	if !ok {
		t.Fatalf("authority type = %T", owned)
	}
	if err := unixOwned.closeWithoutUnlock(); err != nil {
		t.Fatalf("simulated process handle close = %v", err)
	}

	contenderPath, _ := platform.Canonical(path)
	contender, err := platform.Acquire(contenderPath, 0, &recordingWaiter{})
	if err != nil {
		t.Fatalf("Acquire() after kernel handle close error = %v", err)
	}
	if err := contender.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestUnixProfileStoreExistingStateInodeIsAuthority(t *testing.T) {
	parent := privateDir(t)
	path := filepath.Join(parent, "profiles.json")
	writePrivateFile(t, path)
	platform := newUnixStorePlatform()
	canonical, _ := platform.Canonical(path)
	owned, err := platform.Acquire(canonical, 0, &recordingWaiter{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(parent, ".profiles.json.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("existing state created sidecar: %v", err)
	}
	contenderPath, _ := platform.Canonical(path)
	if _, err := platform.Acquire(contenderPath, 0, &recordingWaiter{}); !errors.Is(err, errStoreAuthorityBusy) {
		t.Fatalf("independent state handle error = %v, want busy", err)
	}
	if err := owned.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestUnixProfileStoreCommitPublishesWhileLockedAndCleansAbsentSidecar(t *testing.T) {
	parent := privateDir(t)
	path := filepath.Join(parent, "profiles.json")
	platform := newUnixStorePlatform()
	canonical, _ := platform.Canonical(path)
	owned, err := platform.Acquire(canonical, 0, &recordingWaiter{})
	if err != nil {
		t.Fatal(err)
	}
	if err := platform.Commit(owned, nil, []byte("next-state")); err != nil {
		t.Fatalf("Commit(absent) = %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "next-state" {
		t.Fatalf("published state = %q, %v", got, err)
	}
	if mode := fileMode(t, path).Perm(); mode != 0o600 {
		t.Fatalf("state mode = %o", mode)
	}
	if _, err := os.Lstat(filepath.Join(parent, ".profiles.json.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("authority sidecar survived handoff: %v", err)
	}
	contenderPath, _ := platform.Canonical(path)
	if _, err := platform.Acquire(contenderPath, 0, &recordingWaiter{}); !errors.Is(err, errStoreAuthorityBusy) {
		t.Fatalf("new state inode was not authoritative: %v", err)
	}
	if err := owned.Release(); err != nil {
		t.Fatal(err)
	}
	retryPath, _ := platform.Canonical(path)
	retry, err := platform.Acquire(retryPath, 0, &recordingWaiter{})
	if err != nil {
		t.Fatal(err)
	}
	_ = retry.Release()
}

func TestUnixProfileStoreCommitFailuresRestorePriorBytesModeAndResidue(t *testing.T) {
	for _, stage := range []string{"write", "sync", "replace", "dirsync", "cleanup"} {
		t.Run(stage, func(t *testing.T) {
			parent := privateDir(t)
			path := filepath.Join(parent, "profiles.json")
			prior := []byte("prior-state")
			if err := os.WriteFile(path, prior, 0o600); err != nil {
				t.Fatal(err)
			}
			platform := unixStorePlatform{fail: func(got string) error {
				if got == stage {
					return errors.New("injected commit failure")
				}
				return nil
			}}
			canonical, _ := platform.Canonical(path)
			owned, err := platform.Acquire(canonical, 0, &recordingWaiter{})
			if err != nil {
				t.Fatal(err)
			}
			if err := platform.Commit(owned, prior, []byte("next-state")); err == nil {
				t.Fatal("Commit unexpectedly succeeded")
			}
			if got, err := os.ReadFile(path); err != nil || string(got) != string(prior) {
				t.Fatalf("restored state = %q, %v", got, err)
			}
			if mode := fileMode(t, path).Perm(); mode != 0o600 {
				t.Fatalf("restored mode = %o", mode)
			}
			if matches, _ := filepath.Glob(filepath.Join(parent, ".profiles-*.tmp")); len(matches) != 0 {
				t.Fatalf("temporary residue = %v", matches)
			}
			if err := owned.Release(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestUnixProfileStoreCommitReportsFailedRollback(t *testing.T) {
	parent := privateDir(t)
	path := filepath.Join(parent, "profiles.json")
	prior := []byte("prior-state")
	if err := os.WriteFile(path, prior, 0o600); err != nil {
		t.Fatal(err)
	}
	platform := unixStorePlatform{fail: func(stage string) error {
		if stage == "dirsync" || stage == "restore" {
			return errors.New("injected failure")
		}
		return nil
	}}
	canonical, _ := platform.Canonical(path)
	owned, _ := platform.Acquire(canonical, 0, &recordingWaiter{})
	err := platform.Commit(owned, prior, []byte("next-state"))
	if !errors.Is(err, errStoreRollbackFailed) {
		t.Fatalf("Commit rollback error = %v", err)
	}
	_ = owned.Release()
}

type recordingWaiter struct {
	now     time.Time
	sleeps  []time.Duration
	onSleep func()
}

func (w *recordingWaiter) Now() time.Time { return w.now }
func (w *recordingWaiter) Sleep(d time.Duration) {
	w.sleeps = append(w.sleeps, d)
	w.now = w.now.Add(d)
	if w.onSleep != nil {
		w.onSleep()
	}
}

func privateDir(t *testing.T) string {
	t.Helper()
	path := t.TempDir()
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func writePrivateFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("state"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fileIdentity(t *testing.T, path string) string {
	t.Helper()
	var stat unix.Stat_t
	if err := unix.Lstat(path, &stat); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
}

func closeUnixPath(t *testing.T, path storePath) {
	t.Helper()
	unixPath, ok := path.platform.(*unixStorePath)
	if !ok {
		t.Fatalf("platform path type = %T", path.platform)
	}
	if err := unixPath.parent.Close(); err != nil {
		t.Fatal(err)
	}
}
