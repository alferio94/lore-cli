package install

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
)

//go:embed assets/opencode/*
var opencodeInstallAssets embed.FS

// opencodePluginAssetDir is the embed.FS directory containing the
// OpenCode plugin .ts files bundled with the installer.
const opencodePluginAssetDir = "assets/opencode/plugins"

// opencodeTUISettingsAssetPath is the embed.FS path for the OpenCode
// TUI settings JSON file bundled with the installer.
const opencodeTUISettingsAssetPath = "assets/opencode/tui.json"

// opencodePromptAssetDir is the embed.FS directory containing the
// OpenCode-native prompt files bundled with the installer. These are
// separate from portable `skills/<agent>/SKILL.md` assets so later
// native OpenCode config can point agents at prompt files without
// depending on skill layout.
const opencodePromptAssetDir = "assets/opencode/prompts"

// managedOpenCodePromptAssets maps embedded prompt asset paths to the
// managed paths written under ~/.config/opencode. Keep this list
// explicit so missing prompt files fail at render time instead of
// silently producing an opencode.json prompt reference with no file.
var managedOpenCodePromptAssets = map[string]string{
	"lore.md":        "prompts/lore.md",
	"lore-worker.md": "prompts/lore-worker.md",
	"sdd/init.md":    "prompts/sdd/init.md",
	"sdd/explore.md": "prompts/sdd/explore.md",
	"sdd/propose.md": "prompts/sdd/propose.md",
	"sdd/spec.md":    "prompts/sdd/spec.md",
	"sdd/design.md":  "prompts/sdd/design.md",
	"sdd/tasks.md":   "prompts/sdd/tasks.md",
	"sdd/apply.md":   "prompts/sdd/apply.md",
	"sdd/verify.md":  "prompts/sdd/verify.md",
	"sdd/archive.md": "prompts/sdd/archive.md",
}

// managedOpenCodePluginAssetNames is the bounded set of plugin .ts
// files the OpenCode installer copies. Native OpenCode agents do not
// require a Lore-managed plugin, so the current managed set is empty.
// Legacy Lore-owned runtime emulation plugins (`background-agents.ts`,
// `lore-models.ts`, and `model-variants.ts`) are intentionally absent.
// The manifest-scoped stale-file cleanup backs up and removes prior
// Lore-managed copies when ownership is proven. Excluded plugins such
// as `sdd-engram` and `logo` are NOT in this list; see the static guard
// in `static_guards_test.go` for the explicit rejection invariant.
var managedOpenCodePluginAssetNames = []string{}

// excludedOpenCodePluginNames is the explicit set of plugin names the
// OpenCode installer must NEVER bundle, render, or reference. The
// static guard in `static_guards_test.go` and the OpenCode plugin
// asset render both consult this list.
var excludedOpenCodePluginNames = []string{
	"sdd-engram",
	"logo",
}

// readOpenCodePromptAsset returns the bundled content for an
// OpenCode-native prompt asset by its relative embedded path (for
// example "sdd/apply.md"). It rejects paths that are not in the
// explicit managed prompt map so callers cannot accidentally render
// arbitrary embedded files.
func readOpenCodePromptAsset(name string) ([]byte, error) {
	managedPath, ok := managedOpenCodePromptAssets[name]
	if !ok || managedPath == "" {
		return nil, fmt.Errorf("opencode prompt %q is not in the managed prompt asset list", name)
	}
	data, err := opencodeInstallAssets.ReadFile(filepath.ToSlash(filepath.Join(opencodePromptAssetDir, name)))
	if err != nil {
		return nil, fmt.Errorf("read opencode prompt asset %q: %w", name, err)
	}
	return data, nil
}

// renderOpenCodePromptAssets returns the bundled OpenCode-native
// prompt files as rendered managed files. Prompt assets are part of
// the core OpenCode pack because the later native agent config points
// to `./prompts/...` paths rather than plugin-owned runtime shims.
func renderOpenCodePromptAssets() ([]RenderedFile, error) {
	names := make([]string, 0, len(managedOpenCodePromptAssets))
	for name := range managedOpenCodePromptAssets {
		names = append(names, name)
	}
	sort.Strings(names)

	rendered := make([]RenderedFile, 0, len(names))
	for _, name := range names {
		content, err := readOpenCodePromptAsset(name)
		if err != nil {
			return nil, err
		}
		rendered = append(rendered, RenderedFile{
			Component:    ComponentCorePack,
			RelativePath: filepath.ToSlash(managedOpenCodePromptAssets[name]),
			MergeMode:    MergeModeReplace,
			Content:      content,
		})
	}
	return rendered, nil
}

// readOpenCodePluginAsset returns the bundled content for a plugin
// .ts file by its short filename (e.g. "opencode-subagent-statusline.ts"). It
// rejects any plugin name that is not in the managed set, and
// surfaces an explicit error when a caller attempts to read an
// excluded plugin (defense-in-depth on top of the static guard).
func readOpenCodePluginAsset(name string) ([]byte, error) {
	for _, excluded := range excludedOpenCodePluginNames {
		if matchesExcludedOpenCodePlugin(name, excluded) {
			return nil, fmt.Errorf("opencode plugin %q is explicitly excluded from the installer bundle", name)
		}
	}
	allowed := false
	for _, managed := range managedOpenCodePluginAssetNames {
		if name == managed {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, fmt.Errorf("opencode plugin %q is not in the managed plugin asset list", name)
	}
	data, err := opencodeInstallAssets.ReadFile(opencodePluginAssetDir + "/" + name)
	if err != nil {
		return nil, fmt.Errorf("read opencode plugin asset %q: %w", name, err)
	}
	return data, nil
}

// readOpenCodeTUISettingsAsset returns the bundled `tui.json` for the
// OpenCode harness. The asset is a small, validated JSON document
// that uses the native singular `plugin` array and intentionally
// registers no Lore-managed plugins.
func readOpenCodeTUISettingsAsset() ([]byte, error) {
	data, err := opencodeInstallAssets.ReadFile(opencodeTUISettingsAssetPath)
	if err != nil {
		return nil, fmt.Errorf("read opencode tui settings asset: %w", err)
	}
	return data, nil
}

// opencodeEmbeddedAssetFS exposes the embedded OpenCode asset
// sub-tree for static guard tests that need to verify the asset
// directory is reachable and that no excluded plugin name appears
// anywhere in the bundled bytes.
func opencodeEmbeddedAssetFS() fs.FS {
	return opencodeInstallAssets
}
