package install

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alferio94/lore-cli/internal/agentpack"
)

const (
	antigravityPromptStartMarker = "<!-- lore-cli:antigravity:start -->"
	antigravityPromptEndMarker   = "<!-- lore-cli:antigravity:end -->"
)

type antigravityAdapter struct {
	target       TargetID
	title        string
	capabilities map[CapabilityID]Capability
}

func defaultAntigravityAdapter() HarnessAdapter {
	return antigravityAdapter{
		target: TargetAntigravity,
		title:  "Antigravity",
		capabilities: map[CapabilityID]Capability{
			CapabilityAgentPack: {
				ID:               CapabilityAgentPack,
				Component:        ComponentCorePack,
				Description:      "Render the portable Lore core pack for Antigravity-owned prompt and skills surfaces.",
				EnabledByDefault: true,
			},
			CapabilityPrompt: {
				ID:          CapabilityPrompt,
				Description: "Harness-owned prompt merge support.",
			},
			CapabilitySkills: {
				ID:          CapabilitySkills,
				Description: "Harness-owned skills installation support.",
			},
			CapabilityLoreServerMCP: {
				ID:               CapabilityLoreServerMCP,
				Component:        ComponentLoreServerMCP,
				Description:      "Optional MCP configuration support for Antigravity.",
				Optional:         true,
				EnabledByDefault: true,
			},
		},
	}
}

func (a antigravityAdapter) ID() TargetID  { return a.target }
func (a antigravityAdapter) Title() string { return a.title }

func (a antigravityAdapter) Capabilities() map[CapabilityID]Capability {
	copyMap := make(map[CapabilityID]Capability, len(a.capabilities))
	for key, value := range a.capabilities {
		copyMap[key] = value
	}
	return copyMap
}

func (a antigravityAdapter) Supports(component ComponentID) bool {
	for _, capability := range a.capabilities {
		if capability.Component == component {
			return true
		}
	}
	return false
}

func ResolveAntigravityLayout(homeDir string) HarnessLayout {
	geminiDir := filepath.Join(homeDir, ".gemini")
	rootDir := filepath.Join(geminiDir, "antigravity-cli")
	manifestPath := filepath.Join(rootDir, "lore-install.json")
	sharedPromptPath := filepath.Join(geminiDir, "GEMINI.md")
	skillsDir := filepath.Join(rootDir, "skills")
	geminiConfigDir := filepath.Join(geminiDir, "config")
	mcpPath := filepath.Join(geminiConfigDir, "mcp_config.json")
	agentsDir := filepath.Join(geminiConfigDir, "agents")
	agentProfilePath := filepath.Join(agentsDir, "lore.json")
	return HarnessLayout{
		Target:       TargetAntigravity,
		RootDir:      rootDir,
		ManifestPath: manifestPath,
		Paths: map[string]string{
			"gemini_dir":        geminiDir,
			"gemini_config_dir": geminiConfigDir,
			"shared_prompt":     sharedPromptPath,
			"skills_dir":        skillsDir,
			"manifest":          manifestPath,
			"mcp_config":        mcpPath,
			"agents_dir":        agentsDir,
			"agent_profile":     agentProfilePath,
			"harness_root":      rootDir,
			"antigravity_dir":   rootDir,
		},
	}
}

func (a antigravityAdapter) Render(_ context.Context, req RenderRequest) ([]RenderedFile, error) {
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

	definition := req.effectiveDefinition()
	rendered := []RenderedFile{{
		Component:    ComponentCorePack,
		RelativePath: filepath.ToSlash(filepath.Join("..", "GEMINI.md")),
		MergeMode:    MergeMode("marker-merge"),
		Content:      renderAntigravityPrompt(definition),
	}}
	rendered = append(rendered, renderAntigravitySkills(req)...)
	agentProfile, err := renderAntigravityAgentProfile(definition)
	if err != nil {
		return nil, err
	}
	rendered = append(rendered, RenderedFile{
		Component:    ComponentCorePack,
		RelativePath: filepath.ToSlash(filepath.Join("..", "config", "agents", "lore.json")),
		MergeMode:    MergeModeReplace,
		Content:      agentProfile,
	})
	if containsComponent(components, ComponentLoreServerMCP) {
		content, err := renderAntigravityMCPConfig(req.ServerURL, req.SavedToken)
		if err != nil {
			return nil, err
		}
		rendered = append(rendered, RenderedFile{
			Component:    ComponentLoreServerMCP,
			RelativePath: filepath.ToSlash(filepath.Join("..", "config", "mcp_config.json")),
			MergeMode:    MergeModeAdditiveJSON,
			Content:      content,
		})
	}
	sort.Slice(rendered, func(i, j int) bool { return rendered[i].RelativePath < rendered[j].RelativePath })
	return rendered, nil
}

func (a antigravityAdapter) RenderManagedAgents(context.Context, RenderRequest) ([]RenderedFile, error) {
	return nil, nil
}

func renderAntigravityPrompt(definition agentpack.Definition) []byte {
	if definition.SchemaVersion == 0 {
		definition = agentpack.DefaultDefinition()
	}
	phases := make([]string, 0, len(definition.Workflow.Phases))
	for _, phase := range definition.Workflow.Phases {
		phases = append(phases, agentpack.PhaseAgentName(phase.ID))
	}
	text := strings.Join([]string{
		antigravityPromptStartMarker,
		"# Lore Runtime",
		"",
		"This section is managed for Antigravity and should be appended or refreshed in place without replacing unrelated shared prompt content.",
		"",
		fmt.Sprintf("- Persona: `%s`", definition.Persona.Name),
		"- Managed skills guidance: `~/.gemini/antigravity-cli/skills`",
		"- Managed manifest: `~/.gemini/antigravity-cli/lore-install.json`",
		fmt.Sprintf("- Managed SDD phases: `%s`", strings.Join(phases, "`, `")),
		"",
		"Load the Lore-managed skill files from the Antigravity skills directory when a task explicitly requires them.",
		"For SDD, the orchestrator delegates each phase to the matching managed sdd-* phase worker when available; phase workers persist full artifacts and return compact envelopes.",
		"Do not manually author SDD phase artifacts from the orchestrator as a shortcut unless inline execution was explicitly requested or delegation is unavailable.",
		antigravityPromptEndMarker,
	}, "\n") + "\n"
	return []byte(text)
}

func renderAntigravitySkills(req RenderRequest) []RenderedFile {
	managedAgents := req.effectiveManagedAgents(agentpack.AntigravitySkillPathResolver())
	rendered := make([]RenderedFile, 0, len(managedAgents))
	for _, agent := range managedAgents {
		content := strings.Join([]string{
			"---",
			fmt.Sprintf("name: %s", agent.Name),
			fmt.Sprintf("description: %s", agent.Description),
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
	sort.Slice(rendered, func(i, j int) bool { return rendered[i].RelativePath < rendered[j].RelativePath })
	return rendered
}

type antigravityAgentProfile struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	SystemInstruction string `json:"systemInstruction"`
	Default           bool   `json:"default"`
}

func renderAntigravityAgentProfile(definition agentpack.Definition) ([]byte, error) {
	payload := antigravityAgentProfile{
		ID:                "lore",
		Name:              "Lore",
		Description:       "Global Lore orchestrator specialized in SDD workflows and persistent context through Lore MCP",
		SystemInstruction: renderAntigravityAgentSystemInstruction(definition),
		Default:           true,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode antigravity agent profile: %w", err)
	}
	return append(data, '\n'), nil
}

func renderAntigravityAgentSystemInstruction(definition agentpack.Definition) string {
	base := strings.TrimRight(agentpack.RenderOrchestratorSystemInstruction(definition), "\n")
	suffix := strings.Join([]string{
		"Antigravity/Gemini runtime notes:",
		"- Keep the shared Lore-managed skills under `~/.gemini/antigravity-cli/skills`.",
		"- Keep the Lore-managed manifest at `~/.gemini/antigravity-cli/lore-install.json`.",
		"- Treat `~/.gemini/GEMINI.md` as the shared prompt file and `~/.gemini/config/agents/lore.json` as the managed Gemini agent profile installed by Lore CLI.",
		"- Optional managed MCP config lives at `~/.gemini/config/mcp_config.json`.",
		"- Keep Antigravity on its managed prompt-and-skills path; do not assume Pi-style overlays, daemons, autostart, or background subagent parity.",
		"- Lore MCP tools are exposed according to the user's current role and permissions, so `tools` is intentionally omitted from this profile.",
	}, "\n")
	return base + "\n\n" + suffix + "\n"
}

func renderAntigravityMCPConfig(serverURL, token string) ([]byte, error) {
	normalizedServerURL := strings.TrimRight(strings.TrimSpace(serverURL), "/")
	if normalizedServerURL == "" {
		return nil, fmt.Errorf("server url is required")
	}
	trimmedToken := strings.TrimSpace(token)
	if trimmedToken == "" {
		return nil, fmt.Errorf("saved token is required")
	}
	payload := map[string]any{
		"mcpServers": map[string]any{
			"lore": map[string]any{
				"serverUrl": normalizedServerURL + "/v1/mcp",
				"headers": map[string]any{
					"Authorization": "Bearer " + trimmedToken,
				},
			},
		},
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode antigravity mcp config: %w", err)
	}
	return append(data, '\n'), nil
}

func mergeAntigravityPrompt(existing, managed []byte) ([]byte, error) {
	existingText := strings.TrimRight(string(existing), "\n")
	managedText := strings.TrimSpace(string(managed))
	if managedText == "" {
		return nil, fmt.Errorf("managed Antigravity prompt block is required")
	}
	start := strings.Index(existingText, antigravityPromptStartMarker)
	end := strings.Index(existingText, antigravityPromptEndMarker)
	if start >= 0 || end >= 0 {
		if start < 0 || end < 0 || end < start {
			return nil, fmt.Errorf("existing GEMINI.md contains an incomplete Lore-managed Antigravity prompt block")
		}
		end += len(antigravityPromptEndMarker)
		prefix := strings.TrimRight(existingText[:start], "\n")
		suffix := strings.TrimLeft(existingText[end:], "\n")
		parts := make([]string, 0, 3)
		if prefix != "" {
			parts = append(parts, prefix)
		}
		parts = append(parts, managedText)
		if suffix != "" {
			parts = append(parts, suffix)
		}
		return []byte(strings.Join(parts, "\n\n") + "\n"), nil
	}
	if strings.TrimSpace(existingText) == "" {
		return []byte(managedText + "\n"), nil
	}
	return []byte(existingText + "\n\n" + managedText + "\n"), nil
}

func buildAntigravityManifest(layout HarnessLayout, req InstallRequest, files []RenderedFile) (Manifest, []string, error) {
	if layout.Target != TargetAntigravity {
		return Manifest{}, nil, fmt.Errorf("layout target %q does not match antigravity", layout.Target)
	}
	components, err := NormalizeComponentSelection(TargetAntigravity, req.Components)
	if err != nil {
		return Manifest{}, nil, err
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	records := make([]ManagedFileRecord, 0, len(files))
	managedPaths := make([]string, 0, len(files))
	for _, file := range files {
		absolutePath := antigravityAbsolutePath(layout, file.RelativePath)
		managedPaths = append(managedPaths, absolutePath)
		records = append(records, ManagedFileRecord{
			Path:        absolutePath,
			Component:   file.Component,
			MergeMode:   file.MergeMode,
			ContentHash: contentHash(file.Content),
		})
	}
	manifest := Manifest{
		SchemaVersion: PortableManifestSchemaVersion,
		Target:        TargetAntigravity,
		AuthMode:      "cli-request",
		ServerURL:     strings.TrimSpace(req.ServerURL),
		LoreBinary:    strings.TrimSpace(req.LoreBinaryPath),
		LoreConfigDir: strings.TrimSpace(req.LoreConfigDir),
		Components:    append([]ComponentID(nil), components...),
		ManagedFiles:  records,
		BackupRoot:    filepath.Join(layout.RootDir, "backups", now.UTC().Format("20060102T150405Z")),
		InstalledAt:   now.UTC().Format(time.RFC3339),
		CLIVersion:    strings.TrimSpace(req.LoreCLIVersion),
	}
	return manifest, managedPaths, nil
}

func antigravityAbsolutePath(layout HarnessLayout, relativePath string) string {
	cleanRelativePath := filepath.ToSlash(relativePath)
	switch cleanRelativePath {
	case filepath.ToSlash(filepath.Join("..", "GEMINI.md")):
		return layout.Paths["shared_prompt"]
	case filepath.ToSlash(filepath.Join("..", "config", "mcp_config.json")):
		return layout.Paths["mcp_config"]
	case filepath.ToSlash(filepath.Join("..", "config", "agents", "lore.json")):
		return layout.Paths["agent_profile"]
	default:
		return filepath.Join(layout.RootDir, filepath.FromSlash(cleanRelativePath))
	}
}
