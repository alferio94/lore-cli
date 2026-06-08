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

// opencodeConfigSchemaURL is the documented `$schema` URL for the
// native OpenCode `opencode.json` file. The installer always writes
// this schema reference so editor tooling can validate the file.
const opencodeConfigSchemaURL = "https://opencode.ai/config.json"

// opencodeTUISettingsSchemaURL is the documented `$schema` URL for
// the native OpenCode `tui.json` file.
const opencodeTUISettingsSchemaURL = "https://opencode.ai/tui.json"

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
// These keys describe the schema that the installer renders into
// the native `opencode.json` file. The `opencodeLoreBlockKey` is no
// longer written into the opencode.json payload (the post-repair
// shape uses the native `agent` overlay block, not a top-level
// `lore` metadata object), but the constant is preserved as the
// ownership marker for the migration layer so legacy `lore`-shaped
// blocks in existing files are detected and stripped.
const (
	opencodeLoreBlockKey     = "lore"
	opencodeManagedByKey     = "managed_by"
	opencodeManagedByValue   = "lore-cli"
	opencodeSchemaVersionKey = "schema_version"
	opencodeAgentsKey        = "agent"
	opencodeSkillsDirKey     = "skills"
	opencodeSkillsDirPath    = "~/.config/opencode/skills"
	opencodeThemeKey         = "theme"
	opencodeThemeValue       = "system"
	opencodeMCPLoreKey       = "lore"
)

// opencodeManagedPluginNames is the bounded set of plugin names
// the OpenCode installer registers in the native `tui.json` file.
// The OpenCode TUI native shape uses a singular `plugin` string
// array (e.g. `["opencode-subagent-statusline"]`), and only the
// community statusline is registered — local plugin .ts files
// (background-agents.ts, model-variants.ts) are copied into the
// plugins/ directory but are NOT registered as native TUI plugins
// (they are picked up automatically from the plugins/ directory).
const (
	opencodeCommunityStatuslinePlugin = "opencode-subagent-statusline"
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
			opencodeConfigRootPathKey:  filepath.Join(homeDir, opencodeConfigRootDirName),
			opencodeDirPathKey:         rootDir,
			opencodeAgentsPathKey:      filepath.Join(rootDir, opencodeAgentsFileName),
			opencodeJSONPathKey:        filepath.Join(rootDir, opencodeConfigFileName),
			opencodeSkillsDirPathKey:   filepath.Join(rootDir, opencodeSkillsDirName),
			opencodeManifestPathKey:    manifestPath,
			opencodePluginsDirPathKey:  filepath.Join(rootDir, "plugins"),
			opencodeTUISettingsPathKey: filepath.Join(rootDir, "tui.json"),
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
		"- Managed settings merge target: `~/.config/opencode/opencode.json` (native OpenCode shape: `$schema: https://opencode.ai/config.json`, the native `agent` overlay wiring every SDD phase agent to its `~/.config/opencode/skills/<name>/SKILL.md` prompt via `{file:...}` references, and — when lore-server-mcp is selected — the documented `mcp.lore` remote entry). The installer never writes a top-level Lore-only `lore` metadata block into opencode.json.",
		"- Managed plugin bundle: `~/.config/opencode/plugins/` (default component: `opencode-plugins`). Bundled assets: `background-agents.ts`, `model-variants.ts`, and the community `opencode-subagent-statusline`. The native `tui.json` registers ONLY the community statusline in its singular `plugin` string array; local plugin .ts files are picked up automatically from the plugins/ directory and are not registered in tui.json.",
		"- Explicit exclusions: the installer NEVER bundles, renders, or registers `sdd-engram` or `logo`. The exclusion list is enforced at the embed.FS static guard, the plugin asset reader, and the tui.json plugin allowlist.",
		"- Managed manifest: `~/.config/opencode/lore-install.json`",
		"- Scope boundary: config-only Lore projection; no profiles, bootstrap/package-manager behavior, or native/runtime subagents in this slice. The bundled plugin set is bounded to the three plugin .ts files plus `tui.json` and the explicit exclusion list above.",
		"- Lore server MCP token: when lore-server-mcp is selected, the bearer token is persisted in opencode.json under `mcp.lore.headers.Authorization` and a plaintext-token warning (`auth_header=plaintext-bearer-token`) appears at install time. The install summary never embeds the saved token; only the path, the server URL, and the auth header name are surfaced.",
		"- `mcp.lore` ownership: the installer writes a `managed_by: lore-cli` marker on the `mcp.lore` block. The installer only ever overwrites the `mcp.lore` subtree when the existing block is already Lore-owned; a non-Lore-owned `mcp.lore` block (owned by another tool, missing the marker, or hand-edited) is treated as a foreign MCP configuration and the installer fails closed with a typed conflict error. The existing file is backed up to the managed backup root before the installer aborts, the error names the conflicting `mcp.lore` `type` and `url` (without the token), and the resolution guidance points the user at editing the existing block or removing the conflicting `mcp.lore` subtree.",
		"- Migration: when an existing install was produced by the legacy `lore`-shaped renderer (a top-level `lore` block in opencode.json, or a plural `plugins` object array plus a top-level `lore` block in tui.json), the additive merge drops the stale shape and writes the native shape on the next run. The existing user-owned top-level keys (e.g. `theme`, custom `mcp.<other>` entries, user `agent` overrides) are preserved.",
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

// opencodeAgentOverlay returns the native `agent` overlay object
// that wires each canonical SDD phase agent into OpenCode's native
// `opencode.json` config. The overlay is shaped like the
// documented OpenCode `agent` block: each agent maps to a
// `{model, prompt}` pair, and the `prompt` is a `{file:./path}`
// reference to the corresponding managed SKILL.md file. The
// overlay is intentionally a NATIVE OpenCode config artifact, not
// a Lore metadata block; OpenCode consumes it without any
// adapter layer. The model values come from the per-agent
// agent-config.json overrides when present and fall back to the
// agentpack default model otherwise.
func opencodeAgentOverlay(cfg agentconfig.Config) map[string]any {
	models := openCodeAgentModels(cfg)
	overlay := make(map[string]any, len(models))
	for _, name := range agentpack.SDDPhaseAgentNames() {
		overlay[name] = map[string]any{
			"model":  models[name],
			"prompt": "{file:./skills/" + name + "/SKILL.md}",
		}
	}
	return overlay
}

// opencodeSkillsBlock returns the native `skills` block for the
// `opencode.json` file. The block declares the path of the
// managed skills directory so OpenCode can resolve the
// `{file:./skills/<name>/SKILL.md}` references declared on each
// `agent.prompt` field. The value is a small `{path, ...}` map
// that matches the documented OpenCode shape and never includes
// any Lore-specific metadata.
func opencodeSkillsBlock() map[string]any {
	return map[string]any{
		"path": opencodeSkillsDirPath,
	}
}

// renderOpenCodeNativeConfig returns the opencode.json payload in
// the native OpenCode shape, with NO top-level `lore` metadata
// block. The shape is: `$schema`, `theme`, the native `agent`
// overlay (model + `{file:./skills/<name>/SKILL.md}` prompt
// reference per SDD phase), and a `skills.path` declaration.
// When the caller wants the MCP-enabled variant they should call
// `renderOpenCodeMCPConfig` instead, which extends this shape
// with the documented top-level `mcp.lore` remote entry.
//
// The function is the bounded post-repair replacement for the
// legacy `renderOpenCodeLoreBlock` helper, which produced a
// `lore`-only metadata blob. The new shape is what the
// OpenCode-native config contract expects and is the source of
// truth for the user-owned `~/.config/opencode/opencode.json`
// file.
func renderOpenCodeNativeConfig(cfg agentconfig.Config) ([]byte, error) {
	payload := map[string]any{
		opencodeSchemaKey():       opencodeConfigSchemaURL,
		opencodeThemeKey:          opencodeThemeValue,
		opencodeAgentsKey:         opencodeAgentOverlay(cfg),
		opencodeSkillsDirKey:      opencodeSkillsBlock(),
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode opencode native config: %w", err)
	}
	return append(data, '\n'), nil
}

// opencodeSchemaKey returns the JSON key for the `$schema`
// property. Encoding it as `map[string]any{...}` would force the
// field name to be the literal string `opencodeSchemaKey` instead
// of `$schema`; the helper centralizes the escape so callers do
// not have to remember it. The key is intentionally a function so
// it cannot be shadowed by a const rename.
func opencodeSchemaKey() string { return "$schema" }

// renderOpenCodeMCPConfig returns the opencode.json file in the
// native OpenCode shape with the documented top-level `mcp.lore`
// remote entry appended. The shape is identical to
// `renderOpenCodeNativeConfig` (no top-level `lore` block,
// native `agent` overlay, native `skills` block) plus the
// `mcp.lore` remote entry. Shaped like the documented OpenCode
// remote MCP contract: `type: remote`, normalized server URL, and
// a Bearer Authorization header.
//
// The `managed_by: lore-cli` marker on the mcp.lore block is the
// ownership contract: the additive merge in
// `mergeOpenCodeConfigJSON` is allowed to overwrite the mcp.lore
// subtree when and only when the existing block is already
// Lore-owned. A foreign mcp.lore block (managed by anything else,
// or missing the marker entirely) MUST fail closed with a clear
// conflict error so the installer never silently clobbers a
// user-owned or third-party MCP configuration. The token is
// intentionally NOT surfaced in the conflict error.
func renderOpenCodeMCPConfig(cfg agentconfig.Config, serverURL, token string) ([]byte, error) {
	normalizedServerURL := strings.TrimRight(strings.TrimSpace(serverURL), "/")
	if normalizedServerURL == "" {
		return nil, fmt.Errorf("server-url is required for OpenCode MCP config")
	}
	trimmedToken := strings.TrimSpace(token)
	if trimmedToken == "" {
		return nil, fmt.Errorf("saved token is required for OpenCode MCP config")
	}

	mcpPayload := map[string]any{
		opencodeManagedByKey: opencodeManagedByValue,
		"type":               "remote",
		"url":                normalizedServerURL + "/v1/mcp",
		"enabled":            true,
		"headers": map[string]any{
			"Authorization": "Bearer " + trimmedToken,
		},
	}

	payload := map[string]any{
		opencodeSchemaKey():  opencodeConfigSchemaURL,
		opencodeThemeKey:     opencodeThemeValue,
		opencodeAgentsKey:    opencodeAgentOverlay(cfg),
		opencodeSkillsDirKey: opencodeSkillsBlock(),
		opencodeMCPBlockKey:  map[string]any{opencodeMCPLoreKey: mcpPayload},
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode opencode mcp config: %w", err)
	}
	return append(data, '\n'), nil
}
