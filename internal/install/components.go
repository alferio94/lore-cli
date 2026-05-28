package install

import (
	"fmt"
)

type ComponentID string

const (
	ComponentCorePack      ComponentID = "core-pack"
	ComponentLoreServerMCP ComponentID = "lore-server-mcp"
	ComponentPiExtensions  ComponentID = "pi-extensions"
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
			Description: "Optional direct Lore Server MCP configuration.",
			Optional:    true,
			DefaultForTarget: map[TargetID]bool{
				TargetAntigravity: true,
			},
		},
		ComponentPiExtensions: {
			ID:          ComponentPiExtensions,
			Title:       "Pi Extensions",
			Description: "Pi-native Lore extensions path kept as the default Pi backend.",
			DefaultForTarget: map[TargetID]bool{
				TargetPi: true,
			},
		},
	}
}

func DefaultComponentSelection(target TargetID) []ComponentID {
	catalog := ComponentCatalog()
	ordered := []ComponentID{ComponentCorePack, ComponentPiExtensions, ComponentLoreServerMCP}
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
