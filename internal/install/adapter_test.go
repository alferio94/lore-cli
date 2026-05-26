package install

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/agentpack"
)

func TestDefaultComponentSelectionKeepsPiSafeAndMCPOptional(t *testing.T) {
	if got := DefaultComponentSelection(TargetPi); !equalComponentIDs(got, []ComponentID{ComponentCorePack, ComponentPiExtensions}) {
		t.Fatalf("DefaultComponentSelection(pi) = %v, want core-pack + pi-extensions", got)
	}
	if got := DefaultComponentSelection(TargetAntigravity); !equalComponentIDs(got, []ComponentID{ComponentCorePack}) {
		t.Fatalf("DefaultComponentSelection(antigravity) = %v, want core-pack only", got)
	}
	if got := DefaultComponentSelection(TargetClaudeCode); !equalComponentIDs(got, []ComponentID{ComponentCorePack}) {
		t.Fatalf("DefaultComponentSelection(claude-code) = %v, want core-pack only", got)
	}

	resolved, err := NormalizeComponentSelection(TargetPi, []ComponentID{ComponentLoreServerMCP, ComponentCorePack, ComponentCorePack})
	if err != nil {
		t.Fatalf("NormalizeComponentSelection(pi) error = %v, want nil", err)
	}
	if !equalComponentIDs(resolved, []ComponentID{ComponentCorePack, ComponentLoreServerMCP}) {
		t.Fatalf("NormalizeComponentSelection(pi) = %v, want deduped ordered components", resolved)
	}

	resolved, err = NormalizeComponentSelection(TargetAntigravity, []ComponentID{ComponentLoreServerMCP, ComponentCorePack, ComponentCorePack})
	if err != nil {
		t.Fatalf("NormalizeComponentSelection(antigravity) error = %v, want nil", err)
	}
	if !equalComponentIDs(resolved, []ComponentID{ComponentCorePack, ComponentLoreServerMCP}) {
		t.Fatalf("NormalizeComponentSelection(antigravity) = %v, want core-pack + optional MCP", resolved)
	}

	if _, err := NormalizeComponentSelection(TargetAntigravity, []ComponentID{ComponentPiExtensions}); err == nil || !containsAll(err.Error(), string(TargetAntigravity), string(ComponentPiExtensions), "supported") {
		t.Fatalf("NormalizeComponentSelection(antigravity, pi-extensions) error = %v, want unsupported-component guardrail", err)
	}
}

func TestRenderRequestValidateRejectsUnknownComponentsAndWrongSchema(t *testing.T) {
	request := RenderRequest{
		Target:     TargetPi,
		Definition: agentpack.DefaultDefinition(),
		Components: []ComponentID{ComponentCorePack, ComponentPiExtensions},
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("Validate error = %v, want nil", err)
	}

	request.Definition.SchemaVersion = 99
	if err := request.Validate(); err == nil {
		t.Fatal("Validate error = nil, want schema version rejection")
	}

	request = RenderRequest{
		Target:     TargetPi,
		Definition: agentpack.DefaultDefinition(),
		Components: []ComponentID{"unknown"},
	}
	if err := request.Validate(); err == nil {
		t.Fatal("Validate error = nil, want unknown component rejection")
	}
}

func TestRegistryResolveReturnsTargetAdapterAndCapabilities(t *testing.T) {
	registry, err := defaultInstallRegistry()
	if err != nil {
		t.Fatalf("defaultInstallRegistry error = %v, want nil", err)
	}

	adapter, err := registry.Resolve(TargetPi)
	if err != nil {
		t.Fatalf("Resolve(pi) error = %v, want nil", err)
	}
	if adapter.ID() != TargetPi {
		t.Fatalf("adapter.ID() = %q, want %q", adapter.ID(), TargetPi)
	}
	if adapter.Title() != "Pi" {
		t.Fatalf("adapter.Title() = %q, want Pi", adapter.Title())
	}
	capabilities := adapter.Capabilities()
	if capabilities[CapabilityAgentPack].Component != ComponentCorePack {
		t.Fatalf("CapabilityAgentPack = %+v, want core-pack mapping", capabilities[CapabilityAgentPack])
	}
	capabilities[CapabilityAgentPack] = Capability{}
	if adapter.Capabilities()[CapabilityAgentPack].Component != ComponentCorePack {
		t.Fatal("Capabilities() returned shared map, want defensive copy")
	}
	if !adapter.Supports(ComponentPiExtensions) {
		t.Fatal("Supports(pi-extensions) = false, want true")
	}
	if adapter.Supports(ComponentLoreServerMCP) {
		t.Fatal("Supports(lore-server-mcp) = true, want false for Pi scaffold")
	}

	adapter, err = registry.Resolve(TargetAntigravity)
	if err != nil {
		t.Fatalf("Resolve(antigravity) error = %v, want nil", err)
	}
	if adapter.Title() != "Antigravity" {
		t.Fatalf("adapter.Title() = %q, want Antigravity", adapter.Title())
	}
	antigravityCapabilities := adapter.Capabilities()
	if antigravityCapabilities[CapabilityPrompt].Description == "" || antigravityCapabilities[CapabilitySkills].Description == "" {
		t.Fatalf("antigravity capabilities = %+v, want prompt and skills capability flags", antigravityCapabilities)
	}
	if got := antigravityCapabilities[CapabilityLoreServerMCP]; got.Component != ComponentLoreServerMCP || !got.Optional {
		t.Fatalf("CapabilityLoreServerMCP = %+v, want optional MCP capability mapping", got)
	}
	if adapter.Supports(ComponentPiExtensions) {
		t.Fatal("Supports(pi-extensions) = true, want false for Antigravity groundwork")
	}
	if !adapter.Supports(ComponentLoreServerMCP) {
		t.Fatal("Supports(lore-server-mcp) = false, want true for optional Antigravity MCP groundwork")
	}

	if _, err := registry.Resolve(TargetOpenCode); err == nil {
		t.Fatal("Resolve(opencode) error = nil, want unavailable target error")
	}
}

func TestDefaultPiAdapterRenderUsesDefinitionAndPiAssets(t *testing.T) {
	adapter := defaultPiAdapter()
	definition := agentpack.DefaultDefinition()
	definition.Persona.Name = "Lore Custom"
	definition.Profiles = append(definition.Profiles, agentpack.Profile{
		ID:           "audit",
		Description:  "Audit profile",
		DefaultModel: "gpt-5",
	})

	rendered, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetPi,
		Definition: definition,
		Components: []ComponentID{ComponentCorePack, ComponentPiExtensions},
	})
	if err != nil {
		t.Fatalf("Render error = %v, want nil", err)
	}
	if len(rendered) != 3 {
		t.Fatalf("len(rendered) = %d, want 3 managed files", len(rendered))
	}

	files := map[string]RenderedFile{}
	for _, file := range rendered {
		files[file.RelativePath] = file
	}
	settings := string(files["settings.json"].Content)
	if !containsAll(settings,
		`"packages": [`,
		piRemoteSubagentsPackage,
		`"theme": "alferio"`,
		`"pack_id": "portable-agent-pack"`,
		`"persona_name": "Lore Custom"`,
		`"profile_ids": [`,
		`"audit"`,
		`"role_names": [`,
		`"sdd-propose"`,
		`"sdd_phases": [`,
		`"sdd-propose"`) {
		t.Fatalf("settings.json = %q, want rendered agent-pack metadata and remote package", settings)
	}
	if strings.Contains(settings, `"sdd-proposal"`) {
		t.Fatalf("settings.json = %q, want canonical sdd-propose role/phase names only", settings)
	}
	if files["settings.json"].MergeMode != MergeModeAdditiveJSON {
		t.Fatalf("settings merge mode = %q, want additive-json", files["settings.json"].MergeMode)
	}
	if _, ok := files["extensions/lore-delegation.ts"]; ok {
		t.Fatal("rendered files unexpectedly include extensions/lore-delegation.ts")
	}
	if got := files["extensions/lore-footer.ts"].MergeMode; got != MergeModeReplace {
		t.Fatalf("lore-footer merge mode = %q, want replace", got)
	}
}

func TestRenderRequestReplacementsNormalizesSettingsPathAndManagedExtensions(t *testing.T) {
	replacements, err := renderRequestReplacements(RenderRequest{
		Definition:     agentpack.DefaultDefinition(),
		ServerURL:      " https://lore.example ",
		LoreBinaryPath: " /usr/local/bin/lore ",
		LoreConfigDir:  " /tmp/lore ",
		LoreCLIVersion: " v1.2.3 ",
		SettingsPath:   `C:\Users\Lore\.pi\agent\settings.json`,
	}, []ComponentID{ComponentCorePack})
	if err != nil {
		t.Fatalf("renderRequestReplacements error = %v, want nil", err)
	}
	if got, want := replacements["{{LORE_SETTINGS_PATH}}"], "C:/Users/Lore/.pi/agent/settings.json"; got != want {
		t.Fatalf("settings path replacement = %q, want %q", got, want)
	}
	if got, want := replacements["{{LORE_SERVER_URL}}"], "https://lore.example"; got != want {
		t.Fatalf("server url replacement = %q, want %q", got, want)
	}
	if got := replacements["{{LORE_MANAGED_EXTENSIONS}}"]; got != "[]" {
		t.Fatalf("managed extensions replacement = %q, want [] when Pi extensions are not selected", got)
	}
}

func TestDefaultPiAdapterRenderManagedAgentsUsesCanonicalAgentpack(t *testing.T) {
	adapter := defaultPiAdapter()
	definition := agentpack.DefaultDefinition()

	rendered, err := adapter.RenderManagedAgents(context.Background(), RenderRequest{
		Target:     TargetPi,
		Definition: definition,
		Components: []ComponentID{ComponentCorePack, ComponentPiExtensions},
	})
	if err != nil {
		t.Fatalf("RenderManagedAgents error = %v, want nil", err)
	}
	if len(rendered) != len(definition.ManagedAgents) {
		t.Fatalf("len(rendered) = %d, want %d managed overlay agents", len(rendered), len(definition.ManagedAgents))
	}
	files := map[string]RenderedFile{}
	for _, file := range rendered {
		files[file.RelativePath] = file
	}
	workerPath := "agents/lore-managed-lore-worker.md"
	applyPath := "agents/lore-managed-sdd-apply.md"
	proposePath := "agents/lore-managed-sdd-propose.md"
	if got := files[workerPath].RelativePath; got != workerPath {
		t.Fatalf("managed overlay path = %q, want %q", got, workerPath)
	}
	if got := string(files[workerPath].Content); !containsAll(got,
		"tools:",
		"requiredEnvelope: worker",
		"You are the canonical Lore repository worker.",
		"Return ONLY one JSON object with exactly these keys: `status`, `summary`, `artifacts`, `next`, `question`, `options`, `risks`, `skill_resolution`.",
		"`summary`: one compact operational line, <= 280 chars",
		"`artifacts`: string array with <= 8 artifact references, each <= 160 chars",
		"`next`: string <= 160 chars or null",
		"`question`: string <= 220 chars or null",
		"`options`: string array with <= 5 compact choices",
		"`risks`: string array with <= 5 compact items, each <= 180 chars",
		"`skill_resolution`: `injected` | `fallback-registry` | `fallback-path` | `none` and <= 80 chars",
		"Persist or reference long details in artifacts; do not embed long logs, diffs, or narratives in the envelope itself.",
	) {
		t.Fatalf("worker managed overlay content = %q, want compact worker envelope snippets in rendered markdown", got)
	} else {
		for _, forbidden := range []string{"managedBy: lore-cli", "managedLayer: global-overlay", "managedPackId: portable-agent-pack", "`kind`", "`specialization`", "`memory_saved`"} {
			if strings.Contains(got, forbidden) {
				t.Fatalf("worker managed overlay content = %q, want %q omitted from the rendered active contract", got, forbidden)
			}
		}
		if exception := "`judgment-day` remains explicitly out of scope"; strings.Contains(got, "judgment-day") && !strings.Contains(got, exception) {
			t.Fatalf("worker managed overlay content = %q, want strict child-envelope judgment-day exclusion %q when judgment-day is mentioned", got, exception)
		}
	}
	if got := string(files[applyPath].Content); !containsAll(got, "phase: apply", "skillPolicyMode: explicit", "~/.pi/agent/skills/sdd-apply/SKILL.md", "You execute the SDD apply phase.") {
		t.Fatalf("apply managed overlay content = %q, want canonical SDD apply metadata and body", got)
	}
	if got := string(files[proposePath].Content); !containsAll(got, "phase: propose", "You execute the SDD propose phase.") || strings.Contains(got, "phase: proposal") {
		t.Fatalf("propose managed overlay content = %q, want render-time propose phase mapping", got)
	}
}

func TestDefaultPiAdapterCanonicalAssetsStayByteCompatibleWithProjectedDefinition(t *testing.T) {
	adapter := defaultPiAdapter()
	assets := agentpack.DefaultOperationalAssets()
	definitionRequest := RenderRequest{
		Target:     TargetPi,
		Definition: assets.Definition(),
		Components: []ComponentID{ComponentCorePack, ComponentPiExtensions},
	}
	assetRequest := RenderRequest{
		Target:     TargetPi,
		Assets:     assets,
		Components: []ComponentID{ComponentCorePack, ComponentPiExtensions},
	}

	definitionFiles, err := adapter.Render(context.Background(), definitionRequest)
	if err != nil {
		t.Fatalf("Render(definition) error = %v, want nil", err)
	}
	assetFiles, err := adapter.Render(context.Background(), assetRequest)
	if err != nil {
		t.Fatalf("Render(assets) error = %v, want nil", err)
	}
	if !reflect.DeepEqual(assetFiles, definitionFiles) {
		t.Fatalf("Render(assets) drifted from projected definition\nassets=%+v\ndefinition=%+v", assetFiles, definitionFiles)
	}

	definitionAgents, err := adapter.RenderManagedAgents(context.Background(), definitionRequest)
	if err != nil {
		t.Fatalf("RenderManagedAgents(definition) error = %v, want nil", err)
	}
	assetAgents, err := adapter.RenderManagedAgents(context.Background(), assetRequest)
	if err != nil {
		t.Fatalf("RenderManagedAgents(assets) error = %v, want nil", err)
	}
	if !reflect.DeepEqual(assetAgents, definitionAgents) {
		t.Fatalf("RenderManagedAgents(assets) drifted from projected definition\nassets=%+v\ndefinition=%+v", assetAgents, definitionAgents)
	}
}

func TestLegacyDelegationAssetIsQuarantinedAndNotPartOfCurrentInstall(t *testing.T) {
	asset, err := piAssets.ReadFile("assets/pi/lore-delegation.ts")
	if err != nil {
		t.Fatalf("ReadFile lore-delegation.ts error = %v, want nil", err)
	}
	text := string(asset)
	if !containsAll(text, "LEGACY QUARANTINED ASSET", "do not treat its worker envelope schema as the current contract") {
		t.Fatalf("lore-delegation.ts = %q, want quarantine banner", text)
	}
}

func TestRenderManagedAgentMarkdownEmitsManagedFrontmatterOnlyWhenContractSupportsIt(t *testing.T) {
	agent := agentpack.DefaultDefinition().ManagedAgents[0]

	defaultRendered := renderManagedAgentMarkdown(agent, "portable-agent-pack", defaultRuntimeContract())
	if strings.Contains(defaultRendered, "managedBy:") || strings.Contains(defaultRendered, "managedLayer:") || strings.Contains(defaultRendered, "managedPackId:") {
		t.Fatalf("default rendered overlay = %q, want managed frontmatter omitted", defaultRendered)
	}

	contract := defaultRuntimeContract()
	contract.AgentResolution.SupportsManagedFrontmatter = true
	explicitRendered := renderManagedAgentMarkdown(agent, "portable-agent-pack", contract)
	if !containsAll(explicitRendered, "managedBy: lore-cli", "managedLayer: global-overlay", "managedPackId: portable-agent-pack") {
		t.Fatalf("explicit rendered overlay = %q, want managed frontmatter when contract supports it", explicitRendered)
	}
}

func equalComponentIDs(got, want []ComponentID) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
