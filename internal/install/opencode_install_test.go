package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alferio94/lore-cli/internal/agentconfig"
)

func TestOpenCodePlanCreatesManagedActions(t *testing.T) {
	homeDir := t.TempDir()
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	service := Service{AgentConfigStore: &fakeAgentConfigStore{path: filepath.Join(homeDir, ".lore", "agent-config.json"), cfg: agentconfig.DefaultConfig()}}

	plan, err := service.PlanOpenCodeInstall(InstallRequest{
		HomeDir:        homeDir,
		Target:         TargetOpenCode,
		Components:     []ComponentID{ComponentCorePack, ComponentExtendedSkills},
		LoreCLIVersion: "v0.4.2",
		Now:            now,
	})
	if err != nil {
		t.Fatalf("PlanOpenCodeInstall error: %v", err)
	}
	if plan.Layout.Target != TargetOpenCode {
		t.Fatalf("plan.Layout.Target = %q, want %q", plan.Layout.Target, TargetOpenCode)
	}
	assertPlanFileAction(t, plan.Files, opencodeAgentsFileName, "create")
	assertPlanFileAction(t, plan.Files, opencodeConfigFileName, "create")
	assertPlanFileAction(t, plan.Files, filepath.ToSlash(filepath.Join(opencodeSkillsDirName, "sdd-apply", "SKILL.md")), "create")
	assertPlanFileAction(t, plan.Files, opencodeManifestFileName, "create")
}

func TestOpenCodeExecuteWritesFilesAndManifest(t *testing.T) {
	homeDir := t.TempDir()
	now := time.Date(2026, 6, 2, 12, 5, 0, 0, time.UTC)
	service := Service{AgentConfigStore: &fakeAgentConfigStore{path: filepath.Join(homeDir, ".lore", "agent-config.json"), cfg: agentconfig.DefaultConfig()}}

	plan, err := service.PlanOpenCodeInstall(InstallRequest{
		HomeDir:        homeDir,
		Target:         TargetOpenCode,
		Components:     []ComponentID{ComponentCorePack, ComponentExtendedSkills},
		LoreCLIVersion: "v0.4.2",
		Now:            now,
	})
	if err != nil {
		t.Fatalf("PlanOpenCodeInstall error: %v", err)
	}
	result, err := service.ExecuteOpenCodeInstall(plan, InstallCommandOptions{})
	if err != nil {
		t.Fatalf("ExecuteOpenCodeInstall error: %v", err)
	}
	if result.Target != TargetOpenCode {
		t.Fatalf("result.Target = %q, want %q", result.Target, TargetOpenCode)
	}
	if len(result.Summary.Failed) != 0 {
		t.Fatalf("Summary.Failed = %v, want none", result.Summary.Failed)
	}
	if !containsSummaryEntry(result.Summary.Created, opencodeAgentsFileName) || !containsSummaryEntry(result.Summary.Created, opencodeConfigFileName) {
		t.Fatalf("Summary.Created = %v, want AGENTS.md and opencode.json", result.Summary.Created)
	}

	agentsPath := filepath.Join(homeDir, ".config", "opencode", opencodeAgentsFileName)
	agentsContent, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("ReadFile(AGENTS.md) error: %v", err)
	}
	if !containsAll(string(agentsContent), "Lore Runtime", "~/.config/opencode/opencode.json") {
		t.Fatalf("AGENTS.md = %q, want Lore Runtime OpenCode guidance", string(agentsContent))
	}

	configPath := filepath.Join(homeDir, ".config", "opencode", opencodeConfigFileName)
	configContent, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(opencode.json) error: %v", err)
	}
	if !containsAll(string(configContent), `"lore": {`, `"managed_by": "lore-cli"`, `"skills_dir": "~/.config/opencode/skills"`) {
		t.Fatalf("opencode.json = %q, want Lore-managed OpenCode block", string(configContent))
	}

	manifest, err := LoadManifest(filepath.Join(homeDir, ".config", "opencode", opencodeManifestFileName))
	if err != nil {
		t.Fatalf("LoadManifest error: %v", err)
	}
	if manifest.Target != TargetOpenCode || manifest.AuthMode != "config-only" {
		t.Fatalf("manifest = %+v, want config-only OpenCode manifest", manifest)
	}
	if err := manifest.ValidateForLayout(result.Layout, managedManifestPaths(manifest), filepath.Join(result.Layout.RootDir, "backups")); err != nil {
		t.Fatalf("ValidateForLayout error: %v", err)
	}
}

func TestOpenCodeBackupBeforeOverwrite(t *testing.T) {
	homeDir := t.TempDir()
	rootDir := filepath.Join(homeDir, ".config", "opencode")
	if err := os.MkdirAll(filepath.Join(rootDir, opencodeSkillsDirName, "sdd-apply"), 0o755); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, opencodeAgentsFileName), []byte("# user agents\n"), 0o600); err != nil {
		t.Fatalf("WriteFile agents error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, opencodeConfigFileName), []byte(`{"theme":"midnight"}`), 0o600); err != nil {
		t.Fatalf("WriteFile opencode.json error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, opencodeSkillsDirName, "sdd-apply", "SKILL.md"), []byte("old skill\n"), 0o600); err != nil {
		t.Fatalf("WriteFile skill error: %v", err)
	}

	now := time.Date(2026, 6, 2, 12, 10, 0, 0, time.UTC)
	service := Service{AgentConfigStore: &fakeAgentConfigStore{path: filepath.Join(homeDir, ".lore", "agent-config.json"), cfg: agentconfig.DefaultConfig()}}
	plan, err := service.PlanOpenCodeInstall(InstallRequest{HomeDir: homeDir, Target: TargetOpenCode, Components: []ComponentID{ComponentCorePack}, Now: now})
	if err != nil {
		t.Fatalf("PlanOpenCodeInstall error: %v", err)
	}
	assertPlanFileAction(t, plan.Files, opencodeAgentsFileName, "update")
	assertPlanFileAction(t, plan.Files, opencodeConfigFileName, "update")
	assertPlanFileAction(t, plan.Files, filepath.ToSlash(filepath.Join(opencodeSkillsDirName, "sdd-apply", "SKILL.md")), "update")

	result, err := service.ExecuteOpenCodeInstall(plan, InstallCommandOptions{})
	if err != nil {
		t.Fatalf("ExecuteOpenCodeInstall error: %v", err)
	}
	backupRoot := result.Manifest.BackupRoot
	for relativePath, want := range map[string]string{
		opencodeAgentsFileName: "# user agents",
		opencodeConfigFileName: `{"theme":"midnight"}`,
		filepath.ToSlash(filepath.Join(opencodeSkillsDirName, "sdd-apply", "SKILL.md")): "old skill",
	} {
		backupContent, err := os.ReadFile(filepath.Join(backupRoot, filepath.FromSlash(relativePath)))
		if err != nil {
			t.Fatalf("ReadFile(%s backup) error: %v", relativePath, err)
		}
		if !strings.Contains(string(backupContent), want) {
			t.Fatalf("backup %s = %q, want substring %q", relativePath, string(backupContent), want)
		}
	}
	if !containsSummaryEntry(result.Summary.Updated, opencodeAgentsFileName) || !containsSummaryEntry(result.Summary.Updated, opencodeConfigFileName) {
		t.Fatalf("Summary.Updated = %v, want updated AGENTS/opencode.json entries", result.Summary.Updated)
	}
}

func TestOpenCodeManifestUsesMergedJSONHash(t *testing.T) {
	homeDir := t.TempDir()
	rootDir := filepath.Join(homeDir, ".config", "opencode")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, opencodeConfigFileName), []byte(`{"theme":"midnight"}`), 0o600); err != nil {
		t.Fatalf("WriteFile opencode.json error: %v", err)
	}

	now := time.Date(2026, 6, 2, 12, 15, 0, 0, time.UTC)
	service := Service{AgentConfigStore: &fakeAgentConfigStore{path: filepath.Join(homeDir, ".lore", "agent-config.json"), cfg: agentconfig.DefaultConfig()}}
	plan, err := service.PlanOpenCodeInstall(InstallRequest{HomeDir: homeDir, Target: TargetOpenCode, Components: []ComponentID{ComponentCorePack}, Now: now})
	if err != nil {
		t.Fatalf("PlanOpenCodeInstall error: %v", err)
	}
	result, err := service.ExecuteOpenCodeInstall(plan, InstallCommandOptions{})
	if err != nil {
		t.Fatalf("ExecuteOpenCodeInstall error: %v", err)
	}

	configPath := filepath.Join(rootDir, opencodeConfigFileName)
	actualConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(opencode.json) error: %v", err)
	}
	var matched *ManagedFileRecord
	for i := range result.Manifest.ManagedFiles {
		record := &result.Manifest.ManagedFiles[i]
		if filepath.Clean(record.Path) == filepath.Clean(configPath) {
			matched = record
			break
		}
	}
	if matched == nil {
		t.Fatalf("manifest managed files = %+v, want opencode.json entry", result.Manifest.ManagedFiles)
	}
	if matched.MergeMode != MergeModeAdditiveJSON {
		t.Fatalf("opencode.json merge mode = %q, want %q", matched.MergeMode, MergeModeAdditiveJSON)
	}
	if matched.ContentHash != contentHash(actualConfig) {
		t.Fatalf("opencode.json content hash = %q, want actual merged file hash %q", matched.ContentHash, contentHash(actualConfig))
	}
}

func TestOpenCodeIdempotentRerunKeepsFilesUnchanged(t *testing.T) {
	homeDir := t.TempDir()
	now := time.Date(2026, 6, 2, 12, 20, 0, 0, time.UTC)
	service := Service{AgentConfigStore: &fakeAgentConfigStore{path: filepath.Join(homeDir, ".lore", "agent-config.json"), cfg: agentconfig.DefaultConfig()}}

	plan1, err := service.PlanOpenCodeInstall(InstallRequest{HomeDir: homeDir, Target: TargetOpenCode, Components: []ComponentID{ComponentCorePack, ComponentExtendedSkills}, LoreCLIVersion: "v0.4.2", Now: now})
	if err != nil {
		t.Fatalf("PlanOpenCodeInstall first error: %v", err)
	}
	if _, err := service.ExecuteOpenCodeInstall(plan1, InstallCommandOptions{}); err != nil {
		t.Fatalf("ExecuteOpenCodeInstall first error: %v", err)
	}

	plan2, err := service.PlanOpenCodeInstall(InstallRequest{HomeDir: homeDir, Target: TargetOpenCode, Components: []ComponentID{ComponentCorePack, ComponentExtendedSkills}, LoreCLIVersion: "v0.4.2", Now: now})
	if err != nil {
		t.Fatalf("PlanOpenCodeInstall second error: %v", err)
	}
	if _, err := service.ExecuteOpenCodeInstall(plan2, InstallCommandOptions{}); err != nil {
		t.Fatalf("ExecuteOpenCodeInstall second error: %v", err)
	}

	unchanged := 0
	for _, action := range plan2.Files {
		if action.Action == "unchanged" {
			unchanged++
		}
	}
	if unchanged == 0 {
		t.Fatalf("second plan actions = %+v, want unchanged rerun", plan2.Files)
	}
	assertPlanFileAction(t, plan2.Files, opencodeConfigFileName, "unchanged")
	assertPlanFileAction(t, plan2.Files, opencodeManifestFileName, "unchanged")
}

func TestOpenCodeManifestValidationRejectsDuplicatePaths(t *testing.T) {
	layout := ResolveOpenCodeLayout("/tmp/home")
	manifest := Manifest{
		SchemaVersion: PortableManifestSchemaVersion,
		Target:        TargetOpenCode,
		AuthMode:      "config-only",
		Components:    []ComponentID{ComponentCorePack},
		ManagedFiles: []ManagedFileRecord{
			{Path: filepath.Join(layout.RootDir, opencodeAgentsFileName), Component: ComponentCorePack, MergeMode: MergeModeReplace, ContentHash: contentHash([]byte("agents"))},
			{Path: filepath.Join(layout.RootDir, opencodeAgentsFileName), Component: ComponentCorePack, MergeMode: MergeModeReplace, ContentHash: contentHash([]byte("agents-2"))},
		},
		BackupRoot:  filepath.Join(layout.RootDir, "backups", "20260602T122500Z"),
		InstalledAt: time.Date(2026, 6, 2, 12, 25, 0, 0, time.UTC).Format(time.RFC3339),
	}
	if err := manifest.ValidateForLayout(layout, nil, filepath.Join(layout.RootDir, "backups")); err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("ValidateForLayout error = %v, want duplicate-path rejection", err)
	}
}

// --- Repair tests: MCP-selected OpenCode install path ---

func TestOpenCodePlanWithMCPSelectsLoreAndMCPLoreBlocks(t *testing.T) {
	// When lore-server-mcp is selected and ServerURL/SavedToken are provided,
	// the rendered opencode.json must contain BOTH the top-level `lore` block
	// and the top-level `mcp` block with `mcp.lore` remote entry.
	homeDir := t.TempDir()
	now := time.Date(2026, 6, 2, 14, 0, 0, 0, time.UTC)
	service := Service{AgentConfigStore: &fakeAgentConfigStore{path: filepath.Join(homeDir, ".lore", "agent-config.json"), cfg: agentconfig.DefaultConfig()}}

	plan, err := service.PlanOpenCodeInstall(InstallRequest{
		HomeDir:        homeDir,
		Target:         TargetOpenCode,
		Components:     []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
		ServerURL:      "https://lore.example/v1/mcp",
		SavedToken:     "secret-test-token",
		LoreCLIVersion: "v0.4.2",
		Now:            now,
	})
	if err != nil {
		t.Fatalf("PlanOpenCodeInstall with MCP error: %v", err)
	}

	result, err := service.ExecuteOpenCodeInstall(plan, InstallCommandOptions{})
	if err != nil {
		t.Fatalf("ExecuteOpenCodeInstall error: %v", err)
	}
	if len(result.Summary.Failed) > 0 {
		t.Fatalf("Summary.Failed = %v, want none", result.Summary.Failed)
	}

	configPath := filepath.Join(homeDir, ".config", "opencode", opencodeConfigFileName)
	configContent, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(opencode.json) error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(configContent, &parsed); err != nil {
		t.Fatalf("opencode.json is not valid JSON: %v", err)
	}

	// Top-level `lore` block must be present.
	if _, ok := parsed["lore"]; !ok {
		t.Fatalf("opencode.json top-level keys = %v, want `lore` block", mapKeys(parsed))
	}
	loreObj, ok := parsed["lore"].(map[string]any)
	if !ok {
		t.Fatalf("lore value = %T, want map", parsed["lore"])
	}
	if got, want := loreObj["managed_by"], "lore-cli"; got != want {
		t.Fatalf("lore.managed_by = %v, want %q", got, want)
	}

	// Top-level `mcp` block must be present with `lore` sub-entry.
	if _, ok := parsed["mcp"]; !ok {
		t.Fatalf("opencode.json top-level keys = %v, want `mcp` block", mapKeys(parsed))
	}
	mcpObj, ok := parsed["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("mcp value = %T, want map", parsed["mcp"])
	}
	if _, ok := mcpObj["lore"]; !ok {
		t.Fatalf("mcp sub-keys = %v, want `lore` entry", mapKeys(mcpObj))
	}
	loreEntry, ok := mcpObj["lore"].(map[string]any)
	if !ok {
		t.Fatalf("mcp.lore = %T, want map", mcpObj["lore"])
	}
	if got, want := loreEntry["type"], "remote"; got != want {
		t.Fatalf("mcp.lore.type = %v, want %q", got, want)
	}
	if got, want := loreEntry["url"], "https://lore.example/v1/mcp"; got != want {
		t.Fatalf("mcp.lore.url = %v, want %q", got, want)
	}
	// Token must be in config (MCP server needs it) but must not leak in test failure messages.
	headers, ok := loreEntry["headers"].(map[string]any)
	if !ok {
		t.Fatalf("mcp.lore.headers = %T, want map", loreEntry["headers"])
	}
	auth, ok := headers["Authorization"].(string)
	if !ok {
		t.Fatalf("mcp.lore.headers.Authorization = %T, want string", headers["Authorization"])
	}
	if !strings.HasPrefix(auth, "Bearer ") {
		t.Fatalf("mcp.lore.headers.Authorization = %q, want Bearer prefix", auth)
	}
	// Token value present in config (for MCP server) but not echoed in test assertion.
	_ = strings.TrimPrefix(auth, "Bearer ")
}

func TestOpenCodePlanWithoutMCPDoesNotRenderMCPLore(t *testing.T) {
	// When lore-server-mcp is NOT selected, the rendered opencode.json
	// must NOT contain the `mcp` block.
	homeDir := t.TempDir()
	now := time.Date(2026, 6, 2, 14, 30, 0, 0, time.UTC)
	service := Service{AgentConfigStore: &fakeAgentConfigStore{path: filepath.Join(homeDir, ".lore", "agent-config.json"), cfg: agentconfig.DefaultConfig()}}

	plan, err := service.PlanOpenCodeInstall(InstallRequest{
		HomeDir:        homeDir,
		Target:         TargetOpenCode,
		Components:     []ComponentID{ComponentCorePack, ComponentExtendedSkills},
		ServerURL:      "https://lore.example",
		SavedToken:     "some-token",
		LoreCLIVersion: "v0.4.2",
		Now:            now,
	})
	if err != nil {
		t.Fatalf("PlanOpenCodeInstall without MCP error: %v", err)
	}

	_, err = service.ExecuteOpenCodeInstall(plan, InstallCommandOptions{})
	if err != nil {
		t.Fatalf("ExecuteOpenCodeInstall error: %v", err)
	}

	configContent, err := os.ReadFile(filepath.Join(homeDir, ".config", "opencode", opencodeConfigFileName))
	if err != nil {
		t.Fatalf("ReadFile(opencode.json) error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(configContent, &parsed); err != nil {
		t.Fatalf("opencode.json is not valid JSON: %v", err)
	}

	// `lore` block must be present (always; lore block is added for all OpenCode installs).
	if _, ok := parsed["lore"]; !ok {
		t.Fatalf("opencode.json top-level keys = %v, want `lore` block (always added for OpenCode config)", mapKeys(parsed))
	}
	// `mcp` block must NOT be present when lore-server-mcp is not selected.
	if _, ok := parsed["mcp"]; ok {
		t.Fatalf("opencode.json top-level keys = %v, want NO `mcp` block when lore-server-mcp is not selected", mapKeys(parsed))
	}
}

func TestOpenCodeMCPMergePreservesExistingLoreBlock(t *testing.T) {
	// When opencode.json already has a managed lore block, merging with
	// MCP selected must preserve the lore block and add mcp.lore.
	homeDir := t.TempDir()
	rootDir := filepath.Join(homeDir, ".config", "opencode")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}
	existingContent := `{"theme":"midnight","lore":{"managed_by":"lore-cli","agents":{"sdd-apply":{"model":"gpt-4.1"}},"skills_dir":"~/.config/opencode/skills"}}`
	if err := os.WriteFile(filepath.Join(rootDir, opencodeConfigFileName), []byte(existingContent), 0o600); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	now := time.Date(2026, 6, 2, 14, 45, 0, 0, time.UTC)
	service := Service{AgentConfigStore: &fakeAgentConfigStore{path: filepath.Join(homeDir, ".lore", "agent-config.json"), cfg: agentconfig.DefaultConfig()}}

	plan, err := service.PlanOpenCodeInstall(InstallRequest{
		HomeDir:        homeDir,
		Target:         TargetOpenCode,
		Components:     []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
		ServerURL:      "https://lore.example/v1/mcp",
		SavedToken:     "secret-token",
		LoreCLIVersion: "v0.4.2",
		Now:            now,
	})
	if err != nil {
		t.Fatalf("PlanOpenCodeInstall error: %v", err)
	}

	result, err := service.ExecuteOpenCodeInstall(plan, InstallCommandOptions{})
	if err != nil {
		t.Fatalf("ExecuteOpenCodeInstall error: %v", err)
	}
	if len(result.Summary.Failed) > 0 {
		t.Fatalf("Summary.Failed = %v, want none", result.Summary.Failed)
	}

	configPath := filepath.Join(rootDir, opencodeConfigFileName)
	configContent, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(opencode.json) error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(configContent, &parsed); err != nil {
		t.Fatalf("opencode.json is not valid JSON: %v", err)
	}

	// lore block must be managed by lore-cli.
	loreObj := parsed["lore"].(map[string]any)
	if got, want := loreObj["managed_by"], "lore-cli"; got != want {
		t.Fatalf("lore.managed_by = %v, want %q", got, want)
	}

	// mcp block must be added (lore-server-mcp selected).
	mcpObj, ok := parsed["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("merged keys = %v, want mcp block", mapKeys(parsed))
	}
	if mcpObj["lore"] == nil {
		t.Fatalf("mcp sub-keys = %v, want lore entry after merge", mapKeys(mcpObj))
	}
	loreEntry := mcpObj["lore"].(map[string]any)
	if got, want := loreEntry["type"], "remote"; got != want {
		t.Fatalf("mcp.lore.type = %v, want %q", got, want)
	}
}
