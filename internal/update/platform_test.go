package update

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alferio94/lore-cli/internal/version"
)

func TestResolveBinaryTargetRefusesPathMismatch(t *testing.T) {
	t.Parallel()

	targetDir := t.TempDir()
	execPath := filepath.Join(targetDir, "lore")
	pathPath := filepath.Join(targetDir, "bin", "lore")

	target, err := resolveBinaryTarget(resolveTargetOptions{
		ExecPath: execPath,
		LookPath: pathPath,
	})
	if err != nil {
		t.Fatalf("resolveBinaryTarget() error = %v", err)
	}
	if got, want := target.Status, TargetStatusUnsafe; got != want {
		t.Fatalf("target.Status = %q, want %q", got, want)
	}
	if got, want := target.Reason, ReasonPathMismatch; got != want {
		t.Fatalf("target.Reason = %q, want %q", got, want)
	}
}

func TestResolveBinaryTargetRefusesSymlinkedExecutable(t *testing.T) {
	t.Parallel()

	targetDir := t.TempDir()
	realPath := filepath.Join(targetDir, "real", "lore")
	symlinkPath := filepath.Join(targetDir, "link", "lore")
	if err := os.MkdirAll(filepath.Dir(realPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(real) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(symlinkPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(link) error = %v", err)
	}
	if err := os.WriteFile(realPath, []byte("binary"), 0o755); err != nil {
		t.Fatalf("WriteFile(real) error = %v", err)
	}
	if err := os.Symlink(realPath, symlinkPath); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	target, err := resolveBinaryTarget(resolveTargetOptions{
		ExecPath: symlinkPath,
		LookPath: symlinkPath,
	})
	if err != nil {
		t.Fatalf("resolveBinaryTarget() error = %v", err)
	}
	if got, want := target.Status, TargetStatusUnsafe; got != want {
		t.Fatalf("target.Status = %q, want %q", got, want)
	}
	if got, want := target.Reason, ReasonSymlinkedTarget; got != want {
		t.Fatalf("target.Reason = %q, want %q", got, want)
	}
	if got, want := target.ResolvedPath, realPath; got != want {
		t.Fatalf("target.ResolvedPath = %q, want %q", got, want)
	}
}

func TestApplyReturnsManualGuidanceOnWindows(t *testing.T) {
	t.Parallel()

	svc := Service{
		Now:       func() time.Time { return time.Date(2026, 5, 20, 22, 0, 0, 0, time.UTC) },
		ExecPath:  func() (string, error) { return `C:\\Lore\\lore.exe`, nil },
		LookPath:  func(string) (string, error) { return `C:\\Lore\\lore.exe`, nil },
		ConfigDir: func() (string, error) { return t.TempDir(), nil },
		GOOS:      "windows",
		GOARCH:    "amd64",
		BuildInfo: version.Info{Version: "v0.2.5"},
	}

	result, err := svc.Apply(context.Background(), Plan{
		Status:    StatusUnsupported,
		LatestTag: "v0.2.6",
		Target: BinaryTarget{
			ExecutablePath: `C:\\Lore\\lore.exe`,
			ResolvedPath:   `C:\\Lore\\lore.exe`,
			PathPath:       `C:\\Lore\\lore.exe`,
			GOOS:           "windows",
		},
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if got, want := result.Status, ResultStatusUnsupported; got != want {
		t.Fatalf("result.Status = %q, want %q", got, want)
	}
	if got := result.ManualRecovery; got == "" {
		t.Fatal("result.ManualRecovery = empty, want actionable Windows guidance")
	}
}
