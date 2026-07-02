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
	wantPaths, err := os.ReadFile(filepath.Join("testdata", "codex", "paths.golden"))
	if err != nil {
		t.Fatalf("read codex paths golden: %v", err)
	}
	for _, want := range strings.Split(strings.TrimSpace(string(wantPaths)), "\n") {
		if want == "lore-install.json" {
			continue
		}
		if want == "skills/sdd-apply/SKILL.md" || want == "skills/_shared/sdd-phase-common.md" {
			continue
		}
		if want != "AGENTS.md" && want != "config.toml" {
			t.Fatalf("unexpected codex paths golden entry %q", want)
		}
	}
}
