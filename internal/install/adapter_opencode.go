package install

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alferio94/lore-cli/internal/agentconfig"
	"github.com/alferio94/lore-cli/internal/agentpack"
)

const (
	opencodeConfigRootDirName = ".config"
	opencodeRootDirName       = "opencode"
	opencodeAgentsFileName    = "AGENTS.md"
	opencodeConfigFileName    = "opencode.json"
	opencodeSkillsDirName     = "skills"
	opencodeCommandsDirName   = "commands"
	opencodeManifestFileName  = "lore-install.json"
)

type opencodeAdapter struct {
	target       TargetID
	title        string
	capabilities map[CapabilityID]Capability
}

func defaultOpenCodeAdapter() HarnessAdapter {
	return opencodeAdapter{
		target: TargetOpenCode,
		title:  "OpenCode",
		capabilities: map[CapabilityID]Capability{
			CapabilityAgentPack: {
				ID:               CapabilityAgentPack,
				Component:        ComponentCorePack,
				Description:      "Render Lore-managed OpenCode AGENTS.md and managed skill files from the portable agent pack.",
				EnabledByDefault: true,
			},
			CapabilityExtendedSkills: {
				ID:          CapabilityExtendedSkills,
				Component:   ComponentExtendedSkills,
				Description: "Portable extended skill bundle for CLI-managed non-agent skills.",
				Optional:    true,
			},
		},
	}
}

func (a opencodeAdapter) ID() TargetID  { return a.target }
func (a opencodeAdapter) Title() string { return a.title }

func (a opencodeAdapter) Capabilities() map[CapabilityID]Capability {
	copyMap := make(map[CapabilityID]Capability, len(a.capabilities))
	for key, value := range a.capabilities {
		copyMap[key] = value
	}
	return copyMap
}

func (a opencodeAdapter) Supports(component ComponentID) bool {
	for _, capability := range a.capabilities {
		if capability.Component == component {
			return true
		}
	}
	return false
}

func ResolveOpenCodeLayout(homeDir string) HarnessLayout {
	rootDir := filepath.Join(homeDir, opencodeConfigRootDirName, opencodeRootDirName)
	manifestPath := filepath.Join(rootDir, opencodeManifestFileName)
	skillsDir := filepath.Join(rootDir, opencodeSkillsDirName)
	commandsDir := filepath.Join(rootDir, opencodeCommandsDirName)
	return HarnessLayout{
		Target:       TargetOpenCode,
		RootDir:      rootDir,
		ManifestPath: manifestPath,
		Paths: map[string]string{
			opencodeConfigRootPathKey:  filepath.Join(homeDir, opencodeConfigRootDirName),
			opencodeDirPathKey:         rootDir,
			opencodeAgentsPathKey:      filepath.Join(rootDir, opencodeAgentsFileName),
			opencodeJSONPathKey:        filepath.Join(rootDir, opencodeConfigFileName),
			opencodeSkillsDirPathKey:   skillsDir,
			opencodeCommandsDirPathKey: commandsDir,
			opencodeManifestPathKey:    manifestPath,
			"harness_root":             rootDir,
		},
	}
}

func (a opencodeAdapter) Render(_ context.Context, req RenderRequest) ([]RenderedFile, error) {
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

	rendered := make([]RenderedFile, 0, 1+len(req.effectiveManagedAgents(OpenCodeSkillPathResolver())))
	if containsComponent(components, ComponentCorePack) {
		agentsContent, err := renderOpenCodeAgentsMD(req)
		if err != nil {
			return nil, err
		}
		rendered = append(rendered, RenderedFile{
			Component:    ComponentCorePack,
			RelativePath: opencodeAgentsFileName,
			MergeMode:    MergeModeReplace,
			Content:      agentsContent,
		})
		rendered = append(rendered, renderOpenCodeManagedSkills(req)...)
	}
	if containsComponent(components, ComponentExtendedSkills) {
		rendered = append(rendered, renderOpenCodeExtendedSkills(req)...)
	}
	commands, err := renderOpenCodeCommandFiles(req, false)
	if err != nil {
		return nil, err
	}
	if len(commands) > 0 {
		rendered = append(rendered, commands...)
	}
	sort.Slice(rendered, func(i, j int) bool { return rendered[i].RelativePath < rendered[j].RelativePath })
	return rendered, nil
}

func (a opencodeAdapter) RenderManagedAgents(context.Context, RenderRequest) ([]RenderedFile, error) {
	return nil, nil
}

func (a opencodeAdapter) RenderExtendedSkills(_ context.Context, req RenderRequest, _ PiLayout) ([]RenderedFile, error) {
	return renderOpenCodeExtendedSkills(req), nil
}

func renderOpenCodeAgentsMD(req RenderRequest) ([]byte, error) {
	definition := req.effectiveDefinition()
	models := openCodeAgentModels(req.AgentConfig)
	modelLines := make([]string, 0, len(models))
	for _, name := range agentpack.SDDPhaseAgentNames() {
		modelLines = append(modelLines, "- "+name+": `"+models[name]+"`")
	}

	instruction := strings.TrimRight(agentpack.RenderOrchestratorSystemInstruction(definition), "\n")
	text := strings.Join([]string{
		"# Lore Runtime",
		"",
		"This file is managed by `lore install --target opencode` and should not be edited manually.",
		"",
		"## OpenCode managed surface",
		"- Managed skills directory: `~/.config/opencode/skills`",
		"- Managed settings merge target: `~/.config/opencode/opencode.json` (Lore owns only the top-level `lore` block)",
		"- Managed manifest: `~/.config/opencode/lore-install.json`",
		"- Optional managed commands directory: `~/.config/opencode/commands` (omitted until an approved explicit command boundary exists)",
		"- Scope boundary: config-only Lore projection; no plugins, profiles, bootstrap/package-manager behavior, native/runtime subagents, or token persistence.",
		"",
		"## Managed SDD model declarations",
		strings.Join(modelLines, "\n"),
		"",
		"## Orchestrator instruction",
		"",
		instruction,
	}, "\n") + "\n"

	return []byte(text), nil
}

func renderOpenCodeManagedSkills(req RenderRequest) []RenderedFile {
	managedAgents := req.effectiveManagedAgents(OpenCodeSkillPathResolver())
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
			RelativePath: filepath.ToSlash(filepath.Join(opencodeSkillsDirName, agent.Name, "SKILL.md")),
			MergeMode:    MergeModeReplace,
			Content:      []byte(content),
		})
	}
	return rendered
}

func renderOpenCodeExtendedSkills(req RenderRequest) []RenderedFile {
	extendedSkills := req.effectiveExtendedSkills(OpenCodeSkillPathResolver())
	if len(extendedSkills) == 0 {
		return nil
	}
	rendered := make([]RenderedFile, 0, len(extendedSkills))
	for _, skill := range extendedSkills {
		content := renderManagedSkillMarkdown(skill)
		rendered = append(rendered, RenderedFile{
			Component:    ComponentExtendedSkills,
			RelativePath: filepath.ToSlash(filepath.Join(opencodeSkillsDirName, skill.Name, "SKILL.md")),
			MergeMode:    MergeModeReplace,
			Content:      []byte(content),
		})
	}
	return rendered
}

func OpenCodeSkillPathResolver() agentpack.SkillPathResolver {
	return agentpackSkillPathResolverFunc(func(ref agentpack.SkillRef) string {
		return filepath.ToSlash(filepath.Join("~/.config/opencode/skills", ref.Name, "SKILL.md"))
	})
}

func openCodeAgentModels(cfg agentconfig.Config) map[string]string {
	models := make(map[string]string, len(agentpack.SDDPhaseAgentNames()))
	for _, name := range agentpack.SDDPhaseAgentNames() {
		models[name] = agentpack.DefaultSDDModel
	}
	for name, agent := range cfg.SDDAgents {
		if strings.TrimSpace(agent.Model) == "" {
			continue
		}
		if _, ok := models[name]; ok {
			models[name] = strings.TrimSpace(agent.Model)
		}
	}
	return models
}
