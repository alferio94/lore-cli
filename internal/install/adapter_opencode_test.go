package install

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/agentconfig"
	"github.com/alferio94/lore-cli/internal/agentpack"
)

// expectedOrchestratorModelForDefaultDefinition returns the model
// the production `opencodeOrchestratorModel` helper resolves when
// given `agentpack.DefaultDefinition()`. It is a local helper kept
// in the test file so the assertions read as a one-liner
// (`expectedOrchestratorModelForDefaultDefinition()`) instead of
// repeating the (profile, err) dance for `agentpack.Definition.Profile`.
//
// The helper is intentionally NOT exported: the production code
// path is `opencodeOrchestratorModel`, and the test's job is to
// assert that the rendered entry matches the production lookup.
func expectedOrchestratorModelForDefaultDefinition() string {
	definition := agentpack.DefaultDefinition()
	profile, err := definition.Profile(agentpack.ProfileBalanced)
	if err != nil {
		return openCodeModelOrDefault(agentpack.DefaultSDDModel)
	}
	if model := profile.ModelForRole(agentpack.RoleOrchestrator); model != "" {
		return openCodeModelOrDefault(model)
	}
	return openCodeModelOrDefault(agentpack.DefaultSDDModel)
}

// TestOpenCodeLayoutPathsAreBoundedToConfigRoot verifies the foundation-slice
// invariant: OpenCode-managed files live under ~/.config/opencode/ and the
// harness root is exactly that directory.
func TestOpenCodeLayoutPathsAreBoundedToConfigRoot(t *testing.T) {
	layout := ResolveOpenCodeLayout("/tmp/home")
	if got, want := layout.Target, TargetOpenCode; got != want {
		t.Fatalf("layout.Target = %q, want %q", got, want)
	}
	if got, want := layout.RootDir, filepath.Join("/tmp/home", ".config", "opencode"); got != want {
		t.Fatalf("layout.RootDir = %q, want %q", got, want)
	}
	if got, want := layout.Paths[opencodeJSONPathKey], filepath.Join("/tmp/home", ".config", "opencode", "opencode.json"); got != want {
		t.Fatalf("layout opencode.json path = %q, want %q", got, want)
	}
	if got, want := layout.Paths[opencodeAgentsPathKey], filepath.Join("/tmp/home", ".config", "opencode", "AGENTS.md"); got != want {
		t.Fatalf("layout AGENTS.md path = %q, want %q", got, want)
	}
	if got, want := layout.Paths[opencodeManifestPathKey], filepath.Join("/tmp/home", ".config", "opencode", "lore-install.json"); got != want {
		t.Fatalf("layout manifest path = %q, want %q", got, want)
	}
	if got, want := layout.Paths[opencodeSkillsDirPathKey], filepath.Join("/tmp/home", ".config", "opencode", "skills"); got != want {
		t.Fatalf("layout skills dir = %q, want %q", got, want)
	}
}

// TestDefaultOpenCodeAdapterReportsExpectedCapabilities verifies the
// bounded foundation-slice capability map and that Supports/Title/ID are
// wired correctly.
func TestDefaultOpenCodeAdapterReportsExpectedCapabilities(t *testing.T) {
	adapter := defaultOpenCodeAdapter()
	if got := adapter.ID(); got != TargetOpenCode {
		t.Fatalf("adapter.ID() = %q, want %q", got, TargetOpenCode)
	}
	if got := adapter.Title(); got != "OpenCode" {
		t.Fatalf("adapter.Title() = %q, want OpenCode", got)
	}
	if !adapter.Supports(ComponentCorePack) {
		t.Fatal("Supports(core-pack) = false, want true for OpenCode foundation slice")
	}
	if !adapter.Supports(ComponentLoreServerMCP) {
		t.Fatal("Supports(lore-server-mcp) = false, want true for OpenCode optional MCP")
	}
	if !adapter.Supports(ComponentExtendedSkills) {
		t.Fatal("Supports(extended-skills) = false, want true for OpenCode extended skills")
	}
	// Defensive copy: mutating the returned map must not change adapter state.
	caps := adapter.Capabilities()
	caps[CapabilityAgentPack] = Capability{}
	if adapter.Capabilities()[CapabilityAgentPack].Component != ComponentCorePack {
		t.Fatal("Capabilities() returned a shared map; want defensive copy")
	}
}

// TestDefaultOpenCodeAdapterRenderProducesAGENTSAndSkills verifies the
// bounded foundation-slice render: AGENTS.md, a managed skill per
// canonical SDD phase, and an extended-skill bundle. opencode.json is
// produced by the install pipeline, not by Render().
func TestDefaultOpenCodeAdapterRenderProducesAGENTSAndSkills(t *testing.T) {
	adapter := defaultOpenCodeAdapter()
	definition := agentpack.DefaultDefinition()
	rendered, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetOpenCode,
		Definition: definition,
		Components: []ComponentID{ComponentCorePack, ComponentExtendedSkills},
	})
	if err != nil {
		t.Fatalf("Render error = %v, want nil", err)
	}
	if len(rendered) < 1 {
		t.Fatalf("len(rendered) = %d, want at least 1 (AGENTS.md)", len(rendered))
	}

	files := map[string]RenderedFile{}
	for _, file := range rendered {
		files[file.RelativePath] = file
	}
	agents, ok := files["AGENTS.md"]
	if !ok {
		t.Fatal("rendered files missing AGENTS.md")
	}
	if agents.MergeMode != MergeModeReplace {
		t.Fatalf("AGENTS.md merge mode = %q, want replace", agents.MergeMode)
	}
	agentsText := string(agents.Content)
	for _, want := range []string{
		"# Lore Runtime",
		"## OpenCode managed surface",
		"~/.config/opencode/skills",
		"~/.config/opencode/opencode.json",
		"## Managed SDD model declarations",
		"## Orchestrator instruction",
	} {
		if !strings.Contains(agentsText, want) {
			t.Fatalf("AGENTS.md = %q, want substring %q", agentsText, want)
		}
	}
	// Managed surface section MUST list the native-safe plugin set,
	// rejected legacy runtime plugins, and the explicit exclusion list.
	for _, want := range []string{
		"Managed plugin bundle",
		"Legacy Lore-owned runtime emulation plugins",
		"background-agents.ts",
		"lore-models.ts",
		"opencode-subagent-statusline",
		"tui.json",
		"opencode-plugins",
		"Explicit exclusions",
		"sdd-engram",
		"logo",
		"plaintext-token warning",
		"auth_header=plaintext-bearer-token",
		// 3.3 repair: the AGENTS.md managed surface MUST mention
		// the fail-closed mcp.lore ownership contract so the
		// install-time behavior is documented in the user-facing
		// managed surface copy.
		"`mcp.lore` ownership",
		"managed_by: lore-cli",
		"fails closed",
		// Post-repair shape: the AGENTS.md managed surface MUST
		// describe the native opencode.json shape (`$schema`,
		// native `agent` overlay, no top-level Lore-only `lore`)
		// so the user-facing copy matches what the installer
		// actually writes.
		"native OpenCode shape",
		"https://opencode.ai/config.json",
		"native `agent` overlay",
		"never writes a top-level Lore-only `lore`",
		"Migration:",
	} {
		if !strings.Contains(agentsText, want) {
			t.Fatalf("AGENTS.md managed surface = %q, want substring %q", agentsText, want)
		}
	}
	for _, forbidden := range []string{
		"no plugins, profiles",
		"no plugins, profiles,",
		"config-only Lore projection; no plugins",
	} {
		if strings.Contains(agentsText, forbidden) {
			t.Fatalf("AGENTS.md managed surface still contains stale %q; managed surface must be updated to reflect the opencode-plugins bundle", forbidden)
		}
	}
	// Skill paths use the canonical phase name (e.g. "sdd-propose") — not
	// the long "sdd-proposal" form. The sdd- prefix is preserved.
	proposePath := "skills/sdd-propose/SKILL.md"
	if _, ok := files[proposePath]; !ok {
		t.Fatalf("rendered files missing canonical skill %q; got %v", proposePath, keysOfFiles(files))
	}
	if _, ok := files["skills/sdd-proposal/SKILL.md"]; ok {
		t.Fatalf("rendered files unexpectedly include non-canonical skills/sdd-proposal/SKILL.md; got %v", keysOfFiles(files))
	}
	// AGENTS.md's OpenCode managed surface section must not leak Gentle
	// wording. The surface section is allowed to *describe* the bounded
	// slice (e.g. "no plugins, profiles, ... or native/runtime subagents
	// in this slice"); the assertion is that the section does not
	// accidentally pull in Gentle-authored copy or claim plugin assets
	// in this slice.
	managedSurface := agentsText
	if idx := strings.Index(agentsText, "## Orchestrator instruction"); idx >= 0 {
		managedSurface = agentsText[:idx]
	}
	for _, forbidden := range []string{
		"gentle",
		"gentle-ai",
		"gentleprogramming",
	} {
		if strings.Contains(strings.ToLower(managedSurface), forbidden) {
			t.Fatalf("AGENTS.md OpenCode managed surface leaked %q; got: %q", forbidden, managedSurface)
		}
	}
}

// TestDefaultOpenCodeAdapterRenderRejectsUnknownComponent verifies the
// adapter is wired to the same NormalizeComponentSelection guard that
// other adapters use.
func TestDefaultOpenCodeAdapterRenderRejectsUnknownComponent(t *testing.T) {
	adapter := defaultOpenCodeAdapter()
	_, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetOpenCode,
		Definition: agentpack.DefaultDefinition(),
		Components: []ComponentID{"unknown-component"},
	})
	if err == nil {
		t.Fatal("Render(unknown) error = nil, want unknown-component rejection")
	}
}

// TestDefaultOpenCodeAdapterRenderRejectsMCPWithoutAuth verifies the
// RenderRequest.Validate gate: when lore-server-mcp is selected for
// OpenCode, server URL and saved token must be present (matches
// Antigravity/Codex behavior).
func TestDefaultOpenCodeAdapterRenderRejectsMCPWithoutAuth(t *testing.T) {
	adapter := defaultOpenCodeAdapter()
	_, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetOpenCode,
		Definition: agentpack.DefaultDefinition(),
		Components: []ComponentID{ComponentLoreServerMCP},
	})
	if err == nil {
		t.Fatal("Render(mcp, no auth) error = nil, want server-url/token validation error")
	}
}

// TestOpenCodeMCPConfigRendersRemoteMCPBlock verifies the native
// OpenCode shape with the documented top-level `mcp.lore` remote
// entry: the payload carries the native `$schema` reference, the
// native `agent` overlay (one entry per SDD phase agent with
// `model` + `{file:./prompts/sdd/<phase>.md}` prompt reference),
// the native `skills` block, and the `mcp.lore` remote entry with
// `type=remote`, a normalized server URL, and a Bearer
// Authorization header. The post-repair shape MUST NOT contain a
// top-level Lore-only `lore` block.
func TestOpenCodeMCPConfigRendersRemoteMCPBlock(t *testing.T) {
	data, err := renderOpenCodeMCPConfig(agentpack.DefaultDefinition(), agentconfig.Config{}, "https://lore.example", "secret-token")
	if err != nil {
		t.Fatalf("renderOpenCodeMCPConfig error = %v, want nil", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	// Post-repair shape: NO top-level `lore` block. The renderer's
	// job is to produce a native opencode.json, not a Lore metadata
	// blob.
	if _, ok := payload["lore"]; ok {
		t.Fatalf("payload carries top-level `lore` object after repair; want native opencode.json without a Lore-only metadata block: %v", payload)
	}
	if got := payload["$schema"]; got != opencodeConfigSchemaURL {
		t.Fatalf("payload $schema = %v, want %q", got, opencodeConfigSchemaURL)
	}
	agent, ok := payload["agent"].(map[string]any)
	if !ok {
		t.Fatalf("payload missing top-level `agent` overlay: %v", payload)
	}
	// Primary `lore` orchestrator entry MUST be present in the
	// native `agent` overlay so OpenCode can boot into the global
	// Lore orchestrator instead of falling back to the built-in
	// `build` agent. The model is derived from
	// `ProfileBalanced.RoleModels["orchestrator"]` and the prompt
	// references the managed prompts/lore.md file.
	loreEntry, ok := agent[opencodePrimaryAgentName].(map[string]any)
	if !ok {
		t.Fatalf("agent overlay missing primary %q entry: %v", opencodePrimaryAgentName, agent)
	}
	wantOrchestratorModel := expectedOrchestratorModelForDefaultDefinition()
	if got := loreEntry["model"]; got != wantOrchestratorModel {
		t.Fatalf("agent.%s.model = %v, want %q (from ProfileBalanced.RoleModels[orchestrator])", opencodePrimaryAgentName, got, wantOrchestratorModel)
	}
	wantPrompt := "{file:./" + opencodePrimaryAgentPromptFile + "}"
	if got, _ := loreEntry["prompt"].(string); got != wantPrompt {
		t.Fatalf("agent.%s.prompt = %q, want %q", opencodePrimaryAgentName, got, wantPrompt)
	}
	if _, ok := loreEntry["description"]; !ok {
		t.Fatalf("agent.%s missing description: %v", opencodePrimaryAgentName, loreEntry)
	}
	for _, phaseAgent := range agentpack.SDDPhaseAgentNames() {
		entry, ok := agent[phaseAgent].(map[string]any)
		if !ok {
			t.Fatalf("agent overlay missing %q entry: %v", phaseAgent, agent)
		}
		if _, ok := entry["model"]; !ok {
			t.Fatalf("agent.%s missing model: %v", phaseAgent, entry)
		}
		prompt, _ := entry["prompt"].(string)
		wantPrompt := expectedOpenCodeManagedAgentPrompt(phaseAgent)
		if prompt != wantPrompt {
			t.Fatalf("agent.%s.prompt = %q, want %q (native {file:...} prompt asset reference)", phaseAgent, prompt, wantPrompt)
		}
	}
	skills, ok := payload["skills"].(map[string]any)
	if !ok {
		t.Fatalf("payload missing top-level `skills` block: %v", payload)
	}
	paths, ok := skills["paths"].([]any)
	if !ok || len(paths) != 1 || paths[0] != opencodeSkillsDirPath {
		t.Fatalf("skills.paths = %v, want [%q]", skills["paths"], opencodeSkillsDirPath)
	}
	if _, present := skills["path"]; present {
		t.Fatalf("skills.path unexpectedly present; want schema-safe skills.paths only: %v", skills)
	}
	mcp, ok := payload["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("payload missing top-level `mcp` object: %v", payload)
	}
	loreMCP, ok := mcp["lore"].(map[string]any)
	if !ok {
		t.Fatalf("payload missing mcp.lore entry: %v", mcp)
	}
	if got := loreMCP["type"]; got != "remote" {
		t.Fatalf("mcp.lore.type = %v, want remote", got)
	}
	if got := loreMCP["url"]; got != "https://lore.example/v1/mcp" {
		t.Fatalf("mcp.lore.url = %v, want https://lore.example/v1/mcp", got)
	}
	// Native OpenCode MCP schema rejects Lore-only marker fields, so
	// the renderer must not put managed_by inside mcp.lore. Ownership
	// is inferred during merge from legacy markers or the remote
	// /v1/mcp + Authorization shape.
	if _, present := loreMCP["managed_by"]; present {
		t.Fatalf("mcp.lore unexpectedly carries managed_by marker; rendered block must stay native-schema-valid: %v", loreMCP)
	}
	headers, _ := loreMCP["headers"].(map[string]any)
	if got := headers["Authorization"]; got != "Bearer secret-token" {
		t.Fatalf("mcp.lore.headers.Authorization = %v, want Bearer secret-token", got)
	}
	if got := payload[opencodeDefaultAgentKey]; got != opencodePrimaryAgentName {
		t.Fatalf("payload default_agent = %v, want %q", got, opencodePrimaryAgentName)
	}
}

// TestOpenCodeMCPConfigRequiresServerURLAndToken verifies the failure
// modes of the opencode MCP renderer.
func TestOpenCodeMCPConfigRequiresServerURLAndToken(t *testing.T) {
	if _, err := renderOpenCodeMCPConfig(agentpack.DefaultDefinition(), agentconfig.Config{}, "", "token"); err == nil {
		t.Fatal("renderOpenCodeMCPConfig(empty server) error = nil, want server-url validation error")
	}
	if _, err := renderOpenCodeMCPConfig(agentpack.DefaultDefinition(), agentconfig.Config{}, "https://lore.example", "  "); err == nil {
		t.Fatal("renderOpenCodeMCPConfig(empty token) error = nil, want token validation error")
	}
}

// TestOpenCodeNativeConfigDeclaresLorePrimaryOrchestratorAgent is
// the focused regression gate for the primary `lore` orchestrator
// entry. The native `opencode.json` (with no lore-server-mcp
// component selected) MUST declare the `agent.lore` entry sourced
// from the `ProfileBalanced.RoleModels["orchestrator"]` mapping of
// the active agentpack definition, with a prompt reference to the
// managed AGENTS.md file. The test is the safety gate that keeps
// the primary orchestrator in the opencode.json on every render,
// so OpenCode can boot into the global Lore orchestrator instead
// of falling back to the built-in `build` agent.
//
// The test also asserts:
//
//   - The 9 sdd-* phase agent entries are still present (the
//     primary entry is layered on top additively, not as a
//     replacement).
//   - The primary `lore` entry is NOT one of the sdd-* phase
//     agents (sanity check: the canonical phase list is
//     unchanged).
//   - The `agent` overlay is the only top-level block that
//     contains a `lore` key (no top-level `lore` metadata block;
//     the `lore` identity lives under `agent.lore`).
//   - The top-level `default_agent` is `lore` so OpenCode boots into
//     the managed Lore orchestrator by default.
//   - The `agent.lore` bypass-style permission is scoped to that
//     agent only via `permission: "allow"`.
//   - The orchestrator model fallback path is exercised when the
//     definition is empty (a zero-value `Definition{}` MUST
//     resolve to `agentpack.DefaultSDDModel` via the fallback in
//     `opencodeOrchestratorModel`).
func TestOpenCodeNativeConfigDeclaresLorePrimaryOrchestratorAgent(t *testing.T) {
	data, err := renderOpenCodeNativeConfig(agentpack.DefaultDefinition(), agentconfig.Config{})
	if err != nil {
		t.Fatalf("renderOpenCodeNativeConfig error = %v, want nil", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	agent, ok := payload["agent"].(map[string]any)
	if !ok {
		t.Fatalf("payload missing top-level `agent` overlay: %v", payload)
	}

	// Primary `lore` orchestrator entry: present, with the
	// expected model, prompt reference, and description.
	loreEntry, ok := agent[opencodePrimaryAgentName].(map[string]any)
	if !ok {
		t.Fatalf("agent overlay missing primary %q entry; got keys %v", opencodePrimaryAgentName, keysOfMapForOverlay(agent))
	}
	wantModel := expectedOrchestratorModelForDefaultDefinition()
	if got := loreEntry["model"]; got != wantModel {
		t.Fatalf("agent.%s.model = %v, want %q (from ProfileBalanced.RoleModels[orchestrator])", opencodePrimaryAgentName, got, wantModel)
	}
	wantPrompt := "{file:./" + opencodePrimaryAgentPromptFile + "}"
	if got, _ := loreEntry["prompt"].(string); got != wantPrompt {
		t.Fatalf("agent.%s.prompt = %q, want %q (native {file:...} reference to the managed AGENTS.md)", opencodePrimaryAgentName, got, wantPrompt)
	}
	description, _ := loreEntry["description"].(string)
	if !strings.Contains(strings.ToLower(description), "orchestrator") {
		t.Fatalf("agent.%s.description = %q, want substring \"orchestrator\"", opencodePrimaryAgentName, description)
	}

	// Sanity: the 9 sdd-* phase agent entries are still present.
	for _, phaseAgent := range agentpack.SDDPhaseAgentNames() {
		if _, ok := agent[phaseAgent].(map[string]any); !ok {
			t.Fatalf("agent overlay missing sdd-* phase agent %q; got keys %v", phaseAgent, keysOfMapForOverlay(agent))
		}
	}

	// Sanity: the primary `lore` key is NOT one of the sdd-*
	// phase agents (canonical phase list is unchanged).
	for _, phaseAgent := range agentpack.SDDPhaseAgentNames() {
		if opencodePrimaryAgentName == phaseAgent {
			t.Fatalf("primary agent name %q collides with SDD phase agent %q; cannot coexist", opencodePrimaryAgentName, phaseAgent)
		}
	}

	// Sanity: the `agent` overlay is the only top-level block
	// that contains a `lore` key. There is no top-level `lore`
	// metadata block; the `lore` identity lives under `agent.lore`.
	if _, ok := payload["lore"]; ok {
		t.Fatalf("payload carries top-level `lore` object; want only `agent.lore` (no metadata blob); got keys %v", keysOfMapForOverlay(payload))
	}
	if got := payload[opencodeDefaultAgentKey]; got != opencodePrimaryAgentName {
		t.Fatalf("payload default_agent = %v, want %q", got, opencodePrimaryAgentName)
	}
	if got := loreEntry[opencodeAgentModeKey]; got != opencodePrimaryModeValue {
		t.Fatalf("agent.%s.mode = %v, want %q", opencodePrimaryAgentName, got, opencodePrimaryModeValue)
	}
	assertOpenCodePrimaryPermission(t, loreEntry)
	if _, ok := payload[opencodePermissionKey]; ok {
		t.Fatalf("payload unexpectedly carries top-level permission; managed permissions must be per-agent only: %v", payload)
	}
	// The `lore-worker` repository worker introduced by the
	// `add-opencode-lore-models-plugin` change MUST be present
	// in the `agent` overlay with `mode: "subagent"` and managed
	// subagent-safe permissions.
	workerEntry, ok := agent[opencodeLoreWorkerAgentName].(map[string]any)
	if !ok {
		t.Fatalf("agent overlay missing %q entry; got keys %v", opencodeLoreWorkerAgentName, keysOfMapForOverlay(agent))
	}
	if _, ok := workerEntry["model"]; !ok {
		t.Fatalf("agent.%s missing model: %v", opencodeLoreWorkerAgentName, workerEntry)
	}
	if got, want := workerEntry[opencodeAgentModeKey], opencodeSubagentModeValue; got != want {
		t.Fatalf("agent.%s.mode = %v, want %q", opencodeLoreWorkerAgentName, got, want)
	}
	assertOpenCodeSubagentPermission(t, opencodeLoreWorkerAgentName, workerEntry)
	// Non-lore Lore-managed agents (sdd-* and lore-worker) MUST
	// render `mode: "subagent"`.
	for _, phaseAgent := range agentpack.SDDPhaseAgentNames() {
		entry, ok := agent[phaseAgent].(map[string]any)
		if !ok {
			t.Fatalf("agent overlay missing %q entry", phaseAgent)
		}
		if got, want := entry[opencodeAgentModeKey], opencodeSubagentModeValue; got != want {
			t.Fatalf("agent.%s.mode = %v, want %q", phaseAgent, got, want)
		}
	}
}

func TestOpenCodeAgentOverlayNormalizesModelsToProviderModel(t *testing.T) {
	cfg := agentconfig.Config{
		SchemaVersion: agentconfig.SchemaVersion,
		SDDAgents: map[string]agentconfig.Agent{
			"sdd-apply": {Model: "gpt-4o"},
		},
	}
	overlay := opencodeAgentOverlay(agentpack.DefaultDefinition(), cfg, map[string]openCodeExistingAgentEntry{
		opencodePrimaryAgentName: {Model: "gpt-5.4"},
		"sdd-design":             {Model: "claude-3-5-sonnet"},
		"sdd-tasks":              {Model: "not-a-known-opencode-model"},
	})
	for _, name := range expectedOpenCodeManagedAgentNames() {
		entry, ok := overlay[name].(map[string]any)
		if !ok {
			t.Fatalf("overlay missing %q", name)
		}
		model, _ := entry[opencodeAgentModelKey].(string)
		if model == "" || !strings.Contains(model, "/") {
			t.Fatalf("overlay.%s.model = %q, want provider/model form", name, model)
		}
		if strings.HasPrefix(model, "gpt-") {
			t.Fatalf("overlay.%s.model = %q, want no bare gpt-* identifier", name, model)
		}
	}
	if got := overlay[opencodePrimaryAgentName].(map[string]any)[opencodeAgentModelKey]; got != "openai/gpt-5.4" {
		t.Fatalf("primary model = %v, want openai/gpt-5.4", got)
	}
	if got := overlay["sdd-apply"].(map[string]any)[opencodeAgentModelKey]; got != "openai/gpt-4o" {
		t.Fatalf("sdd-apply model = %v, want openai/gpt-4o", got)
	}
	if got := overlay["sdd-design"].(map[string]any)[opencodeAgentModelKey]; got != "anthropic/claude-3-5-sonnet" {
		t.Fatalf("sdd-design model = %v, want anthropic/claude-3-5-sonnet", got)
	}
}

// TestOpenCodeOrchestratorModelUsesBalancedProfileRoleMapping is
// the focused regression gate for the orchestrator model lookup.
// The `ProfileBalanced.RoleModels["orchestrator"]` mapping is the
// source of truth for the primary agent's model; an empty
// definition MUST fall back to `agentpack.DefaultSDDModel` rather
// than producing an empty model string.
func TestOpenCodeOrchestratorModelUsesBalancedProfileRoleMapping(t *testing.T) {
	definition := agentpack.DefaultDefinition()
	profile, err := definition.Profile(agentpack.ProfileBalanced)
	if err != nil {
		t.Fatalf("DefaultDefinition().Profile(balanced) error = %v, want nil", err)
	}
	want := openCodeModelOrDefault(profile.ModelForRole(agentpack.RoleOrchestrator))
	if got := opencodeOrchestratorModel(definition); got != want {
		t.Fatalf("opencodeOrchestratorModel(DefaultDefinition) = %q, want %q", got, want)
	}
	fallback := openCodeModelOrDefault(agentpack.DefaultSDDModel)
	if got := opencodeOrchestratorModel(agentpack.Definition{}); got != fallback {
		t.Fatalf("opencodeOrchestratorModel(empty) = %q, want fallback %q", got, fallback)
	}
}

// TestOpenCodeAgentOverlayPrimaryIsLayeredOnTopOfSddPhases is the
// focused regression gate for the additive layering of the primary
// `lore` orchestrator entry on top of the 9 sdd-* phase agent
// entries and the `lore-worker` repository worker introduced by
// the `add-opencode-lore-models-plugin` change. The test asserts:
//
//   - The overlay contains 11 entries (1 primary + `lore-worker` +
//     9 sdd-*).
//   - The primary `lore` entry uses the `ProfileBalanced` orchestrator
//     model, renders `mode: "primary"`, and references the managed
//     prompts/lore.md file. The `permission` field MUST NOT be present
//     (the previous `permission: "allow"` bypass was removed).
//   - The `lore-worker` entry is present, renders `mode: "subagent"`,
//     and references the managed `prompts/lore-worker.md` file.
//   - Each sdd-* entry uses the per-agent model from agent-config
//     when present, otherwise the agentpack default, renders
//     `mode: "subagent"`, and references the corresponding
//     `prompts/sdd/<phase>.md` file.
//   - The overlay is compatible with the documented OpenCode
//     `agent` block contract: `{description, model, mode, prompt}`
//     for the primary, `{model, mode, prompt}` for the non-lore
//     Lore-managed agents.
func TestOpenCodeAgentOverlayPrimaryIsLayeredOnTopOfSddPhases(t *testing.T) {
	cfg := agentconfig.Config{
		SchemaVersion: agentconfig.SchemaVersion,
		SDDAgents: map[string]agentconfig.Agent{
			"sdd-apply": {Model: "gpt-5-custom-apply"},
		},
	}
	overlay := opencodeAgentOverlay(agentpack.DefaultDefinition(), cfg, nil)
	wantCount := 2 + len(agentpack.SDDPhaseAgentNames()) // primary + worker + 9 sdd-*
	if got, want := len(overlay), wantCount; got != want {
		t.Fatalf("opencodeAgentOverlay size = %d, want %d (1 primary + 1 worker + %d sdd-*)", got, want, len(agentpack.SDDPhaseAgentNames()))
	}
	loreEntry, ok := overlay[opencodePrimaryAgentName].(map[string]any)
	if !ok {
		t.Fatalf("overlay missing primary %q entry; got keys %v", opencodePrimaryAgentName, keysOfMapForOverlay(overlay))
	}
	if _, ok := loreEntry["description"]; !ok {
		t.Fatalf("primary %s entry missing description: %v", opencodePrimaryAgentName, loreEntry)
	}
	if _, ok := loreEntry["model"]; !ok {
		t.Fatalf("primary %s entry missing model: %v", opencodePrimaryAgentName, loreEntry)
	}
	if _, ok := loreEntry["prompt"]; !ok {
		t.Fatalf("primary %s entry missing prompt: %v", opencodePrimaryAgentName, loreEntry)
	}
	if got, want := loreEntry[opencodeAgentModeKey], opencodePrimaryModeValue; got != want {
		t.Fatalf("primary %s.%s = %v, want %q", opencodePrimaryAgentName, opencodeAgentModeKey, got, want)
	}
	assertOpenCodePrimaryPermission(t, loreEntry)
	workerEntry, ok := overlay[opencodeLoreWorkerAgentName].(map[string]any)
	if !ok {
		t.Fatalf("overlay missing %q entry; got keys %v", opencodeLoreWorkerAgentName, keysOfMapForOverlay(overlay))
	}
	if _, ok := workerEntry["model"]; !ok {
		t.Fatalf("%s entry missing model: %v", opencodeLoreWorkerAgentName, workerEntry)
	}
	if got, want := workerEntry[opencodeAgentModeKey], opencodeSubagentModeValue; got != want {
		t.Fatalf("%s.%s = %v, want %q", opencodeLoreWorkerAgentName, opencodeAgentModeKey, got, want)
	}
	wantWorkerPrompt := "{file:./" + opencodeLoreWorkerPromptFile + "}"
	if got, _ := workerEntry["prompt"].(string); got != wantWorkerPrompt {
		t.Fatalf("%s.prompt = %q, want %q", opencodeLoreWorkerAgentName, got, wantWorkerPrompt)
	}
	assertOpenCodeSubagentPermission(t, opencodeLoreWorkerAgentName, workerEntry)
	for _, name := range agentpack.SDDPhaseAgentNames() {
		entry, ok := overlay[name].(map[string]any)
		if !ok {
			t.Fatalf("overlay missing %q entry; got keys %v", name, keysOfMapForOverlay(overlay))
		}
		model, _ := entry["model"].(string)
		wantModel := openCodeModelOrDefault(agentpack.DefaultSDDModel)
		if name == "sdd-apply" {
			wantModel = "openai/gpt-5-custom-apply"
		}
		if model != wantModel {
			t.Fatalf("overlay.%s.model = %q, want %q (per-agent override or default)", name, model, wantModel)
		}
		if got, want := entry[opencodeAgentModeKey], opencodeSubagentModeValue; got != want {
			t.Fatalf("overlay.%s.%s = %v, want %q", name, opencodeAgentModeKey, got, want)
		}
		assertOpenCodeSubagentPermission(t, name, entry)
		wantPrompt := expectedOpenCodeManagedAgentPrompt(name)
		if got, _ := entry["prompt"].(string); got != wantPrompt {
			t.Fatalf("overlay.%s.prompt = %q, want %q", name, got, wantPrompt)
		}
	}
}

// TestOpenCodeNativeConfigRegressionCoversManagedAgentShape is the
// Phase 4 regression lock for the complete native OpenCode agent
// contract. It intentionally checks every managed agent in one pass
// so future changes cannot accidentally drop mode, prompt, task, or
// question permissions from a subset of SDD agents while preserving
// the happy-path renderer tests.
func TestOpenCodeNativeConfigRegressionCoversManagedAgentShape(t *testing.T) {
	data, err := renderOpenCodeMCPConfig(agentpack.DefaultDefinition(), agentconfig.Config{}, "https://lore.example", "secret-token")
	if err != nil {
		t.Fatalf("renderOpenCodeMCPConfig() error = %v, want nil", err)
	}
	if err := validateOpenCodeStartupSafeConfig(data, opencodeConfigFileName); err != nil {
		t.Fatalf("validateOpenCodeStartupSafeConfig(rendered) error = %v, want nil", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode rendered config: %v", err)
	}
	for _, forbiddenTopLevel := range []string{opencodeLoreBlockKey, "plugins", "tools"} {
		if _, present := payload[forbiddenTopLevel]; present {
			t.Fatalf("rendered opencode.json carries forbidden top-level %q: %v", forbiddenTopLevel, payload[forbiddenTopLevel])
		}
	}
	if got := payload["$schema"]; got != opencodeConfigSchemaURL {
		t.Fatalf("rendered $schema = %v, want %q", got, opencodeConfigSchemaURL)
	}
	if got := payload[opencodeDefaultAgentKey]; got != opencodePrimaryAgentName {
		t.Fatalf("rendered default_agent = %v, want %q", got, opencodePrimaryAgentName)
	}
	mcp, ok := payload[opencodeMCPBlockKey].(map[string]any)
	if !ok {
		t.Fatalf("rendered payload missing native mcp block; got keys %v", keysOfMapForOverlay(payload))
	}
	loreMCP, ok := mcp[opencodeMCPLoreKey].(map[string]any)
	if !ok {
		t.Fatalf("rendered mcp missing lore entry: %v", mcp)
	}
	if got := loreMCP["type"]; got != "remote" {
		t.Fatalf("mcp.lore.type = %v, want remote", got)
	}
	if _, present := loreMCP[opencodeManagedByKey]; present {
		t.Fatalf("mcp.lore carries Lore-only ownership marker; native OpenCode schema must stay clean: %v", loreMCP)
	}

	agents, ok := payload[opencodeAgentsKey].(map[string]any)
	if !ok {
		t.Fatalf("rendered payload missing agent overlay; got keys %v", keysOfMapForOverlay(payload))
	}
	wantAgents := map[string]struct{}{
		opencodePrimaryAgentName:    {},
		opencodeLoreWorkerAgentName: {},
	}
	for _, name := range agentpack.SDDPhaseAgentNames() {
		wantAgents[name] = struct{}{}
	}
	for name := range wantAgents {
		entry, ok := agents[name].(map[string]any)
		if !ok {
			t.Fatalf("agent overlay missing managed agent %q; got keys %v", name, keysOfMapForOverlay(agents))
		}
		if _, present := entry["tools"]; present {
			t.Fatalf("agent.%s carries deprecated tools block: %v", name, entry)
		}
		if _, ok := entry[opencodeAgentModelKey].(string); !ok {
			t.Fatalf("agent.%s.model missing or non-string: %v", name, entry)
		}
		prompt, ok := entry[opencodeAgentPromptKey].(string)
		if !ok {
			t.Fatalf("agent.%s.prompt missing or non-string: %v", name, entry)
		}
		if name == opencodePrimaryAgentName {
			if got := entry[opencodeAgentModeKey]; got != opencodePrimaryModeValue {
				t.Fatalf("agent.%s.mode = %v, want %q", name, got, opencodePrimaryModeValue)
			}
			if prompt != "{file:./"+opencodePrimaryAgentPromptFile+"}" {
				t.Fatalf("agent.%s.prompt = %q, want managed primary prompt", name, prompt)
			}
			assertOpenCodePrimaryPermission(t, entry)
			continue
		}
		if got := entry[opencodeAgentModeKey]; got != opencodeSubagentModeValue {
			t.Fatalf("agent.%s.mode = %v, want %q", name, got, opencodeSubagentModeValue)
		}
		if !strings.HasPrefix(prompt, "{file:./prompts/") || strings.Contains(prompt, "./skills/") {
			t.Fatalf("agent.%s.prompt = %q, want managed prompt asset path under ./prompts", name, prompt)
		}
		assertOpenCodeSubagentPermission(t, name, entry)
	}
}

// TestOpenCodeStartupSafeConfigValidationRejectsBadAgentShape is the
// startup-safety gate for task 2.1: bad managed agent prompt references
// must be rejected before opencode.json can be planned or written.
func assertOpenCodePrimaryPermission(t *testing.T, entry map[string]any) {
	t.Helper()
	permission, ok := entry[opencodePermissionKey].(map[string]any)
	if !ok {
		t.Fatalf("agent.%s missing permission object: %v", opencodePrimaryAgentName, entry)
	}
	if got := permission[opencodePermissionQuestionKey]; got != opencodePermissionAllowValue {
		t.Fatalf("agent.%s.permission.%s = %v, want %q", opencodePrimaryAgentName, opencodePermissionQuestionKey, got, opencodePermissionAllowValue)
	}
	task, ok := permission[opencodePermissionTaskKey].(map[string]any)
	if !ok {
		t.Fatalf("agent.%s.permission.%s = %v, want task routing object", opencodePrimaryAgentName, opencodePermissionTaskKey, permission[opencodePermissionTaskKey])
	}
	wantRoutes := map[string]string{
		"*":                         opencodePermissionDenyValue,
		opencodeLoreWorkerAgentName: opencodePermissionAllowValue,
		"sdd-*":                     opencodePermissionAllowValue,
	}
	if len(task) != len(wantRoutes) {
		t.Fatalf("agent.%s.permission.%s routes = %v, want exactly %v", opencodePrimaryAgentName, opencodePermissionTaskKey, task, wantRoutes)
	}
	for route, want := range wantRoutes {
		if got := task[route]; got != want {
			t.Fatalf("agent.%s.permission.%s[%q] = %v, want %q", opencodePrimaryAgentName, opencodePermissionTaskKey, route, got, want)
		}
	}
}

func assertOpenCodeSubagentPermission(t *testing.T, name string, entry map[string]any) {
	t.Helper()
	permission, ok := entry[opencodePermissionKey].(map[string]any)
	if !ok {
		t.Fatalf("agent.%s missing permission object: %v", name, entry)
	}
	if got := permission[opencodePermissionQuestionKey]; got != opencodePermissionAllowValue {
		t.Fatalf("agent.%s.permission.%s = %v, want %q", name, opencodePermissionQuestionKey, got, opencodePermissionAllowValue)
	}
	if got := permission[opencodePermissionTaskKey]; got != opencodePermissionDenyValue {
		t.Fatalf("agent.%s.permission.%s = %v, want %q", name, opencodePermissionTaskKey, got, opencodePermissionDenyValue)
	}
}

func TestOpenCodeStartupSafeConfigValidationRejectsBadAgentShape(t *testing.T) {
	data, err := renderOpenCodeNativeConfig(agentpack.DefaultDefinition(), agentconfig.Config{})
	if err != nil {
		t.Fatalf("renderOpenCodeNativeConfig() error = %v, want nil", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode rendered config: %v", err)
	}
	agents := payload[opencodeAgentsKey].(map[string]any)
	design := agents["sdd-design"].(map[string]any)
	design[opencodeAgentPromptKey] = "{file:./skills/sdd-design/SKILL.md}"
	bad, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode bad config: %v", err)
	}
	err = validateOpenCodeStartupSafeConfig(bad, opencodeConfigFileName)
	if err == nil {
		t.Fatal("validateOpenCodeStartupSafeConfig(bad prompt) error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "agent.sdd-design.prompt") || !strings.Contains(err.Error(), "prompts/sdd/design.md") {
		t.Fatalf("validation error = %v, want prompt-path failure", err)
	}
}

func TestOpenCodeStartupSafeConfigValidationRejectsBadPermissionShape(t *testing.T) {
	data, err := renderOpenCodeNativeConfig(agentpack.DefaultDefinition(), agentconfig.Config{})
	if err != nil {
		t.Fatalf("renderOpenCodeNativeConfig() error = %v, want nil", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode rendered config: %v", err)
	}
	agents := payload[opencodeAgentsKey].(map[string]any)
	lore := agents[opencodePrimaryAgentName].(map[string]any)
	permission := lore[opencodePermissionKey].(map[string]any)
	permission[opencodePermissionTaskKey] = map[string]any{
		"*":     opencodePermissionDenyValue,
		"sdd-*": opencodePermissionAllowValue,
		// Missing lore-worker allow route: managed primary must be able to launch the repository worker.
	}
	bad, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode bad config: %v", err)
	}
	err = validateOpenCodeStartupSafeConfig(bad, opencodeConfigFileName)
	if err == nil {
		t.Fatal("validateOpenCodeStartupSafeConfig(bad primary task routing) error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "agent.lore.permission.task") {
		t.Fatalf("validation error = %v, want primary task permission failure", err)
	}

	// `question` is a native OpenCode permission key but accepts the shorthand action only.
	permission[opencodePermissionTaskKey] = openCodePrimaryAgentPermission()[opencodePermissionTaskKey]
	permission[opencodePermissionQuestionKey] = map[string]any{"*": opencodePermissionAllowValue}
	bad, err = json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode bad question config: %v", err)
	}
	err = validateOpenCodeStartupSafeConfig(bad, opencodeConfigFileName)
	if err == nil {
		t.Fatal("validateOpenCodeStartupSafeConfig(bad question permission) error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "agent.lore.permission.question") {
		t.Fatalf("validation error = %v, want question permission failure", err)
	}
}

func TestOpenCodeStartupSafeConfigValidationRejectsDeprecatedToolsBlock(t *testing.T) {
	data, err := renderOpenCodeNativeConfig(agentpack.DefaultDefinition(), agentconfig.Config{})
	if err != nil {
		t.Fatalf("renderOpenCodeNativeConfig() error = %v, want nil", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode rendered config: %v", err)
	}
	agents := payload[opencodeAgentsKey].(map[string]any)
	worker := agents[opencodeLoreWorkerAgentName].(map[string]any)
	worker["tools"] = map[string]any{opencodePermissionQuestionKey: true}
	bad, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode bad tools config: %v", err)
	}
	err = validateOpenCodeStartupSafeConfig(bad, opencodeConfigFileName)
	if err == nil {
		t.Fatal("validateOpenCodeStartupSafeConfig(deprecated tools) error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "agent.lore-worker.tools") || !strings.Contains(err.Error(), "deprecated") {
		t.Fatalf("validation error = %v, want deprecated tools failure", err)
	}
}

func TestOpenCodeStartupSafeConfigValidationAllowsMCPAndRejectsLegacyTopLevel(t *testing.T) {
	data, err := renderOpenCodeMCPConfig(agentpack.DefaultDefinition(), agentconfig.Config{}, "https://lore.example", "secret-token")
	if err != nil {
		t.Fatalf("renderOpenCodeMCPConfig() error = %v, want nil", err)
	}
	if err := validateOpenCodeStartupSafeConfig(data, opencodeConfigFileName); err != nil {
		t.Fatalf("validateOpenCodeStartupSafeConfig(valid MCP) error = %v, want nil", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode rendered config: %v", err)
	}
	payload[opencodeLoreBlockKey] = map[string]any{opencodeManagedByKey: opencodeManagedByValue}
	bad, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode bad config: %v", err)
	}
	err = validateOpenCodeStartupSafeConfig(bad, opencodeConfigFileName)
	if err == nil {
		t.Fatal("validateOpenCodeStartupSafeConfig(legacy lore block) error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "top-level \"lore\"") {
		t.Fatalf("validation error = %v, want top-level lore rejection", err)
	}
}

// TestOpenCodeAgentOverlayPreservesExistingModelAndVariant is the
// focused regression gate for the `add-opencode-lore-models-plugin`
// reinstall-preservation contract: when the existing
// `opencode.json` carries a non-empty `agent.<name>.model` or
// `agent.<name>.variant` for a Lore-managed agent, the renderer
// MUST project those values into the managed overlay so the merge
// preserves them. Agents without an effective variant MUST NOT
// have a `variant` key invented.
func TestOpenCodeAgentOverlayPreservesExistingModelAndVariant(t *testing.T) {
	cfg := agentconfig.Config{SchemaVersion: agentconfig.SchemaVersion}
	existing := map[string]openCodeExistingAgentEntry{
		opencodePrimaryAgentName: {Model: "openai/user-orchestrator-model", Variant: "user-orchestrator-variant"},
		opencodeLoreWorkerAgentName: {
			Model:   "openai/user-worker-model",
			Variant: "",
		},
		"sdd-design": {Model: "openai/user-design-model", Variant: "user-design-variant"},
		"sdd-apply":  {Model: "", Variant: "user-apply-variant"},
		// Foreign agent must be ignored, not preserved.
		"some-foreign-agent": {Model: "user-foreign-model", Variant: "user-foreign-variant"},
	}
	overlay := opencodeAgentOverlay(agentpack.DefaultDefinition(), cfg, existing)
	primary := overlay[opencodePrimaryAgentName].(map[string]any)
	if got, want := primary["model"], "openai/user-orchestrator-model"; got != want {
		t.Fatalf("primary model = %v, want %q (preserved from existing opencode.json)", got, want)
	}
	if got, want := primary["variant"], "user-orchestrator-variant"; got != want {
		t.Fatalf("primary variant = %v, want %q (preserved from existing opencode.json)", got, want)
	}
	worker := overlay[opencodeLoreWorkerAgentName].(map[string]any)
	if got, want := worker["model"], "openai/user-worker-model"; got != want {
		t.Fatalf("worker model = %v, want %q (preserved from existing opencode.json)", got, want)
	}
	if _, present := worker["variant"]; present {
		t.Fatalf("worker unexpectedly carries variant=%v; empty effective variant must be omitted", worker["variant"])
	}
	design := overlay["sdd-design"].(map[string]any)
	if got, want := design["model"], "openai/user-design-model"; got != want {
		t.Fatalf("sdd-design model = %v, want %q (preserved from existing opencode.json)", got, want)
	}
	if got, want := design["variant"], "user-design-variant"; got != want {
		t.Fatalf("sdd-design variant = %v, want %q (preserved from existing opencode.json)", got, want)
	}
	apply := overlay["sdd-apply"].(map[string]any)
	if _, present := apply["model"]; !present {
		t.Fatalf("sdd-apply missing model; want managed default since existing entry was empty")
	}
	if got, want := apply["variant"], "user-apply-variant"; got != want {
		t.Fatalf("sdd-apply variant = %v, want %q (preserved from existing opencode.json)", got, want)
	}
	if _, present := overlay["some-foreign-agent"]; present {
		t.Fatalf("overlay unexpectedly includes foreign agent %q; only Lore-managed agents are rendered", "some-foreign-agent")
	}
	// Agents without an effective variant must not have an invented
	// variant value in the overlay.
	for _, name := range agentpack.SDDPhaseAgentNames() {
		if name == "sdd-design" || name == "sdd-apply" {
			continue
		}
		entry, ok := overlay[name].(map[string]any)
		if !ok {
			t.Fatalf("overlay missing %q entry", name)
		}
		if v, present := entry["variant"]; present {
			t.Fatalf("overlay.%s.variant = %v, want field omitted (no effective variant)", name, v)
		}
	}
}

// TestOpenCodeAgentsMDDocumentsPrimaryLoreOrchestratorAgent
// verifies the AGENTS.md managed surface copy documents the
// primary `lore` orchestrator entry: the managed surface section
// MUST mention the `agent.lore` entry, the ProfileBalanced model
// source, the AGENTS.md prompt reference, the explicit-selector
// instruction (so users know `opencode --agent lore` still works),
// and the managed default_agent + scoped permission behavior.
func TestOpenCodeAgentsMDDocumentsPrimaryLoreOrchestratorAgent(t *testing.T) {
	adapter := defaultOpenCodeAdapter()
	definition := agentpack.DefaultDefinition()
	rendered, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetOpenCode,
		Definition: definition,
		Components: []ComponentID{ComponentCorePack},
	})
	if err != nil {
		t.Fatalf("Render error = %v, want nil", err)
	}
	files := map[string]RenderedFile{}
	for _, file := range rendered {
		files[file.RelativePath] = file
	}
	agents, ok := files["AGENTS.md"]
	if !ok {
		t.Fatal("rendered files missing AGENTS.md")
	}
	text := string(agents.Content)
	for _, want := range []string{
		"Primary `lore` orchestrator",
		"`agent.lore`",
		"ProfileBalanced.RoleModels[\"orchestrator\"]",
		"{file:./prompts/lore.md}",
		"opencode --agent lore",
		"default_agent: \"lore\"",
		// The add-opencode-lore-models-plugin change removed the
		// `permission: "allow"` bypass; the AGENTS.md managed
		// surface copy MUST reflect that the installer never
		// grants a `permission: "allow"` (or any other) bypass
		// on `agent.lore`.
		"never grants a `permission: \"allow\"`",
		// The managed SDD model declarations section now also lists
		// the primary `lore` orchestrator model at the top.
		"- " + opencodePrimaryAgentName + " (primary orchestrator): ",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("AGENTS.md = %q, want substring %q", text, want)
		}
	}
	// The previous `permission: "allow"` bypass phrase MUST NOT
	// appear anywhere in the AGENTS.md managed surface copy.
	for _, forbidden := range []string{
		"`permission: \"allow\"` only on `agent.lore`",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("AGENTS.md managed surface still contains the removed bypass phrase %q; the add-opencode-lore-models-plugin change removed the `permission: \"allow\"` bypass", forbidden)
		}
	}
}

// keysOfMapForOverlay returns the keys of m for diagnostic output.
// It is a local helper kept in this file so the new tests do not
// depend on helpers in opencode_install_test.go.
func keysOfMapForOverlay(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	return out
}

// TestOpenCodeAdapterRenderExtendedSkillsProducesBoundedBundle verifies
// the extended skills render produces the bounded CLI-managed bundle
// (skill-creator, skill-registry, judgment-day) under the OpenCode
// `skills/` directory and never under user-owned paths.
func TestOpenCodeAdapterRenderExtendedSkillsProducesBoundedBundle(t *testing.T) {
	adapter := defaultOpenCodeAdapter()
	files, err := adapter.RenderExtendedSkills(context.Background(), RenderRequest{
		Target:     TargetOpenCode,
		Definition: agentpack.DefaultDefinition(),
		Components: []ComponentID{ComponentExtendedSkills},
	}, PiLayout{})
	if err != nil {
		t.Fatalf("RenderExtendedSkills error = %v, want nil", err)
	}
	if len(files) == 0 {
		t.Fatalf("RenderExtendedSkills returned 0 files, want at least 1 (extended skills bundle)")
	}
	for _, file := range files {
		if !strings.HasPrefix(file.RelativePath, "skills/") || !strings.HasSuffix(file.RelativePath, "/SKILL.md") {
			t.Fatalf("RenderExtendedSkills emitted %q, want skills/<name>/SKILL.md", file.RelativePath)
		}
		if file.Component != ComponentExtendedSkills {
			t.Fatalf("RenderExtendedSkills component = %q, want %q", file.Component, ComponentExtendedSkills)
		}
	}
}

func keysOfFiles(files map[string]RenderedFile) []string {
	out := make([]string, 0, len(files))
	for k := range files {
		out = append(out, k)
	}
	return out
}
