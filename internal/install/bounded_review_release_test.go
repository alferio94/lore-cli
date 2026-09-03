package install

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/agentpack"
)

func TestBoundedReviewReleaseFreshInstallAndIdempotency(t *testing.T) {
	layout := ResolvePiLayout(t.TempDir())
	if err := applyBoundedReviewRelease(layout); err != nil {
		t.Fatal(err)
	}
	_, releaseDir, currentPath, err := boundedReviewPaths(layout, agentpack.BoundedReviewContractVersion, agentpack.BoundedReviewManifestRevision)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateBoundedReviewReleaseDir(releaseDir); err != nil {
		t.Fatalf("release invalid: %v", err)
	}
	before, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyBoundedReviewRelease(layout); err != nil {
		t.Fatalf("idempotent reinstall: %v", err)
	}
	after, _ := os.ReadFile(currentPath)
	if string(before) != string(after) {
		t.Fatal("current pointer changed on same-version reinstall")
	}
	if _, err := os.Stat(filepath.Join(layout.AgentDir, "judgment-review.json")); !os.IsNotExist(err) {
		t.Fatalf("runtime activation config unexpectedly written: %v", err)
	}
}

func TestBoundedReviewReleaseRejectsIncompleteMixedOrTamperedPair(t *testing.T) {
	layout := ResolvePiLayout(t.TempDir())
	if err := applyBoundedReviewRelease(layout); err != nil {
		t.Fatal(err)
	}
	_, releaseDir, currentPath, _ := boundedReviewPaths(layout, agentpack.BoundedReviewContractVersion, agentpack.BoundedReviewManifestRevision)
	old, _ := os.ReadFile(currentPath)
	if err := os.Remove(filepath.Join(releaseDir, "fix.md")); err != nil {
		t.Fatal(err)
	}
	if err := applyBoundedReviewRelease(layout); err == nil || !strings.Contains(err.Error(), "existing bounded review release is invalid") {
		t.Fatalf("mixed/incomplete reinstall err=%v, want failure", err)
	}
	got, _ := os.ReadFile(currentPath)
	if string(got) != string(old) {
		t.Fatal("invalid replacement changed current pointer")
	}
}

func TestBoundedReviewReleaseStageAndPointerFailureRollBack(t *testing.T) {
	layout := ResolvePiLayout(t.TempDir())
	root, _, currentPath, _ := boundedReviewPaths(layout, agentpack.BoundedReviewContractVersion, agentpack.BoundedReviewManifestRevision)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	old := []byte(`{"version":"old","revision":9,"manifest_path":"releases/old-r9/manifest.json"}` + "\n")
	if err := writeFileAtomic(currentPath, old, 0o600); err != nil {
		t.Fatal(err)
	}
	original := boundedReviewAtomicWrite
	defer func() { boundedReviewAtomicWrite = original }()
	calls := 0
	boundedReviewAtomicWrite = func(path string, data []byte, mode os.FileMode) error {
		calls++
		if calls == 2 {
			return errors.New("interrupted stage")
		}
		return original(path, data, mode)
	}
	if err := applyBoundedReviewRelease(layout); err == nil || !strings.Contains(err.Error(), "stage fix") {
		t.Fatalf("stage failure err=%v", err)
	}
	if _, err := os.Stat(currentPath); err != nil {
		t.Fatalf("stage failure removed old pointer: %v", err)
	}
	calls = 0
	boundedReviewAtomicWrite = func(path string, data []byte, mode os.FileMode) error {
		calls++
		if calls == 3 {
			return errors.New("manifest write failure")
		}
		return original(path, data, mode)
	}
	if err := applyBoundedReviewRelease(layout); err == nil || !strings.Contains(err.Error(), "stage manifest") {
		t.Fatalf("manifest-stage failure err=%v", err)
	}
	calls = 0
	boundedReviewAtomicWrite = func(path string, data []byte, mode os.FileMode) error {
		calls++
		if calls == 4 {
			return errors.New("pointer write failure")
		}
		return original(path, data, mode)
	}
	if err := applyBoundedReviewRelease(layout); err == nil || !strings.Contains(err.Error(), "current pointer") {
		t.Fatalf("pointer failure err=%v", err)
	}
	got, _ := os.ReadFile(currentPath)
	if string(got) != string(old) {
		t.Fatalf("pointer rollback=%q, want old pointer", got)
	}
}

func TestBoundedReviewReleaseManifestAndFixtureAreDeterministic(t *testing.T) {
	layout := ResolvePiLayout(t.TempDir())
	release, err := renderBoundedReviewRelease(layout)
	if err != nil {
		t.Fatal(err)
	}
	var manifest boundedReviewReleaseManifest
	if err := json.Unmarshal(release.manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.PairManifest.Roles[0].Role != agentpack.BoundedReviewJudgeRole || manifest.PairManifest.Roles[1].Role != agentpack.BoundedReviewFixRole {
		t.Fatal("manifest does not contain one canonical judge/fix pair")
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "bounded_review_release.json"))
	if err != nil {
		t.Fatal(err)
	}
	var releaseFixture struct {
		JudgeBodyBytes  int    `json:"judge_body_bytes"`
		JudgeBodySHA256 string `json:"judge_body_sha256"`
		FixBodyBytes    int    `json:"fix_body_bytes"`
		FixBodySHA256   string `json:"fix_body_sha256"`
	}
	if err := json.Unmarshal(fixture, &releaseFixture); err != nil {
		t.Fatalf("parse release fixture: %v", err)
	}
	pair := agentpack.BoundedReviewPair()
	if !strings.Contains(string(fixture), agentpack.BoundedReviewStaticContractDigest()) || !strings.Contains(string(fixture), "judgment-day-static/v1") ||
		releaseFixture.JudgeBodyBytes != len(pair.Judge.Body) || releaseFixture.FixBodyBytes != len(pair.Fix.Body) ||
		releaseFixture.JudgeBodySHA256 != releaseSHA256([]byte(pair.Judge.Body)) || releaseFixture.FixBodySHA256 != releaseSHA256([]byte(pair.Fix.Body)) {
		t.Fatal("release fixture body bytes or hashes drift")
	}
	for _, bad := range []string{"../x", "/absolute", `C:\\x`} {
		if _, _, _, err := boundedReviewPaths(layout, bad, 1); err == nil {
			t.Fatalf("unsafe path %q accepted", bad)
		}
	}
}

func TestBoundedReviewProjectionIsPiOnly(t *testing.T) {
	for _, target := range []TargetID{TargetOpenCode, TargetCodex, TargetAntigravity, TargetClaudeCode} {
		if containsComponent(DefaultComponentSelection(target), ComponentBoundedReviewProjection) {
			t.Fatalf("%s default unexpectedly contains Pi projection", target)
		}
	}
}
