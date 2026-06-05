package install

import (
	"fmt"
)

type ComponentID string

const (
	ComponentCorePack         ComponentID = "core-pack"
	ComponentLoreServerMCP    ComponentID = "lore-server-mcp"
	ComponentPiExtensions     ComponentID = "pi-extensions"
	ComponentExtendedSkills   ComponentID = "extended-skills"
	ComponentCodexAgentConfig ComponentID = "codex-agent-config"
	// ComponentOpenCodePlugins is the bounded OpenCode plugin asset bundle
	// (background-agents.ts, model-variants.ts, opencode-subagent-statusline,
	// and the tui.json referencing the community statusline). The excluded
	// plugins `sdd-engram` and `logo` are explicitly NEVER bundled; see
	// `excludedOpenCodePluginNames` and the static guard in
	// `static_guards_test.go` for the explicit rejection invariant.
	ComponentOpenCodePlugins ComponentID = "opencode-plugins"

	// PiHostedMCPPackageRepo is the canonical git source for the hosted Lore MCP adapter.
	PiHostedMCPPackageRepo = "github.com/nicobailon/pi-mcp-adapter"
	// PiHostedMCPPackageRef is the immutable commit SHA for the hosted MCP adapter.
	// Update this value when a new stable release is approved.
	PiHostedMCPPackageRef = "1091b34da83d58bd2d9fcaff2dc31f449a94bf1f"
)

const (
	LegacyPiManifestSchemaVersion = "1"
	PortableManifestSchemaVersion = "2"
)

type Component struct {
	ID               ComponentID
	Title            string
	Description      string
	Optional         bool
	DefaultForTarget map[TargetID]bool
}

func PiHostedMCPPackageSource() string {
	return "git:" + PiHostedMCPPackageRepo + "@" + PiHostedMCPPackageRef
}

func ComponentCatalog() map[ComponentID]Component {
	return map[ComponentID]Component{
		ComponentCorePack: {
			ID:          ComponentCorePack,
			Title:       "Core Pack",
			Description: "Canonical persona, workflow, roles, and routing definition.",
			DefaultForTarget: map[TargetID]bool{
				TargetPi:          true,
				TargetClaudeCode:  true,
				TargetOpenCode:    true,
				TargetCodex:       true,
				TargetAntigravity: true,
			},
		},
		ComponentLoreServerMCP: {
			ID:          ComponentLoreServerMCP,
			Title:       "Lore Server MCP",
			Description: "Managed Lore MCP configuration for supported targets.",
			Optional:    false,
			DefaultForTarget: map[TargetID]bool{
				TargetPi:          true,
				TargetAntigravity: true,
				TargetCodex:       true,
			},
		},
		ComponentPiExtensions: {
			ID:               ComponentPiExtensions,
			Title:            "Pi Extensions",
			Description:      "Optional Pi-native Lore extension bundle (lore-footer UI status only). The deprecated lore-memory extension was removed and is not available in any install path.",
			Optional:         true,
			DefaultForTarget: map[TargetID]bool{},
		},
		ComponentExtendedSkills: {
			ID:          ComponentExtendedSkills,
			Title:       "Extended Skills",
			Description: "Portable skill bundle: skill-creator, skill-registry, and judgment-day.",
			DefaultForTarget: map[TargetID]bool{
				TargetPi:          true,
				TargetAntigravity: true,
				TargetCodex:       true,
			},
		},
		ComponentCodexAgentConfig: {
			ID:          ComponentCodexAgentConfig,
			Title:       "Codex Agent Config",
			Description: "Project Lore-managed agent-config.json into Codex (auto-managed, not selectable).",
			Optional:    true,
			DefaultForTarget: map[TargetID]bool{
				TargetCodex: true,
			},
		},
		ComponentOpenCodePlugins: {
			ID:          ComponentOpenCodePlugins,
			Title:       "OpenCode Plugins",
			Description: "Bounded OpenCode plugin asset bundle: background-agents.ts, model-variants.ts, and the community opencode-subagent-statusline (tui.json reference). Excludes sdd-engram and logo.",
			Optional:    true,
			DefaultForTarget: map[TargetID]bool{
				TargetOpenCode: true,
			},
		},
	}
}

func DefaultComponentSelection(target TargetID) []ComponentID {
	catalog := ComponentCatalog()
	ordered := []ComponentID{ComponentCorePack, ComponentPiExtensions, ComponentLoreServerMCP, ComponentExtendedSkills, ComponentOpenCodePlugins}
	supported := supportedComponentsForTarget(target)
	selection := make([]ComponentID, 0, len(ordered))
	for _, id := range ordered {
		if _, ok := supported[id]; !ok {
			continue
		}
		if catalog[id].DefaultForTarget[target] {
			selection = append(selection, id)
		}
	}
	if len(selection) == 0 && supported[ComponentCorePack] {
		selection = append(selection, ComponentCorePack)
	}
	return selection
}

func NormalizeComponentSelection(target TargetID, requested []ComponentID) ([]ComponentID, error) {
	catalog := ComponentCatalog()
	supported := supportedComponentsForTarget(target)
	if len(requested) == 0 {
		return DefaultComponentSelection(target), nil
	}
	seen := map[ComponentID]struct{}{}
	ordered := []ComponentID{ComponentCorePack}
	seen[ComponentCorePack] = struct{}{}
	for _, id := range requested {
		if _, ok := catalog[id]; !ok {
			return nil, fmt.Errorf("unknown component %q", id)
		}
		if _, ok := supported[id]; !ok {
			return nil, fmt.Errorf("component %q is not supported by target %q", id, target)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ordered = append(ordered, id)
	}
	return ordered, nil
}

func supportedComponentsForTarget(target TargetID) map[ComponentID]bool {
	registry, err := defaultInstallRegistry()
	if err == nil {
		if adapter, err := registry.Resolve(target); err == nil {
			catalog := ComponentCatalog()
			supported := map[ComponentID]bool{}
			for _, capability := range adapter.Capabilities() {
				if capability.Component == "" {
					continue
				}
				supported[capability.Component] = true
			}
			for id, component := range catalog {
				if component.Optional {
					supported[id] = true
				}
			}
			if supported[ComponentCorePack] {
				return supported
			}
		}
	}

	catalog := ComponentCatalog()
	supported := map[ComponentID]bool{ComponentCorePack: true}
	for id, component := range catalog {
		if component.DefaultForTarget[target] {
			supported[id] = true
		}
	}
	return supported
}
