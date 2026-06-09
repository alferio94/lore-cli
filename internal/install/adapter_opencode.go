package install

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
//
// The `opencodeAgentVariantKey` is added by the
// `add-opencode-lore-models-plugin` change to support per-agent
// `variant` persistence in `agent.<name>.variant`. The
// `opencodeSubagentModeValue` is the documented OpenCode
// subagent-mode value for non-primary Lore-managed agents (SDD
// phase agents + the `lore-worker` repository worker). The
// `opencodeLoreWorkerAgentName` is the canonical worker agent name
// rendered into the `agent` overlay alongside the SDD phase agents.
const (
	opencodeLoreBlockKey         = "lore"
	opencodeManagedByKey         = "managed_by"
	opencodeManagedByValue       = "lore-cli"
	opencodeSchemaVersionKey     = "schema_version"
	opencodeAgentsKey            = "agent"
	opencodeSkillsDirKey         = "skills"
	opencodeSkillsDirPath        = "~/.config/opencode/skills"
	opencodeThemeKey             = "theme"
	opencodeThemeValue           = "system"
	opencodeMCPLoreKey           = "lore"
	opencodeDefaultAgentKey      = "default_agent"
	opencodePermissionKey        = "permission"
	opencodeAgentModeKey         = "mode"
	opencodePrimaryModeValue     = "primary"
	opencodeSubagentModeValue    = "subagent"
	opencodeAgentModelKey        = "model"
	opencodeAgentVariantKey      = "variant"
	opencodeLoreWorkerAgentName  = "lore-worker"
	opencodeLoreWorkerPromptFile = "skills/lore-worker/SKILL.md"
)

// opencodePrimaryAgentName is the canonical name of the primary
// orchestrator agent declared in the native `opencode.json`
// `agent` overlay. The name mirrors the Antigravity agent profile
// convention (`antigravityAgentProfile.ID = "lore"`) so the same
// `lore` identity is the primary orchestrator across every
// Lore-managed harness. Selecting this agent is the documented way
// to run OpenCode with the global Lore orchestrator (which owns
// decisions, pacing, user-facing synthesis, and SDD phase
// delegation) rather than letting OpenCode fall back to the
// built-in `build` agent.
const opencodePrimaryAgentName = "lore"

// opencodePrimaryAgentPromptFile is the relative path of the
// managed prompt body for the primary orchestrator agent. The
// managed AGENTS.md file already contains the canonical
// orchestrator system instruction rendered by
// `agentpack.RenderOrchestratorSystemInstruction`, so the primary
// agent reuses that file via a `{file:./<path>}` reference instead
// of duplicating the instruction into a separate SKILL.md. The
// reference is resolved relative to the opencode.json file
// (i.e. `~/.config/opencode/`).
const opencodePrimaryAgentPromptFile = "AGENTS.md"

// opencodePrimaryAgentDescription is the human-readable description
// for the primary orchestrator agent entry. It is rendered into the
// native `agent` overlay so OpenCode tooling (CLI help, picker UIs)
// can show the primary agent's intent. The description is
// intentionally short and self-contained so the opencode.json file
// stays compact.
const opencodePrimaryAgentDescription = "Global Lore orchestrator. Owns decisions, pacing, user-facing synthesis, and SDD phase delegation. Loads ~/.config/opencode/AGENTS.md (rendered from agentpack.RenderOrchestratorSystemInstruction)."

// opencodePrimaryAgentModelFallback is the model used for the
// primary orchestrator agent when the `ProfileBalanced.RoleModels["orchestrator"]`
// lookup fails or returns an empty string. The fallback aliases
// `agentpack.DefaultSDDModel` to avoid a hard dependency on a
// specific model name in the additive-merge regression gates.
const opencodePrimaryAgentModelFallback = agentpack.DefaultSDDModel

// opencodeManagedPluginNames is the bounded set of plugin names
// the OpenCode installer registers in the native `tui.json` file.
// The OpenCode TUI native shape uses a singular `plugin` string
// array (e.g. `["opencode-subagent-statusline"]`), and only the
// community statusline is registered — local plugin .ts files
// (background-agents.ts, lore-models.ts) are copied into the
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
				Description: "Bounded OpenCode plugin asset bundle: background-agents.ts and lore-models.ts are copied to ~/.config/opencode/plugins/; the community opencode-subagent-statusline is registered in tui.json. The previous `model-variants.ts` asset was renamed to `lore-models.ts` by the `add-opencode-lore-models-plugin` change; `lore-models.ts` keeps the provider/model/variant discovery cache behavior of the previous asset and additionally exposes a safe opencode.json hot-edit entrypoint for in-OpenCode model/variant configuration. Excludes sdd-engram and logo.",
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
	modelLines := make([]string, 0, len(models)+1)
	// Primary orchestrator model is documented in the managed
	// SDD model declarations section so the user can see which
	// model the `agent.lore` entry uses at a glance, alongside
	// the per-phase SDD agents.
	modelLines = append(modelLines, "- "+opencodePrimaryAgentName+" (primary orchestrator): "+opencodeOrchestratorModel(definition))
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
		"- Managed settings merge target: `~/.config/opencode/opencode.json` (native OpenCode shape: `$schema: https://opencode.ai/config.json`, `default_agent: \"lore\"`, the native `agent` overlay wiring the primary `lore` orchestrator (model from `ProfileBalanced.RoleModels[\"orchestrator\"]`, `mode: \"primary\"`, prompt reference `{file:./AGENTS.md}` resolved against the managed orchestrator system instruction below) plus the `lore-worker` repository worker (model from `ProfileBalanced.RoleModels[\"lore-worker\"]`, `mode: \"subagent\"`, prompt reference `{file:./skills/lore-worker/SKILL.md}`) plus every SDD phase agent to its `~/.config/opencode/skills/<name>/SKILL.md` prompt via `{file:...}` references — non-lore agents render `mode: \"subagent\"`. Each Lore-managed agent MAY carry a `variant` field sourced from a previous user-driven `opencode.json` `agent.<name>.variant` value so user-chosen variants survive reinstall; the installer never invents unsupported variants. The documented `mcp.lore` remote entry is included when lore-server-mcp is selected/defaulted. The installer never writes a top-level Lore-only `lore` metadata block into opencode.json, and it never grants a `permission: \"allow\"` (or any other) bypass on `agent.lore`; users who need elevated permissions configure them out-of-band on a sibling agent.",
		"- Primary `lore` orchestrator: the native `default_agent: \"lore\"` and `agent.lore` entry are the documented way to run OpenCode with the global Lore orchestrator instead of letting OpenCode fall back to the built-in `build` agent; explicit selection via `opencode --agent lore` also works. The `agent.lore` entry is owned by the installer (the additive merge replaces it on every render); users who want a custom primary add a sibling entry under a different key (e.g. `agent.lore-custom`) instead of editing `agent.lore` directly.",
		"- Managed plugin bundle: `~/.config/opencode/plugins/` (default component: `opencode-plugins`). Bundled assets: `background-agents.ts`, `lore-models.ts`, and the community `opencode-subagent-statusline`. The native `tui.json` registers ONLY the community statusline in its singular `plugin` string array; local plugin .ts files are picked up automatically from the plugins/ directory and are not registered in tui.json. The previous `model-variants.ts` asset was renamed to `lore-models.ts`; the `lore-models.ts` plugin keeps the provider/model/variant discovery cache behavior of the previous asset AND exposes a safe `opencode.json` hot-edit entrypoint (`lore_models_set_agent` / `lore_models_list_agents` plugin tools) for in-OpenCode model/variant configuration.",
		"- Explicit exclusions: the installer NEVER bundles, renders, or registers `sdd-engram` or `logo`. The exclusion list is enforced at the embed.FS static guard, the plugin asset reader, and the tui.json plugin allowlist.",
		"- Managed manifest: `~/.config/opencode/lore-install.json`",
		"- Scope boundary: config-only Lore projection; no profiles or bootstrap/package-manager behavior in this slice. OpenCode plugin behavior is bounded to the three managed plugin .ts files plus `tui.json`: `background-agents.ts` provides async delegation tools, `lore-models.ts` provides in-OpenCode model/variant configuration plus a best-effort provider/variant discovery cache, and the community statusline is registered through the native TUI `plugin` array.",
		"- Lore server MCP token: when lore-server-mcp is selected/defaulted, the bearer token is persisted in opencode.json under `mcp.lore.headers.Authorization` and a plaintext-token warning (`auth_header=plaintext-bearer-token`) appears at install time. The install summary never embeds the saved token; only the path, the server URL, and the auth header name are surfaced.",
		"- `mcp.lore` ownership: the installer only overwrites an existing `mcp.lore` subtree when it is recognizably Lore-owned (legacy `managed_by: lore-cli`, or a remote `/v1/mcp` endpoint with an Authorization header). A non-Lore-owned `mcp.lore` block is treated as a foreign MCP configuration and the installer fails closed with a typed conflict error. The rendered OpenCode MCP block intentionally omits Lore-only marker fields so it remains valid against the native OpenCode schema. The existing file is backed up to the managed backup root before the installer aborts, the error names the conflicting `mcp.lore` `type` and `url` (without the token), and the resolution guidance points the user at editing the existing block or removing the conflicting `mcp.lore` subtree.",
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
// `opencode.json` config, plus the primary `lore` orchestrator
// agent that lets OpenCode boot into the global Lore orchestrator
// instead of falling back to the built-in `build` agent, plus the
// `lore-worker` repository worker agent introduced by the
// `add-opencode-lore-models-plugin` change. The overlay is shaped
// like the documented OpenCode `agent` block: each agent maps to
// a `{description, model, prompt, mode}` entry, and the `prompt`
// is a `{file:./path}` reference to the corresponding managed file
// (AGENTS.md for the primary orchestrator, the per-phase SKILL.md
// for the SDD agents, the worker SKILL.md for `lore-worker`). The
// overlay is intentionally a NATIVE OpenCode config artifact, not
// a Lore metadata block; OpenCode consumes it without any adapter
// layer. The per-phase model values come from the per-agent
// agent-config.json overrides when present and fall back to the
// agentpack default model otherwise. The primary orchestrator's
// model is derived from the `ProfileBalanced.RoleModels["orchestrator"]`
// mapping of the active agentpack definition (with a safe
// fallback to `agentpack.DefaultSDDModel`). The `lore-worker`
// model is derived from the same profile's
// `RoleModels["lore-worker"]` mapping (with a safe fallback to
// `agentpack.DefaultSDDModel`).
//
// The primary `lore` entry is intentionally NOT part of the
// sdd_agents keys in agent-config.json: that map is scoped to the
// nine canonical SDD phase agents and adding an orchestrator key
// there would break the agent-config.json contract (see
// `agentconfig.Config.Validate`). The primary orchestrator is
// owned by the installer and is layered into the `agent` overlay
// additively alongside the SDD phase agents.
//
// `mode` is rendered for every Lore-managed agent: the primary
// `lore` orchestrator uses `mode: "primary"`; every non-lore
// Lore-managed agent (the nine sdd-* phases and the `lore-worker`
// repository worker) uses `mode: "subagent"`. The installer does
// NOT render a `permission` field for any agent: removing the
// previous `agent.lore.permission = "allow"` bypass-style
// permission narrows the managed surface to documented OpenCode
// fields only and avoids broadening global or default
// permissions. Users who need elevated permissions for the
// primary Lore orchestrator must set them out-of-band (e.g. via
// the documented OpenCode user-level `permission` field on a
// sibling agent).
//
// `variant` is rendered when an effective variant value exists for
// a Lore-managed agent. The effective variant is read from the
// optional `existingAgent` map (typically the user-modified
// `agent.<name>.variant` value from `~/.config/opencode/opencode.json`
// before the current install). When no effective variant exists
// for an agent, the `variant` key is omitted entirely so the
// renderer does not invent unsupported variants. Persisted variant
// values flow through the install pipeline (see
// `effectiveOpenCodeExistingAgent`) so user-chosen variants
// survive reinstall.
//
// The overlay is intentionally the SOLE source of truth for the
// Lore-managed primary identity: the additive merge in
// `mergeOpenCodeConfigJSON` replaces the `agent` subtree
// recursively (via `mergeMaps`), so any user customization under
// `agent.lore` is overwritten on the next install. The installer's
// managed surface copy (rendered into AGENTS.md) documents this
// ownership contract so users who want a custom primary add it
// under a different key (e.g. `agent.lore-custom`) instead of
// editing `agent.lore` directly.
func opencodeAgentOverlay(definition agentpack.Definition, cfg agentconfig.Config, existingAgent map[string]openCodeExistingAgentEntry) map[string]any {
	models := openCodeAgentModels(cfg)
	variants := openCodeAgentVariants(cfg, existingAgent)
	// Reinstall preservation: when the existing `opencode.json`
	// carries a non-empty `agent.<name>.model` for a Lore-managed
	// agent, project that user-chosen value into the managed
	// overlay. The function is bounded to the same Lore-managed
	// agent set as `openCodeAgentVariants` (see
	// `isLoreManagedOverlayAgent`) so foreign `agent.<name>`
	// entries never affect the managed surface.
	existingModels := openCodeAgentModelsFromExisting(existingAgent)
	overlay := make(map[string]any, len(models)+2)
	// Primary orchestrator agent: declared first so the
	// `agent.lore` entry is the canonical primary the user
	// selects. The prompt references the managed AGENTS.md file
	// (which already contains the orchestrator system
	// instruction), and the model is derived from the
	// `ProfileBalanced.RoleModels["orchestrator"]` mapping.
	// User-chosen orchestrator model values from the existing
	// `opencode.json` flow through `existingModels` so the
	// managed overlay preserves them on reinstall. The
	// `mode: "primary"` field is the documented OpenCode mode
	// for the orchestrator. No `permission` field is rendered:
	// the previous `permission: "allow"` bypass was removed by
	// the `add-opencode-lore-models-plugin` change so the
	// installer never broadens permissions beyond the documented
	// managed surface.
	primaryModel := opencodeOrchestratorModel(definition)
	if existing, ok := existingModels[opencodePrimaryAgentName]; ok {
		primaryModel = existing
	}
	primaryEntry := map[string]any{
		"description":        opencodePrimaryAgentDescription,
		"model":              primaryModel,
		opencodeAgentModeKey: opencodePrimaryModeValue,
		"prompt":             "{file:./" + opencodePrimaryAgentPromptFile + "}",
	}
	if variant, ok := variants[opencodePrimaryAgentName]; ok {
		primaryEntry[opencodeAgentVariantKey] = variant
	}
	overlay[opencodePrimaryAgentName] = primaryEntry
	// `lore-worker` repository worker: declared alongside the SDD
	// phase agents so the OpenCode `agent` overlay covers every
	// Lore-managed subagent. The prompt references the
	// canonical worker SKILL.md path; the model is sourced from
	// the `ProfileBalanced.RoleModels["lore-worker"]` mapping of
	// the active agentpack definition (with a safe fallback to
	// `agentpack.DefaultSDDModel`). User-chosen worker model
	// values from the existing `opencode.json` flow through
	// `existingModels` so the managed overlay preserves them on
	// reinstall. `mode: "subagent"` is the documented OpenCode
	// subagent mode.
	workerModel := opencodeWorkerModel(definition)
	if existing, ok := existingModels[opencodeLoreWorkerAgentName]; ok {
		workerModel = existing
	}
	workerEntry := map[string]any{
		"model":              workerModel,
		opencodeAgentModeKey: opencodeSubagentModeValue,
		"prompt":             "{file:./" + opencodeLoreWorkerPromptFile + "}",
	}
	if variant, ok := variants[opencodeLoreWorkerAgentName]; ok {
		workerEntry[opencodeAgentVariantKey] = variant
	}
	overlay[opencodeLoreWorkerAgentName] = workerEntry
	for _, name := range agentpack.SDDPhaseAgentNames() {
		entryModel := models[name]
		if existing, ok := existingModels[name]; ok {
			entryModel = existing
		}
		entry := map[string]any{
			"model":              entryModel,
			opencodeAgentModeKey: opencodeSubagentModeValue,
			"prompt":             "{file:./skills/" + name + "/SKILL.md}",
		}
		if variant, ok := variants[name]; ok {
			entry[opencodeAgentVariantKey] = variant
		}
		overlay[name] = entry
	}
	return overlay
}

// openCodeAgentModelsFromExisting returns the per-agent model map
// the renderer projects into the managed `agent` overlay from the
// pre-install `opencode.json` `agent.<name>.model` slice. Entries
// with a non-empty model win. The function is bounded to the same
// Lore-managed agent set as `openCodeAgentVariants` so foreign
// `agent.<name>` entries never affect the managed surface.
func openCodeAgentModelsFromExisting(existingAgent map[string]openCodeExistingAgentEntry) map[string]string {
	models := make(map[string]string, len(existingAgent))
	for name, entry := range existingAgent {
		model := strings.TrimSpace(entry.Model)
		if model == "" {
			continue
		}
		if !isLoreManagedOverlayAgent(name) {
			continue
		}
		models[name] = model
	}
	return models
}

// openCodeExistingAgentEntry is the per-agent slice of the
// `agent` subtree in an existing `~/.config/opencode/opencode.json`
// that the installer reads before render to preserve user-chosen
// `model` and `variant` values across reinstall. Only the
// `model` and `variant` fields are consumed by the renderer; the
// other fields (`mode`, `description`, `prompt`, `permission`,
// `tools`, etc.) are intentionally NOT read here so the installer's
// managed overlay remains the sole source of truth for the
// non-preserved fields.
type openCodeExistingAgentEntry struct {
	Model   string
	Variant string
}

// openCodeAgentVariants returns the per-agent variant map the
// renderer projects into the managed `agent` overlay. The source
// of truth is the user-modified `agent.<name>.variant` value from
// the existing `~/.config/opencode/opencode.json` (passed via
// `existingAgent`); entries with a non-empty `variant` win. The
// function intentionally ignores `agentconfig.Config.SDDAgents`
// for variants: the cross-target agent-config.json contract does
// not yet carry variant values (this is the change that
// introduces per-agent variant persistence via direct
// `opencode.json` hot-edits), and the primary `lore` orchestrator
// plus the `lore-worker` subagent are not part of the
// agent-config.json SDD agent map at all. Agents without an
// effective variant are omitted from the returned map so the
// renderer does not emit a `variant` field.
func openCodeAgentVariants(_ agentconfig.Config, existingAgent map[string]openCodeExistingAgentEntry) map[string]string {
	variants := make(map[string]string, len(existingAgent))
	for name, entry := range existingAgent {
		variant := strings.TrimSpace(entry.Variant)
		if variant == "" {
			continue
		}
		if !isLoreManagedOverlayAgent(name) {
			continue
		}
		variants[name] = variant
	}
	return variants
}

// isLoreManagedOverlayAgent reports whether the given agent name
// is rendered into the OpenCode `agent` overlay by the installer.
// The set is the primary `lore` orchestrator, the `lore-worker`
// repository worker, and the nine canonical SDD phase agents.
func isLoreManagedOverlayAgent(name string) bool {
	if name == opencodePrimaryAgentName {
		return true
	}
	if name == opencodeLoreWorkerAgentName {
		return true
	}
	return agentpack.IsKnownSDDAgent(name)
}

// opencodeOrchestratorModel returns the model the primary Lore
// orchestrator agent should be declared with. The model is
// derived from the `RoleOrchestrator` role in the
// `ProfileBalanced` profile of the active agentpack definition,
// with a safe fallback to `agentpack.DefaultSDDModel` when:
//
//   - the definition is empty (zero-value `Definition{}`),
//   - the `ProfileBalanced` profile is missing from the
//     definition, or
//   - the profile lookup returns an empty model string.
//
// An empty definition is intentionally NOT auto-resolved to
// `agentpack.DefaultDefinition()`: the orchestrator model is
// allowed to differ from the default profile's role mapping, and
// the installer's default-definition substitution happens at a
// higher layer (see `renderOpenCodeFiles`). The fallback to
// `agentpack.DefaultSDDModel` is a safety net so the orchestrator
// entry is always declared with a non-empty model.
//
// The orchestrator model is intentionally NOT read from
// `agentconfig.Config.SDDAgents` because that map is scoped to
// the canonical SDD phase agents and does not yet carry an
// orchestrator key. Adding an orchestrator key to
// `agentconfig.Config` is a larger contract change tracked
// outside this slice; for now the installer is the sole owner
// of the primary orchestrator's model.
func opencodeOrchestratorModel(definition agentpack.Definition) string {
	if definition.SchemaVersion == 0 {
		return opencodePrimaryAgentModelFallback
	}
	profile, err := definition.Profile(agentpack.ProfileBalanced)
	if err != nil || strings.TrimSpace(profile.ID) == "" {
		return opencodePrimaryAgentModelFallback
	}
	model := strings.TrimSpace(profile.ModelForRole(agentpack.RoleOrchestrator))
	if model == "" {
		return opencodePrimaryAgentModelFallback
	}
	return model
}

// opencodeWorkerModel returns the model the `lore-worker`
// repository worker agent should be declared with. The model is
// derived from the `RoleLoreWorker` role in the `ProfileBalanced`
// profile of the active agentpack definition, with the same
// safe fallback to `agentpack.DefaultSDDModel` as
// `opencodeOrchestratorModel`. The worker is intentionally NOT
// read from `agentconfig.Config.SDDAgents` (it is not a canonical
// SDD phase agent) and is intentionally NOT read from
// `~/.config/opencode/opencode.json` (user-chosen worker model
// values flow through the install pipeline via the existing-agent
// `model` read in `effectiveOpenCodeExistingAgent`).
func opencodeWorkerModel(definition agentpack.Definition) string {
	if definition.SchemaVersion == 0 {
		return opencodePrimaryAgentModelFallback
	}
	profile, err := definition.Profile(agentpack.ProfileBalanced)
	if err != nil || strings.TrimSpace(profile.ID) == "" {
		return opencodePrimaryAgentModelFallback
	}
	model := strings.TrimSpace(profile.ModelForRole(agentpack.RoleLoreWorker))
	if model == "" {
		return opencodePrimaryAgentModelFallback
	}
	return model
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

// effectiveOpenCodeExistingAgent reads the existing
// `~/.config/opencode/opencode.json` (when present and valid JSON)
// and returns the per-agent `model` and `variant` values for
// Lore-managed agents. The function is the read-side of the
// reinstall-preservation contract: the values it returns are
// threaded into `opencodeAgentOverlay` so the renderer projects
// user-chosen model/variant values into the merged opencode.json
// instead of resetting them to managed defaults. The function
// intentionally:
//
//   - returns an empty map (not an error) when the file is
//     missing, empty, or not valid JSON — the renderer falls back
//     to managed defaults and the install pipeline proceeds
//     normally.
//   - ignores foreign `agent.<name>` entries (entries the
//     installer does not render). The function is bounded to the
//     `lore`, `lore-worker`, and the nine canonical SDD phase
//     agents.
//   - never surfaces the raw file content; only the trimmed
//     `model`/`variant` string values are returned.
//
// The function is best-effort: any read or parse failure is
// swallowed and the caller proceeds with managed defaults. The
// contract is "preserve when readable", not "fail when the file
// is hard to parse", because the installer's additive merge
// already rejects malformed `opencode.json` upstream in
// `mergeOpenCodeConfigJSON`; a parse failure here would
// double-report the same condition.
func effectiveOpenCodeExistingAgent(layout HarnessLayout) map[string]openCodeExistingAgentEntry {
	out := map[string]openCodeExistingAgentEntry{}
	if layout.Target != TargetOpenCode {
		return out
	}
	path := layout.Paths[opencodeJSONPathKey]
	if strings.TrimSpace(path) == "" {
		return out
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return out
	}
	payload := map[string]any{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return out
	}
	agent, ok := payload["agent"].(map[string]any)
	if !ok {
		return out
	}
	for name, raw := range agent {
		if !isLoreManagedOverlayAgent(name) {
			continue
		}
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		model, _ := entry["model"].(string)
		variant, _ := entry["variant"].(string)
		out[name] = openCodeExistingAgentEntry{
			Model:   strings.TrimSpace(model),
			Variant: strings.TrimSpace(variant),
		}
	}
	return out
}

// renderOpenCodeNativeConfig returns the opencode.json payload in
// the native OpenCode shape, with NO top-level `lore` metadata
// block. The shape is: `$schema`, `theme`, the native `agent`
// overlay (primary `lore` orchestrator + the `lore-worker`
// repository worker + one entry per SDD phase agent with
// `model` + `{file:./skills/<name>/SKILL.md}` prompt reference),
// and a `skills.path` declaration. When the caller wants the
// MCP-enabled variant they should call `renderOpenCodeMCPConfig`
// instead, which extends this shape with the documented top-level
// `mcp.lore` remote entry.
//
// The primary `lore` orchestrator entry is the contract the
// installer relies on so OpenCode boots into the global Lore
// orchestrator instead of falling back to the built-in `build`
// agent. The entry is sourced from
// `opencodeOrchestratorModel(definition)` (the
// `ProfileBalanced.RoleModels["orchestrator"]` mapping) and
// references the managed AGENTS.md file via a `{file:./AGENTS.md}`
// prompt reference. The entry is documented in the AGENTS.md
// managed surface copy so users can find it.
//
// The function is the bounded post-repair replacement for the
// legacy `renderOpenCodeLoreBlock` helper, which produced a
// `lore`-only metadata blob. The new shape is what the
// OpenCode-native config contract expects and is the source of
// truth for the user-owned `~/.config/opencode/opencode.json`
// file.
func renderOpenCodeNativeConfig(definition agentpack.Definition, cfg agentconfig.Config) ([]byte, error) {
	return renderOpenCodeNativeConfigWithExisting(definition, cfg, nil)
}

// renderOpenCodeNativeConfigWithExisting is the test/reinstall-aware
// form of `renderOpenCodeNativeConfig`. The `existingAgent` map is
// the per-agent slice of the pre-install `opencode.json` `agent`
// subtree; non-empty `model`/`variant` values are projected into
// the rendered overlay so user-chosen choices survive reinstall.
// When `existingAgent` is nil the renderer falls back to managed
// defaults (used by the bounded foundation-slice callers and by
// the install pipeline's plan phase when the on-disk file is
// unreadable).
func renderOpenCodeNativeConfigWithExisting(definition agentpack.Definition, cfg agentconfig.Config, existingAgent map[string]openCodeExistingAgentEntry) ([]byte, error) {
	payload := map[string]any{
		opencodeSchemaKey():     opencodeConfigSchemaURL,
		opencodeThemeKey:        opencodeThemeValue,
		opencodeDefaultAgentKey: opencodePrimaryAgentName,
		opencodeAgentsKey:       opencodeAgentOverlay(definition, cfg, existingAgent),
		opencodeSkillsDirKey:    opencodeSkillsBlock(),
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
// native `agent` overlay with the primary `lore` orchestrator +
// the per-phase SDD agents, native `skills` block) plus the
// `mcp.lore` remote entry. Shaped like the documented OpenCode
// remote MCP contract: `type: remote`, normalized server URL, and
// a Bearer Authorization header.
//
// The rendered mcp.lore block intentionally omits Lore-only marker
// fields so it remains valid against the native OpenCode schema. The
// additive merge in `mergeOpenCodeConfigJSON` still fails closed on
// foreign mcp.lore blocks: it recognizes legacy `managed_by:
// lore-cli` blocks and native remote `/v1/mcp` blocks with an
// Authorization header as Lore-owned, and rejects other existing
// mcp.lore shapes. The token is intentionally NOT surfaced in the
// conflict error.
func renderOpenCodeMCPConfig(definition agentpack.Definition, cfg agentconfig.Config, serverURL, token string) ([]byte, error) {
	return renderOpenCodeMCPConfigWithExisting(definition, cfg, serverURL, token, nil)
}

// renderOpenCodeMCPConfigWithExisting is the test/reinstall-aware
// form of `renderOpenCodeMCPConfig`. The `existingAgent` map is
// the per-agent slice of the pre-install `opencode.json` `agent`
// subtree; non-empty `model`/`variant` values are projected into
// the rendered overlay so user-chosen choices survive reinstall.
// When `existingAgent` is nil the renderer falls back to managed
// defaults.
func renderOpenCodeMCPConfigWithExisting(definition agentpack.Definition, cfg agentconfig.Config, serverURL, token string, existingAgent map[string]openCodeExistingAgentEntry) ([]byte, error) {
	normalizedServerURL := strings.TrimRight(strings.TrimSpace(serverURL), "/")
	if normalizedServerURL == "" {
		return nil, fmt.Errorf("server-url is required for OpenCode MCP config")
	}
	trimmedToken := strings.TrimSpace(token)
	if trimmedToken == "" {
		return nil, fmt.Errorf("saved token is required for OpenCode MCP config")
	}

	mcpPayload := map[string]any{
		"type":    "remote",
		"url":     normalizedServerURL + "/v1/mcp",
		"enabled": true,
		"headers": map[string]any{
			"Authorization": "Bearer " + trimmedToken,
		},
	}

	payload := map[string]any{
		opencodeSchemaKey():     opencodeConfigSchemaURL,
		opencodeThemeKey:        opencodeThemeValue,
		opencodeDefaultAgentKey: opencodePrimaryAgentName,
		opencodeAgentsKey:       opencodeAgentOverlay(definition, cfg, existingAgent),
		opencodeSkillsDirKey:    opencodeSkillsBlock(),
		opencodeMCPBlockKey:     map[string]any{opencodeMCPLoreKey: mcpPayload},
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode opencode mcp config: %w", err)
	}
	return append(data, '\n'), nil
}
