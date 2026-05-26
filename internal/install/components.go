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
			Description: "Optional auth-safe MCP bridge configuration.",
			Optional:    true,
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
	selection := make([]ComponentID, 0, len(ordered))
	for _, id := range ordered {
		if catalog[id].DefaultForTarget[target] {
			selection = append(selection, id)
		}
	}
	return selection
}

func NormalizeComponentSelection(target TargetID, requested []ComponentID) ([]ComponentID, error) {
	catalog := ComponentCatalog()
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
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ordered = append(ordered, id)
	}
	return ordered, nil
}
