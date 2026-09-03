package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alferio94/lore-cli/internal/agentpack"
)

func TestBoundedReviewRegenerationIsExplicitAndPublishesCoherentPairBeforeManifest(t *testing.T) {
	home := t.TempDir()
	layout := ResolvePiLayout(home)
	prior := boundedReviewPiRequest(home, time.Date(2026, 7, 31, 19, 59, 0, 0, time.UTC))
	prior.Components = []ComponentID{ComponentCorePack, ComponentLoreServerMCP, ComponentContext7MCP, ComponentExtendedSkills}
	if _, err := (Service{}).InstallPi(prior); err != nil {
		t.Fatalf("install prior non-projection version: %v", err)
	}
	legacyManifest, err := os.ReadFile(layout.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	// Planning is read-only: an existing installation without the pair gains no
	// projection until the user explicitly executes reinstall/regeneration.
	plan, err := Service{}.PlanPiInstall(boundedReviewPiRequest(home, time.Date(2026, 7, 31, 20, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(layout.AgentDir, boundedReviewReleaseRootRelativePath, "current.json")); !os.IsNotExist(err) {
		t.Fatalf("plan wrote a pair or pointer: %v", err)
	}
	if got, err := os.ReadFile(layout.ManifestPath); err != nil || string(got) != string(legacyManifest) {
		t.Fatalf("plan changed existing manifest: %q err=%v", got, err)
	}
	if got, want := len(plan.BoundedReviewPaths), 4; got != want {
		t.Fatalf("plan bounded-review paths=%v, want deterministic pair+manifest+pointer", plan.BoundedReviewPaths)
	}

	result, err := (Service{}).ExecutePiInstall(plan, InstallCommandOptions{AssumeYes: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Manifest.BoundedReviewRelease == nil || result.Manifest.BoundedReviewRelease.Status != boundedReviewReleaseStatusCompatibleCandidateStaged || result.Manifest.BoundedReviewRelease.RuntimeActive {
		t.Fatalf("reinstall published activation or omitted staged state: %+v", result.Manifest.BoundedReviewRelease)
	}
	_, releaseDir, currentPath, _ := boundedReviewPaths(layout, agentpack.BoundedReviewContractVersion, agentpack.BoundedReviewManifestRevision)
	if err := validateBoundedReviewReleaseDir(releaseDir); err != nil {
		t.Fatalf("fresh regenerated release is incoherent: %v", err)
	}
	if _, err := os.Stat(filepath.Join(layout.AgentDir, "judgment-review.json")); !os.IsNotExist(err) {
		t.Fatalf("release presence created runtime config: %v", err)
	}
	if _, err := os.Stat(currentPath); err != nil {
		t.Fatalf("coherent release did not publish pointer last: %v", err)
	}
}

func TestBoundedReviewUpgradeBackupAndRollbackKeepPairAndManifestCoherent(t *testing.T) {
	home := t.TempDir()
	firstNow := time.Date(2026, 7, 31, 20, 1, 0, 0, time.UTC)
	if _, err := (Service{}).InstallPi(boundedReviewPiRequest(home, firstNow)); err != nil {
		t.Fatalf("initial install: %v", err)
	}
	layout := ResolvePiLayout(home)
	_, releaseDir, currentPath, _ := boundedReviewPaths(layout, agentpack.BoundedReviewContractVersion, agentpack.BoundedReviewManifestRevision)
	paths := []string{filepath.Join(releaseDir, "judge.md"), filepath.Join(releaseDir, "fix.md"), filepath.Join(releaseDir, "manifest.json"), currentPath, layout.ManifestPath}
	before := readBoundedReviewFiles(t, paths)

	plan, err := (Service{}).PlanPiInstall(boundedReviewPiRequest(home, firstNow.Add(time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	result, err := (Service{}).ExecutePiInstall(plan, InstallCommandOptions{AssumeYes: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.FullBackup == nil {
		t.Fatal("upgrade did not create a full rollback backup")
	}
	for path, want := range before {
		rel, err := filepath.Rel(filepath.Join(home, ".pi"), path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(result.FullBackup.BackupPath, rel))
		if err != nil || string(got) != want {
			t.Fatalf("rollback backup %s = %q err=%v, want coherent prior file", rel, got, err)
		}
	}
	if err := validateBoundedReviewReleaseDir(releaseDir); err != nil {
		t.Fatalf("idempotent upgrade left incoherent pair: %v", err)
	}
	loaded, err := LoadManifest(layout.ManifestPath)
	if err != nil || loaded.BoundedReviewRelease == nil || loaded.BoundedReviewRelease.RuntimeActive {
		t.Fatalf("upgrade manifest is not staged/fallback-safe: %+v err=%v", loaded.BoundedReviewRelease, err)
	}
}

func TestBoundedReviewInterruptedOrStaleUpgradeKeepsFallbackAuthoritative(t *testing.T) {
	layout := ResolvePiLayout(t.TempDir())
	if err := ApplyBoundedReviewRelease(layout); err != nil {
		t.Fatal(err)
	}
	_, releaseDir, currentPath, _ := boundedReviewPaths(layout, agentpack.BoundedReviewContractVersion, agentpack.BoundedReviewManifestRevision)
	oldPointer, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(layout.AgentDir, boundedReviewReleaseRootRelativePath, ".staging", "stale-r9", "judge.md"), []byte("partial"), 0o600); err == nil {
		t.Fatal("stale staging unexpectedly wrote without its parent directory")
	}
	if err := os.MkdirAll(filepath.Join(layout.AgentDir, boundedReviewReleaseRootRelativePath, ".staging", "stale-r9"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(layout.AgentDir, boundedReviewReleaseRootRelativePath, ".staging", "stale-r9", "judge.md"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ApplyBoundedReviewRelease(layout); err != nil {
		t.Fatalf("stale staging prevented safe regeneration: %v", err)
	}
	if got, _ := os.ReadFile(currentPath); string(got) != string(oldPointer) {
		t.Fatal("same-version regeneration changed current pointer")
	}
	if err := os.Remove(filepath.Join(releaseDir, "fix.md")); err != nil {
		t.Fatal(err)
	}
	if err := validateBoundedReviewReleaseDir(releaseDir); err == nil {
		t.Fatal("mixed Judge/Fix release was accepted")
	}
	// A malformed current selection is never a native activation signal; absent
	// discovery keeps the self-contained fallback authoritative.
	if strings.TrimSpace(string(oldPointer)) == "" {
		t.Fatal("test fixture lost prior pointer")
	}
}

func boundedReviewPiRequest(home string, now time.Time) PiInstallRequest {
	return PiInstallRequest{HomeDir: home, ServerURL: "https://lore.example", LoreBinaryPath: "/usr/local/bin/lore", LoreConfigDir: filepath.Join(home, ".lore"), LoreCLIVersion: "vtest", SavedToken: "test-token", Now: now}
}

func readBoundedReviewFiles(t *testing.T, paths []string) map[string]string {
	t.Helper()
	out := make(map[string]string, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		out[path] = string(data)
	}
	return out
}
