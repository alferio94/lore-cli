package install

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed assets/opencode/*
var opencodeInstallAssets embed.FS

// opencodePluginAssetDir is the embed.FS directory containing the
// OpenCode plugin .ts files bundled with the installer.
const opencodePluginAssetDir = "assets/opencode/plugins"

// opencodeTUISettingsAssetPath is the embed.FS path for the OpenCode
// TUI settings JSON file bundled with the installer.
const opencodeTUISettingsAssetPath = "assets/opencode/tui.json"

// managedOpenCodePluginAssetNames is the bounded set of plugin .ts
// files the OpenCode installer bundles. The list is intentionally
// small and explicit: `background-agents`, `lore-models`, and the
// community `opencode-subagent-statusline`. The previous
// `model-variants.ts` asset was renamed to `lore-models.ts` by the
// `add-opencode-lore-models-plugin` change; the stale file is
// cleaned up by the OpenCode install pipeline's manifest-scoped
// stale-file pass when prior manifest ownership is proven, so
// upgraded installs that still carry the old file see it backed up
// and removed on the next reinstall. Excluded plugins such as
// `sdd-engram` and `logo` are NOT in this list; see the static
// guard in `static_guards_test.go` for the explicit rejection
// invariant.
var managedOpenCodePluginAssetNames = []string{
	"background-agents.ts",
	"lore-models.ts",
	"opencode-subagent-statusline.ts",
}

// excludedOpenCodePluginNames is the explicit set of plugin names the
// OpenCode installer must NEVER bundle, render, or reference. The
// static guard in `static_guards_test.go` and the OpenCode plugin
// asset render both consult this list.
var excludedOpenCodePluginNames = []string{
	"sdd-engram",
	"logo",
}

// readOpenCodePluginAsset returns the bundled content for a plugin
// .ts file by its short filename (e.g. "background-agents.ts"). It
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
// that references the community `opencode-subagent-statusline`
// plugin and records the explicit `sdd-engram` and `logo` exclusion
// list under a `lore.plugins_excluded` key.
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
