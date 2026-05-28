package install

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alferio94/lore-cli/internal/agentpack"
)

type piAdapter struct {
	target       TargetID
	title        string
	capabilities map[CapabilityID]Capability
}

func defaultPiAdapter() HarnessAdapter {
	return piAdapter{
		target: TargetPi,
		title:  "Pi",
		capabilities: map[CapabilityID]Capability{
			CapabilityAgentPack: {
				ID:               CapabilityAgentPack,
				Component:        ComponentCorePack,
				Description:      "Render the canonical Lore agent pack definition.",
				EnabledByDefault: true,
			},
			CapabilityPiExtensions: {
				ID:               CapabilityPiExtensions,
				Component:        ComponentPiExtensions,
				Description:      "Keep Pi-native Lore extensions as the default backend.",
				EnabledByDefault: true,
			},
		},
	}
}

func (a piAdapter) ID() TargetID  { return a.target }
func (a piAdapter) Title() string { return a.title }
func (a piAdapter) Capabilities() map[CapabilityID]Capability {
	copyMap := make(map[CapabilityID]Capability, len(a.capabilities))
	for key, value := range a.capabilities {
		copyMap[key] = value
	}
	return copyMap
}
func (a piAdapter) Supports(component ComponentID) bool {
	for _, capability := range a.capabilities {
		if capability.Component == component {
			return true
		}
	}
	return false
}

func (a piAdapter) Render(_ context.Context, req RenderRequest) ([]RenderedFile, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	components, err := NormalizeComponentSelection(req.Target, req.Components)
	if err != nil {
		return nil, err
	}
	for _, component := range components {
		if !a.Supports(component) {
			return nil, fmt.Errorf("component %q is not supported by target %q", component, a.target)
		}
	}

	replacements, err := renderRequestReplacements(req, components)
	if err != nil {
		return nil, err
	}
	assets := []struct {
		assetPath    string
		component    ComponentID
		relativePath string
		mergeMode    MergeMode
	}{
		{assetPath: "assets/pi/lore-memory.ts", component: ComponentPiExtensions, relativePath: managedPiExtensionRelativePaths[0], mergeMode: MergeModeReplace},
		{assetPath: "assets/pi/lore-footer.ts", component: ComponentPiExtensions, relativePath: managedPiExtensionRelativePaths[1], mergeMode: MergeModeReplace},
		{assetPath: "assets/pi/settings.json", component: ComponentCorePack, relativePath: "settings.json", mergeMode: MergeModeAdditiveJSON},
	}

	rendered := make([]RenderedFile, 0, len(assets))
	for _, asset := range assets {
		if !containsComponent(components, asset.component) {
			continue
		}
		content, err := installAssets.ReadFile(asset.assetPath)
		if err != nil {
			return nil, fmt.Errorf("read asset %s: %w", asset.assetPath, err)
		}
		resolved := string(content)
		for placeholder, value := range replacements {
			resolved = strings.ReplaceAll(resolved, placeholder, value)
		}
		rendered = append(rendered, RenderedFile{
			Component:    asset.component,
			RelativePath: asset.relativePath,
			MergeMode:    asset.mergeMode,
			Content:      []byte(resolved),
		})
	}
	return rendered, nil
}

func (a piAdapter) RenderManagedAgents(_ context.Context, req RenderRequest) ([]RenderedFile, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	contract := req.RuntimeContract
	if contract.Version == 0 {
		contract = defaultRuntimeContract()
	}
	definition := req.effectiveDefinition()
	managedAgents := req.effectiveManagedAgents(agentpack.PiSkillPathResolver())
	rendered := make([]RenderedFile, 0, len(managedAgents))
	for _, agent := range managedAgents {
		relativePath := filepath.ToSlash(filepath.Join("agents", contract.AgentResolution.ManagedFilenamePrefix+agent.Name+".md"))
		content := renderManagedAgentMarkdown(agent, definition.PackID, contract)
		rendered = append(rendered, RenderedFile{Component: ComponentCorePack, RelativePath: relativePath, MergeMode: MergeModeReplace, Content: []byte(content)})
	}
	sort.Slice(rendered, func(i, j int) bool { return rendered[i].RelativePath < rendered[j].RelativePath })
	return rendered, nil
}

func renderManagedAgentMarkdown(agent agentpack.ManagedAgent, packID string, contract RuntimeContract) string {
	var builder strings.Builder
	builder.WriteString("---\n")
	builder.WriteString(fmt.Sprintf("name: %s\n", agent.Name))
	builder.WriteString(fmt.Sprintf("description: %s\n", agent.Description))
	if len(agent.Tools) > 0 {
		builder.WriteString("tools:\n")
		for _, tool := range agent.Tools {
			builder.WriteString(fmt.Sprintf("  - %s\n", tool))
		}
	}
	if strings.TrimSpace(agent.Role) != "" {
		builder.WriteString(fmt.Sprintf("role: %s\n", agent.Role))
	}
	if agent.Phase != "" {
		builder.WriteString(fmt.Sprintf("phase: %s\n", renderManagedAgentPhase(agent.Phase)))
	}
	if strings.TrimSpace(agent.RequiredEnvelope) != "" {
		builder.WriteString(fmt.Sprintf("requiredEnvelope: %s\n", agent.RequiredEnvelope))
	}
	if strings.TrimSpace(agent.SkillPolicy.Mode) != "" {
		builder.WriteString(fmt.Sprintf("skillPolicyMode: %s\n", agent.SkillPolicy.Mode))
		if len(agent.SkillPolicy.Files) > 0 {
			builder.WriteString("skillPolicyFiles:\n")
			for _, file := range agent.SkillPolicy.Files {
				builder.WriteString(fmt.Sprintf("  - %s\n", file))
			}
		}
	}
	builder.WriteString(fmt.Sprintf("systemPromptMode: %s\n", agent.SystemPromptMode))
	builder.WriteString(fmt.Sprintf("inheritProjectContext: %t\n", agent.InheritProjectContext))
	if contract.AgentResolution.SupportsManagedFrontmatter {
		builder.WriteString(fmt.Sprintf("managedBy: %s\n", contract.AgentResolution.ManagedBy))
		builder.WriteString(fmt.Sprintf("managedLayer: %s\n", contract.AgentResolution.ManagedLayer))
		builder.WriteString(fmt.Sprintf("managedPackId: %s\n", packID))
	}
	builder.WriteString("---\n")
	builder.WriteString(agent.Body)
	if !strings.HasSuffix(agent.Body, "\n") {
		builder.WriteByte('\n')
	}
	return builder.String()
}

func renderManagedAgentPhase(phase agentpack.PhaseID) string {
	if phase == agentpack.PhaseProposal {
		return "propose"
	}
	return string(phase)
}

func piTemplateReplacements(definition agentpack.Definition, components []ComponentID) (map[string]string, error) {
	phases := make([]string, 0, len(definition.Workflow.Phases))
	for _, phase := range definition.Workflow.Phases {
		phases = append(phases, agentpack.PhaseAgentName(phase.ID))
	}
	profiles := make([]string, 0, len(definition.Profiles))
	for _, profile := range definition.Profiles {
		profiles = append(profiles, profile.ID)
	}
	roles := make([]string, 0, len(definition.Roles))
	for _, role := range definition.Roles {
		roles = append(roles, role.Name)
	}
	managedExtensions := []string{}
	if containsComponent(components, ComponentPiExtensions) {
		managedExtensions = append([]string(nil), managedPiExtensionRelativePaths...)
	}

	sddPhasesJSON, err := json.MarshalIndent(phases, "    ", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal SDD phases: %w", err)
	}
	profileIDsJSON, err := json.MarshalIndent(profiles, "      ", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal profile ids: %w", err)
	}
	roleNamesJSON, err := json.MarshalIndent(roles, "      ", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal role names: %w", err)
	}
	managedExtensionsJSON, err := json.MarshalIndent(managedExtensions, "    ", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal managed extensions: %w", err)
	}

	return map[string]string{
		"{{LORE_SDD_PHASES}}":         string(sddPhasesJSON),
		"{{LORE_PACK_ID}}":            definition.PackID,
		"{{LORE_PERSONA_NAME}}":       definition.Persona.Name,
		"{{LORE_PROFILE_IDS}}":        string(profileIDsJSON),
		"{{LORE_ROLE_NAMES}}":         string(roleNamesJSON),
		"{{LORE_MANAGED_EXTENSIONS}}": string(managedExtensionsJSON),
	}, nil
}

func renderRequestReplacements(req RenderRequest, components []ComponentID) (map[string]string, error) {
	replacements, err := piTemplateReplacements(req.effectiveDefinition(), components)
	if err != nil {
		return nil, err
	}
	replacements["{{LORE_SERVER_URL}}"] = strings.TrimSpace(req.ServerURL)
	replacements["{{LORE_BINARY_PATH}}"] = strings.TrimSpace(req.LoreBinaryPath)
	replacements["{{LORE_CONFIG_DIR}}"] = strings.TrimSpace(req.LoreConfigDir)
	replacements["{{LORE_CLI_VERSION}}"] = strings.TrimSpace(req.LoreCLIVersion)
	replacements["{{LORE_SETTINGS_PATH}}"] = filepath.ToSlash(strings.ReplaceAll(strings.TrimSpace(req.SettingsPath), "\\", "/"))
	return replacements, nil
}
