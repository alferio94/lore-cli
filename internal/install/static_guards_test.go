package install

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/agentpack"
)

// TestNoActiveSourceTeachesStalePiEnvelopeContract is a focused static guard for the
// spec invariant: no active (non-test) Go file under `internal/` may teach the old
// Pi delegation envelope contract. Forbidden patterns include the old `next` field,
// the old `running` final-status wording, the old SDD key list with `next`, the old
// worker key list with `next`, and the old explicit `next` field. Test files are
// excluded because they may legitimately mention these patterns in negative regression
// assertions or historical references.
func TestNoActiveSourceTeachesStalePiEnvelopeContract(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	// Walk only internal/ — the same scope as the OpenCode guard.
	absRoot := filepath.Join(repoRoot, "internal")
	_ = filepath.WalkDir(absRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := filepath.Base(path)
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		if strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(repoRoot, path)
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(content)

		// Forbidden canonical-field patterns (the old envelope shape with `next`).
		forbidden := []struct {
			name    string
			pattern string
		}{
			{"old next-only worker key list", "exactly these keys: `status`, `summary`, `artifacts`, `next`, "},
			{"old next+continuation worker key list", "exactly these keys: `status`, `summary`, `artifacts`, `next`, `continuation`"},
			{"old SDD envelope with next", "envelope with keys `status`, `phase`, `summary`, `artifacts`, `next`"},
			{"old status-with-running pattern", "`status`: `completed` | `running` | `needs_user_input` | `failed`"},
			{"old final-status-with-running pattern", "Final output status must be one of: `completed`, `running`, `needs_user_input`, `failed`"},
		}
		for _, f := range forbidden {
			if strings.Contains(text, f.pattern) {
				t.Fatalf("active non-test source %s contains forbidden %s pattern: %q", rel, f.name, f.pattern)
			}
		}
		return nil
	})
}

// TestNoActiveSourceTeachesLegacyDelegationOwnership is a focused static guard for
// the spec invariant: no active (non-test) Go file under `internal/` may describe
// the legacy `lore-delegation.ts` Pi extension as the active delegation owner.
// The current owner is `lore-pi-runtime`; the legacy extension is currently
// disabled/blocked. Test files are excluded.
func TestNoActiveSourceTeachesLegacyDelegationOwnership(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	absRoot := filepath.Join(repoRoot, "internal")
	_ = filepath.WalkDir(absRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := filepath.Base(path)
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		if strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(repoRoot, path)
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(content)
		// Forbidden ownership claims: anything that asserts the legacy
		// `lore-delegation` extension is the active owner.
		// Note: the literal string `lore-delegation` may legitimately appear
		// as the disabled extension name in `pi-runtime.contract.json` paths
		// or in comments; the test only flags the active-owner pattern.
		forbidden := []struct {
			name    string
			pattern string
		}{
			{"legacy delegation is active", "delegation is provided by the legacy `lore-delegation` extension"},
			{"legacy delegation extension is active", "the `lore-delegation` Pi extension is active"},
		}
		for _, f := range forbidden {
			if strings.Contains(text, f.pattern) {
				t.Fatalf("active non-test source %s contains forbidden %s: %q", rel, f.name, f.pattern)
			}
		}
		return nil
	})
}

// TestDeprecatedLoreMemoryAssetNotEmbedded is a focused guard for the spec
// invariant: the deprecated `internal/install/assets/pi/lore-memory.ts` file MUST
// NOT be present in the install asset directory, and the embed.FS in `pi.go` MUST
// NOT be able to read it. This enforces the "not available at any moment" contract
// for the deprecated Pi-native memory extension.
func TestDeprecatedLoreMemoryAssetNotEmbedded(t *testing.T) {
	assetPath := filepath.Join("assets", "pi", "lore-memory.ts")
	if _, err := installAssets.ReadFile(assetPath); err == nil {
		t.Fatalf("installAssets.ReadFile(%q) succeeded; want deprecated asset to be removed from the embed.FS", assetPath)
	} else if !strings.Contains(err.Error(), "file does not exist") && !os.IsNotExist(err) {
		// Embed.FS ReadFile returns a *PathError wrapping fs.ErrNotExist; accept any
		// not-exist form, but reject any other failure mode.
		t.Fatalf("installAssets.ReadFile(%q) error = %v, want not-exist error", assetPath, err)
	}
}

// TestDefaultPiLayoutDoesNotIncludeLoreMemory is a focused guard for the spec
// invariant: the default Pi layout's ManagedFiles list MUST NOT include the
// deprecated `extensions/lore-memory.ts` path. The path is reserved for
// historical manifest upgrade filtering only.
func TestDefaultPiLayoutDoesNotIncludeLoreMemory(t *testing.T) {
	layout := ResolvePiLayout(t.TempDir())
	for _, managed := range layout.ManagedFiles {
		if strings.HasSuffix(managed, managedDeprecatedLoreMemoryRelativePath) || strings.Contains(managed, "lore-memory.ts") {
			t.Fatalf("layout.ManagedFiles includes deprecated lore-memory.ts: %v", layout.ManagedFiles)
		}
	}
}

// TestDefaultPiAdapterRenderDoesNotEmitLoreMemory is a focused guard for the
// spec invariant: the default Pi adapter's default-component render MUST NOT
// include any file at the deprecated `extensions/lore-memory.ts` path, even
// when the optional `pi-extensions` component is explicitly selected.
func TestDefaultPiAdapterRenderDoesNotEmitLoreMemory(t *testing.T) {
	adapter := defaultPiAdapter()
	definition := agentpack.DefaultDefinition()

	for _, components := range [][]ComponentID{
		{ComponentCorePack, ComponentLoreServerMCP, ComponentExtendedSkills},
		{ComponentCorePack, ComponentLoreServerMCP, ComponentExtendedSkills, ComponentPiExtensions},
		{ComponentCorePack, ComponentPiExtensions},
	} {
		rendered, err := adapter.Render(context.Background(), RenderRequest{
			Target:     TargetPi,
			Definition: definition,
			Components: components,
		})
		if err != nil {
			t.Fatalf("Render(%v) error = %v, want nil", components, err)
		}
		for _, file := range rendered {
			if strings.HasSuffix(file.RelativePath, "lore-memory.ts") {
				t.Fatalf("Render(%v) emitted deprecated %s; lore-memory.ts must not be rendered in any install path", components, file.RelativePath)
			}
		}
	}
}
