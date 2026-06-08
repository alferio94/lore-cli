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
		return agentpack.DefaultSDDModel
	}
	if model := profile.ModelForRole(agentpack.RoleOrchestrator); model != "" {
		return model
	}
	return agentpack.DefaultSDDModel
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
	// 3.x docs/UI slice: the managed surface section MUST list the
	// bundled plugin set and the explicit exclusion list. The legacy
	// "no plugins" wording MUST be gone.
	for _, want := range []string{
		"Managed plugin bundle",
		"background-agents.ts",
		"model-variants.ts",
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
// `model` + `{file:./skills/<name>/SKILL.md}` prompt reference),
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
	// references the managed AGENTS.md file.
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
		wantPrompt := "{file:./skills/" + phaseAgent + "/SKILL.md}"
		if prompt != wantPrompt {
			t.Fatalf("agent.%s.prompt = %q, want %q (native {file:...} reference)", phaseAgent, prompt, wantPrompt)
		}
	}
	skills, ok := payload["skills"].(map[string]any)
	if !ok {
		t.Fatalf("payload missing top-level `skills` block: %v", payload)
	}
	if got := skills["path"]; got != opencodeSkillsDirPath {
		t.Fatalf("skills.path = %v, want %q", got, opencodeSkillsDirPath)
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
	// Ownership contract: the rendered mcp.lore block must carry the
	// `managed_by: lore-cli` marker so the additive merge in
	// mergeOpenCodeConfigJSON can detect when an existing mcp.lore
	// block is Lore-owned vs foreign. A missing or mismatched
	// marker is what triggers the fail-closed ownership error.
	if got := loreMCP["managed_by"]; got != "lore-cli" {
		t.Fatalf("mcp.lore.managed_by = %v, want lore-cli (ownership marker for additive merge)", got)
	}
	headers, _ := loreMCP["headers"].(map[string]any)
	if got := headers["Authorization"]; got != "Bearer secret-token" {
		t.Fatalf("mcp.lore.headers.Authorization = %v, want Bearer secret-token", got)
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
//   - The local contract for the `agent` block does not include a
//     documented `default` field. The installer emits no `default`
//     marker; the primary must be selected explicitly by name
//     (e.g. `opencode --agent lore`).
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
	if _, ok := agent["default"]; ok {
		t.Fatalf("agent overlay carries a `default` key; the local contract does not document a default field and the installer must not emit one")
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
	want := profile.ModelForRole(agentpack.RoleOrchestrator)
	if got := opencodeOrchestratorModel(definition); got != want {
		t.Fatalf("opencodeOrchestratorModel(DefaultDefinition) = %q, want %q", got, want)
	}
	if got := opencodeOrchestratorModel(agentpack.Definition{}); got != agentpack.DefaultSDDModel {
		t.Fatalf("opencodeOrchestratorModel(empty) = %q, want fallback %q", got, agentpack.DefaultSDDModel)
	}
}

// TestOpenCodeAgentOverlayPrimaryIsLayeredOnTopOfSddPhases is the
// focused regression gate for the additive layering of the primary
// `lore` orchestrator entry on top of the 9 sdd-* phase agent
// entries. The test asserts:
//
//   - The overlay contains 10 entries (1 primary + 9 sdd-*).
//   - The primary `lore` entry uses the `ProfileBalanced` orchestrator
//     model and references the managed AGENTS.md file.
//   - Each sdd-* entry uses the per-agent model from agent-config
//     when present, otherwise the agentpack default, and references
//     the corresponding `skills/<name>/SKILL.md` file.
//   - The overlay is compatible with the documented OpenCode
//     `agent` block contract: `{description, model, prompt}` for the
//     primary, `{model, prompt}` for the sdd-* entries.
func TestOpenCodeAgentOverlayPrimaryIsLayeredOnTopOfSddPhases(t *testing.T) {
	cfg := agentconfig.Config{
		SchemaVersion: agentconfig.SchemaVersion,
		SDDAgents: map[string]agentconfig.Agent{
			"sdd-apply": {Model: "gpt-5-custom-apply"},
		},
	}
	overlay := opencodeAgentOverlay(agentpack.DefaultDefinition(), cfg)
	if got, want := len(overlay), 1+len(agentpack.SDDPhaseAgentNames()); got != want {
		t.Fatalf("opencodeAgentOverlay size = %d, want %d (1 primary + %d sdd-*)", got, want, len(agentpack.SDDPhaseAgentNames()))
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
	for _, name := range agentpack.SDDPhaseAgentNames() {
		entry, ok := overlay[name].(map[string]any)
		if !ok {
			t.Fatalf("overlay missing %q entry; got keys %v", name, keysOfMapForOverlay(overlay))
		}
		model, _ := entry["model"].(string)
		wantModel := agentpack.DefaultSDDModel
		if name == "sdd-apply" {
			wantModel = "gpt-5-custom-apply"
		}
		if model != wantModel {
			t.Fatalf("overlay.%s.model = %q, want %q (per-agent override or default)", name, model, wantModel)
		}
		wantPrompt := "{file:./skills/" + name + "/SKILL.md}"
		if got, _ := entry["prompt"].(string); got != wantPrompt {
			t.Fatalf("overlay.%s.prompt = %q, want %q", name, got, wantPrompt)
		}
	}
}

// TestOpenCodeAgentsMDDocumentsPrimaryLoreOrchestratorAgent
// verifies the AGENTS.md managed surface copy documents the
// primary `lore` orchestrator entry: the managed surface section
// MUST mention the `agent.lore` entry, the ProfileBalanced model
// source, the AGENTS.md prompt reference, the explicit-selector
// instruction (so users know to run `opencode --agent lore`), and
// the lack of a documented `default` field in the local contract.
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
		"{file:./AGENTS.md}",
		"opencode --agent lore",
		"no supported `default` field",
		// The managed SDD model declarations section now also lists
		// the primary `lore` orchestrator model at the top.
		"- " + opencodePrimaryAgentName + " (primary orchestrator): ",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("AGENTS.md = %q, want substring %q", text, want)
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
