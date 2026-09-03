package install

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/agentpack"
)

func TestBoundedReviewProjectionIsolatedToPiPathSetAndInactive(t *testing.T) {
	layout := ResolvePiLayout(t.TempDir())
	release, err := RenderBoundedReviewProjection(layout)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyBoundedReviewRelease(layout); err != nil {
		t.Fatal(err)
	}
	_, releaseDir, currentPath, err := boundedReviewPaths(layout, agentpack.BoundedReviewContractVersion, agentpack.BoundedReviewManifestRevision)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(releaseDir, filepath.Join(layout.AgentDir, "lore", "judgment-review")) || !strings.HasPrefix(currentPath, filepath.Join(layout.AgentDir, "lore", "judgment-review")) {
		t.Fatalf("Pi projection escaped its scoped layout: release=%q current=%q", releaseDir, currentPath)
	}
	if strings.Contains(string(release.judge)+string(release.fix)+string(release.manifest)+string(release.current), "/Users/") {
		t.Fatal("Pi projection leaked a machine path")
	}
	var current boundedReviewCurrent
	if err := json.Unmarshal(release.current, &current); err != nil {
		t.Fatal(err)
	}
	if current.ManifestPath != "releases/1.0.0-r1/manifest.json" {
		t.Fatalf("current pointer = %+v, want deterministic relative manifest path", current)
	}
	if _, err := filepath.Abs(current.ManifestPath); err != nil { // retain an explicit path construction smoke check
		t.Fatal(err)
	}
	if got := filepath.Clean(current.ManifestPath); filepath.IsAbs(got) || strings.HasPrefix(got, "..") {
		t.Fatalf("current manifest path is unsafe: %q", current.ManifestPath)
	}
	if containsComponent(DefaultComponentSelection(TargetPi), ComponentBoundedReviewProjection) == false {
		t.Fatal("Pi default does not select its staged projection")
	}
	for _, target := range []TargetID{TargetOpenCode, TargetCodex, TargetAntigravity} {
		if containsComponent(DefaultComponentSelection(target), ComponentBoundedReviewProjection) {
			t.Fatalf("%s default selected Pi-only projection", target)
		}
	}
}

func TestNonPiRenderersHaveNoBoundedReviewAssetsOrNativeClaims(t *testing.T) {
	registry, err := defaultInstallRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []TargetID{TargetOpenCode, TargetCodex, TargetAntigravity} {
		t.Run(string(target), func(t *testing.T) {
			adapter, err := registry.Resolve(target)
			if err != nil {
				t.Fatal(err)
			}
			if adapter.Supports(ComponentBoundedReviewProjection) {
				t.Fatalf("%s advertises Pi-only projection", target)
			}
			files, err := adapter.Render(context.Background(), RenderRequest{
				Target: target, Definition: agentpack.DefaultDefinition(), Components: []ComponentID{ComponentCorePack},
			})
			if err != nil {
				t.Fatal(err)
			}
			seen := map[string]bool{}
			for _, file := range files {
				if seen[file.RelativePath] {
					t.Fatalf("duplicate rendered path %q", file.RelativePath)
				}
				seen[file.RelativePath] = true
				body := string(file.Content)
				for _, forbidden := range []string{"lore/judgment-review", "lore.judgment-day.bounded-review", "compatible-candidate-staged", "native lifecycle", "native activation"} {
					if strings.Contains(file.RelativePath, forbidden) || strings.Contains(body, forbidden) {
						t.Fatalf("%s rendered Pi-only lifecycle claim/path %q in %s", target, forbidden, file.RelativePath)
					}
				}
			}
		})
	}
}
