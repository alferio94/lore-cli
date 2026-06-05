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

// TestOpenCodePluginAssetsExcludeSddEngramAndLogo is a focused
// regression gate: the explicitly-excluded plugin names `sdd-engram`
// and `logo` must never appear as a managed plugin asset or in the
// `tui.json` plugin list. The `tui.json` file is allowed to
// DECLARE the exclusion list under the `lore.plugins_excluded` key
// (the positive assertion); the test only verifies that no excluded
// plugin id is registered as an enabled plugin.
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
	// in its `plugins` array; only the community
	// `opencode-subagent-statusline` is referenced. The exclusion
	// list may appear under the `lore.plugins_excluded` key as a
	// positive declaration.
	tuiContent, err := readOpenCodeTUISettingsAsset()
	if err != nil {
		t.Fatalf("readOpenCodeTUISettingsAsset() error = %v, want nil", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(tuiContent, &payload); err != nil {
		t.Fatalf("decode tui.json: %v", err)
	}
	plugins, _ := payload["plugins"].([]any)
	for _, entry := range plugins {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		id, _ := entryMap["id"].(string)
		for _, excluded := range excludedOpenCodePluginNames {
			if matchesExcludedOpenCodePlugin(id, excluded) {
				t.Fatalf("tui.json plugins array references explicitly excluded plugin id %q", excluded)
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
