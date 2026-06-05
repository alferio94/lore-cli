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

// TestOpenCodeMCPConfigRendersRemoteMCPBlock verifies the Pi/Antigravity-
// shaped remote MCP block: top-level `lore` block, `mcp.lore` entry with
// type=remote, a normalized server URL, and a Bearer Authorization
// header that exactly mirrors the Antigravity local plaintext-token
// tradeoff.
func TestOpenCodeMCPConfigRendersRemoteMCPBlock(t *testing.T) {
	data, err := renderOpenCodeMCPConfig(agentconfig.Config{}, "https://lore.example", "secret-token")
	if err != nil {
		t.Fatalf("renderOpenCodeMCPConfig error = %v, want nil", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	lore, ok := payload["lore"].(map[string]any)
	if !ok {
		t.Fatalf("payload missing top-level `lore` object: %v", payload)
	}
	if got := lore["managed_by"]; got != "lore-cli" {
		t.Fatalf("lore.managed_by = %v, want lore-cli", got)
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
	if _, err := renderOpenCodeMCPConfig(agentconfig.Config{}, "", "token"); err == nil {
		t.Fatal("renderOpenCodeMCPConfig(empty server) error = nil, want server-url validation error")
	}
	if _, err := renderOpenCodeMCPConfig(agentconfig.Config{}, "https://lore.example", "  "); err == nil {
		t.Fatal("renderOpenCodeMCPConfig(empty token) error = nil, want token validation error")
	}
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
