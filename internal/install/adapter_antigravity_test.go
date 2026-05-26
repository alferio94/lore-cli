package install

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alferio94/lore-cli/internal/agentpack"
)

func TestAntigravityAdapterRenderProducesPromptSkillsAndOptionalMCPWithoutPiArtifacts(t *testing.T) {
	adapter := defaultAntigravityAdapter()

	files, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetAntigravity,
		Definition: agentpack.DefaultDefinition(),
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
	if !containsAll(string(prompt.Content), "<!-- lore-cli:antigravity:start -->", "append", "~/.gemini/antigravity-cli/skills") {
		t.Fatalf("prompt content = %q, want managed Antigravity markers and skills guidance", string(prompt.Content))
	}
	if _, ok := byPath[filepath.ToSlash(filepath.Join("skills", "sdd-apply", "SKILL.md"))]; !ok {
		t.Fatalf("Render(core-pack) paths = %v, want skills/sdd-apply/SKILL.md", sortedRenderedPaths(files))
	}
	if _, ok := byPath[filepath.ToSlash(filepath.Join("skills", "lore-worker", "SKILL.md"))]; !ok {
		t.Fatalf("Render(core-pack) paths = %v, want skills/lore-worker/SKILL.md", sortedRenderedPaths(files))
	}
	if _, ok := byPath["mcp_config.json"]; ok {
		t.Fatal("Render(core-pack) unexpectedly produced optional mcp_config.json")
	}

	withMCP, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetAntigravity,
		Definition: agentpack.DefaultDefinition(),
		Components: []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
	})
	if err != nil {
		t.Fatalf("Render(core-pack+mcp) error = %v, want nil", err)
	}
	mcpFiles := map[string]RenderedFile{}
	for _, file := range withMCP {
		mcpFiles[file.RelativePath] = file
	}
	if got := string(mcpFiles["mcp_config.json"].Content); !containsAll(got, `"mcpServers"`, `"lore"`) {
		t.Fatalf("mcp_config.json = %q, want Lore MCP stub config", got)
	}
}

func TestAntigravityPromptMergeRefreshesManagedBlockWithoutDuplicates(t *testing.T) {
	adapter := defaultAntigravityAdapter()
	files, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetAntigravity,
		Definition: agentpack.DefaultDefinition(),
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

func TestAntigravityManifestTracksPromptAndSkillsWithoutPiOverlays(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolveAntigravityLayout(homeDir)
	adapter := defaultAntigravityAdapter()

	files, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetAntigravity,
		Definition: agentpack.DefaultDefinition(),
		Components: []ComponentID{ComponentCorePack},
	})
	if err != nil {
		t.Fatalf("Render(core-pack) error = %v, want nil", err)
	}
	req := InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://example.test",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v0.1.0",
		Target:         TargetAntigravity,
		Components:     []ComponentID{ComponentCorePack},
		Definition:     agentpack.DefaultDefinition(),
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
		if strings.Contains(path, string(filepath.Separator)+"agents"+string(filepath.Separator)) || strings.Contains(path, string(filepath.Separator)+"extensions"+string(filepath.Separator)) {
			t.Fatalf("managed path %q leaked Pi overlay semantics", path)
		}
		if strings.HasSuffix(path, string(filepath.Separator)+"mcp_config.json") {
			t.Fatalf("managed path %q unexpectedly included optional MCP file", path)
		}
	}

	repeatManifest, repeatManagedPaths, err := buildAntigravityManifest(layout, req, files)
	if err != nil {
		t.Fatalf("repeat buildAntigravityManifest() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(manifest, repeatManifest) || !reflect.DeepEqual(managedPaths, repeatManagedPaths) {
		t.Fatalf("repeat manifest build drifted\nfirst=%+v\nsecond=%+v\nfirstPaths=%v\nsecondPaths=%v", manifest, repeatManifest, managedPaths, repeatManagedPaths)
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
