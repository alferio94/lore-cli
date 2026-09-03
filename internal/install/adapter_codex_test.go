package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alferio94/lore-cli/internal/agentconfig"
	"github.com/alferio94/lore-cli/internal/agentpack"
)

func TestCodexAdapterID(t *testing.T) {
	adapter := defaultCodexAdapter()
	if got := adapter.ID(); got != TargetCodex {
		t.Fatalf("adapter.ID() = %q, want %q", got, TargetCodex)
	}
}

func TestCodexAdapterTitle(t *testing.T) {
	adapter := defaultCodexAdapter()
	if got := adapter.Title(); got != "Codex" {
		t.Fatalf("adapter.Title() = %q, want %q", got, "Codex")
	}
}

func TestCodexAdapterCapabilities(t *testing.T) {
	adapter := defaultCodexAdapter()
	caps := adapter.Capabilities()

	if cap, ok := caps[CapabilityAgentPack]; !ok {
		t.Fatalf("capability %q not found", CapabilityAgentPack)
	} else if cap.Component != ComponentCorePack {
		t.Fatalf("capability %q component = %q, want %q", CapabilityAgentPack, cap.Component, ComponentCorePack)
	}

	if cap, ok := caps[CapabilityLoreServerMCP]; !ok {
		t.Fatal("Codex should have lore-server-mcp capability")
	} else if cap.Component != ComponentLoreServerMCP {
		t.Fatalf("capability %q component = %q, want %q", CapabilityLoreServerMCP, cap.Component, ComponentLoreServerMCP)
	}
}

func TestCodexAdapterSupports(t *testing.T) {
	adapter := defaultCodexAdapter()
	if !adapter.Supports(ComponentCorePack) {
		t.Fatal("Codex adapter should support core-pack")
	}
	if !adapter.Supports(ComponentExtendedSkills) {
		t.Fatal("Codex adapter should support extended-skills")
	}
	if !adapter.Supports(ComponentLoreServerMCP) {
		t.Fatal("Codex adapter should support lore-server-mcp")
	}
}

func TestResolveCodexLayout(t *testing.T) {
	homeDir := "/home/user"
	layout := ResolveCodexLayout(homeDir)

	if layout.Target != TargetCodex {
		t.Fatalf("layout.Target = %q, want %q", layout.Target, TargetCodex)
	}
	if layout.RootDir != filepath.Join(homeDir, ".codex") {
		t.Fatalf("layout.RootDir = %q, want %q", layout.RootDir, filepath.Join(homeDir, ".codex"))
	}
	if layout.Paths["agents_md"] != filepath.Join(homeDir, ".codex", "AGENTS.md") {
		t.Fatalf("agents_md = %q, want %q", layout.Paths["agents_md"], filepath.Join(homeDir, ".codex", "AGENTS.md"))
	}
	if layout.Paths["skills_dir"] != filepath.Join(homeDir, ".codex", "skills") {
		t.Fatalf("skills_dir = %q, want %q", layout.Paths["skills_dir"], filepath.Join(homeDir, ".codex", "skills"))
	}
	if layout.ManifestPath != filepath.Join(homeDir, ".codex", "lore-install.json") {
		t.Fatalf("manifest_path = %q, want %q", layout.ManifestPath, filepath.Join(homeDir, ".codex", "lore-install.json"))
	}
	if layout.Paths["config_toml"] != filepath.Join(homeDir, ".codex", "config.toml") {
		t.Fatalf("config_toml = %q, want %q", layout.Paths["config_toml"], filepath.Join(homeDir, ".codex", "config.toml"))
	}
}

func TestCodexAdapterRenderAgentsMD(t *testing.T) {
	tmpDir := t.TempDir()
	layout := ResolveCodexLayout(tmpDir)

	// Create agent-config.json with test data.
	agentConfig := agentconfig.Config{
		SchemaVersion: 1,
		SDDAgents: map[string]agentconfig.Agent{
			"sdd-init":   {Model: "gpt-5.4"},
			"sdd-verify": {Model: "gpt-5.4"},
		},
	}

	req := RenderRequest{
		Target:      TargetCodex,
		Assets:      agentpack.DefaultOperationalAssets(),
		Components:  []ComponentID{ComponentCorePack},
		AgentConfig: agentConfig,
	}

	adapter := defaultCodexAdapter()
	files, err := adapter.Render(context.Background(), req)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	// Find AGENTS.md in rendered files.
	var agentsFile *RenderedFile
	for _, f := range files {
		if filepath.ToSlash(f.RelativePath) == "AGENTS.md" {
			agentsFile = &f
			break
		}
	}
	if agentsFile == nil {
		t.Fatal("AGENTS.md not found in rendered files")
	}

	content := string(agentsFile.Content)
	if !strings.Contains(content, "# Lore Configuration") {
		t.Fatal("AGENTS.md should contain Lore Configuration header")
	}
	if !strings.Contains(content, "- sdd-init: gpt-5.4") {
		t.Fatalf("AGENTS.md should contain sdd-init with gpt-5.4, got: %s", content)
	}
	if !strings.Contains(content, "~/.codex/config.toml") {
		t.Fatal("AGENTS.md should reference ~/.codex/config.toml")
	}
	if !strings.Contains(content, "remote MCP entry") {
		t.Fatal("AGENTS.md should describe managed remote MCP config")
	}
	if strings.Contains(content, "[mcp_servers]") {
		t.Fatal("AGENTS.md should NOT inline TOML MCP blocks")
	}
	if !strings.Contains(content, "~/.codex/skills") {
		t.Fatal("AGENTS.md should reference ~/.codex/skills")
	}
	_ = layout // layout constructed OK, just verify the files render
}

func TestCodexAdapterRenderWithExtendedSkills(t *testing.T) {
	req := RenderRequest{
		Target:     TargetCodex,
		Assets:     agentpack.DefaultOperationalAssets(),
		Components: []ComponentID{ComponentCorePack, ComponentExtendedSkills},
	}

	adapter := defaultCodexAdapter()
	files, err := adapter.Render(context.Background(), req)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	if len(files) == 0 {
		t.Fatal("Render should produce files")
	}

	// Should have AGENTS.md.
	hasAgentsMD := false
	hasSkillFiles := false
	for _, f := range files {
		if filepath.ToSlash(f.RelativePath) == "AGENTS.md" {
			hasAgentsMD = true
		}
		if strings.Contains(f.RelativePath, "skills/") && strings.HasSuffix(f.RelativePath, ".md") {
			hasSkillFiles = true
		}
	}
	if !hasAgentsMD {
		t.Fatal("Render should produce AGENTS.md")
	}
	if !hasSkillFiles {
		t.Fatal("Render should produce skill files")
	}
	filesByPath := make(map[string]RenderedFile, len(files))
	for _, file := range files {
		filesByPath[filepath.ToSlash(file.RelativePath)] = file
	}
	proposePath := "skills/sdd-propose/SKILL.md"
	propose, ok := filesByPath[proposePath]
	if !ok {
		t.Fatalf("Render paths = %v, want %s", sortedRenderedPaths(files), proposePath)
	}
	if strings.Contains(string(propose.Content), "name: sdd-proposal") {
		t.Fatalf("%s uses a non-canonical proposal agent name", proposePath)
	}
	if _, ok := filesByPath["skills/sdd-proposal/SKILL.md"]; ok {
		t.Fatalf("Render produced non-canonical proposal path: %v", sortedRenderedPaths(files))
	}
	for path, required := range map[string][]string{
		"skills/lore-worker/SKILL.md": {"You are the canonical Lore repository worker.", "`status`, `summary`, `artifacts`, `files`, `validations`, `risks`, `next_step`, `continuation`, `question`, `options`, `skill_resolution`"},
		"skills/sdd-apply/SKILL.md":   {"You execute the SDD apply phase.", "set `phase` to `apply`", "`status`, `phase`, `summary`, `artifacts`, `files`, `validations`, `risks`, `next_step`, `continuation`, `question`, `options`, `skill_resolution`"},
	} {
		file, ok := filesByPath[path]
		if !ok {
			t.Fatalf("Render paths = %v, want %s", sortedRenderedPaths(files), path)
		}
		content := string(file.Content)
		if !containsAll(content, required...) {
			t.Fatalf("%s omitted canonical role/envelope semantics: %s", path, content)
		}
		for _, forbidden := range []string{"lore-pi-runtime", "Pi Lore delegation adapter contract", "runtime injects a response contract"} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("%s leaked Pi-only runtime contract %q: %s", path, forbidden, content)
			}
		}
	}
}

func TestCodexAdapterRenderWithManagedRemoteMCP(t *testing.T) {
	req := RenderRequest{
		Target:     TargetCodex,
		Assets:     agentpack.DefaultOperationalAssets(),
		Components: []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
		ServerURL:  "https://example.test",
		SavedToken: "secret-token",
	}

	adapter := defaultCodexAdapter()
	files, err := adapter.Render(context.Background(), req)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	foundConfig := false
	for _, f := range files {
		if f.RelativePath == codexConfigTomlRelativePath {
			foundConfig = true
			content := string(f.Content)
			if !containsAll(content, codexMCPBlockStartMarker, "[mcp_servers.lore]", `url = "https://example.test/v1/mcp"`, `[mcp_servers.lore.http_headers]`, `Authorization = "Bearer secret-token"`) {
				t.Fatalf("config.toml = %q, want managed Lore MCP block", content)
			}
			if strings.Contains(content, `[mcp_servers.lore.headers]`) || strings.Contains(content, `bearer_token_env_var`) {
				t.Fatalf("config.toml = %q, want http_headers auth only", content)
			}
		}
	}
	if !foundConfig {
		t.Fatal("Render should produce config.toml when lore-server-mcp is selected")
	}
}

func TestCodexAssets(t *testing.T) {
	cfg := agentconfig.Config{SDDAgents: map[string]agentconfig.Agent{"sdd-apply": {Model: "gpt-test"}}}

	section := renderCodexSDDSection(cfg)
	if !containsAll(section,
		"## Codex-native SDD orchestration",
		"spawn_agent",
		"wait_agent",
		"close_agent",
		"sdd-apply",
		"model `gpt-test`",
		"reasoning effort `high`",
		"inline/sequentially",
	) {
		t.Fatalf("Codex SDD section missing routing/fallback details:\n%s", section)
	}
	if strings.Contains(section, "gentle-ai") {
		t.Fatalf("Codex SDD section should be Lore-owned, got gentle-ai reference:\n%s", section)
	}

	approvalFragment := renderCodexLoreMCPApprovalFragment()
	if !containsAll(approvalFragment,
		"[mcp_servers.lore.tools.lore_me]",
		"[mcp_servers.lore.tools.lore_project_list]",
		"[mcp_servers.lore.tools.lore_memory_get]",
		`approval_mode = "approve"`,
	) {
		t.Fatalf("approval fragment missing expected Lore MCP tool approvals:\n%s", approvalFragment)
	}
	if strings.Contains(approvalFragment, "lore_skill_") || strings.Contains(approvalFragment, "default_tools_approval_mode") {
		t.Fatalf("approval fragment should stay narrow, got:\n%s", approvalFragment)
	}
}

func TestRenderCodexProfiles(t *testing.T) {
	cfg := agentconfig.Config{SDDAgents: map[string]agentconfig.Agent{"sdd-apply": {Model: "gpt-custom"}}}
	files := renderCodexSDDProfiles(cfg)

	want := map[string]string{
		codexStrongProfileRelativePath: `model_reasoning_effort = "high"`,
		codexMidProfileRelativePath:    `model_reasoning_effort = "medium"`,
		codexCheapProfileRelativePath:  `model_reasoning_effort = "low"`,
	}
	if len(files) != len(want) {
		t.Fatalf("renderCodexSDDProfiles returned %d files, want %d", len(files), len(want))
	}
	for _, file := range files {
		relativePath := filepath.ToSlash(file.RelativePath)
		wantEffort, ok := want[relativePath]
		if !ok {
			t.Fatalf("unexpected profile path %q", relativePath)
		}
		content := string(file.Content)
		if file.Component != ComponentCorePack || file.MergeMode != MergeModeReplace {
			t.Fatalf("profile %q component/mode = %q/%q, want %q/%q", relativePath, file.Component, file.MergeMode, ComponentCorePack, MergeModeReplace)
		}
		if !containsAll(content, `model = "gpt-custom"`, wantEffort) {
			t.Fatalf("profile %q content = %q, want model and effort", relativePath, content)
		}
		delete(want, relativePath)
	}
	if len(want) != 0 {
		t.Fatalf("missing profile paths: %v", want)
	}
}

func TestCodexRenderProfilesAndHooksAbsent(t *testing.T) {
	req := RenderRequest{
		Target:     TargetCodex,
		Assets:     agentpack.DefaultOperationalAssets(),
		Components: []ComponentID{ComponentCorePack},
		AgentConfig: agentconfig.Config{SDDAgents: map[string]agentconfig.Agent{
			"sdd-apply": {Model: "gpt-custom"},
		}},
	}

	adapter := defaultCodexAdapter()
	files, err := adapter.Render(context.Background(), req)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	seen := map[string]bool{}
	for _, file := range files {
		relativePath := filepath.ToSlash(file.RelativePath)
		seen[relativePath] = true
		if relativePath == "AGENTS.md" {
			content := string(file.Content)
			if !containsAll(content, "Codex-native SDD orchestration", "spawn_agent", "sdd-strong.config.toml", "inline/sequentially") {
				t.Fatalf("AGENTS.md missing Codex-native routing guidance:\n%s", content)
			}
		}
		if relativePath == "hooks.json" {
			t.Fatal("Codex render plan must not include hooks.json in this change")
		}
	}
	for _, required := range []string{"AGENTS.md", codexStrongProfileRelativePath, codexMidProfileRelativePath, codexCheapProfileRelativePath} {
		if !seen[required] {
			t.Fatalf("rendered files missing %q; got %v", required, seen)
		}
	}
}

func TestCodexRenderMCPApprovalFragments(t *testing.T) {
	req := RenderRequest{
		Target:     TargetCodex,
		Assets:     agentpack.DefaultOperationalAssets(),
		Components: []ComponentID{ComponentLoreServerMCP},
		ServerURL:  "https://example.test",
		SavedToken: "secret-token",
	}

	adapter := defaultCodexAdapter()
	files, err := adapter.Render(context.Background(), req)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	for _, file := range files {
		if filepath.ToSlash(file.RelativePath) != codexConfigTomlRelativePath {
			continue
		}
		content := string(file.Content)
		if !containsAll(content,
			"[mcp_servers.lore.tools.lore_me]",
			"[mcp_servers.lore.tools.lore_project_activity]",
			"[mcp_servers.lore.tools.lore_memory_search]",
			`approval_mode = "approve"`,
		) {
			t.Fatalf("config.toml missing Lore MCP approval fragments:\n%s", content)
		}
		if strings.Contains(content, "lore_skill_") || strings.Contains(content, "[mcp_servers.filesystem") || strings.Contains(content, "[mcp_servers.shell") {
			t.Fatalf("config.toml approval fragments are overbroad:\n%s", content)
		}
		return
	}
	t.Fatal("Render should produce config.toml when lore-server-mcp is selected")
}

func TestCodexSkillPathResolver(t *testing.T) {
	resolver := CodexSkillPathResolver()

	// Test regular skill.
	ref := agentpack.SkillRef{Name: "sdd-apply"}
	got := resolver.ResolveSkillRef(ref)
	want := "~/.codex/skills/sdd-apply/SKILL.md"
	if got != want {
		t.Errorf("ResolveSkillRef(%v) = %q, want %q", ref, got, want)
	}

	// Test shared skill.
	sharedRef := agentpack.SkillRef{Name: "sdd-apply", Shared: true}
	got = resolver.ResolveSkillRef(sharedRef)
	want = "~/.codex/skills/sdd-apply.md"
	if got != want {
		t.Errorf("ResolveSkillRef(%v) = %q, want %q", sharedRef, got, want)
	}
}

func TestCodexBackupRelativePath(t *testing.T) {
	tests := []struct {
		relativePath string
		want         string
	}{
		{"AGENTS.md", "AGENTS.md"},
		{"skills/sdd-apply/SKILL.md", "skills/sdd-apply/SKILL.md"},
	}

	for _, tt := range tests {
		got := codexBackupRelativePath(tt.relativePath)
		if got != tt.want {
			t.Errorf("codexBackupRelativePath(%q) = %q, want %q", tt.relativePath, got, tt.want)
		}
	}
}

func TestCodexAbsolutePath(t *testing.T) {
	layout := ResolveCodexLayout("/home/user")

	tests := []struct {
		relativePath string
		want         string
	}{
		{"AGENTS.md", filepath.Join(layout.RootDir, "AGENTS.md")},
		{"config.toml", filepath.Join(layout.RootDir, "config.toml")},
		{"lore-install.json", layout.ManifestPath},
		{"skills/sdd-apply/SKILL.md", filepath.Join(layout.RootDir, "skills", "sdd-apply", "SKILL.md")},
	}

	for _, tt := range tests {
		got := codexAbsolutePath(layout, tt.relativePath)
		if got != tt.want {
			t.Errorf("codexAbsolutePath(%q) = %q, want %q", tt.relativePath, got, tt.want)
		}
	}
}

func TestPlanCodexInstallCreatesAgentConfig(t *testing.T) {
	// This test verifies that PlanCodexInstall calls EnsureDefault.
	// We use a fake AgentConfigStore that records calls.
	calls := 0
	fakeStore := &testCodexAgentConfigStore{
		onEnsureDefault: func() (agentconfig.Config, bool, error) {
			calls++
			return agentconfig.DefaultConfig(), true, nil
		},
	}

	svc := Service{AgentConfigStore: fakeStore}
	req := InstallRequest{
		HomeDir:    t.TempDir(),
		Target:     TargetCodex,
		Components: []ComponentID{ComponentCorePack},
	}

	plan, err := svc.PlanCodexInstall(req)
	if err != nil {
		t.Fatalf("PlanCodexInstall error: %v", err)
	}
	if plan.Layout.Target != TargetCodex {
		t.Fatalf("plan.Layout.Target = %q, want %q", plan.Layout.Target, TargetCodex)
	}
	if calls != 1 {
		t.Errorf("EnsureDefault called %d times, want 1", calls)
	}
}

func TestPlanCodexInstallResolvesLayout(t *testing.T) {
	svc := Service{}
	tmpDir := t.TempDir()
	req := InstallRequest{
		HomeDir:    tmpDir,
		Target:     TargetCodex,
		Components: []ComponentID{ComponentCorePack},
	}

	plan, err := svc.PlanCodexInstall(req)
	if err != nil {
		t.Fatalf("PlanCodexInstall error: %v", err)
	}
	if plan.Layout.RootDir != filepath.Join(tmpDir, ".codex") {
		t.Fatalf("layout.RootDir = %q, want %q", plan.Layout.RootDir, filepath.Join(tmpDir, ".codex"))
	}
	if plan.Layout.Target != TargetCodex {
		t.Fatalf("layout.Target = %q, want %q", plan.Layout.Target, TargetCodex)
	}
}

// testCodexAgentConfigStore implements AgentConfigStore for testing.
type testCodexAgentConfigStore struct {
	onEnsureDefault func() (agentconfig.Config, bool, error)
	onLoad          func() (agentconfig.Config, error)
	onPath          func() (string, error)
}

func (f *testCodexAgentConfigStore) Path() (string, error) {
	if f.onPath != nil {
		return f.onPath()
	}
	return "/fake/agent-config.json", nil
}

func (f *testCodexAgentConfigStore) Load() (agentconfig.Config, error) {
	if f.onLoad != nil {
		return f.onLoad()
	}
	return agentconfig.Config{}, nil
}

func (f *testCodexAgentConfigStore) EnsureDefault() (agentconfig.Config, bool, error) {
	if f.onEnsureDefault != nil {
		return f.onEnsureDefault()
	}
	return agentconfig.Config{}, false, nil
}

func TestExecuteCodexInstallDryRun(t *testing.T) {
	svc := Service{}
	tmpDir := t.TempDir()
	req := InstallRequest{
		HomeDir:    tmpDir,
		Target:     TargetCodex,
		Components: []ComponentID{ComponentCorePack},
	}

	plan, err := svc.PlanCodexInstall(req)
	if err != nil {
		t.Fatalf("PlanCodexInstall error: %v", err)
	}

	result, err := svc.ExecuteCodexInstall(plan, InstallCommandOptions{DryRun: true})
	if err != nil {
		t.Fatalf("ExecuteCodexInstall dry-run error: %v", err)
	}
	if result.Target != TargetCodex {
		t.Fatalf("result.Target = %q, want %q", result.Target, TargetCodex)
	}
	// Dry run should not create files.
	agentsPath := filepath.Join(tmpDir, ".codex", "AGENTS.md")
	if _, err := os.Stat(agentsPath); !os.IsNotExist(err) {
		t.Errorf("dry-run should not create %s", agentsPath)
	}
}

func TestExecuteCodexInstallCreatesFiles(t *testing.T) {
	svc := Service{}
	tmpDir := t.TempDir()
	req := InstallRequest{
		HomeDir:    tmpDir,
		Target:     TargetCodex,
		Components: []ComponentID{ComponentCorePack},
	}

	plan, err := svc.PlanCodexInstall(req)
	if err != nil {
		t.Fatalf("PlanCodexInstall error: %v", err)
	}

	result, err := svc.ExecuteCodexInstall(plan, InstallCommandOptions{DryRun: false})
	if err != nil {
		t.Fatalf("ExecuteCodexInstall error: %v", err)
	}
	if result.Target != TargetCodex {
		t.Fatalf("result.Target = %q, want %q", result.Target, TargetCodex)
	}

	// Verify files created.
	agentsPath := filepath.Join(tmpDir, ".codex", "AGENTS.md")
	data, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("should create AGENTS.md: %v", err)
	}
	if !strings.Contains(string(data), "# Lore Configuration") {
		t.Fatalf("AGENTS.md should contain Lore Configuration header, got: %s", string(data))
	}

	// Verify manifest created.
	manifestPath := filepath.Join(tmpDir, ".codex", "lore-install.json")
	if _, err := os.ReadFile(manifestPath); err != nil {
		t.Fatalf("should create lore-install.json: %v", err)
	}
}

func TestExecuteCodexInstallDoesNotWriteManifestWhenPromptApplyFails(t *testing.T) {
	svc := Service{}
	tmpDir := t.TempDir()
	originalApply := applyCodexPlannedContent
	applyCodexPlannedContent = func(action PlanFileAction, desired []byte) error {
		if action.RelativePath == "AGENTS.md" {
			return errors.New("injected AGENTS.md apply failure")
		}
		return originalApply(action, desired)
	}
	t.Cleanup(func() { applyCodexPlannedContent = originalApply })

	plan, err := svc.PlanCodexInstall(InstallRequest{HomeDir: tmpDir, Target: TargetCodex, Components: []ComponentID{ComponentCorePack}})
	if err != nil {
		t.Fatalf("PlanCodexInstall error: %v", err)
	}
	_, err = svc.ExecuteCodexInstall(plan, InstallCommandOptions{})
	if err == nil || !strings.Contains(err.Error(), "AGENTS.md") {
		t.Fatalf("ExecuteCodexInstall error = %v, want AGENTS.md apply failure", err)
	}
	manifestPath := filepath.Join(tmpDir, ".codex", "lore-install.json")
	if _, statErr := os.Stat(manifestPath); !os.IsNotExist(statErr) {
		t.Fatalf("manifest stat err=%v, want not written after AGENTS.md failure", statErr)
	}
}

func TestExecuteCodexInstallDoesNotWriteManifestWhenLegacyCleanupFails(t *testing.T) {
	svc := Service{}
	originalAliasCheck := aliasesCodexCanonicalPrompt
	aliasesCodexCanonicalPrompt = func(HarnessLayout, string) bool { return false }
	t.Cleanup(func() { aliasesCodexCanonicalPrompt = originalAliasCheck })
	tmpDir := t.TempDir()
	layout := ResolveCodexLayout(tmpDir)
	legacyPath := filepath.Join(layout.RootDir, "agents.md")
	if err := os.MkdirAll(layout.RootDir, 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}
	legacyContent := []byte("# Lore Configuration\n\nThis file is managed by `lore install --target codex` and should not be edited manually.\n")
	if err := os.WriteFile(legacyPath, legacyContent, 0o600); err != nil {
		t.Fatalf("write legacy prompt: %v", err)
	}
	manifest := Manifest{
		SchemaVersion: PortableManifestSchemaVersion,
		Target:        TargetCodex,
		AuthMode:      "config-only",
		Components:    []ComponentID{ComponentCorePack},
		ManagedFiles:  []ManagedFileRecord{{Path: legacyPath, Component: ComponentCorePack, MergeMode: MergeModeReplace, ContentHash: contentHash(legacyContent)}},
		BackupRoot:    filepath.Join(layout.RootDir, "backups", "20260529T120000Z"),
		InstalledAt:   "2026-05-29T12:00:00Z",
	}
	data, err := marshalManifest(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(layout.ManifestPath, data, 0o600); err != nil {
		t.Fatalf("write old manifest: %v", err)
	}
	oldManifest := append([]byte(nil), data...)

	originalApply := applyCodexPlannedContent
	applyCodexPlannedContent = func(action PlanFileAction, desired []byte) error {
		if action.RelativePath == "agents.md" {
			return errors.New("injected legacy cleanup failure")
		}
		return originalApply(action, desired)
	}
	t.Cleanup(func() { applyCodexPlannedContent = originalApply })

	plan, err := svc.PlanCodexInstall(InstallRequest{HomeDir: tmpDir, Target: TargetCodex, Components: []ComponentID{ComponentCorePack}})
	if err != nil {
		t.Fatalf("PlanCodexInstall error: %v", err)
	}
	_, err = svc.ExecuteCodexInstall(plan, InstallCommandOptions{})
	if err == nil || !strings.Contains(err.Error(), "agents.md") {
		t.Fatalf("ExecuteCodexInstall error = %v, want legacy cleanup failure", err)
	}
	got, readErr := os.ReadFile(layout.ManifestPath)
	if readErr != nil || string(got) != string(oldManifest) {
		t.Fatalf("manifest content=%q err=%v, want previous manifest preserved after cleanup failure", string(got), readErr)
	}
}

func TestExecuteCodexInstallBackupExistingAgentsMD(t *testing.T) {
	svc := Service{}
	tmpDir := t.TempDir()
	codexDir := filepath.Join(tmpDir, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}

	// Create existing AGENTS.md.
	existingContent := "# Old AGENTS.md\nThis is not managed by Lore."
	if err := os.WriteFile(filepath.Join(codexDir, "AGENTS.md"), []byte(existingContent), 0o600); err != nil {
		t.Fatalf("write existing AGENTS.md: %v", err)
	}

	req := InstallRequest{
		HomeDir:        tmpDir,
		ServerURL:      "https://lore.test",
		LoreBinaryPath: "/usr/local/bin/lore",
		Target:         TargetCodex,
		Components:     []ComponentID{ComponentCorePack},
	}

	plan, err := svc.PlanCodexInstall(req)
	if err != nil {
		t.Fatalf("PlanCodexInstall error: %v", err)
	}

	// Verify plan includes backup action.
	var agentsAction *PlanFileAction
	for _, f := range plan.Files {
		if filepath.ToSlash(f.RelativePath) == "AGENTS.md" {
			agentsAction = &f
			break
		}
	}
	if agentsAction == nil {
		t.Fatal("AGENTS.md action not found in plan")
	}
	if agentsAction.Action != "update" {
		t.Fatalf("AGENTS.md action = %q, want update (should backup existing)", agentsAction.Action)
	}
	if agentsAction.BackupPath == "" {
		t.Fatal("AGENTS.md backup path should be set")
	}

	// Execute install.
	result, err := svc.ExecuteCodexInstall(plan, InstallCommandOptions{DryRun: false})
	if err != nil {
		t.Fatalf("ExecuteCodexInstall error: %v", err)
	}
	_ = result

	// Verify backup was created.
	if _, err := os.ReadFile(agentsAction.BackupPath); err != nil {
		t.Fatalf("backup should exist at %s: %v", agentsAction.BackupPath, err)
	}

	// Verify current AGENTS.md is the new managed content.
	currentContent, err := os.ReadFile(filepath.Join(codexDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read current AGENTS.md: %v", err)
	}
	if string(currentContent) == existingContent {
		t.Fatal("AGENTS.md should be replaced with managed content")
	}
}

func TestExecuteCodexInstallIdempotent(t *testing.T) {
	svc := Service{}
	tmpDir := t.TempDir()
	codexDir := filepath.Join(tmpDir, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}

	// First install.
	req := InstallRequest{
		HomeDir:    tmpDir,
		ServerURL:  "https://lore.test",
		Target:     TargetCodex,
		Components: []ComponentID{ComponentCorePack},
	}

	plan1, err := svc.PlanCodexInstall(req)
	if err != nil {
		t.Fatalf("PlanCodexInstall error: %v", err)
	}
	result1, err := svc.ExecuteCodexInstall(plan1, InstallCommandOptions{DryRun: false})
	if err != nil {
		t.Fatalf("ExecuteCodexInstall error: %v", err)
	}

	// Second install (should be idempotent).
	plan2, err := svc.PlanCodexInstall(req)
	if err != nil {
		t.Fatalf("PlanCodexInstall error: %v", err)
	}
	result2, err := svc.ExecuteCodexInstall(plan2, InstallCommandOptions{DryRun: false})
	if err != nil {
		t.Fatalf("ExecuteCodexInstall error: %v", err)
	}

	// All files should be "unchanged" on second run.
	unchanged := 0
	for _, f := range plan2.Files {
		if f.Action == "unchanged" {
			unchanged++
		}
	}
	if unchanged == 0 {
		t.Fatalf("second run should have unchanged files, got actions: %v", planActions(plan2.Files))
	}
	_ = result1
	_ = result2
	_ = codexDir
}

func planActions(files []PlanFileAction) []string {
	actions := make([]string, 0, len(files))
	for _, f := range files {
		actions = append(actions, f.RelativePath+":"+f.Action)
	}
	return actions
}

func TestExecuteCodexInstallWritesConfigToml(t *testing.T) {
	svc := Service{}
	tmpDir := t.TempDir()

	req := InstallRequest{
		HomeDir:        tmpDir,
		ServerURL:      "https://lore.test",
		SavedToken:     "secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		Target:         TargetCodex,
		Components:     []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
	}

	plan, err := svc.PlanCodexInstall(req)
	if err != nil {
		t.Fatalf("PlanCodexInstall error: %v", err)
	}

	_, err = svc.ExecuteCodexInstall(plan, InstallCommandOptions{DryRun: false})
	if err != nil {
		t.Fatalf("ExecuteCodexInstall error: %v", err)
	}

	configTomlPath := filepath.Join(tmpDir, ".codex", "config.toml")
	content, err := os.ReadFile(configTomlPath)
	if err != nil {
		t.Fatalf("ReadFile(config.toml) error: %v", err)
	}
	if !containsAll(string(content), codexMCPBlockStartMarker, `[mcp_servers.lore]`, `url = "https://lore.test/v1/mcp"`, `[mcp_servers.lore.http_headers]`, `Authorization = "Bearer secret-token"`) {
		t.Fatalf("config.toml = %q, want managed Lore MCP block", string(content))
	}
	if strings.Contains(string(content), `[mcp_servers.lore.headers]`) || strings.Contains(string(content), `bearer_token_env_var`) {
		t.Fatalf("config.toml = %q, want http_headers auth only", string(content))
	}
}

func TestCodexLoreMCPApprovals(t *testing.T) {
	content, err := renderCodexMCPConfig("https://lore.test", "secret-token")
	if err != nil {
		t.Fatalf("renderCodexMCPConfig error: %v", err)
	}
	text := string(content)
	for _, tool := range codexLoreMCPApprovedTools {
		want := "[mcp_servers.lore.tools." + tool + "]\napproval_mode = \"approve\""
		if !strings.Contains(text, want) {
			t.Fatalf("config.toml missing approval for %s:\n%s", tool, text)
		}
	}
	for _, forbidden := range []string{
		"default_tools_approval_mode",
		"lore_*",
		"lore_skill_",
		"lore_skill_approve",
		"lore_skill_publish",
		"lore_skill_reject",
		"lore_skill_review_list",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("config.toml contains forbidden approval surface %q:\n%s", forbidden, text)
		}
	}
}

func TestCodexApprovalMerge(t *testing.T) {
	managed := []byte(strings.Join([]string{
		codexMCPBlockStartMarker,
		"[mcp_servers.lore]",
		`url = "https://lore.test/v1/mcp"`,
		"",
		"[mcp_servers.lore.http_headers]",
		`Authorization = "Bearer new-token"`,
		"",
		renderCodexLoreMCPApprovalFragment(),
		codexMCPBlockEndMarker,
		"",
	}, "\n"))
	existing := strings.Join([]string{
		`model = "gpt-5"`,
		"",
		"[mcp_servers.other.tools.safe_tool]",
		`approval_mode = "approve"`,
		"",
		codexMCPBlockStartMarker,
		"[mcp_servers.lore]",
		`url = "https://old.example/v1/mcp"`,
		"",
		"[mcp_servers.lore.tools.lore_skill_publish]",
		`approval_mode = "approve"`,
		"",
		"[mcp_servers.lore.tools.lore_*]",
		`approval_mode = "approve"`,
		codexMCPBlockEndMarker,
		"",
		"[mcp_servers.another.tools.other_tool]",
		`approval_mode = "approve"`,
		"",
	}, "\n")

	merged, _, err := mergeCodexConfigToml([]byte(existing), managed, true)
	if err != nil {
		t.Fatalf("mergeCodexConfigToml error: %v", err)
	}
	text := string(merged)
	if !containsAll(text,
		`model = "gpt-5"`,
		"[mcp_servers.other.tools.safe_tool]",
		"[mcp_servers.another.tools.other_tool]",
		"[mcp_servers.lore.tools.lore_me]",
		"[mcp_servers.lore.tools.lore_project_list]",
		"[mcp_servers.lore.tools.lore_memory_search]",
		`approval_mode = "approve"`,
	) {
		t.Fatalf("merged config.toml missing preserved config or allowlist approvals:\n%s", text)
	}
	for _, forbidden := range []string{
		"old.example",
		"[mcp_servers.lore.tools.lore_skill_publish]",
		"[mcp_servers.lore.tools.lore_*]",
		"default_tools_approval_mode",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("merged config.toml contains forbidden/stale Lore approval %q:\n%s", forbidden, text)
		}
	}
}

func TestMergeCodexConfigTomlEnforcesMultiAgentAndAgentsDefaults(t *testing.T) {
	managed := strings.Join([]string{
		codexMCPBlockStartMarker,
		"[mcp_servers.lore]",
		`url = "https://lore.test/v1/mcp"`,
		codexMCPBlockEndMarker,
		"",
	}, "\n")
	existing := strings.Join([]string{
		`model = "gpt-5"`,
		"",
		"[features]",
		"multi_agent = false",
		"",
		"[agents]",
		"max_threads = 8",
		"",
		"[profiles.default]",
		`approval_policy = "on-request"`,
		"",
	}, "\n")

	merged, summary, err := mergeCodexConfigToml([]byte(existing), []byte(managed), false)
	if err != nil {
		t.Fatalf("mergeCodexConfigToml error: %v", err)
	}
	text := string(merged)
	if !summary.MultiAgentOverridden {
		t.Fatal("merge summary should record multi_agent false-to-true override")
	}
	if !containsAll(text, `model = "gpt-5"`, "[features]", "multi_agent = true", "[agents]", "max_threads = 8", "max_depth = 2", "[profiles.default]", `approval_policy = "on-request"`, codexMCPBlockStartMarker) {
		t.Fatalf("merged config.toml = %q, want preserved config, enforced multi_agent, and agent defaults", text)
	}
	if strings.Contains(text, "multi_agent = false") {
		t.Fatalf("merged config.toml = %q, want multi_agent false overridden", text)
	}
}

func TestMergeCodexConfigTomlFailsClosedOnUnsafeRuntimeTables(t *testing.T) {
	managed := []byte(codexMCPBlockStartMarker + "\n[mcp_servers.lore]\nurl = \"https://lore.test/v1/mcp\"\n" + codexMCPBlockEndMarker + "\n")
	for _, existing := range []string{
		"features = false\n",
		"agents = 4\n",
	} {
		if _, _, err := mergeCodexConfigToml([]byte(existing), managed, false); err == nil {
			t.Fatalf("mergeCodexConfigToml(%q) error = nil, want unsafe TOML shape failure", existing)
		}
	}
}

func TestCodexRegressionFalseToTrueOverride(t *testing.T) {
	managed := []byte(strings.Join([]string{
		codexMCPBlockStartMarker,
		"[mcp_servers.lore]",
		`url = "https://lore.test/v1/mcp"`,
		codexMCPBlockEndMarker,
		"",
	}, "\n"))
	existing := []byte(strings.Join([]string{
		`model = "gpt-5"`,
		"",
		"[features]",
		"multi_agent = false # user disabled before Lore Codex projection",
		"",
	}, "\n"))

	merged, summary, err := mergeCodexConfigToml(existing, managed, false)
	if err != nil {
		t.Fatalf("mergeCodexConfigToml error: %v", err)
	}
	text := string(merged)
	if !summary.MultiAgentOverridden {
		t.Fatal("merge summary should record explicit false-to-true multi_agent override")
	}
	if !containsAll(text, "[features]", "multi_agent = true", "[agents]", "max_threads = 4", "max_depth = 2") {
		t.Fatalf("merged config.toml = %q, want enforced multi_agent and agent defaults", text)
	}
	if strings.Contains(text, "multi_agent = false") {
		t.Fatalf("merged config.toml = %q, want previous false value removed", text)
	}
}

func TestCodexRegressionFailsClosedBeforeMutationOnUnsafeConfig(t *testing.T) {
	svc := Service{}
	tmpDir := t.TempDir()
	configTomlPath := filepath.Join(tmpDir, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configTomlPath), 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}
	existing := strings.Join([]string{
		`model = "gpt-5"`,
		"features = false",
		"",
	}, "\n")
	if err := os.WriteFile(configTomlPath, []byte(existing), 0o600); err != nil {
		t.Fatalf("write existing config.toml: %v", err)
	}

	_, err := svc.PlanCodexInstall(InstallRequest{
		HomeDir:        tmpDir,
		ServerURL:      "https://lore.test",
		SavedToken:     "secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		Target:         TargetCodex,
		Components:     []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
	})
	if err == nil || !strings.Contains(err.Error(), `"features" is not a TOML table`) {
		t.Fatalf("PlanCodexInstall error = %v, want unsafe features table conflict", err)
	}
	got, readErr := os.ReadFile(configTomlPath)
	if readErr != nil || string(got) != existing {
		t.Fatalf("config.toml content=%q err=%v, want original preserved", string(got), readErr)
	}
	if _, statErr := os.Stat(filepath.Join(tmpDir, ".codex", "lore-install.json")); !os.IsNotExist(statErr) {
		t.Fatalf("manifest stat err = %v, want no manifest written during failed plan", statErr)
	}
}

func TestCodexRegressionNoHooksPathPlanned(t *testing.T) {
	svc := Service{}
	tmpDir := t.TempDir()
	plan, err := svc.PlanCodexInstall(InstallRequest{
		HomeDir:        tmpDir,
		ServerURL:      "https://lore.test",
		SavedToken:     "secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		Target:         TargetCodex,
		Components:     []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
	})
	if err != nil {
		t.Fatalf("PlanCodexInstall error: %v", err)
	}
	for _, action := range plan.Files {
		if strings.Contains(filepath.ToSlash(action.RelativePath), "hooks") || strings.Contains(filepath.ToSlash(action.AbsolutePath), "hooks") {
			t.Fatalf("Codex plan must not include hooks in this change: %+v", action)
		}
	}
}

func TestCodexRegressionAllowlistOnlyApprovals(t *testing.T) {
	content, err := renderCodexMCPConfig("https://lore.test", "secret-token")
	if err != nil {
		t.Fatalf("renderCodexMCPConfig error: %v", err)
	}
	text := string(content)
	allowed := map[string]bool{}
	for _, tool := range codexLoreMCPApprovedTools {
		allowed[tool] = true
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "[mcp_servers.lore.tools.") || !strings.HasSuffix(line, "]") {
			continue
		}
		tool := strings.TrimSuffix(strings.TrimPrefix(line, "[mcp_servers.lore.tools."), "]")
		if !allowed[tool] {
			t.Fatalf("approval table %q is outside the approved Lore MCP allowlist:\n%s", tool, text)
		}
		seen[tool] = true
	}
	for _, tool := range codexLoreMCPApprovedTools {
		if !seen[tool] {
			t.Fatalf("approval table for %q missing from config:\n%s", tool, text)
		}
	}
	for _, forbidden := range []string{"default_tools_approval_mode", "lore_*", "approval_policy", "[mcp_servers.shell", "[mcp_servers.filesystem"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("config.toml contains forbidden broad approval surface %q:\n%s", forbidden, text)
		}
	}
}

func TestCodexMultiAgentOverrideCreatesBackupEvidence(t *testing.T) {
	svc := Service{}
	tmpDir := t.TempDir()
	codexDir := filepath.Join(tmpDir, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}
	configTomlPath := filepath.Join(codexDir, "config.toml")
	existing := strings.Join([]string{
		`model = "gpt-5"`,
		"",
		"[features]",
		"multi_agent = false",
		"",
	}, "\n")
	if err := os.WriteFile(configTomlPath, []byte(existing), 0o600); err != nil {
		t.Fatalf("write existing config.toml: %v", err)
	}

	plan, err := svc.PlanCodexInstall(InstallRequest{
		HomeDir:        tmpDir,
		ServerURL:      "https://lore.test",
		SavedToken:     "secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		Target:         TargetCodex,
		Components:     []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
		Now:            time.Date(2026, 7, 2, 7, 40, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("PlanCodexInstall error: %v", err)
	}
	var configAction PlanFileAction
	for _, action := range plan.Files {
		if action.RelativePath == "config.toml" {
			configAction = action
		}
	}
	if configAction.Action != "update" || configAction.BackupPath == "" {
		t.Fatalf("config.toml action = %+v, want update with backup evidence", configAction)
	}

	result, err := svc.ExecuteCodexInstall(plan, InstallCommandOptions{})
	if err != nil {
		t.Fatalf("ExecuteCodexInstall error: %v", err)
	}
	merged, err := os.ReadFile(configTomlPath)
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	if !containsAll(string(merged), "[features]", "multi_agent = true", "[agents]", "max_threads = 4", "max_depth = 2") {
		t.Fatalf("config.toml = %q, want enforced multi-agent and agents defaults", string(merged))
	}
	if got, err := os.ReadFile(configAction.BackupPath); err != nil || string(got) != existing {
		t.Fatalf("backup content=%q err=%v, want original config", string(got), err)
	}
	if !containsSummaryEntry(result.Summary.BackedUp, "config.toml") {
		t.Fatalf("BackedUp = %v, want config.toml backup summary evidence", result.Summary.BackedUp)
	}
}

func TestExecuteCodexInstallMergesConfigToml(t *testing.T) {
	svc := Service{}
	tmpDir := t.TempDir()
	codexDir := filepath.Join(tmpDir, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}
	configTomlPath := filepath.Join(codexDir, "config.toml")
	existing := strings.Join([]string{
		"model = \"gpt-5\"",
		"",
		"[mcp_servers.existing]",
		"command = \"keep-me\"",
		"",
		"[mcp_servers.lore]",
		"url = \"https://old.example/v1/mcp\"",
		"bearer_token_env_var = \"old-token\"",
		"",
		"[mcp_servers.lore.headers]",
		"Authorization = \"Bearer old-token\"",
		"",
		"[mcp_servers.lore.http_headers]",
		"Authorization = \"Bearer old-token\"",
		"",
	}, "\n")
	if err := os.WriteFile(configTomlPath, []byte(existing), 0o600); err != nil {
		t.Fatalf("write existing config.toml: %v", err)
	}
	layout := ResolveCodexLayout(tmpDir)
	manifest := Manifest{
		SchemaVersion: PortableManifestSchemaVersion,
		Target:        TargetCodex,
		AuthMode:      "config-only",
		Components:    []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
		ManagedFiles:  []ManagedFileRecord{{Path: configTomlPath, Component: ComponentLoreServerMCP, MergeMode: MergeModeReplace, ContentHash: "old"}},
		BackupRoot:    filepath.Join(layout.RootDir, "backups", "20260529T120000Z"),
		InstalledAt:   "2026-05-29T12:00:00Z",
	}
	manifestData, err := marshalManifest(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(layout.ManifestPath, manifestData, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	req := InstallRequest{
		HomeDir:        tmpDir,
		ServerURL:      "https://lore.test",
		SavedToken:     "secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		Target:         TargetCodex,
		Components:     []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
	}
	plan, err := svc.PlanCodexInstall(req)
	if err != nil {
		t.Fatalf("PlanCodexInstall error: %v", err)
	}
	_, err = svc.ExecuteCodexInstall(plan, InstallCommandOptions{DryRun: false})
	if err != nil {
		t.Fatalf("ExecuteCodexInstall error: %v", err)
	}

	merged, err := os.ReadFile(configTomlPath)
	if err != nil {
		t.Fatalf("ReadFile(config.toml) error: %v", err)
	}
	text := string(merged)
	if !containsAll(text, `model = "gpt-5"`, `[mcp_servers.existing]`, `command = "keep-me"`, `[mcp_servers.lore.http_headers]`, `url = "https://lore.test/v1/mcp"`, `Authorization = "Bearer secret-token"`) {
		t.Fatalf("merged config.toml = %q, want existing content preserved plus managed Lore MCP block", text)
	}
	if strings.Contains(text, "old-token") || strings.Contains(text, "https://old.example") || strings.Contains(text, `[mcp_servers.lore.headers]`) || strings.Contains(text, `bearer_token_env_var`) {
		t.Fatalf("merged config.toml = %q, want stale Lore MCP entry replaced", text)
	}
}

// TestCodexInstallUsesCustomAgentConfigModels verifies that persisted
// agent-config.json custom model values drive Codex AGENTS.md projection.
func TestExecuteCodexInstallMigratesObsoleteCodexLoreEnvTable(t *testing.T) {
	svc := Service{}
	tmpDir := t.TempDir()
	codexDir := filepath.Join(tmpDir, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}
	configTomlPath := filepath.Join(codexDir, "config.toml")
	existing := strings.Join([]string{
		"model = \"gpt-5\"",
		"",
		"[mcp_servers.other.env]",
		"KEEP_ME = \"yes\"",
		"",
		"[mcp_servers.lore.env]",
		"LORE_MCP_URL = \"https://old.example/v1/mcp\"",
		"LORE_MCP_AUTHORIZATION = \"Bearer old-test-token\"",
		"",
		"[profiles.default]",
		"approval_policy = \"on-request\"",
		"",
	}, "\n")
	if err := os.WriteFile(configTomlPath, []byte(existing), 0o600); err != nil {
		t.Fatalf("write existing config.toml: %v", err)
	}

	plan, err := svc.PlanCodexInstall(InstallRequest{
		HomeDir:        tmpDir,
		ServerURL:      "https://lore.test",
		SavedToken:     "new-test-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		Target:         TargetCodex,
		Components:     []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
	})
	if err != nil {
		t.Fatalf("PlanCodexInstall error: %v", err)
	}
	if _, err := svc.ExecuteCodexInstall(plan, InstallCommandOptions{DryRun: false}); err != nil {
		t.Fatalf("ExecuteCodexInstall error: %v", err)
	}

	merged, err := os.ReadFile(configTomlPath)
	if err != nil {
		t.Fatalf("ReadFile(config.toml) error: %v", err)
	}
	text := string(merged)
	if !containsAll(text, `model = "gpt-5"`, `[mcp_servers.other.env]`, `KEEP_ME = "yes"`, `[profiles.default]`, `approval_policy = "on-request"`, codexMCPBlockStartMarker, `[mcp_servers.lore]`, `url = "https://lore.test/v1/mcp"`, `[mcp_servers.lore.http_headers]`, `Authorization = "Bearer new-test-token"`) {
		t.Fatal("merged config.toml should preserve unrelated config and add the managed Lore MCP block")
	}
	if strings.Contains(text, `[mcp_servers.lore.env]`) || strings.Contains(text, `LORE_MCP_URL`) || strings.Contains(text, `LORE_MCP_AUTHORIZATION`) || strings.Contains(text, `old.example`) || strings.Contains(text, `old-test-token`) {
		t.Fatal("merged config.toml should remove the obsolete Lore MCP env table")
	}
}

func TestExecuteCodexInstallReplacesManagedBlockAndRemovesObsoleteEnvTable(t *testing.T) {
	svc := Service{}
	tmpDir := t.TempDir()
	codexDir := filepath.Join(tmpDir, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}
	configTomlPath := filepath.Join(codexDir, "config.toml")
	existing := strings.Join([]string{
		"model = \"gpt-5\"",
		"",
		"[mcp_servers.lore.env]",
		"LORE_MCP_URL = \"https://old.example/v1/mcp\"",
		"LORE_MCP_AUTHORIZATION = \"Bearer old-test-token\"",
		"",
		codexMCPBlockStartMarker,
		"[mcp_servers.lore]",
		"url = \"https://old.example/v1/mcp\"",
		"",
		"[mcp_servers.lore.http_headers]",
		"Authorization = \"Bearer old-test-token\"",
		codexMCPBlockEndMarker,
		"",
		"[mcp_servers.other.env]",
		"KEEP_ME = \"yes\"",
		"",
	}, "\n")
	if err := os.WriteFile(configTomlPath, []byte(existing), 0o600); err != nil {
		t.Fatalf("write existing config.toml: %v", err)
	}

	plan, err := svc.PlanCodexInstall(InstallRequest{
		HomeDir:        tmpDir,
		ServerURL:      "https://lore.test",
		SavedToken:     "new-test-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		Target:         TargetCodex,
		Components:     []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
	})
	if err != nil {
		t.Fatalf("PlanCodexInstall error: %v", err)
	}
	if _, err := svc.ExecuteCodexInstall(plan, InstallCommandOptions{DryRun: false}); err != nil {
		t.Fatalf("ExecuteCodexInstall error: %v", err)
	}

	merged, err := os.ReadFile(configTomlPath)
	if err != nil {
		t.Fatalf("ReadFile(config.toml) error: %v", err)
	}
	text := string(merged)
	if !containsAll(text, `model = "gpt-5"`, `[mcp_servers.other.env]`, `KEEP_ME = "yes"`, codexMCPBlockStartMarker, `[mcp_servers.lore]`, `url = "https://lore.test/v1/mcp"`, `[mcp_servers.lore.http_headers]`, `Authorization = "Bearer new-test-token"`) {
		t.Fatal("merged config.toml should replace the managed block, preserve unrelated env, and use the new remote MCP config")
	}
	if strings.Contains(text, `[mcp_servers.lore.env]`) || strings.Contains(text, `LORE_MCP_URL`) || strings.Contains(text, `LORE_MCP_AUTHORIZATION`) || strings.Contains(text, `old.example`) || strings.Contains(text, `old-test-token`) {
		t.Fatal("merged config.toml should remove obsolete Lore MCP env state and stale managed values")
	}
}

func TestPlanCodexInstallFailsClosedOnUnmarkedUserLoreMCPBlock(t *testing.T) {
	svc := Service{}
	for _, tt := range []struct {
		name   string
		header string
	}{
		{name: "bare", header: "[mcp_servers.lore]"},
		{name: "inline comment", header: "[mcp_servers.lore] # user-owned Lore MCP"},
		{name: "spaced dotted key", header: "[ mcp_servers . lore ]"},
		{name: "quoted dotted segments", header: "[\"mcp_servers\".\"lore\"] # user-owned Lore MCP"},
		{name: "quoted http headers subtable", header: "['mcp_servers' . 'lore' . 'http_headers'] # user-owned Lore MCP"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configTomlPath := filepath.Join(tmpDir, ".codex", "config.toml")
			if err := os.MkdirAll(filepath.Dir(configTomlPath), 0o755); err != nil {
				t.Fatalf("mkdir codex dir: %v", err)
			}
			existing := strings.Join([]string{
				"model = \"gpt-5\"",
				"",
				tt.header,
				"command = \"user-owned\"",
				"args = [\"mcp\"]",
				"",
			}, "\n")
			if err := os.WriteFile(configTomlPath, []byte(existing), 0o600); err != nil {
				t.Fatalf("write existing config.toml: %v", err)
			}
			_, err := svc.PlanCodexInstall(InstallRequest{
				HomeDir:        tmpDir,
				ServerURL:      "https://lore.test",
				SavedToken:     "secret-token",
				LoreBinaryPath: "/usr/local/bin/lore",
				Target:         TargetCodex,
				Components:     []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
			})
			if err == nil || !strings.Contains(err.Error(), "refusing to overwrite unowned [mcp_servers.lore]") {
				t.Fatalf("PlanCodexInstall error = %v, want unowned Lore MCP block conflict", err)
			}
			got, readErr := os.ReadFile(configTomlPath)
			if readErr != nil || string(got) != existing {
				t.Fatalf("config.toml content=%q err=%v, want preserved", string(got), readErr)
			}
		})
	}
}

func TestCodexConfigLoreMCPDetectionRecognizesTOMLTableHeaderForms(t *testing.T) {
	for _, tt := range []struct {
		line string
		want bool
	}{
		{line: "[mcp_servers.lore]", want: true},
		{line: "[mcp_servers.lore] # inline comment", want: true},
		{line: "[ mcp_servers . lore . headers ]", want: true},
		{line: "[\"mcp_servers\" . \"lore\" . \"http_headers\"] # inline comment", want: true},
		{line: "['mcp_servers'.'lore']", want: true},
		{line: "[mcp_servers.other]", want: false},
		{line: "[\"mcp_servers.lore\"]", want: false},
		{line: "[mcp_servers.lore] trailing", want: false},
	} {
		t.Run(tt.line, func(t *testing.T) {
			if got := isCodexLoreTableHeader(tt.line); got != tt.want {
				t.Fatalf("isCodexLoreTableHeader(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestStripLegacyCodexLoreMCPBlockRecognizesTOMLTableHeaderForms(t *testing.T) {
	existing := strings.Join([]string{
		"model = \"gpt-5\"",
		"",
		"[ \"mcp_servers\" . \"lore\" ] # old managed block",
		"url = \"https://old.example/v1/mcp\"",
		"",
		"['mcp_servers'.'lore'.'http_headers'] # old managed headers",
		"Authorization = \"Bearer old-token\"",
		"",
		"[mcp_servers.existing] # keep this table",
		"command = \"keep-me\"",
		"",
	}, "\n")
	stripped := stripLegacyCodexLoreMCPBlock(existing)
	if !containsAll(stripped, "model = \"gpt-5\"", "[mcp_servers.existing] # keep this table", "command = \"keep-me\"") {
		t.Fatalf("stripped config = %q, want unrelated config preserved", stripped)
	}
	if strings.Contains(stripped, "old-token") || strings.Contains(stripped, "old.example") || strings.Contains(stripped, "http_headers") {
		t.Fatalf("stripped config = %q, want old Lore MCP tables removed", stripped)
	}
}

func TestCodexInstallUsesCustomAgentConfigModels(t *testing.T) {
	tmpDir := t.TempDir()

	// Pre-create agent-config.json with a custom model for sdd-verify.
	codexDir := filepath.Join(tmpDir, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}
	agentConfigPath := filepath.Join(tmpDir, ".lore")
	if err := os.MkdirAll(agentConfigPath, 0o700); err != nil {
		t.Fatalf("mkdir lore dir: %v", err)
	}
	// Include all 9 canonical SDD agents to pass validation.
	customCfg := agentconfig.Config{
		SchemaVersion: 1,
		SDDAgents: map[string]agentconfig.Agent{
			"sdd-init":    {Model: "gpt-5.4"},
			"sdd-explore": {Model: "gpt-5.4"},
			"sdd-propose": {Model: "gpt-5.4"},
			"sdd-spec":    {Model: "gpt-5.4"},
			"sdd-design":  {Model: "gpt-5.4"},
			"sdd-tasks":   {Model: "gpt-5.4"},
			"sdd-apply":   {Model: "gpt-5.4"},
			"sdd-verify":  {Model: "gpt-4o"}, // Custom model for sdd-verify
			"sdd-archive": {Model: "gpt-5.4"},
		},
	}
	store := agentconfig.NewStore(agentConfigPath)
	if err := store.Save(customCfg); err != nil {
		t.Fatalf("save custom agent-config: %v", err)
	}

	svc := Service{AgentConfigStore: store}
	req := InstallRequest{
		HomeDir:    tmpDir,
		ServerURL:  "https://lore.test",
		Target:     TargetCodex,
		Components: []ComponentID{ComponentCorePack},
	}

	plan, err := svc.PlanCodexInstall(req)
	if err != nil {
		t.Fatalf("PlanCodexInstall error: %v", err)
	}

	_, err = svc.ExecuteCodexInstall(plan, InstallCommandOptions{DryRun: false})
	if err != nil {
		t.Fatalf("ExecuteCodexInstall error: %v", err)
	}

	// Read the generated AGENTS.md.
	agentsPath := filepath.Join(codexDir, "AGENTS.md")
	data, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	content := string(data)

	// Custom model gpt-4o should appear in the generated AGENTS.md for sdd-verify.
	if !strings.Contains(content, "sdd-verify: gpt-4o") {
		t.Errorf("AGENTS.md should contain custom model sdd-verify: gpt-4o, got:\n%s", content)
	}
	// Verify the default model (gpt-5.4) also appears for sdd-init.
	if !strings.Contains(content, "sdd-init: gpt-5.4") {
		t.Errorf("AGENTS.md should contain sdd-init: gpt-5.4, got:\n%s", content)
	}
	// Make sure we're not falling back to the wrong default.
	if strings.Contains(content, "sdd-verify: gpt-5.4") {
		t.Errorf("AGENTS.md should NOT contain default fallback sdd-verify: gpt-5.4 when custom model is set")
	}
}

func TestExecuteCodexInstallManifestValid(t *testing.T) {
	svc := Service{}
	tmpDir := t.TempDir()

	req := InstallRequest{
		HomeDir:        tmpDir,
		ServerURL:      "https://lore.test",
		LoreBinaryPath: "/usr/local/bin/lore",
		Target:         TargetCodex,
		Components:     []ComponentID{ComponentCorePack},
	}

	plan, err := svc.PlanCodexInstall(req)
	if err != nil {
		t.Fatalf("PlanCodexInstall error: %v", err)
	}

	result, err := svc.ExecuteCodexInstall(plan, InstallCommandOptions{DryRun: false})
	if err != nil {
		t.Fatalf("ExecuteCodexInstall error: %v", err)
	}

	// Verify manifest is valid and loaded.
	if result.Manifest.SchemaVersion == "" {
		t.Fatal("manifest should have schema version")
	}
	if len(result.Manifest.ManagedFiles) == 0 {
		t.Fatal("manifest should track managed files")
	}
	for _, mf := range result.Manifest.ManagedFiles {
		if mf.Path == "" {
			t.Fatal("managed file path should not be empty")
		}
	}
}

func TestCodexAdapterDoesNotRenderLegacyLowercasePrompt(t *testing.T) {
	files, err := defaultCodexAdapter().Render(context.Background(), RenderRequest{
		Target:     TargetCodex,
		Assets:     agentpack.DefaultOperationalAssets(),
		Components: []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
		ServerURL:  "https://example.test/",
		SavedToken: "secret-token",
	})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	for _, file := range files {
		if filepath.ToSlash(file.RelativePath) == "agents.md" {
			t.Fatalf("rendered legacy lowercase agents.md: paths=%v", sortedRenderedPaths(files))
		}
	}
	if _, ok := renderedFileByPathOK(files, "AGENTS.md"); !ok {
		t.Fatalf("rendered paths=%v, want AGENTS.md", sortedRenderedPaths(files))
	}
	shared, ok := renderedFileByPathOK(files, filepath.ToSlash(filepath.Join("skills", "_shared", "sdd-phase-common.md")))
	if !ok || !strings.Contains(string(shared.Content), "SDD Phase Common Protocol") {
		t.Fatalf("rendered shared skill = %q ok=%v, want installed shared SDD phase protocol", string(shared.Content), ok)
	}
}

func TestCodexPromptEntryNamePrefersExactLowercaseBeforeCaseFoldAlias(t *testing.T) {
	got, ok := preferCodexPromptEntryName([]string{"AGENTS.md", "agents.md"}, "agents.md")
	if !ok || got != "agents.md" {
		t.Fatalf("preferCodexPromptEntryName() = %q, %v; want exact lowercase agents.md", got, ok)
	}
}

func TestExecuteCodexInstallCleansManifestOwnedLegacyLowercasePrompt(t *testing.T) {
	svc := Service{}
	originalAliasCheck := aliasesCodexCanonicalPrompt
	aliasesCodexCanonicalPrompt = func(HarnessLayout, string) bool { return false }
	t.Cleanup(func() { aliasesCodexCanonicalPrompt = originalAliasCheck })
	tmpDir := t.TempDir()
	layout := ResolveCodexLayout(tmpDir)
	legacyPath := filepath.Join(layout.RootDir, "agents.md")
	if err := os.MkdirAll(layout.RootDir, 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}
	legacyContent := []byte("# Lore Configuration\n\nThis file is managed by `lore install --target codex` and should not be edited manually.\n")
	if err := os.WriteFile(legacyPath, legacyContent, 0o600); err != nil {
		t.Fatalf("write legacy prompt: %v", err)
	}
	manifest := Manifest{
		SchemaVersion: PortableManifestSchemaVersion,
		Target:        TargetCodex,
		AuthMode:      "config-only",
		Components:    []ComponentID{ComponentCorePack},
		ManagedFiles:  []ManagedFileRecord{{Path: legacyPath, Component: ComponentCorePack, MergeMode: MergeModeReplace, ContentHash: contentHash(legacyContent)}},
		BackupRoot:    filepath.Join(layout.RootDir, "backups", "20260529T120000Z"),
		InstalledAt:   "2026-05-29T12:00:00Z",
	}
	data, err := marshalManifest(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(layout.ManifestPath, data, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	plan, err := svc.PlanCodexInstall(InstallRequest{HomeDir: tmpDir, Target: TargetCodex, Components: []ComponentID{ComponentCorePack}, Now: time.Date(2026, 5, 29, 12, 1, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("PlanCodexInstall error: %v", err)
	}
	assertPlanFileAction(t, plan.Files, "agents.md", "delete")
	var deleteAction PlanFileAction
	for _, action := range plan.Files {
		if action.RelativePath == "agents.md" {
			deleteAction = action
		}
	}
	if deleteAction.BackupPath == "" {
		t.Fatal("legacy cleanup should have a backup path")
	}
	if err := applyCodexPlannedContent(deleteAction, nil); err != nil {
		t.Fatalf("applyCodexPlannedContent(delete legacy) error: %v", err)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy lowercase prompt stat err=%v, want removed", err)
	}
	if got, err := os.ReadFile(deleteAction.BackupPath); err != nil || string(got) != string(legacyContent) {
		t.Fatalf("legacy backup content=%q err=%v, want original", string(got), err)
	}
}

func TestCodexLegacyLowercasePromptFailsClosedOnInjectedCaseInsensitiveAlias(t *testing.T) {
	svc := Service{}
	originalAliasCheck := aliasesCodexCanonicalPrompt
	aliasesCodexCanonicalPrompt = func(HarnessLayout, string) bool { return true }
	t.Cleanup(func() { aliasesCodexCanonicalPrompt = originalAliasCheck })
	for _, tt := range []struct {
		name    string
		content string
	}{
		{name: "unowned", content: "# Personal Codex notes\nkeep me\n"},
		{name: "user-modified ambiguous", content: "# Lore Configuration\n\nUser customization without Lore managed install marker.\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			legacyPath := filepath.Join(tmpDir, ".codex", "agents.md")
			if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
				t.Fatalf("mkdir codex dir: %v", err)
			}
			if err := os.WriteFile(legacyPath, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("write legacy prompt: %v", err)
			}
			_, err := svc.PlanCodexInstall(InstallRequest{HomeDir: tmpDir, Target: TargetCodex, Components: []ComponentID{ComponentCorePack}})
			if err == nil || !strings.Contains(err.Error(), "unowned legacy ~/.codex/agents.md") {
				t.Fatalf("PlanCodexInstall error = %v, want fail-closed preservation error", err)
			}
			got, readErr := os.ReadFile(legacyPath)
			if readErr != nil || string(got) != tt.content {
				t.Fatalf("legacy content=%q err=%v, want preserved %q", string(got), readErr, tt.content)
			}
		})
	}
}

func TestCodexLegacyLowercasePromptSkipsManagedCleanupOnInjectedAliasRisk(t *testing.T) {
	svc := Service{}
	originalAliasCheck := aliasesCodexCanonicalPrompt
	aliasesCodexCanonicalPrompt = func(HarnessLayout, string) bool { return true }
	t.Cleanup(func() { aliasesCodexCanonicalPrompt = originalAliasCheck })
	tmpDir := t.TempDir()
	legacyPath := filepath.Join(tmpDir, ".codex", "agents.md")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}
	legacyContent := []byte("# Lore Configuration\n\nThis file is managed by `lore install --target codex` and should not be edited manually.\n")
	if err := os.WriteFile(legacyPath, legacyContent, 0o600); err != nil {
		t.Fatalf("write legacy prompt: %v", err)
	}
	plan, err := svc.PlanCodexInstall(InstallRequest{HomeDir: tmpDir, Target: TargetCodex, Components: []ComponentID{ComponentCorePack}})
	if err != nil {
		t.Fatalf("PlanCodexInstall error = %v, want alias-safe skip", err)
	}
	for _, action := range plan.Files {
		if action.RelativePath == "agents.md" {
			t.Fatalf("planned alias-risk delete for legacy prompt: %+v", action)
		}
	}
}

func TestCodexLegacyLowercasePromptPreservesUnmanagedCaseSensitiveContent(t *testing.T) {
	svc := Service{}
	originalAliasCheck := aliasesCodexCanonicalPrompt
	aliasesCodexCanonicalPrompt = func(HarnessLayout, string) bool { return false }
	t.Cleanup(func() { aliasesCodexCanonicalPrompt = originalAliasCheck })
	tmpDir := t.TempDir()
	legacyPath := filepath.Join(tmpDir, ".codex", "agents.md")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}
	content := "# Personal Codex notes\nkeep me\n"
	if err := os.WriteFile(legacyPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write legacy prompt: %v", err)
	}
	plan, err := svc.PlanCodexInstall(InstallRequest{HomeDir: tmpDir, Target: TargetCodex, Components: []ComponentID{ComponentCorePack}})
	if err != nil {
		t.Fatalf("PlanCodexInstall error = %v, want preserved non-alias legacy file", err)
	}
	for _, action := range plan.Files {
		if action.RelativePath == "agents.md" {
			t.Fatalf("planned cleanup for unowned non-alias legacy prompt: %+v", action)
		}
	}
	got, readErr := os.ReadFile(legacyPath)
	if readErr != nil || string(got) != content {
		t.Fatalf("legacy content=%q err=%v, want preserved %q", string(got), readErr, content)
	}
}

func renderedFileByPathOK(files []RenderedFile, path string) (RenderedFile, bool) {
	for _, file := range files {
		if filepath.ToSlash(file.RelativePath) == filepath.ToSlash(path) {
			return file, true
		}
	}
	return RenderedFile{}, false
}

func TestCodexGoldenMCPConfigAndPaths(t *testing.T) {
	mcp, err := renderCodexMCPConfig("https://example.test/", "secret-token")
	if err != nil {
		t.Fatalf("renderCodexMCPConfig error: %v", err)
	}
	wantMCP, err := os.ReadFile(filepath.Join("testdata", "codex", "config.toml.golden"))
	if err != nil {
		t.Fatalf("read codex MCP golden: %v", err)
	}
	if string(mcp) != string(wantMCP) {
		t.Fatalf("Codex MCP golden drift\ngot:\n%s\nwant:\n%s", string(mcp), string(wantMCP))
	}

	agents, err := renderCodexAgentsMD(RenderRequest{Target: TargetCodex, Assets: agentpack.DefaultOperationalAssets()})
	if err != nil {
		t.Fatalf("renderCodexAgentsMD error: %v", err)
	}
	wantAgents, err := os.ReadFile(filepath.Join("testdata", "codex", "agents.sdd.golden"))
	if err != nil {
		t.Fatalf("read codex AGENTS golden: %v", err)
	}
	if !strings.Contains(string(agents), string(wantAgents)) {
		t.Fatalf("Codex AGENTS SDD golden drift\ngot AGENTS.md without expected section:\n%s\nwant section:\n%s", string(agents), string(wantAgents))
	}

	for _, profile := range renderCodexSDDProfiles(agentconfig.Config{}) {
		goldenPath := filepath.Join("testdata", "codex", filepath.ToSlash(profile.RelativePath)+".golden")
		wantProfile, err := os.ReadFile(goldenPath)
		if err != nil {
			t.Fatalf("read codex profile golden %q: %v", goldenPath, err)
		}
		if string(profile.Content) != string(wantProfile) {
			t.Fatalf("Codex profile golden drift for %q\ngot:\n%s\nwant:\n%s", profile.RelativePath, string(profile.Content), string(wantProfile))
		}
	}

	wantPaths, err := os.ReadFile(filepath.Join("testdata", "codex", "paths.golden"))
	if err != nil {
		t.Fatalf("read codex paths golden: %v", err)
	}
	allowed := map[string]bool{
		"AGENTS.md":                          true,
		"config.toml":                        true,
		codexStrongProfileRelativePath:       true,
		codexMidProfileRelativePath:          true,
		codexCheapProfileRelativePath:        true,
		"skills/sdd-apply/SKILL.md":          true,
		"skills/_shared/sdd-phase-common.md": true,
		"lore-install.json":                  true,
	}
	seen := map[string]bool{}
	for _, want := range strings.Split(strings.TrimSpace(string(wantPaths)), "\n") {
		if !allowed[want] {
			t.Fatalf("unexpected codex paths golden entry %q", want)
		}
		seen[want] = true
	}
	for required := range allowed {
		if !seen[required] {
			t.Fatalf("codex paths golden missing required entry %q: %s", required, string(wantPaths))
		}
	}
}
