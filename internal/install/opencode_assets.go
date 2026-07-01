package install

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"

	"github.com/alferio94/lore-cli/internal/agentpack"
)

//go:embed assets/opencode/*
var opencodeInstallAssets embed.FS

// opencodePluginAssetDir is the embed.FS directory containing the
// OpenCode plugin .ts files bundled with the installer.
const opencodePluginAssetDir = "assets/opencode/plugins"

// opencodeTUISettingsAssetPath is the embed.FS path for the OpenCode
// TUI settings JSON file bundled with the installer.
const opencodeTUISettingsAssetPath = "assets/opencode/tui.json"

// managedOpenCodePromptAssets maps canonical prompt names to the managed paths
// written under ~/.config/opencode. The content is rendered from internal/agentpack
// at install/plan time; checked-in prompt markdown is not the source of truth.
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

// renderOpenCodePromptAssets returns OpenCode-native prompt files rendered from
// canonical agentpack contracts. Prompt assets are part of the core OpenCode pack
// because native agent config points to `./prompts/...` paths rather than
// plugin-owned runtime shims.
func readOpenCodePromptAsset(name string) ([]byte, error) {
	content, err := renderOpenCodePromptAsset(name)
	if err != nil {
		return nil, err
	}
	return []byte(content), nil
}

func renderOpenCodePromptAsset(name string) (string, error) {
	if _, ok := managedOpenCodePromptAssets[name]; !ok {
		return "", fmt.Errorf("opencode prompt %q is not in the managed prompt asset list", name)
	}
	switch name {
	case "lore.md":
		return agentpack.RenderOpenCodeOrchestratorPrompt(agentpack.DefaultDefinition()), nil
	case "lore-worker.md":
		return agentpack.RenderOpenCodeWorkerPrompt(), nil
	case "sdd/init.md":
		return agentpack.RenderOpenCodeSDDPrompt(agentpack.PhaseInit)
	case "sdd/explore.md":
		return agentpack.RenderOpenCodeSDDPrompt(agentpack.PhaseExplore)
	case "sdd/propose.md":
		return agentpack.RenderOpenCodeSDDPrompt(agentpack.PhaseProposal)
	case "sdd/spec.md":
		return agentpack.RenderOpenCodeSDDPrompt(agentpack.PhaseSpec)
	case "sdd/design.md":
		return agentpack.RenderOpenCodeSDDPrompt(agentpack.PhaseDesign)
	case "sdd/tasks.md":
		return agentpack.RenderOpenCodeSDDPrompt(agentpack.PhaseTasks)
	case "sdd/apply.md":
		return agentpack.RenderOpenCodeSDDPrompt(agentpack.PhaseApply)
	case "sdd/verify.md":
		return agentpack.RenderOpenCodeSDDPrompt(agentpack.PhaseVerify)
	case "sdd/archive.md":
		return agentpack.RenderOpenCodeSDDPrompt(agentpack.PhaseArchive)
	default:
		return "", fmt.Errorf("unsupported opencode prompt %q", name)
	}
}

func renderOpenCodePromptAssets() ([]RenderedFile, error) {
	names := make([]string, 0, len(managedOpenCodePromptAssets))
	for name := range managedOpenCodePromptAssets {
		names = append(names, name)
	}
	sort.Strings(names)

	rendered := make([]RenderedFile, 0, len(names))
	for _, name := range names {
		content, err := renderOpenCodePromptAsset(name)
		if err != nil {
			return nil, err
		}
		rendered = append(rendered, RenderedFile{
			Component:    ComponentCorePack,
			RelativePath: filepath.ToSlash(managedOpenCodePromptAssets[name]),
			MergeMode:    MergeModeReplace,
			Content:      []byte(content),
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
