package install

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/agentpack"
)

// TestOpenCodePluginAssetsAreBoundedToManagedSet verifies the bounded
// plugin asset list: only the three documented plugin .ts files
// (background-agents, model-variants, opencode-subagent-statusline)
// are rendered, in stable order, and no other plugin name appears.
func TestOpenCodePluginAssetsAreBoundedToManagedSet(t *testing.T) {
	pluginFiles, err := renderOpenCodePluginAssets()
	if err != nil {
		t.Fatalf("renderOpenCodePluginAssets() error = %v, want nil", err)
	}
	if len(pluginFiles) != 4 {
		t.Fatalf("renderOpenCodePluginAssets() returned %d files, want 4 (3 plugin .ts + tui.json); got %v",
			len(pluginFiles), relativePathsOf(pluginFiles))
	}
	wantPluginPaths := map[string]bool{
		"plugins/background-agents.ts":            false,
		"plugins/model-variants.ts":               false,
		"plugins/opencode-subagent-statusline.ts": false,
		"tui.json":                                false,
	}
	for _, file := range pluginFiles {
		relative := filepath.ToSlash(file.RelativePath)
		if _, ok := wantPluginPaths[relative]; !ok {
			t.Fatalf("renderOpenCodePluginAssets() emitted unexpected file %q (component=%q merge_mode=%q)",
				relative, file.Component, file.MergeMode)
		}
		wantPluginPaths[relative] = true
		if file.Component != ComponentOpenCodePlugins {
			t.Fatalf("plugin asset %q component = %q, want %q", relative, file.Component, ComponentOpenCodePlugins)
		}
	}
	for path, seen := range wantPluginPaths {
		if !seen {
			t.Fatalf("renderOpenCodePluginAssets() missing expected plugin asset %q", path)
		}
	}
}

// TestOpenCodeTUISettingsUsesNativeShape is the focused regression
// gate for the post-repair native OpenCode tui.json shape:
//
//   - `$schema` is exactly the documented OpenCode schema URL
//     (https://opencode.ai/tui.json), not a placeholder URL.
//   - The `plugin` field is a SINGULAR string array containing
//     ONLY the community `opencode-subagent-statusline` name.
//   - There is NO plural `plugins` array of objects (the legacy
//     shape the previous renderer produced).
//   - There is NO top-level `lore` block (the legacy shape the
//     previous renderer produced).
//   - There is no Gentle-authored copy or other forbidden wording.
func TestOpenCodeTUISettingsUsesNativeShape(t *testing.T) {
	content, err := readOpenCodeTUISettingsAsset()
	if err != nil {
		t.Fatalf("readOpenCodeTUISettingsAsset() error = %v, want nil", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(content, &payload); err != nil {
		t.Fatalf("decode tui.json: %v", err)
	}
	// Schema URL: the documented OpenCode tui.json schema, not a
	// placeholder. The previous renderer used a fake URL
	// (`https://opencode.example/tui.schema.json`); the post-repair
	// asset MUST use the canonical OpenCode URL.
	if got, want := payload["$schema"], opencodeTUISettingsSchemaURL; got != want {
		t.Fatalf("tui.json $schema = %v, want %q", got, want)
	}
	// Singular `plugin` string array, not a plural `plugins` array
	// of objects.
	plugin, ok := payload["plugin"].([]any)
	if !ok {
		t.Fatalf("tui.json missing singular `plugin` string array; got keys %v", keysOfMapTUI(payload))
	}
	if len(plugin) != 1 {
		t.Fatalf("tui.json `plugin` array length = %d, want 1 (only the community statusline)", len(plugin))
	}
	if name, _ := plugin[0].(string); name != opencodeCommunityStatuslinePlugin {
		t.Fatalf("tui.json `plugin`[0] = %q, want %q", name, opencodeCommunityStatuslinePlugin)
	}
	// Legacy plural `plugins` array of objects MUST NOT be present.
	if _, present := payload["plugins"]; present {
		t.Fatalf("tui.json unexpectedly carries legacy plural `plugins` array; want native singular `plugin` string array only")
	}
	// Legacy top-level `lore` block MUST NOT be present.
	if _, present := payload["lore"]; present {
		t.Fatalf("tui.json unexpectedly carries top-level `lore` block; want native OpenCode shape without a Lore metadata blob")
	}
	// No excluded plugin names appear in the singular `plugin` array.
	for _, entry := range plugin {
		name, _ := entry.(string)
		for _, excluded := range excludedOpenCodePluginNames {
			if matchesExcludedOpenCodePlugin(name, excluded) {
				t.Fatalf("tui.json `plugin` array references explicitly excluded plugin id %q", excluded)
			}
		}
	}
	// Defense in depth: the asset bytes contain no Gentle-authored
	// copy. The dedicated Gentle-leakage test already enforces
	// this; the assertion here makes the native-shape test
	// self-contained.
	lower := strings.ToLower(string(content))
	for _, forbidden := range []string{
		"gentle",
		"gentle-ai",
		"gentleprogramming",
		"gentleman-programming",
		"opencode.example",
	} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("tui.json content contains forbidden token %q; want clean native OpenCode shape", forbidden)
		}
	}
}

func keysOfMapTUI(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	return out
}

// TestOpenCodePluginAssetsExcludeSddEngramAndLogo is a focused
// regression gate: the explicitly-excluded plugin names `sdd-engram`
// and `logo` must never appear as a managed plugin asset or in the
// `tui.json` plugin list. The `tui.json` file uses the native
// OpenCode singular `plugin` string array (e.g.
// `["opencode-subagent-statusline"]`); the test verifies that no
// excluded plugin id is registered as a plugin name.
func TestOpenCodePluginAssetsExcludeSddEngramAndLogo(t *testing.T) {
	pluginFiles, err := renderOpenCodePluginAssets()
	if err != nil {
		t.Fatalf("renderOpenCodePluginAssets() error = %v, want nil", err)
	}
	for _, file := range pluginFiles {
		relative := filepath.ToSlash(file.RelativePath)
		basename := filepath.Base(relative)
		// Plugin asset .ts files: the basename must not match an
		// excluded plugin (defense on top of the static guard).
		if strings.HasSuffix(relative, ".ts") {
			for _, excluded := range excludedOpenCodePluginNames {
				if matchesExcludedOpenCodePlugin(basename, excluded) {
					t.Fatalf("renderOpenCodePluginAssets() emitted explicitly excluded plugin asset %q (excluded=%q)",
						relative, excluded)
				}
			}
		}
	}

	// The bundled tui.json must not register any excluded plugin id
	// in its singular `plugin` string array; only the community
	// `opencode-subagent-statusline` is referenced.
	tuiContent, err := readOpenCodeTUISettingsAsset()
	if err != nil {
		t.Fatalf("readOpenCodeTUISettingsAsset() error = %v, want nil", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(tuiContent, &payload); err != nil {
		t.Fatalf("decode tui.json: %v", err)
	}
	plugin, ok := payload["plugin"].([]any)
	if !ok {
		t.Fatalf("tui.json missing singular `plugin` string array; got keys %v", keysOfMapTUI(payload))
	}
	for _, entry := range plugin {
		name, _ := entry.(string)
		for _, excluded := range excludedOpenCodePluginNames {
			if matchesExcludedOpenCodePlugin(name, excluded) {
				t.Fatalf("tui.json `plugin` array references explicitly excluded plugin id %q", excluded)
			}
		}
	}
}

// TestOpenCodePluginAssetsNoGentleWordingLeakage verifies the bounded
// plugin bundle does not leak any Gentle-authored copy. The check
// inspects every rendered plugin asset (including tui.json) and
// rejects any of the documented forbidden tokens.
func TestOpenCodePluginAssetsNoGentleWordingLeakage(t *testing.T) {
	pluginFiles, err := renderOpenCodePluginAssets()
	if err != nil {
		t.Fatalf("renderOpenCodePluginAssets() error = %v, want nil", err)
	}
	forbidden := []string{
		"gentle",
		"gentle-ai",
		"gentleprogramming",
		"gentleman-programming",
	}
	for _, file := range pluginFiles {
		relative := filepath.ToSlash(file.RelativePath)
		lower := strings.ToLower(string(file.Content))
		for _, token := range forbidden {
			if strings.Contains(lower, token) {
				t.Fatalf("plugin asset %q content leaked forbidden Gentle token %q; content=%q",
					relative, token, string(file.Content))
			}
		}
	}
}

// TestOpenCodeAdapterRenderWithPluginsIncludesTUISettingsAndPluginFiles
// verifies that when the `opencode-plugins` component is selected,
// the adapter's `Render()` output includes the three managed plugin
// .ts files and the `tui.json` settings file.
func TestOpenCodeAdapterRenderWithPluginsIncludesTUISettingsAndPluginFiles(t *testing.T) {
	adapter := defaultOpenCodeAdapter()
	definition := agentpack.DefaultDefinition()
	rendered, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetOpenCode,
		Definition: definition,
		Components: []ComponentID{ComponentCorePack, ComponentOpenCodePlugins},
	})
	if err != nil {
		t.Fatalf("Render(core-pack+plugins) error = %v, want nil", err)
	}
	byPath := make(map[string]RenderedFile, len(rendered))
	for _, file := range rendered {
		byPath[filepath.ToSlash(file.RelativePath)] = file
	}
	for _, want := range []string{
		"plugins/background-agents.ts",
		"plugins/model-variants.ts",
		"plugins/opencode-subagent-statusline.ts",
		"tui.json",
	} {
		if _, ok := byPath[want]; !ok {
			t.Fatalf("Render(core-pack+plugins) missing %q; got %v", want, keysOfRendered(byPath))
		}
	}
}

// TestOpenCodeRenderFullSurfaceNoGentleLeakageAcrossAllRenderedFiles
// is the broader regression gate: every OpenCode-specific rendered
// file from the adapter — AGENTS.md, plugin .ts files, tui.json,
// and the opencode.json block — must not leak any Gentle-authored
// copy. The check intentionally scopes to OpenCode-specific files
// only; the agentpack extended skills (skill-creator,
// skill-registry, judgment-day) are shared across every target and
// out of scope for this slice's regression gate.
func TestOpenCodeRenderFullSurfaceNoGentleLeakageAcrossAllRenderedFiles(t *testing.T) {
	adapter := defaultOpenCodeAdapter()
	definition := agentpack.DefaultDefinition()
	components := []ComponentID{ComponentCorePack, ComponentLoreServerMCP, ComponentExtendedSkills, ComponentOpenCodePlugins}
	rendered, err := adapter.Render(context.Background(), RenderRequest{
		Target:         TargetOpenCode,
		Definition:     definition,
		Components:     components,
		ServerURL:      "https://lore.example",
		SavedToken:     "secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  "/tmp/lore-config",
		LoreCLIVersion: "v0.1.0",
	})
	if err != nil {
		t.Fatalf("Render(full surface) error = %v, want nil", err)
	}
	forbidden := []string{
		"gentle",
		"gentle-ai",
		"gentleprogramming",
		"gentleman-programming",
	}
	// Scope the gentle-leakage check to OpenCode-specific surfaces
	// (AGENTS.md, plugin .ts files, tui.json, opencode.json). The
	// extended skills under `skills/<name>/SKILL.md` are agentpack
	// content shared with Pi, Antigravity, and Codex and are out of
	// scope for the OpenCode regression gate.
	opencodeSurfaceSuffixes := []string{
		"/AGENTS.md",
		"/opencode.json",
		"/tui.json",
		"/plugins/background-agents.ts",
		"/plugins/model-variants.ts",
		"/plugins/opencode-subagent-statusline.ts",
	}
	for _, file := range rendered {
		relative := filepath.ToSlash(file.RelativePath)
		if !hasOpenCodeSurfaceSuffix(relative, opencodeSurfaceSuffixes) {
			continue
		}
		// The Bearer token is allowed inside the opencode.json MCP
		// block, but no rendered file may leak the Gentle copy.
		lower := strings.ToLower(string(file.Content))
		for _, token := range forbidden {
			if strings.Contains(lower, token) {
				t.Fatalf("rendered file %q leaked forbidden Gentle token %q; content=%q",
					relative, token, string(file.Content))
			}
		}
	}
}

// TestOpenCodePluginAssetReadRejectsExcludedPluginNames verifies
// that the readOpenCodePluginAsset helper explicitly rejects
// excluded plugin names with a clear error message, even when the
// file does not exist in the embed.FS (defense-in-depth on top of
// the static guard and the plugin render).
func TestOpenCodePluginAssetReadRejectsExcludedPluginNames(t *testing.T) {
	for _, excluded := range excludedOpenCodePluginNames {
		if _, err := readOpenCodePluginAsset(excluded); err == nil {
			t.Fatalf("readOpenCodePluginAsset(%q) error = nil, want explicit exclusion error", excluded)
		} else if !strings.Contains(err.Error(), "explicitly excluded") {
			t.Fatalf("readOpenCodePluginAsset(%q) error = %v, want explicit exclusion error", excluded, err)
		}
		if _, err := readOpenCodePluginAsset(excluded + ".ts"); err == nil {
			t.Fatalf("readOpenCodePluginAsset(%q) error = nil, want explicit exclusion error for .ts variant", excluded+".ts")
		} else if !strings.Contains(err.Error(), "explicitly excluded") {
			t.Fatalf("readOpenCodePluginAsset(%q) error = %v, want explicit exclusion error", excluded+".ts", err)
		}
	}
}

// TestOpenCodeManagedPluginsCapabilityIsWiredAndSelectable verifies
// the bounded OpenCode plugin capability is registered, mapped to
// the opencode-plugins component, optional, and selectable through
// NormalizeComponentSelection for the OpenCode target.
func TestOpenCodeManagedPluginsCapabilityIsWiredAndSelectable(t *testing.T) {
	adapter := defaultOpenCodeAdapter()
	caps := adapter.Capabilities()
	pluginCap, ok := caps[CapabilityOpenCodePlugins]
	if !ok {
		t.Fatalf("OpenCode adapter capabilities missing %q; got %v", CapabilityOpenCodePlugins, keysOfCapabilities(caps))
	}
	if pluginCap.Component != ComponentOpenCodePlugins {
		t.Fatalf("plugin capability component = %q, want %q", pluginCap.Component, ComponentOpenCodePlugins)
	}
	if !pluginCap.Optional {
		t.Fatalf("plugin capability Optional = false, want true")
	}
	selected, err := NormalizeComponentSelection(TargetOpenCode, []ComponentID{ComponentOpenCodePlugins})
	if err != nil {
		t.Fatalf("NormalizeComponentSelection(opencode, [opencode-plugins]) error = %v, want nil", err)
	}
	if !containsComponent(selected, ComponentOpenCodePlugins) {
		t.Fatalf("NormalizeComponentSelection(opencode, [opencode-plugins]) = %v, want opencode-plugins in selection", selected)
	}
}

// hasOpenCodeSurfaceSuffix reports whether the given relative path
// ends with any of the documented OpenCode-specific surface
// suffixes. The match is path-separator aware so the check is
// deterministic across platforms.
func hasOpenCodeSurfaceSuffix(relativePath string, suffixes []string) bool {
	normalized := filepath.ToSlash(relativePath)
	for _, suffix := range suffixes {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}
	return false
}

func relativePathsOf(files []RenderedFile) []string {
	out := make([]string, 0, len(files))
	for _, file := range files {
		out = append(out, filepath.ToSlash(file.RelativePath))
	}
	return out
}

func keysOfRendered(files map[string]RenderedFile) []string {
	out := make([]string, 0, len(files))
	for key := range files {
		out = append(out, key)
	}
	return out
}

func keysOfCapabilities(caps map[CapabilityID]Capability) []string {
	out := make([]string, 0, len(caps))
	for key := range caps {
		out = append(out, string(key))
	}
	return out
}
