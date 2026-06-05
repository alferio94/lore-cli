package install

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alferio94/lore-cli/internal/agentconfig"
	"github.com/alferio94/lore-cli/internal/agentpack"
)

// OpenCode install-path constants. All OpenCode-managed files live under
// `~/.config/opencode/`. The directory is the user-owned OpenCode config
// root; Lore only owns the documented Lore-managed surface and rejects
// ambiguous existing content (negative regression gates are enforced in a
// later slice).
const (
	opencodeConfigRootDirName = ".config"
	opencodeRootDirName       = "opencode"
	opencodeAgentsFileName    = "AGENTS.md"
	opencodeConfigFileName    = "opencode.json"
	opencodeSkillsDirName     = "skills"
	opencodeManifestFileName  = "lore-install.json"
)

// opencodeMCPBlockKey is the top-level key for the OpenCode remote MCP
// config. OpenCode uses `mcp` (not `mcpServers`) as the canonical
// top-level key.
const opencodeMCPBlockKey = "mcp"

// opencode layout path keys (shared with opencode_install.go).
const (
	opencodeConfigRootPathKey  = "config_root"
	opencodeDirPathKey         = "opencode_dir"
	opencodeAgentsPathKey      = "agents_md"
	opencodeJSONPathKey        = "opencode_json"
	opencodeSkillsDirPathKey   = "skills_dir"
	opencodeManifestPathKey    = "manifest"
	opencodePluginsDirPathKey  = "plugins_dir"
	opencodeTUISettingsPathKey = "tui_json"
)

// opencode managed-block keys (shared with opencode_install.go).
const (
	opencodeLoreBlockKey     = "lore"
	opencodeManagedByKey     = "managed_by"
	opencodeManagedByValue   = "lore-cli"
	opencodeSchemaVersionKey = "schema_version"
	opencodeAgentsKey        = "agents"
	opencodeSkillsDirKey     = "skills_dir"
)

// CapabilityOpenCodePlugins is the OpenCode plugin asset bundle
// capability. It is registered as a separate capability so the plugin
// bundle is selectable and testable in isolation, and the explicit
// exclusion list (`excludedOpenCodePluginNames`) is enforced in the
// plugin asset renderer and in the static guard.
const CapabilityOpenCodePlugins CapabilityID = "opencode-plugins"

// opencodeAdapter implements the shared-harness install pattern for
// OpenCode. It is bounded to the foundation slice: AGENTS.md,
// skills/<phase>/SKILL.md, opencode.json (with optional `mcp.lore`),
// and the manifest. Plugin assets and SDD command/prompt bundles
// belong to later slices.
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
			CapabilityLoreServerMCP: {
				ID:          CapabilityLoreServerMCP,
				Component:   ComponentLoreServerMCP,
				Description: "Optional Lore MCP configuration support for OpenCode (shaped like Pi/Antigravity remote MCP).",
				Optional:    true,
			},
			CapabilityExtendedSkills: {
				ID:          CapabilityExtendedSkills,
				Component:   ComponentExtendedSkills,
				Description: "Portable extended skill bundle for CLI-managed non-agent skills.",
				Optional:    true,
			},
			CapabilityOpenCodePlugins: {
				ID:          CapabilityOpenCodePlugins,
				Component:   ComponentOpenCodePlugins,
				Description: "Bounded OpenCode plugin asset bundle: background-agents.ts and model-variants.ts are copied to ~/.config/opencode/plugins/; the community opencode-subagent-statusline is registered in tui.json. Excludes sdd-engram and logo.",
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

// renderOpenCodePluginAssets returns the bundled OpenCode plugin .ts
// files and the `tui.json` settings file as rendered managed files.
// All bundled assets come from the embedded
// `internal/install/assets/opencode/` tree, which the static guard
// tests inspect to assert that `sdd-engram` and `logo` are never
// present in any bundled plugin asset.
func renderOpenCodePluginAssets() ([]RenderedFile, error) {
	rendered := make([]RenderedFile, 0, len(managedOpenCodePluginAssetNames)+1)
	for _, name := range managedOpenCodePluginAssetNames {
		content, err := readOpenCodePluginAsset(name)
		if err != nil {
			return nil, err
		}
		// Defense-in-depth: reject any plugin asset whose name matches
		// an excluded plugin (defense on top of readOpenCodePluginAsset).
		for _, excluded := range excludedOpenCodePluginNames {
			if matchesExcludedOpenCodePlugin(name, excluded) {
				return nil, fmt.Errorf("opencode plugin asset %q resolves to explicitly excluded plugin %q", name, excluded)
			}
		}
		rendered = append(rendered, RenderedFile{
			Component:    ComponentOpenCodePlugins,
			RelativePath: filepath.ToSlash(filepath.Join("plugins", name)),
			MergeMode:    MergeModeReplace,
			Content:      content,
		})
	}
	tuiContent, err := readOpenCodeTUISettingsAsset()
	if err != nil {
		return nil, err
	}
	rendered = append(rendered, RenderedFile{
		Component:    ComponentOpenCodePlugins,
		RelativePath: "tui.json",
		MergeMode:    MergeModeAdditiveJSON,
		Content:      tuiContent,
	})
	return rendered, nil
}

// matchesExcludedOpenCodePlugin reports whether the given plugin
// asset name resolves to an explicitly excluded plugin name. The
// matching is case-insensitive and tolerates a trailing `.ts` so the
// `sdd-engram` / `logo` exclusion list catches both bare names
// (`sdd-engram`) and file names (`sdd-engram.ts`).
func matchesExcludedOpenCodePlugin(assetName, excluded string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(assetName))
	target := strings.ToLower(excluded)
	if trimmed == target {
		return true
	}
	if trimmed == target+".ts" {
		return true
	}
	return false
}

// ResolveOpenCodeLayout returns the OpenCode harness layout for a home
// directory. The managed root is `~/.config/opencode/`.
func ResolveOpenCodeLayout(homeDir string) HarnessLayout {
	rootDir := filepath.Join(homeDir, opencodeConfigRootDirName, opencodeRootDirName)
	manifestPath := filepath.Join(rootDir, opencodeManifestFileName)
	return HarnessLayout{
		Target:       TargetOpenCode,
		RootDir:      rootDir,
		ManifestPath: manifestPath,
		Paths: map[string]string{
			opencodeConfigRootPathKey: filepath.Join(homeDir, opencodeConfigRootDirName),
			opencodeDirPathKey:        rootDir,
			opencodeAgentsPathKey:     filepath.Join(rootDir, opencodeAgentsFileName),
			opencodeJSONPathKey:       filepath.Join(rootDir, opencodeConfigFileName),
			opencodeSkillsDirPathKey:  filepath.Join(rootDir, opencodeSkillsDirName),
			opencodeManifestPathKey:   manifestPath,
			"harness_root":            rootDir,
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
	if containsComponent(components, ComponentOpenCodePlugins) {
		pluginFiles, err := renderOpenCodePluginAssets()
		if err != nil {
			return nil, err
		}
		rendered = append(rendered, pluginFiles...)
	}
	// opencode.json (with the optional mcp.lore block) is produced by
	// renderOpenCodeFiles in opencode_install.go so the install pipeline
	// can reason about merge/backup semantics there. The adapter stays
	// focused on managed markdown surfaces and explicitly does NOT
	// render opencode.json from Render(); the installer layers the
	// mcp-aware file on top when lore-server-mcp is selected.
	sort.Slice(rendered, func(i, j int) bool { return rendered[i].RelativePath < rendered[j].RelativePath })
	return rendered, nil
}

// RenderManagedAgents returns no Pi-style managed overlays for OpenCode.
// OpenCode uses the portable agent pack as the source of truth; managed
// overlays are not part of the bounded foundation slice.
func (a opencodeAdapter) RenderManagedAgents(context.Context, RenderRequest) ([]RenderedFile, error) {
	return nil, nil
}

// RenderExtendedSkills returns the extended skills bundle for OpenCode.
// All paths are CLI-managed and the user-owned paths are out of scope.
func (a opencodeAdapter) RenderExtendedSkills(_ context.Context, req RenderRequest, _ PiLayout) ([]RenderedFile, error) {
	return renderOpenCodeExtendedSkills(req), nil
}

func renderOpenCodeAgentsMD(req RenderRequest) ([]byte, error) {
	definition := req.effectiveDefinition()
	models := openCodeAgentModels(req.AgentConfig)
	modelLines := make([]string, 0, len(models))
	for _, name := range agentpack.SDDPhaseAgentNames() {
		modelLines = append(modelLines, "- "+name+": "+models[name])
	}

	instruction := strings.TrimRight(agentpack.RenderOrchestratorSystemInstruction(definition), "\n")
	text := strings.Join([]string{
		"# Lore Runtime",
		"",
		"This file is managed by `lore install --target opencode` and should not be edited manually.",
		"",
		"## OpenCode managed surface",
		"- Managed skills directory: `~/.config/opencode/skills`",
		"- Managed settings merge target: `~/.config/opencode/opencode.json` (Lore owns the top-level `lore` block and, when lore-server-mcp is selected, the `mcp.lore` remote entry)",
		"- Managed plugin bundle: `~/.config/opencode/plugins/` (default component: `opencode-plugins`). Bundled assets: `background-agents.ts`, `model-variants.ts`, and the community `opencode-subagent-statusline`. Managed TUI settings: `~/.config/opencode/tui.json` references the community statusline and declares the explicit exclusion list under `lore.plugins_excluded`.",
		"- Explicit exclusions: the installer NEVER bundles, renders, or registers `sdd-engram` or `logo`. The exclusion list is enforced at the embed.FS static guard, the plugin asset reader, and the tui.json plugin allowlist.",
		"- Managed manifest: `~/.config/opencode/lore-install.json`",
		"- Scope boundary: config-only Lore projection; no profiles, bootstrap/package-manager behavior, or native/runtime subagents in this slice. The bundled plugin set is bounded to the three plugin .ts files plus `tui.json` and the explicit exclusion list above.",
		"- Lore server MCP token: when lore-server-mcp is selected, the bearer token is persisted in opencode.json under `mcp.lore.headers.Authorization` and a plaintext-token warning (`auth_header=plaintext-bearer-token`) appears at install time. The install summary never embeds the saved token; only the path, the server URL, and the auth header name are surfaced.",
		"- `mcp.lore` ownership: the installer writes a `managed_by: lore-cli` marker on the `mcp.lore` block. The installer only ever overwrites the `mcp.lore` subtree when the existing block is already Lore-owned; a non-Lore-owned `mcp.lore` block (owned by another tool, missing the marker, or hand-edited) is treated as a foreign MCP configuration and the installer fails closed with a typed conflict error. The existing file is backed up to the managed backup root before the installer aborts, the error names the conflicting `mcp.lore` `type` and `url` (without the token), and the resolution guidance points the user at editing the existing block or removing the conflicting `mcp.lore` subtree.",
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
		// Use the canonical phase name (e.g., "sdd-propose" rather than
		// "sdd-proposal") for skill paths and frontmatter so OpenCode
		// references align with the canonical SDD phase naming.
		skillDirName := canonicalOpenCodePhaseName(agentpack.PhaseID(agent.Name))
		content := strings.Join([]string{
			"---",
			"name: " + skillDirName,
			"description: " + agent.Description,
			"---",
			agent.Body,
		}, "\n")
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		rendered = append(rendered, RenderedFile{
			Component:    ComponentCorePack,
			RelativePath: filepath.ToSlash(filepath.Join(opencodeSkillsDirName, skillDirName, "SKILL.md")),
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

// OpenCodeSkillPathResolver returns a SkillPathResolver for the OpenCode
// harness.
func OpenCodeSkillPathResolver() agentpack.SkillPathResolver {
	return agentpackSkillPathResolverFunc(func(ref agentpack.SkillRef) string {
		return filepath.ToSlash(filepath.Join("~/.config/opencode/skills", ref.Name, "SKILL.md"))
	})
}

// openCodeAgentModels derives the per-phase SDD model map from the
// agent-config.json file, falling back to the agentpack default model.
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

// canonicalOpenCodePhaseName returns the approved canonical phase name
// used in OpenCode skill paths and frontmatter. PhaseProposal maps to
// "propose"; all other phases use their raw name.
func canonicalOpenCodePhaseName(phase agentpack.PhaseID) string {
	switch phase {
	case agentpack.PhaseProposal:
		return "propose"
	default:
		return string(phase)
	}
}

// renderOpenCodeLoreBlock returns the standalone lore block (no mcp
// entry). It is used by the opencode_install.go install pipeline when
// the lore-server-mcp component is not selected. The merge-aware flow
// (existing-file detection, backup/restore, additive mcp.lore) belongs
// to a later regression slice.
func renderOpenCodeLoreBlock(cfg agentconfig.Config) ([]byte, error) {
	models := openCodeAgentModels(cfg)
	agents := make(map[string]map[string]string, len(models))
	for _, name := range agentpack.SDDPhaseAgentNames() {
		agents[name] = map[string]string{"model": models[name]}
	}

	lore := map[string]any{
		opencodeManagedByKey:     opencodeManagedByValue,
		opencodeSchemaVersionKey: 1,
		opencodeAgentsKey:        agents,
		opencodeSkillsDirKey:     "~/.config/opencode/skills",
	}

	payload := map[string]any{opencodeLoreBlockKey: lore}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode opencode lore block: %w", err)
	}
	return append(data, '\n'), nil
}

// renderOpenCodeMCPConfig returns the opencode.json file with both the
// top-level `lore` block and the `mcp.lore` remote entry. Shaped like
// Pi/Antigravity remote MCP. This is the "fresh write" path used when
// the installer detects no existing user-managed opencode.json; the
// additive merge path belongs to a later slice.
func renderOpenCodeMCPConfig(cfg agentconfig.Config, serverURL, token string) ([]byte, error) {
	normalizedServerURL := strings.TrimRight(strings.TrimSpace(serverURL), "/")
	if normalizedServerURL == "" {
		return nil, fmt.Errorf("server-url is required for OpenCode MCP config")
	}
	trimmedToken := strings.TrimSpace(token)
	if trimmedToken == "" {
		return nil, fmt.Errorf("saved token is required for OpenCode MCP config")
	}

	models := openCodeAgentModels(cfg)
	agents := make(map[string]map[string]string, len(models))
	for _, name := range agentpack.SDDPhaseAgentNames() {
		agents[name] = map[string]string{"model": models[name]}
	}

	lore := map[string]any{
		opencodeManagedByKey:     opencodeManagedByValue,
		opencodeSchemaVersionKey: 1,
		opencodeAgentsKey:        agents,
		opencodeSkillsDirKey:     "~/.config/opencode/skills",
	}

	// The `managed_by: lore-cli` marker on the mcp.lore block is the
	// ownership contract: the additive merge in `mergeOpenCodeConfigJSON`
	// is allowed to overwrite the mcp.lore subtree when and only when the
	// existing block is already Lore-owned. A foreign mcp.lore block
	// (managed by anything else, or missing the marker entirely) MUST
	// fail closed with a clear conflict error so the installer never
	// silently clobbers a user-owned or third-party MCP configuration.
	// The token is intentionally NOT surfaced in the conflict error.
	mcpPayload := map[string]any{
		"managed_by": opencodeManagedByValue,
		"type":       "remote",
		"url":        normalizedServerURL + "/v1/mcp",
		"enabled":    true,
		"headers": map[string]any{
			"Authorization": "Bearer " + trimmedToken,
		},
	}

	payload := map[string]any{
		opencodeLoreBlockKey: lore,
		opencodeMCPBlockKey:  map[string]any{"lore": mcpPayload},
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode opencode mcp config: %w", err)
	}
	return append(data, '\n'), nil
}
