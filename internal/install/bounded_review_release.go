package install

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alferio94/lore-cli/internal/agentpack"
)

const boundedReviewReleaseRootRelativePath = "lore/judgment-review"

type boundedReviewReleaseManifest struct {
	Version      string                          `json:"version"`
	Revision     int                             `json:"revision"`
	JudgePath    string                          `json:"judge_path"`
	FixPath      string                          `json:"fix_path"`
	JudgeSHA256  string                          `json:"judge_sha256"`
	FixSHA256    string                          `json:"fix_sha256"`
	PairManifest agentpack.BoundedReviewManifest `json:"pair_manifest"`
}

type boundedReviewCurrent struct {
	Version      string `json:"version"`
	Revision     int    `json:"revision"`
	ManifestPath string `json:"manifest_path"`
}

type boundedReviewRelease struct {
	name     string
	root     string
	judge    []byte
	fix      []byte
	manifest []byte
	current  []byte
}

func boundedReviewPaths(layout PiLayout, version string, revision int) (root, release, current string, err error) {
	if strings.TrimSpace(version) == "" || revision < 1 || strings.Contains(version, "/") || strings.Contains(version, "\\") || version == "." || version == ".." {
		return "", "", "", fmt.Errorf("unsafe bounded review release identity")
	}
	root = filepath.Join(layout.AgentDir, filepath.FromSlash(boundedReviewReleaseRootRelativePath))
	name := fmt.Sprintf("%s-r%d", version, revision)
	release = filepath.Join(root, "releases", name)
	current = filepath.Join(root, "current.json")
	return root, release, current, nil
}

// ValidateBoundedReviewPair applies the installer-side compatibility gate before
// a pair can be projected or published.
func ValidateBoundedReviewPair(pair agentpack.BoundedReviewContractPair) error {
	return agentpack.ValidateBoundedReviewPair(pair)
}

// RenderBoundedReviewProjection renders deterministic, disabled Pi-only data.
func RenderBoundedReviewProjection(layout PiLayout) (boundedReviewRelease, error) {
	return renderBoundedReviewRelease(layout)
}

func renderBoundedReviewRelease(layout PiLayout) (boundedReviewRelease, error) {
	pair := agentpack.BoundedReviewPair()
	if err := agentpack.ValidateBoundedReviewPair(pair); err != nil {
		return boundedReviewRelease{}, fmt.Errorf("validate bounded review pair: %w", err)
	}
	root, releaseDir, _, err := boundedReviewPaths(layout, agentpack.BoundedReviewContractVersion, pair.Revision)
	if err != nil {
		return boundedReviewRelease{}, err
	}
	judge := []byte(pair.Judge.Body)
	fix := []byte(pair.Fix.Body)
	manifest := boundedReviewReleaseManifest{
		Version: agentpack.BoundedReviewContractVersion, Revision: pair.Revision,
		JudgePath: "judge.md", FixPath: "fix.md", JudgeSHA256: releaseSHA256(judge), FixSHA256: releaseSHA256(fix), PairManifest: pair.Manifest,
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return boundedReviewRelease{}, fmt.Errorf("encode bounded review release manifest: %w", err)
	}
	manifestBytes = append(manifestBytes, '\n')
	current := boundedReviewCurrent{Version: manifest.Version, Revision: manifest.Revision, ManifestPath: filepath.ToSlash(filepath.Join("releases", filepath.Base(releaseDir), "manifest.json"))}
	currentBytes, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return boundedReviewRelease{}, fmt.Errorf("encode bounded review current pointer: %w", err)
	}
	currentBytes = append(currentBytes, '\n')
	return boundedReviewRelease{name: filepath.Base(releaseDir), root: root, judge: judge, fix: fix, manifest: manifestBytes, current: currentBytes}, nil
}

func releaseSHA256(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func validateBoundedReviewReleaseDir(releaseDir string) error {
	manifestBytes, err := os.ReadFile(filepath.Join(releaseDir, "manifest.json"))
	if err != nil {
		return fmt.Errorf("read release manifest: %w", err)
	}
	var manifest boundedReviewReleaseManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return fmt.Errorf("decode release manifest: %w", err)
	}
	if manifest.Version != agentpack.BoundedReviewContractVersion || manifest.Revision < 1 || manifest.JudgePath != "judge.md" || manifest.FixPath != "fix.md" || filepath.IsAbs(manifest.JudgePath) || filepath.IsAbs(manifest.FixPath) {
		return fmt.Errorf("invalid bounded review release identity or paths")
	}
	judge, err := os.ReadFile(filepath.Join(releaseDir, manifest.JudgePath))
	if err != nil {
		return fmt.Errorf("read judge: %w", err)
	}
	fix, err := os.ReadFile(filepath.Join(releaseDir, manifest.FixPath))
	if err != nil {
		return fmt.Errorf("read fix: %w", err)
	}
	if releaseSHA256(judge) != manifest.JudgeSHA256 || releaseSHA256(fix) != manifest.FixSHA256 {
		return fmt.Errorf("bounded review release digest mismatch")
	}
	pair := agentpack.BoundedReviewPair()
	pair.Revision = manifest.Revision
	pair.Judge.Body, pair.Fix.Body = string(judge), string(fix)
	pair.Judge.Handshake.ManifestRevision, pair.Fix.Handshake.ManifestRevision = manifest.Revision, manifest.Revision
	pair.Manifest = manifest.PairManifest
	if err := agentpack.ValidateBoundedReviewPair(pair); err != nil {
		return fmt.Errorf("bounded review release pair: %w", err)
	}
	return nil
}

var boundedReviewAtomicWrite = writeFileAtomic

// applyBoundedReviewRelease stages and validates both roles before publishing the
// pointer. A directory rename is not a transaction; current.json is the sole
// compatibility marker and is written last. On failure it restores the prior
// pointer or leaves no pointer, so the portable fallback remains selectable.
// ApplyBoundedReviewRelease stages, validates, and publishes only a coherent
// Pi pair; it never writes runtime activation configuration.
func ApplyBoundedReviewRelease(layout PiLayout) error {
	return applyBoundedReviewRelease(layout)
}

func applyBoundedReviewRelease(layout PiLayout) error {
	release, err := renderBoundedReviewRelease(layout)
	if err != nil {
		return err
	}
	_, releaseDir, currentPath, err := boundedReviewPaths(layout, agentpack.BoundedReviewContractVersion, agentpack.BoundedReviewManifestRevision)
	if err != nil {
		return err
	}
	oldCurrent, oldErr := os.ReadFile(currentPath)
	if oldErr != nil && !os.IsNotExist(oldErr) {
		return fmt.Errorf("read existing bounded review pointer: %w", oldErr)
	}
	stageDir := filepath.Join(release.root, ".staging", release.name)
	_ = os.RemoveAll(stageDir) // stale, never-published staging is safe to reconcile.
	if err := boundedReviewAtomicWrite(filepath.Join(stageDir, "judge.md"), release.judge, 0o600); err != nil {
		return fmt.Errorf("stage judge: %w", err)
	}
	if err := boundedReviewAtomicWrite(filepath.Join(stageDir, "fix.md"), release.fix, 0o600); err != nil {
		_ = os.RemoveAll(stageDir)
		return fmt.Errorf("stage fix: %w", err)
	}
	if err := boundedReviewAtomicWrite(filepath.Join(stageDir, "manifest.json"), release.manifest, 0o600); err != nil {
		_ = os.RemoveAll(stageDir)
		return fmt.Errorf("stage manifest: %w", err)
	}
	if err := validateBoundedReviewReleaseDir(stageDir); err != nil {
		_ = os.RemoveAll(stageDir)
		return err
	}
	created := false
	if _, err := os.Stat(releaseDir); err == nil {
		if err := validateBoundedReviewReleaseDir(releaseDir); err != nil {
			_ = os.RemoveAll(stageDir)
			return fmt.Errorf("existing bounded review release is invalid: %w", err)
		}
		_ = os.RemoveAll(stageDir)
	} else if !os.IsNotExist(err) {
		_ = os.RemoveAll(stageDir)
		return fmt.Errorf("inspect release: %w", err)
	} else {
		if err := os.MkdirAll(filepath.Dir(releaseDir), 0o755); err != nil {
			_ = os.RemoveAll(stageDir)
			return fmt.Errorf("create release root: %w", err)
		}
		if err := os.Rename(stageDir, releaseDir); err != nil {
			_ = os.RemoveAll(stageDir)
			return fmt.Errorf("publish staged release: %w", err)
		}
		created = true
	}
	if err := boundedReviewAtomicWrite(currentPath, release.current, 0o600); err != nil {
		if oldErr == nil {
			_ = writeFileAtomic(currentPath, oldCurrent, 0o600)
		} else {
			_ = os.Remove(currentPath)
		}
		if created {
			_ = os.RemoveAll(releaseDir)
		}
		return fmt.Errorf("publish bounded review current pointer: %w", err)
	}
	return nil
}
