package install

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alferio94/lore-cli/internal/agentpack"
)

func TestAntigravityAdapterRenderProducesPromptSkillsAndOptionalMCPWithoutPiArtifacts(t *testing.T) {
	adapter := defaultAntigravityAdapter()
	assets := agentpack.DefaultOperationalAssets()

	files, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetAntigravity,
		Assets:     assets,
		Components: []ComponentID{ComponentCorePack},
	})
	if err != nil {
		t.Fatalf("Render(core-pack) error = %v, want nil", err)
	}
	if len(files) == 0 {
		t.Fatal("Render(core-pack) returned no files, want prompt + skills")
	}

	byPath := map[string]RenderedFile{}
	for _, file := range files {
		byPath[file.RelativePath] = file
		if strings.HasPrefix(file.RelativePath, "agents/") || strings.HasPrefix(file.RelativePath, "extensions/") || file.RelativePath == "settings.json" {
			t.Fatalf("Render(core-pack) produced Pi-only artifact %q", file.RelativePath)
		}
	}
	prompt, ok := byPath["../GEMINI.md"]
	if !ok {
		t.Fatalf("Render(core-pack) paths = %v, want ../GEMINI.md", sortedRenderedPaths(files))
	}
	if !containsAll(string(prompt.Content), "<!-- lore-cli:antigravity:start -->", "append", "~/.gemini/antigravity-cli/skills", "prefer `lore_project_activity` first", "targeted `lore_memory_search`", "`lore_memory_get` for full memory content") {
		t.Fatalf("prompt content = %q, want managed Antigravity markers, skills guidance, and Lore MCP context guidance", string(prompt.Content))
	}
	if strings.Contains(string(prompt.Content), "~/.pi/agent") || strings.Contains(string(prompt.Content), "agents/lore-managed") {
		t.Fatalf("prompt content = %q, want Antigravity-owned prompt semantics without Pi path leakage", string(prompt.Content))
	}

	applySkill, ok := byPath[filepath.ToSlash(filepath.Join("skills", "sdd-apply", "SKILL.md"))]
	if !ok {
		t.Fatalf("Render(core-pack) paths = %v, want skills/sdd-apply/SKILL.md", sortedRenderedPaths(files))
	}
	if !containsAll(string(applySkill.Content), "~/.gemini/antigravity-cli/skills/sdd-apply/SKILL.md", "~/.gemini/antigravity-cli/skills/_shared/sdd-phase-common.md", "You execute the SDD apply phase.") {
		t.Fatalf("sdd-apply skill = %q, want Antigravity skill paths and canonical apply semantics", string(applySkill.Content))
	}
	for _, forbidden := range []string{"~/.pi/agent/skills/", "agents/lore-managed", "managedBy:", "managedLayer:", "managedPackId:", "phase:", "skillPolicyMode:"} {
		if strings.Contains(string(applySkill.Content), forbidden) {
			t.Fatalf("sdd-apply skill = %q, want %q omitted from Antigravity skill output", string(applySkill.Content), forbidden)
		}
	}
	if _, ok := byPath[filepath.ToSlash(filepath.Join("skills", "lore-worker", "SKILL.md"))]; !ok {
		t.Fatalf("Render(core-pack) paths = %v, want skills/lore-worker/SKILL.md", sortedRenderedPaths(files))
	}
	agentProfileRelativePath := filepath.ToSlash(filepath.Join("..", "config", "agents", "lore.json"))
	agentProfile, ok := byPath[agentProfileRelativePath]
	if !ok {
		t.Fatalf("Render(core-pack) paths = %v, want %s", sortedRenderedPaths(files), agentProfileRelativePath)
	}
	if agentProfile.MergeMode != MergeModeReplace {
		t.Fatalf("agent profile merge mode = %q, want replace", agentProfile.MergeMode)
	}
	var payload antigravityAgentProfile
	if err := json.Unmarshal(agentProfile.Content, &payload); err != nil {
		t.Fatalf("json.Unmarshal(agent profile) error = %v, want nil", err)
	}
	instruction := renderAntigravityAgentSystemInstruction(assets.Definition())
	if payload.ID != "lore" || payload.Name != "Lore" || !payload.Default {
		t.Fatalf("agent profile payload = %+v, want lore/Lore/default", payload)
	}
	if payload.Description != "Global Lore orchestrator specialized in SDD workflows and persistent context through Lore MCP" {
		t.Fatalf("agent profile description = %q, want updated English description", payload.Description)
	}
	if payload.SystemInstruction != instruction {
		t.Fatalf("agent profile systemInstruction = %q, want composed agentpack instruction %q", payload.SystemInstruction, instruction)
	}
	if !containsAll(payload.SystemInstruction, "# Lore Runtime", "~/.gemini/antigravity-cli/skills", "`tools` is intentionally omitted") {
		t.Fatalf("agent profile systemInstruction = %q, want agentpack base content plus Antigravity runtime suffix", payload.SystemInstruction)
	}
	if strings.Contains(string(agentProfile.Content), `"tools"`) {
		t.Fatalf("agent profile = %q, want no tools field", string(agentProfile.Content))
	}
	mcpRelativePath := filepath.ToSlash(filepath.Join("..", "config", "mcp_config.json"))
	if _, ok := byPath[mcpRelativePath]; ok {
		t.Fatal("Render(core-pack) unexpectedly produced optional mcp_config.json")
	}

	withMCP, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetAntigravity,
		Assets:     assets,
		Components: []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
		ServerURL:  "https://example.test",
		SavedToken: "secret-token",
	})
	if err != nil {
		t.Fatalf("Render(core-pack+mcp) error = %v, want nil", err)
	}
	mcpFiles := map[string]RenderedFile{}
	for _, file := range withMCP {
		mcpFiles[file.RelativePath] = file
	}
	mcpFile, ok := mcpFiles[mcpRelativePath]
	if !ok {
		t.Fatalf("Render(core-pack+mcp) paths = %v, want %s", sortedRenderedPaths(withMCP), mcpRelativePath)
	}
	if mcpFile.MergeMode != MergeModeAdditiveJSON {
		t.Fatalf("mcp merge mode = %q, want additive-json", mcpFile.MergeMode)
	}
	got := string(mcpFile.Content)
	if !containsAll(got, `"mcpServers"`, `"lore"`, `"serverUrl": "https://example.test/v1/mcp"`, `"headers"`, `"Authorization": "Bearer secret-token"`) {
		t.Fatalf("mcp_config.json = %q, want direct Lore MCP server config", got)
	}
	for _, forbidden := range []string{"\"command\"", "--auth-file", "lore_mcp_auth.json"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("mcp_config.json = %q, want %q omitted from optional MCP config", got, forbidden)
		}
	}
}

func TestAntigravityPromptMergeRefreshesManagedBlockWithoutDuplicates(t *testing.T) {
	adapter := defaultAntigravityAdapter()
	files, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetAntigravity,
		Assets:     agentpack.DefaultOperationalAssets(),
		Components: []ComponentID{ComponentCorePack},
	})
	if err != nil {
		t.Fatalf("Render(core-pack) error = %v, want nil", err)
	}
	prompt := renderedFileByPath(t, files, "../GEMINI.md")

	existing := []byte("# User prompt\n\nKeep this text.\n")
	merged, err := mergeAntigravityPrompt(existing, prompt.Content)
	if err != nil {
		t.Fatalf("mergeAntigravityPrompt(first) error = %v, want nil", err)
	}
	if !containsAll(string(merged), "# User prompt", "Keep this text.", "<!-- lore-cli:antigravity:start -->") {
		t.Fatalf("first merge = %q, want preserved user content plus managed block", string(merged))
	}

	updatedManaged := []byte(strings.Replace(string(prompt.Content), "skills guidance", "skills guidance refreshed", 1))
	refreshed, err := mergeAntigravityPrompt(merged, updatedManaged)
	if err != nil {
		t.Fatalf("mergeAntigravityPrompt(refresh) error = %v, want nil", err)
	}
	if got := strings.Count(string(refreshed), "<!-- lore-cli:antigravity:start -->"); got != 1 {
		t.Fatalf("managed start marker count = %d, want 1 after refresh", got)
	}
	if got := strings.Count(string(refreshed), "<!-- lore-cli:antigravity:end -->"); got != 1 {
		t.Fatalf("managed end marker count = %d, want 1 after refresh", got)
	}
	if !containsAll(string(refreshed), "Keep this text.", "skills guidance refreshed") {
		t.Fatalf("refreshed merge = %q, want preserved user content and updated managed block", string(refreshed))
	}
}

func TestAntigravityMCPConfigMergePreservesExistingServersAndTreatsEmptyAsObject(t *testing.T) {
	managed, err := renderAntigravityMCPConfig("https://example.test", "secret-token")
	if err != nil {
		t.Fatalf("renderAntigravityMCPConfig() error = %v, want nil", err)
	}

	emptyMerged, err := mergeAntigravityMCPConfig([]byte("\n"), managed)
	if err != nil {
		t.Fatalf("mergeAntigravityMCPConfig(empty) error = %v, want nil", err)
	}
	if !containsAll(string(emptyMerged), `"mcpServers"`, `"lore"`, `"serverUrl": "https://example.test/v1/mcp"`, `"Authorization": "Bearer secret-token"`) {
		t.Fatalf("empty merge = %q, want Lore MCP config written over empty file", string(emptyMerged))
	}

	existing := []byte(`{"mcpServers":{"existing":{"command":"keep-me"}},"topLevel":true}`)
	merged, err := mergeAntigravityMCPConfig(existing, managed)
	if err != nil {
		t.Fatalf("mergeAntigravityMCPConfig(existing) error = %v, want nil", err)
	}
	if !containsAll(string(merged), `"existing"`, `"keep-me"`, `"lore"`, `"serverUrl": "https://example.test/v1/mcp"`, `"Authorization": "Bearer secret-token"`, `"topLevel": true`) {
		t.Fatalf("merged config = %q, want existing servers preserved plus Lore MCP entry", string(merged))
	}
}

func TestAntigravityManifestTracksPromptSkillsAndManagedMCPFilesWithoutPiOverlays(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolveAntigravityLayout(homeDir)
	adapter := defaultAntigravityAdapter()
	assets := agentpack.DefaultOperationalAssets()

	files, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetAntigravity,
		Assets:     assets,
		Components: []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
		ServerURL:  "https://example.test",
		SavedToken: "secret-token",
	})
	if err != nil {
		t.Fatalf("Render(core-pack+mcp) error = %v, want nil", err)
	}
	req := InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://example.test",
		SavedToken:     "secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v0.1.0",
		Target:         TargetAntigravity,
		Components:     []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
		Now:            time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC),
	}
	manifest, managedPaths, err := buildAntigravityManifest(layout, req, files)
	if err != nil {
		t.Fatalf("buildAntigravityManifest() error = %v, want nil", err)
	}
	if len(manifest.ManagedAgentOverlays) != 0 {
		t.Fatalf("ManagedAgentOverlays = %v, want none for Antigravity", manifest.ManagedAgentOverlays)
	}
	if err := manifest.ValidateForLayout(layout, managedPaths, filepath.Join(layout.RootDir, "backups")); err != nil {
		t.Fatalf("ValidateForLayout() error = %v, want nil", err)
	}
	for _, path := range managedPaths {
		if strings.Contains(path, string(filepath.Separator)+"extensions"+string(filepath.Separator)) {
			t.Fatalf("managed path %q leaked Pi overlay semantics", path)
		}
		if strings.Contains(path, string(filepath.Separator)+"agents"+string(filepath.Separator)) && !strings.Contains(path, string(filepath.Separator)+"config"+string(filepath.Separator)+"agents"+string(filepath.Separator)) {
			t.Fatalf("managed path %q leaked Pi overlay semantics", path)
		}
	}
	if !containsSummaryEntry(managedPaths, filepath.ToSlash(filepath.Join(".gemini", "config", "mcp_config.json")), "") {
		t.Fatalf("managed paths = %v, want managed MCP config path", managedPaths)
	}
	if !containsSummaryEntry(managedPaths, filepath.ToSlash(filepath.Join(".gemini", "config", "agents", "lore.json")), "") {
		t.Fatalf("managed paths = %v, want managed Gemini agent profile path", managedPaths)
	}

	repeatManifest, repeatManagedPaths, err := buildAntigravityManifest(layout, req, files)
	if err != nil {
		t.Fatalf("repeat buildAntigravityManifest() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(manifest, repeatManifest) || !reflect.DeepEqual(managedPaths, repeatManagedPaths) {
		t.Fatalf("repeat manifest build drifted\nfirst=%+v\nsecond=%+v\nfirstPaths=%v\nsecondPaths=%v", manifest, repeatManifest, managedPaths, repeatManagedPaths)
	}
}

func TestExecuteAntigravityMCPConfigUsesRestrictivePermissionsWhereSupported(t *testing.T) {
	homeDir := t.TempDir()
	service := Service{}
	plan, err := service.PlanAntigravityInstall(InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://example.test",
		SavedToken:     "secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v0.1.0",
		Target:         TargetAntigravity,
		Now:            time.Date(2026, 5, 25, 12, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("PlanAntigravityInstall() error = %v, want nil", err)
	}
	result, err := service.ExecuteAntigravityInstall(plan, InstallCommandOptions{})
	if err != nil {
		t.Fatalf("ExecuteAntigravityInstall() error = %v, want nil", err)
	}
	for key, label := range map[string]string{"mcp_config": "mcp config", "agent_profile": "agent profile"} {
		info, err := os.Stat(result.Layout.Paths[key])
		if err != nil {
			t.Fatalf("Stat(%s) error = %v, want nil", label, err)
		}
		if runtime.GOOS == "windows" {
			continue
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("%s mode = %#o, want 0o600", label, got)
		}
	}
}

func renderedFileByPath(t *testing.T, files []RenderedFile, path string) RenderedFile {
	t.Helper()
	for _, file := range files {
		if file.RelativePath == path {
			return file
		}
	}
	t.Fatalf("rendered file %q missing from %v", path, sortedRenderedPaths(files))
	return RenderedFile{}
}

func sortedRenderedPaths(files []RenderedFile) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.RelativePath)
	}
	return paths
}
