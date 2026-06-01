package install

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/alferio94/lore-cli/internal/agentpack"
)

// codexAdapter implements the shared-harness install pattern for Codex.
// It projects Lore-managed agents.md and skills into ~/.codex.
type codexAdapter struct {
	target       TargetID
	title        string
	capabilities map[CapabilityID]Capability
}

func defaultCodexAdapter() HarnessAdapter {
	return codexAdapter{
		target: TargetCodex,
		title:  "Codex",
		capabilities: map[CapabilityID]Capability{
			CapabilityAgentPack: {
				ID:               CapabilityAgentPack,
				Component:        ComponentCorePack,
				Description:      "Render the portable Lore core pack for Codex-owned agents.md and skills.",
				EnabledByDefault: true,
			},
			CapabilityExtendedSkills: {
				ID:               CapabilityExtendedSkills,
				Component:        ComponentExtendedSkills,
				Description:      "Portable extended skill bundle for CLI-managed non-agent skills.",
				EnabledByDefault: true,
			},
		},
	}
}

func (a codexAdapter) ID() TargetID  { return a.target }
func (a codexAdapter) Title() string { return a.title }

func (a codexAdapter) Capabilities() map[CapabilityID]Capability {
	copyMap := make(map[CapabilityID]Capability, len(a.capabilities))
	for key, value := range a.capabilities {
		copyMap[key] = value
	}
	return copyMap
}

func (a codexAdapter) Supports(component ComponentID) bool {
	for _, capability := range a.capabilities {
		if capability.Component == component {
			return true
		}
	}
	return false
}

// ResolveCodexLayout returns the Codex harness layout for a home directory.
func ResolveCodexLayout(homeDir string) HarnessLayout {
	codexDir := filepath.Join(homeDir, ".codex")
	manifestPath := filepath.Join(codexDir, "lore-install.json")
	agentsPath := filepath.Join(codexDir, "agents.md")
	skillsDir := filepath.Join(codexDir, "skills")
	return HarnessLayout{
		Target:       TargetCodex,
		RootDir:      codexDir,
		ManifestPath: manifestPath,
		Paths: map[string]string{
			"codex_dir":    codexDir,
			"agents_md":    agentsPath,
			"skills_dir":   skillsDir,
			"manifest":     manifestPath,
			"harness_root": codexDir,
		},
	}
}

func (a codexAdapter) Render(_ context.Context, req RenderRequest) ([]RenderedFile, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	components, err := NormalizeComponentSelection(req.Target, req.Components)
	if err != nil {
		return nil, err
	}
	for _, component := range components {
		if !a.Supports(component) {
			return nil, err
		}
	}

	rendered := []RenderedFile{}

	// Render agents.md from agent-config.json source of truth.
	if containsComponent(components, ComponentCorePack) {
		agentsContent, err := renderCodexAgentsMD(req)
		if err != nil {
			return nil, err
		}
		rendered = append(rendered, RenderedFile{
			Component:    ComponentCorePack,
			RelativePath: "agents.md",
			MergeMode:    MergeModeReplace,
			Content:      agentsContent,
		})
	}

	// Render managed agent skills.
	rendered = append(rendered, renderCodexManagedSkills(req)...)

	// Render extended skills.
	rendered = append(rendered, renderCodexExtendedSkills(req)...)

	return rendered, nil
}

// RenderManagedAgents renders Lore-managed agent files for Codex.
// Codex uses agent-pack as the source of truth; no Pi-style managed overlays.
func (a codexAdapter) RenderManagedAgents(_ context.Context, req RenderRequest) ([]RenderedFile, error) {
	return nil, nil
}

// RenderExtendedSkills renders extended skills for Codex.
func (a codexAdapter) RenderExtendedSkills(_ context.Context, req RenderRequest, _ PiLayout) ([]RenderedFile, error) {
	return renderCodexExtendedSkills(req), nil
}

// renderCodexAgentsMD renders the agents.md file from agent-config.json.
// It uses agentconfig as the source of truth for model declarations.
func renderCodexAgentsMD(req RenderRequest) ([]byte, error) {
	// Try to load agent config if available.
	var agentConfig map[string]string
	if len(req.AgentConfig.SDDAgents) > 0 {
		agentConfig = make(map[string]string, len(req.AgentConfig.SDDAgents))
		for name, agent := range req.AgentConfig.SDDAgents {
			agentConfig[name] = agent.Model
		}
	}

	definition := req.effectiveDefinition()

	// Build phase list from definition.
	var phases []string
	for _, phase := range definition.Workflow.Phases {
		name := agentpack.PhaseAgentName(phase.ID)
		model := agentpack.DefaultSDDModel
		if agentConfig != nil {
			if m, ok := agentConfig[name]; ok {
				model = m
			}
		}
		phases = append(phases, "- "+name+": "+model)
	}

	phasesStr := strings.Join(phases, "\n")
	if phasesStr == "" {
		phasesStr = "- (no SDD agents defined)"
	}

	text := strings.Join([]string{
		"# Lore Configuration",
		"",
		"This file is managed by `lore install --target codex` and should not be edited manually.",
		"",
		"## Persona",
		"- Name: `" + definition.Persona.Name + "`",
		"- Identity: " + definition.Persona.Identity,
		"- Tone: " + definition.Persona.Tone,
		"",
		"## Lore Skills",
		"- Managed skills directory: `~/.codex/skills`",
		"- Managed manifest: `~/.codex/lore-install.json`",
		"",
		"## SDD Agents",
		"",
		phasesStr,
		"",
		"## Notes",
		"- This is a **config-only** Lore projection; it does not enable live `codex exec` or Lore MCP runtime.",
		"- No MCP server, runner, npm bootstrap, or per-harness configurators are installed by this target.",
		"",
		"Load the Lore-managed skill files from `~/.codex/skills` when a task explicitly requires them.",
	}, "\n") + "\n"

	return []byte(text), nil
}

// renderCodexManagedSkills renders the managed agent skill files for Codex.
func renderCodexManagedSkills(req RenderRequest) []RenderedFile {
	managedAgents := req.effectiveManagedAgents(CodexSkillPathResolver())
	rendered := make([]RenderedFile, 0, len(managedAgents))
	for _, agent := range managedAgents {
		content := strings.Join([]string{
			"---",
			"name: " + agent.Name,
			"description: " + agent.Description,
			"---",
			agent.Body,
		}, "\n")
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		rendered = append(rendered, RenderedFile{
			Component:    ComponentCorePack,
			RelativePath: filepath.ToSlash(filepath.Join("skills", agent.Name, "SKILL.md")),
			MergeMode:    MergeModeReplace,
			Content:      []byte(content),
		})
	}
	return rendered
}

// renderCodexExtendedSkills renders the extended skill files for Codex.
func renderCodexExtendedSkills(req RenderRequest) []RenderedFile {
	extendedSkills := req.effectiveExtendedSkills(CodexSkillPathResolver())
	if len(extendedSkills) == 0 {
		return nil
	}
	rendered := make([]RenderedFile, 0, len(extendedSkills))
	for _, skill := range extendedSkills {
		content := renderManagedSkillMarkdown(skill)
		rendered = append(rendered, RenderedFile{
			Component:    ComponentExtendedSkills,
			RelativePath: filepath.ToSlash(filepath.Join("skills", skill.Name, "SKILL.md")),
			MergeMode:   MergeModeReplace,
			Content:     []byte(content),
		})
	}
	return rendered
}

type agentpackSkillPathResolverFunc func(agentpack.SkillRef) string

func (f agentpackSkillPathResolverFunc) ResolveSkillRef(ref agentpack.SkillRef) string {
	return f(ref)
}

// CodexSkillPathResolver returns a SkillPathResolver for the Codex harness.
func CodexSkillPathResolver() agentpack.SkillPathResolver {
	return agentpackSkillPathResolverFunc(func(ref agentpack.SkillRef) string {
		if ref.Shared {
			return filepath.ToSlash(filepath.Join("~/.codex/skills", ref.Name+".md"))
		}
		return filepath.ToSlash(filepath.Join("~/.codex/skills", ref.Name, "SKILL.md"))
	})
}

// codexAbsolutePath resolves a relative path within the Codex layout.
func codexAbsolutePath(layout HarnessLayout, relativePath string) string {
	cleanRelativePath := filepath.ToSlash(relativePath)
	switch cleanRelativePath {
	case "agents.md":
		return layout.Paths["agents_md"]
	case "lore-install.json":
		return layout.Paths["manifest"]
	default:
		return filepath.Join(layout.RootDir, filepath.FromSlash(cleanRelativePath))
	}
}