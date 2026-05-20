package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPlanPiInstallBackupDestinationOutsidePiDir(t *testing.T) {
	homeDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(homeDir, ".pi", "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll ~/.pi: %v", err)
	}

	configDir := filepath.Join(homeDir, ".lore")
	now := time.Date(2026, 5, 19, 1, 0, 0, 0, time.UTC)
	plan, err := Service{}.PlanPiInstall(PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  configDir,
		LoreCLIVersion: "v1.2.3",
		Now:            now,
	})
	if err != nil {
		t.Fatalf("PlanPiInstall error: %v", err)
	}

	piRoot := filepath.Join(homeDir, ".pi")
	if plan.FullBackup == nil {
		t.Fatal("FullBackup = nil, want scheduled full backup for existing ~/.pi")
	}
	if plan.FullBackup.BackupPath == piRoot || strings.HasPrefix(plan.FullBackup.BackupPath, piRoot+string(os.PathSeparator)) {
		t.Fatalf("FullBackup.BackupPath = %q, want path outside %q", plan.FullBackup.BackupPath, piRoot)
	}
	wantPrefix := filepath.Join(configDir, "backups", "pi") + string(os.PathSeparator)
	if !strings.HasPrefix(plan.FullBackup.BackupPath, wantPrefix) {
		t.Fatalf("FullBackup.BackupPath = %q, want path under %q", plan.FullBackup.BackupPath, filepath.Join(configDir, "backups", "pi"))
	}
	if got, want := plan.FullBackup.ManifestPath, filepath.Join(plan.FullBackup.BackupPath, "lore-pi-backup.json"); got != want {
		t.Fatalf("FullBackup.ManifestPath = %q, want %q", got, want)
	}
}

func TestExecutePiInstallCreatesFullBackupTree(t *testing.T) {
	homeDir := t.TempDir()
	piRoot := filepath.Join(homeDir, ".pi")
	if err := os.MkdirAll(filepath.Join(piRoot, "nested", "deeper"), 0o755); err != nil {
		t.Fatalf("MkdirAll nested pi tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(piRoot, "nested", "deeper", "marker.txt"), []byte("existing"), 0o644); err != nil {
		t.Fatalf("WriteFile marker.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(piRoot, "root.txt"), []byte("root"), 0o644); err != nil {
		t.Fatalf("WriteFile root.txt: %v", err)
	}

	plan, err := Service{}.PlanPiInstall(PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Now:            time.Date(2026, 5, 19, 1, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("PlanPiInstall error: %v", err)
	}

	result, err := (Service{}).ExecutePiInstall(plan, InstallCommandOptions{AssumeYes: true})
	if err != nil {
		t.Fatalf("ExecutePiInstall error: %v", err)
	}

	for relativePath, want := range map[string]string{
		filepath.Join("nested", "deeper", "marker.txt"): "existing",
		"root.txt": "root",
	} {
		gotBytes, err := os.ReadFile(filepath.Join(plan.FullBackup.BackupPath, relativePath))
		if err != nil {
			t.Fatalf("ReadFile backup %s: %v", relativePath, err)
		}
		if got := string(gotBytes); got != want {
			t.Fatalf("backup %s = %q, want %q", relativePath, got, want)
		}
	}
	manifestBytes, err := os.ReadFile(plan.FullBackup.ManifestPath)
	if err != nil {
		t.Fatalf("ReadFile full backup manifest: %v", err)
	}
	var backupManifest FullPiBackupResult
	if err := json.Unmarshal(manifestBytes, &backupManifest); err != nil {
		t.Fatalf("Unmarshal full backup manifest: %v", err)
	}
	if backupManifest.BackupPath != plan.FullBackup.BackupPath || backupManifest.ManifestPath != plan.FullBackup.ManifestPath {
		t.Fatalf("backup manifest = %+v, want paths for scheduled backup", backupManifest)
	}
	if backupManifest.SourcePath != piRoot || backupManifest.EntriesCopied < 3 {
		t.Fatalf("backup manifest = %+v, want source path and copied entry counts", backupManifest)
	}
	if result.FullBackup == nil || result.Manifest.FullPiBackup == nil {
		t.Fatalf("result full backup = %+v manifest full backup = %+v, want persisted backup metadata", result.FullBackup, result.Manifest.FullPiBackup)
	}
}

func TestPlanPiInstallFailsClosedOnTopLevelUnexpectedPiType(t *testing.T) {
	homeDir := t.TempDir()
	piRoot := filepath.Join(homeDir, ".pi")
	if err := os.WriteFile(piRoot, []byte("not-a-dir"), 0o600); err != nil {
		t.Fatalf("WriteFile ~/.pi: %v", err)
	}

	_, err := Service{}.PlanPiInstall(PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Now:            time.Date(2026, 5, 19, 1, 45, 0, 0, time.UTC),
	})
	if err == nil || !containsAll(strings.ToLower(err.Error()), "refusing", ".pi", "file") {
		t.Fatalf("PlanPiInstall error = %v, want fail-closed actionable top-level type guidance", err)
	}
}

func TestExecutePiInstallPreservesSymlinksInFullBackup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics vary on Windows")
	}

	homeDir := t.TempDir()
	piRoot := filepath.Join(homeDir, ".pi")
	if err := os.MkdirAll(filepath.Join(piRoot, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll nested pi tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(piRoot, "nested", "marker.txt"), []byte("existing"), 0o644); err != nil {
		t.Fatalf("WriteFile marker.txt: %v", err)
	}
	if err := os.Symlink(filepath.Join("nested", "marker.txt"), filepath.Join(piRoot, "current")); err != nil {
		t.Fatalf("Symlink current: %v", err)
	}

	plan, err := Service{}.PlanPiInstall(PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Now:            time.Date(2026, 5, 19, 2, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("PlanPiInstall error: %v", err)
	}

	if _, err := (Service{}).ExecutePiInstall(plan, InstallCommandOptions{AssumeYes: true}); err != nil {
		t.Fatalf("ExecutePiInstall error: %v", err)
	}

	backupLink := filepath.Join(plan.FullBackup.BackupPath, "current")
	info, err := os.Lstat(backupLink)
	if err != nil {
		t.Fatalf("Lstat backup symlink: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("backup mode = %v, want symlink", info.Mode())
	}
	if got, err := os.Readlink(backupLink); err != nil {
		t.Fatalf("Readlink backup symlink: %v", err)
	} else if want := filepath.Join("nested", "marker.txt"); got != want {
		t.Fatalf("backup symlink target = %q, want %q", got, want)
	}
}

func TestExecutePiInstallFailsClosedOnUnreadablePiEntry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics vary on Windows")
	}

	homeDir := t.TempDir()
	piRoot := filepath.Join(homeDir, ".pi")
	blockedPath := filepath.Join(piRoot, "private")
	if err := os.MkdirAll(blockedPath, 0o755); err != nil {
		t.Fatalf("MkdirAll blocked path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blockedPath, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("WriteFile secret.txt: %v", err)
	}
	if err := os.Chmod(blockedPath, 0o000); err != nil {
		t.Fatalf("Chmod blocked path: %v", err)
	}
	defer func() {
		_ = os.Chmod(blockedPath, 0o755)
	}()

	plan, err := Service{}.PlanPiInstall(PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Now:            time.Date(2026, 5, 19, 2, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("PlanPiInstall error: %v", err)
	}

	_, err = (Service{}).ExecutePiInstall(plan, InstallCommandOptions{AssumeYes: true})
	if err == nil || !containsAll(strings.ToLower(err.Error()), "private", "backup") {
		t.Fatalf("ExecutePiInstall error = %v, want backup failure mentioning blocked source path", err)
	}

	layout := ResolvePiLayout(homeDir)
	if _, statErr := os.Stat(layout.ManifestPath); !os.IsNotExist(statErr) {
		t.Fatalf("ManifestPath stat error = %v, want no writes after backup failure", statErr)
	}
	if _, statErr := os.Stat(layout.SettingsPath); !os.IsNotExist(statErr) {
		t.Fatalf("SettingsPath stat error = %v, want no managed writes after backup failure", statErr)
	}
}
