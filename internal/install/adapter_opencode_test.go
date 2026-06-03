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

func TestResolveOpenCodeLayout(t *testing.T) {
	homeDir := "/home/user"
	layout := ResolveOpenCodeLayout(homeDir)

	if layout.Target != TargetOpenCode {
		t.Fatalf("layout.Target = %q, want %q", layout.Target, TargetOpenCode)
	}
	if got, want := layout.RootDir, filepath.Join(homeDir, ".config", "opencode"); got != want {
		t.Fatalf("layout.RootDir = %q, want %q", got, want)
	}
	if got, want := layout.ManifestPath, filepath.Join(homeDir, ".config", "opencode", "lore-install.json"); got != want {
		t.Fatalf("layout.ManifestPath = %q, want %q", got, want)
	}
	if got, want := layout.Paths[opencodeAgentsPathKey], filepath.Join(homeDir, ".config", "opencode", "AGENTS.md"); got != want {
		t.Fatalf("layout.Paths[%q] = %q, want %q", opencodeAgentsPathKey, got, want)
	}
	if got, want := layout.Paths[opencodeJSONPathKey], filepath.Join(homeDir, ".config", "opencode", "opencode.json"); got != want {
		t.Fatalf("layout.Paths[%q] = %q, want %q", opencodeJSONPathKey, got, want)
	}
	if got, want := layout.Paths[opencodeSkillsDirPathKey], filepath.Join(homeDir, ".config", "opencode", "skills"); got != want {
		t.Fatalf("layout.Paths[%q] = %q, want %q", opencodeSkillsDirPathKey, got, want)
	}
	if got, want := layout.Paths[opencodeCommandsDirPathKey], filepath.Join(homeDir, ".config", "opencode", "commands"); got != want {
		t.Fatalf("layout.Paths[%q] = %q, want %q", opencodeCommandsDirPathKey, got, want)
	}
}

func TestOpenCodeRenderProducesAgentsAndManagedSkills(t *testing.T) {
	adapter := defaultOpenCodeAdapter()
	files, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetOpenCode,
		Assets:     agentpack.DefaultOperationalAssets(),
		Components: []ComponentID{ComponentCorePack, ComponentExtendedSkills},
	})
	if err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}

	byPath := map[string]RenderedFile{}
	for _, file := range files {
		byPath[file.RelativePath] = file
	}

	agentsFile, ok := byPath[opencodeAgentsFileName]
	if !ok {
		t.Fatalf("rendered paths = %v, want %q", sortedRenderedPaths(files), opencodeAgentsFileName)
	}
	content := string(agentsFile.Content)
	for _, want := range []string{
		"This file is managed by `lore install --target opencode`",
		"~/.config/opencode/skills",
		"~/.config/opencode/opencode.json",
		"~/.config/opencode/commands",
		"sdd-apply: `gpt-5.4`",
		"## Orchestrator instruction",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("AGENTS.md = %q, want substring %q", content, want)
		}
	}
	for _, forbidden := range []string{"Bearer ", "mcpServers", "codex exec"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("AGENTS.md = %q, want %q omitted", content, forbidden)
		}
	}

	applySkill, ok := byPath[filepath.ToSlash(filepath.Join("skills", "sdd-apply", "SKILL.md"))]
	if !ok {
		t.Fatalf("rendered paths = %v, want OpenCode sdd-apply skill", sortedRenderedPaths(files))
	}
	if !containsAll(string(applySkill.Content), "~/.config/opencode/skills/sdd-apply/SKILL.md", "~/.config/opencode/skills/_shared/sdd-phase-common/SKILL.md") {
		t.Fatalf("sdd-apply skill = %q, want OpenCode skill paths", string(applySkill.Content))
	}

	if _, ok := byPath[filepath.ToSlash(filepath.Join("skills", "skill-creator", "SKILL.md"))]; !ok {
		t.Fatalf("rendered paths = %v, want extended skill output", sortedRenderedPaths(files))
	}
}

func TestOpenCodeAgentConfigUsesCustomModels(t *testing.T) {
	adapter := defaultOpenCodeAdapter()
	cfg := agentconfig.DefaultConfig()
	cfg.SDDAgents["sdd-verify"] = agentconfig.Agent{Model: "gpt-4o-mini"}

	files, err := adapter.Render(context.Background(), RenderRequest{
		Target:      TargetOpenCode,
		Assets:      agentpack.DefaultOperationalAssets(),
		Components:  []ComponentID{ComponentCorePack},
		AgentConfig: cfg,
	})
	if err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}
	content := string(renderedFileByPath(t, files, opencodeAgentsFileName).Content)
	if !strings.Contains(content, "sdd-verify: `gpt-4o-mini`") {
		t.Fatalf("AGENTS.md = %q, want custom sdd-verify model", content)
	}

	block, err := renderOpenCodeLoreBlock(cfg, false)
	if err != nil {
		t.Fatalf("renderOpenCodeLoreBlock() error = %v, want nil", err)
	}
	jsonText := string(block)
	if !containsAll(jsonText, `"managed_by": "lore-cli"`, `"sdd-verify": {`, `"model": "gpt-4o-mini"`, `"skills_dir": "~/.config/opencode/skills"`) {
		t.Fatalf("lore block = %q, want custom model and skills dir", jsonText)
	}
	if strings.Contains(jsonText, `"commands_dir"`) {
		t.Fatalf("lore block = %q, want commands omitted when not explicitly approved", jsonText)
	}
}

func TestOpenCodeCommandsOmittedWithoutApprovedBoundary(t *testing.T) {
	files, err := renderOpenCodeCommandFiles(RenderRequest{Target: TargetOpenCode}, false)
	if err != nil {
		t.Fatalf("renderOpenCodeCommandFiles(false) error = %v, want nil", err)
	}
	if len(files) != 0 {
		t.Fatalf("renderOpenCodeCommandFiles(false) = %v, want no files", files)
	}
}

func TestOpenCodeCommandsFailClosedWithoutApprovedBoundary(t *testing.T) {
	_, err := renderOpenCodeCommandFiles(RenderRequest{Target: TargetOpenCode}, true)
	if err == nil || !strings.Contains(err.Error(), "approved explicit command asset boundary") {
		t.Fatalf("renderOpenCodeCommandFiles(true) error = %v, want fail-closed command-boundary error", err)
	}
}

func TestOpenCodeMergePreservesUserJSON(t *testing.T) {
	desired, err := renderOpenCodeLoreBlock(agentconfig.DefaultConfig(), false)
	if err != nil {
		t.Fatalf("renderOpenCodeLoreBlock() error = %v, want nil", err)
	}
	existing := []byte(`{"theme":"midnight","nested":{"keep":true}}`)
	merged, err := mergeOpenCodeJSON(existing, desired)
	if err != nil {
		t.Fatalf("mergeOpenCodeJSON() error = %v, want nil", err)
	}
	text := string(merged)
	if !containsAll(text, `"theme": "midnight"`, `"nested": {`, `"keep": true`, `"lore": {`, `"managed_by": "lore-cli"`) {
		t.Fatalf("merged opencode.json = %q, want preserved user keys plus lore block", text)
	}
}

func TestOpenCodeMCPContractTopLevelShape(t *testing.T) {
	// Verifies the OpenCode remote MCP config uses a top-level `mcp` object
	// (NOT `mcpServers`). This is the canonical shape confirmed by explore.
	token := "secret-test-token"
	serverURL := "https://lore.example/v1/mcp"

	block, err := renderOpenCodeMCPConfig(agentconfig.DefaultConfig(), serverURL, token)
	if err != nil {
		t.Fatalf("renderOpenCodeMCPConfig() error = %v, want nil", err)
	}

	// Decode into a generic map so we can inspect structure without codegen.
	var parsed map[string]any
	if err := json.Unmarshal(block, &parsed); err != nil {
		t.Fatalf("block is not valid JSON: %v", err)
	}

	// Top-level key must be `mcp`, not `mcpServers`.
	if _, ok := parsed[opencodeMCPBlockKey]; !ok {
		t.Fatalf("top-level keys = %v, want %q key", mapKeys(parsed), opencodeMCPBlockKey)
	}
	if _, ok := parsed["mcpServers"]; ok {
		t.Fatalf("top-level keys = %v, want NO mcpServers key (shape uses top-level mcp)", mapKeys(parsed))
	}

	mcpObj, ok := parsed[opencodeMCPBlockKey].(map[string]any)
	if !ok {
		t.Fatalf("mcp value = %T, want map", parsed[opencodeMCPBlockKey])
	}

	// `lore` sub-key must be present as the managed lore MCP entry.
	if _, ok := mcpObj["lore"]; !ok {
		t.Fatalf("mcp sub-keys = %v, want lore sub-key for managed lore MCP entry", mapKeys(mcpObj))
	}

	loreEntry, ok := mcpObj["lore"].(map[string]any)
	if !ok {
		t.Fatalf("mcp.lore = %T, want map", mcpObj["lore"])
	}

	// `type` must be "remote".
	if got, want := loreEntry["type"], "remote"; got != want {
		t.Fatalf("mcp.lore.type = %v, want %q", got, want)
	}

	// `url` must match the provided server URL.
	if got, want := loreEntry["url"], serverURL; got != want {
		t.Fatalf("mcp.lore.url = %v, want %q", got, want)
	}

	// `enabled` must be true.
	if got, want := loreEntry["enabled"], true; got != want {
		t.Fatalf("mcp.lore.enabled = %v, want %v", got, want)
	}

	// `headers` must be present and be a map.
	headers, ok := loreEntry["headers"].(map[string]any)
	if !ok {
		t.Fatalf("mcp.lore.headers = %T, want map", loreEntry["headers"])
	}

	// `Authorization` must be present and use Bearer scheme.
	auth, ok := headers["Authorization"].(string)
	if !ok {
		t.Fatalf("mcp.lore.headers.Authorization = %T, want string", headers["Authorization"])
	}
	if !strings.HasPrefix(auth, "Bearer ") {
		t.Fatalf("mcp.lore.headers.Authorization = %q, want Bearer scheme", auth)
	}
}

func TestOpenCodeMCPContractNoTokenLeakInSummaryOrLog(t *testing.T) {
	// Verifies the rendered JSON block is suitable for config file persistence.
	// Token redaction in summaries/logs is handled by the caller; this test
	// confirms the Bearer scheme is correct and no additional raw-value
	// markers appear outside the Authorization field.
	token := "super-secret-token-value-12345"
	serverURL := "https://lore.example/v1/mcp"

	block, err := renderOpenCodeMCPConfig(agentconfig.DefaultConfig(), serverURL, token)
	if err != nil {
		t.Fatalf("renderOpenCodeMCPConfig() error = %v, want nil", err)
	}

	jsonStr := string(block)

	// Bearer scheme must be used.
	if !strings.Contains(jsonStr, "Bearer ") {
		t.Fatalf("mcp config = %q, want Bearer auth scheme", jsonStr)
	}

	// Token must be in the config (MCP server needs it), but we assert
	// no auxiliary raw-value markers that could appear in log summaries.
	// The actual Authorization value is scoped to the config file.
	if strings.Contains(jsonStr, "token_value") || strings.Contains(jsonStr, "raw_token") {
		t.Fatalf("mcp config = %q, want no raw token markers outside Authorization", jsonStr)
	}
}

func TestOpenCodeMCPContractFailClosedForEmptyToken(t *testing.T) {
	// Fail-closed: empty token must produce an error, not silently write an
	// empty or malformed Authorization header.
	_, err := renderOpenCodeMCPConfig(agentconfig.DefaultConfig(), "https://lore.example/v1/mcp", "")
	if err == nil {
		t.Fatal("renderOpenCodeMCPConfig with empty token: want error, got nil")
	}
}

func TestOpenCodeMCPContractFailClosedForEmptyURL(t *testing.T) {
	_, err := renderOpenCodeMCPConfig(agentconfig.DefaultConfig(), "", "some-token")
	if err == nil {
		t.Fatal("renderOpenCodeMCPConfig with empty URL: want error, got nil")
	}
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestOpenCodeMCPRendersWhenServerURLAndTokenProvided(t *testing.T) {
	// With the updated routing, adapter.Render fails for lore-server-mcp.
	// This test validates the rendering helper directly.
	cfg := agentconfig.DefaultConfig()
	content, err := renderOpenCodeMCPConfig(cfg, "https://lore.example/v1/mcp", "secret-token")
	if err != nil {
		t.Fatalf("renderOpenCodeMCPConfig() error = %v", err)
	}
	var parsed map[string]any
	json.Unmarshal(content, &parsed)

	// Both lore and mcp blocks must be present.
	if _, ok := parsed[opencodeMCPBlockKey]; !ok {
		t.Fatalf("parsed keys = %v, want mcp block", mapKeys(parsed))
	}
	if parsed["lore"] == nil {
		t.Fatalf("parsed keys = %v, want lore block", mapKeys(parsed))
	}

	mcpObj := parsed[opencodeMCPBlockKey].(map[string]any)
	loreEntry := mcpObj["lore"].(map[string]any)
	if got, want := loreEntry["type"], "remote"; got != want {
		t.Fatalf("mcp.lore.type = %v, want %q", got, want)
	}
}

func TestOpenCodeMCPFailClosedWhenComponentSelectedButNoAuth(t *testing.T) {
	// When ComponentLoreServerMCP is selected but ServerURL or SavedToken is
	// missing, Render must fail closed and not produce a partial MCP config.
	adapter := defaultOpenCodeAdapter()

	// Missing token.
	_, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetOpenCode,
		Assets:     agentpack.DefaultOperationalAssets(),
		Components: []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
		ServerURL:  "https://lore.example",
		SavedToken: "",
	})
	if err == nil {
		t.Fatal("Render with empty token: want error, got nil")
	}

	// Missing URL.
	_, err = adapter.Render(context.Background(), RenderRequest{
		Target:     TargetOpenCode,
		Assets:     agentpack.DefaultOperationalAssets(),
		Components: []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
		ServerURL:  "",
		SavedToken: "some-token",
	})
	if err == nil {
		t.Fatal("Render with empty URL: want error, got nil")
	}
}

// --- Phase 3: narrow opencode.json merge for mcp.lore ---

func TestOpenCodeMergePreservesUnrelatedMCPEntries(t *testing.T) {
	// When opencode.json already has unrelated mcp.* entries (e.g., a custom
	// stdio plugin), mergeOpenCodeJSON must preserve them and add the managed
	// mcp.lore entry without clobbering the unrelated keys.
	desiredLoreBlock, err := renderOpenCodeLoreBlockWithMCP(agentconfig.DefaultConfig(), false)
	if err != nil {
		t.Fatalf("renderOpenCodeLoreBlockWithMCP() error = %v, want nil", err)
	}
	desiredMCPPayload, err := renderOpenCodeMCPConfig(agentconfig.DefaultConfig(), "https://lore.example/v1/mcp", "secret-token")
	if err != nil {
		t.Fatalf("renderOpenCodeMCPConfig() error = %v, want nil", err)
	}
	// Build a combined desired payload that includes both lore and mcp.
	desiredMap := make(map[string]any)
	if err := json.Unmarshal(desiredLoreBlock, &desiredMap); err != nil {
		t.Fatalf("lore block is not valid JSON: %v", err)
	}
	var mcpBlock map[string]any
	if err := json.Unmarshal(desiredMCPPayload, &mcpBlock); err != nil {
		t.Fatalf("mcp payload is not valid JSON: %v", err)
	}
	for k, v := range mcpBlock {
		desiredMap[k] = v
	}
	desiredBytes, err := json.MarshalIndent(desiredMap, "", "  ")
	if err != nil {
		t.Fatalf("marshal combined desired: %v", err)
	}

	existing := []byte(`{"theme":"midnight","mcp":{"custom_plugin":{"type":"stdio","command":"custom"}}}`)
	merged, err := mergeOpenCodeJSON(existing, append(desiredBytes, '\n'))
	if err != nil {
		t.Fatalf("mergeOpenCodeJSON() error = %v, want nil", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(merged, &parsed); err != nil {
		t.Fatalf("merged result is not valid JSON: %v", err)
	}

	// Unrelated top-level user key must be preserved.
	if parsed["theme"] == nil {
		t.Fatalf("merged keys = %v, want preserved theme key", mapKeys(parsed))
	}

	// mcp block must exist.
	mcpObj, ok := parsed[opencodeMCPBlockKey].(map[string]any)
	if !ok {
		t.Fatalf("mcp value = %T, want map", parsed[opencodeMCPBlockKey])
	}

	// Unrelated mcp.custom_plugin must be preserved.
	if mcpObj["custom_plugin"] == nil {
		t.Fatalf("mcp sub-keys = %v, want preserved custom_plugin", mapKeys(mcpObj))
	}

	// Managed mcp.lore must be added.
	if mcpObj["lore"] == nil {
		t.Fatalf("mcp sub-keys = %v, want managed lore entry", mapKeys(mcpObj))
	}
	loreEntry, ok := mcpObj["lore"].(map[string]any)
	if !ok {
		t.Fatalf("mcp.lore = %T, want map", mcpObj["lore"])
	}
	if got, want := loreEntry["type"], "remote"; got != want {
		t.Fatalf("mcp.lore.type = %v, want %q", got, want)
	}
}

func TestOpenCodeMergeFailsClosedForAmbiguousMCPLoreOwnership(t *testing.T) {
	// When opencode.json already has mcp.lore with an ambiguous (non-remote or
	// incomplete) entry, mergeOpenCodeJSON must fail closed and reject the merge
	// rather than silently overwriting an unknown configuration.
	desiredLoreBlock, _ := renderOpenCodeLoreBlock(agentconfig.DefaultConfig(), false)
	desiredMCPPayload, _ := renderOpenCodeMCPConfig(agentconfig.DefaultConfig(), "https://lore.example/v1/mcp", "secret-token")
	desiredMap := make(map[string]any)
	json.Unmarshal(desiredLoreBlock, &desiredMap)
	var mcpBlock map[string]any
	json.Unmarshal(desiredMCPPayload, &mcpBlock)
	for k, v := range mcpBlock {
		desiredMap[k] = v
	}
	desiredBytes, _ := json.MarshalIndent(desiredMap, "", "  ")

	tests := []struct {
		name     string
		existing []byte
		wantErr  string
	}{
		{
			name:     "stdio type is not managed",
			existing: []byte(`{"lore":{"managed_by":"lore-cli","agents":{}},"mcp":{"lore":{"type":"stdio","command":"lore-mcp"}}}`),
			wantErr:  "ambiguous mcp.lore ownership",
		},
		{
			name:     "remote without Bearer header is not managed",
			existing: []byte(`{"lore":{"managed_by":"lore-cli","agents":{}},"mcp":{"lore":{"type":"remote","url":"https://lore.example/v1/mcp"}}}`),
			wantErr:  "ambiguous mcp.lore ownership",
		},
		{
			name:     "remote with empty URL is not managed",
			existing: []byte(`{"lore":{"managed_by":"lore-cli","agents":{}},"mcp":{"lore":{"type":"remote","headers":{"Authorization":"Bearer tok"}}}}`),
			wantErr:  "ambiguous mcp.lore ownership",
		},
		{
			name:     "non-object mcp.lore is ambiguous",
			existing: []byte(`{"lore":{"managed_by":"lore-cli","agents":{}},"mcp":{"lore":"string-not-object"}}`),
			wantErr:  "ambiguous mcp.lore ownership",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mergeOpenCodeJSON(tt.existing, append(desiredBytes, '\n'))
			if err == nil {
				t.Fatalf("mergeOpenCodeJSON() error = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("mergeOpenCodeJSON() error = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestOpenCodeMergeAcceptsAlreadyManagedMCPLore(t *testing.T) {
	// When opencode.json already has a recognizably-managed mcp.lore entry
	// (remote type, non-empty URL, Bearer auth), mergeOpenCodeJSON must accept
	// it and overwrite with the new desired content rather than failing.
	desiredLoreBlock, _ := renderOpenCodeLoreBlock(agentconfig.DefaultConfig(), false)
	desiredMCPPayload, _ := renderOpenCodeMCPConfig(agentconfig.DefaultConfig(), "https://lore.example/v1/mcp", "new-token")
	desiredMap := make(map[string]any)
	json.Unmarshal(desiredLoreBlock, &desiredMap)
	var mcpBlock map[string]any
	json.Unmarshal(desiredMCPPayload, &mcpBlock)
	for k, v := range mcpBlock {
		desiredMap[k] = v
	}
	desiredBytes, _ := json.MarshalIndent(desiredMap, "", "  ")

	existing := []byte(`{"lore":{"managed_by":"lore-cli","agents":{}},"mcp":{"lore":{"type":"remote","url":"https://lore.example/v1/mcp","enabled":true,"headers":{"Authorization":"Bearer old-token"}}}}`)
	merged, err := mergeOpenCodeJSON(existing, append(desiredBytes, '\n'))
	if err != nil {
		t.Fatalf("mergeOpenCodeJSON() error = %v, want nil (already managed mcp.lore should be accepted)", err)
	}

	var parsed map[string]any
	json.Unmarshal(merged, &parsed)
	mcpObj := parsed[opencodeMCPBlockKey].(map[string]any)
	loreEntry := mcpObj["lore"].(map[string]any)
	// New token must be present.
	auth := loreEntry["headers"].(map[string]any)["Authorization"].(string)
	if !strings.Contains(auth, "new-token") {
		t.Fatalf("mcp.lore.headers.Authorization = %q, want updated token", auth)
	}
}

func TestOpenCodeMergeIdempotentRerun(t *testing.T) {
	// A second merge using the same desired content must produce the same
	// merged output; the second call must not mutate the mcp.lore entry
	// (idempotent rerun).
	desiredLoreBlock, _ := renderOpenCodeLoreBlock(agentconfig.DefaultConfig(), false)
	desiredMCPPayload, _ := renderOpenCodeMCPConfig(agentconfig.DefaultConfig(), "https://lore.example/v1/mcp", "secret-token")
	desiredMap := make(map[string]any)
	json.Unmarshal(desiredLoreBlock, &desiredMap)
	var mcpBlock map[string]any
	json.Unmarshal(desiredMCPPayload, &mcpBlock)
	for k, v := range mcpBlock {
		desiredMap[k] = v
	}
	desiredBytes, _ := json.MarshalIndent(desiredMap, "", "  ")

	existing := []byte(`{"theme":"dark","mcp":{"custom_plugin":{"type":"stdio","command":"custom"}}}`)
	first, err := mergeOpenCodeJSON(existing, append(desiredBytes, '\n'))
	if err != nil {
		t.Fatalf("first mergeOpenCodeJSON() error = %v, want nil", err)
	}

	// Second merge (simulating a rerun) must produce identical output.
	second, err := mergeOpenCodeJSON(first, append(desiredBytes, '\n'))
	if err != nil {
		t.Fatalf("second mergeOpenCodeJSON() error = %v, want nil", err)
	}

	if string(first) != string(second) {
		t.Fatalf("idempotent rerun: second output != first output\nfirst:  %s\nsecond: %s", first, second)
	}

	var parsed map[string]any
	json.Unmarshal(second, &parsed)
	mcpObj := parsed[opencodeMCPBlockKey].(map[string]any)

	// mcp.lore must still be present.
	if mcpObj["lore"] == nil {
		t.Fatalf("mcp sub-keys = %v, want lore entry after idempotent rerun", mapKeys(mcpObj))
	}
	// Unrelated entry still preserved.
	if mcpObj["custom_plugin"] == nil {
		t.Fatalf("mcp sub-keys = %v, want custom_plugin after idempotent rerun", mapKeys(mcpObj))
	}
}

func TestOpenCodeMergePreservesExistingLoreBlock(t *testing.T) {
	// mergeOpenCodeJSON must preserve an existing managed lore block when merging
	// a new desired payload that also includes lore.
	desiredLoreBlock, _ := renderOpenCodeLoreBlock(agentconfig.DefaultConfig(), false)
	desiredMCPPayload, _ := renderOpenCodeMCPConfig(agentconfig.DefaultConfig(), "https://lore.example/v1/mcp", "secret-token")
	desiredMap := make(map[string]any)
	json.Unmarshal(desiredLoreBlock, &desiredMap)
	var mcpBlock map[string]any
	json.Unmarshal(desiredMCPPayload, &mcpBlock)
	for k, v := range mcpBlock {
		desiredMap[k] = v
	}
	desiredBytes, _ := json.MarshalIndent(desiredMap, "", "  ")

	existing := []byte(`{"theme":"light","lore":{"managed_by":"lore-cli","agents":{"sdd-apply":{"model":"gpt-4.1"}}}}`)
	merged, err := mergeOpenCodeJSON(existing, append(desiredBytes, '\n'))
	if err != nil {
		t.Fatalf("mergeOpenCodeJSON() error = %v, want nil", err)
	}

	var parsed map[string]any
	json.Unmarshal(merged, &parsed)

	// Unrelated top-level key preserved.
	if parsed["theme"] == nil {
		t.Fatalf("merged top-level keys = %v, want preserved theme", mapKeys(parsed))
	}

	// lore block updated with desired content (managed_by preserved).
	loreObj := parsed["lore"].(map[string]any)
	if got, want := loreObj["managed_by"], "lore-cli"; got != want {
		t.Fatalf("lore.managed_by = %v, want %q", got, want)
	}

	// mcp.lore added.
	mcpObj := parsed[opencodeMCPBlockKey].(map[string]any)
	if mcpObj["lore"] == nil {
		t.Fatalf("mcp sub-keys = %v, want lore entry", mapKeys(mcpObj))
	}
}

// --- Phase 1: opencode-sdd-assets component wiring tests ---

func TestOpenCodeSDDAssetsComponentOmittedByDefault(t *testing.T) {
	// opencode-sdd-assets is optional and must NOT appear in default OpenCode selection.
	homeDir := t.TempDir()
	service := Service{AgentConfigStore: &fakeAgentConfigStore{
		path: filepath.Join(homeDir, ".lore", "agent-config.json"),
		cfg:  agentconfig.DefaultConfig(),
	}}

	plan, err := service.PlanOpenCodeInstall(InstallRequest{
		HomeDir:        homeDir,
		Target:         TargetOpenCode,
		Components:     nil, // default selection
		LoreCLIVersion: "v0.4.2",
	})
	if err != nil {
		t.Fatalf("PlanOpenCodeInstall error: %v", err)
	}

	// Default selection must NOT include opencode-sdd-assets.
	for _, comp := range plan.Components {
		if comp == ComponentOpenCodeSDDAssets {
			t.Fatalf("plan.Components = %v, want NO opencode-sdd-assets in default selection", plan.Components)
		}
	}

	// Verify the component still renders without error (fail-closed paths).
	adapter := defaultOpenCodeAdapter()
	files, err := adapter.Render(context.Background(), RenderRequest{
		Target:    TargetOpenCode,
		Assets:    agentpack.DefaultOperationalAssets(),
		Components: []ComponentID{ComponentCorePack, ComponentOpenCodeSDDAssets},
	})
	if err != nil {
		t.Fatalf("Render with opencode-sdd-assets error = %v, want nil (fail-closed but no panic)", err)
	}
	if len(files) == 0 {
		t.Fatalf("Render with opencode-sdd-assets produced no files, want empty list (fail-closed)")
	}
}

func TestOpenCodeSDDAssetsComponentExplicitlySelectable(t *testing.T) {
	// opencode-sdd-assets can be explicitly selected and is recognized by the adapter.
	adapter := defaultOpenCodeAdapter()

	// Adapter must support the component.
	if !adapter.Supports(ComponentOpenCodeSDDAssets) {
		t.Fatalf("adapter.Supports(ComponentOpenCodeSDDAssets) = false, want true")
	}

	// Verify the capability is registered.
	caps := adapter.Capabilities()
	cap, ok := caps[CapabilityOpenCodeSDDAssets]
	if !ok {
		t.Fatalf("adapter.Capabilities() missing CapabilityOpenCodeSDDAssets, got %v", caps)
	}
	if cap.Component != ComponentOpenCodeSDDAssets {
		t.Fatalf("cap.Component = %q, want %q", cap.Component, ComponentOpenCodeSDDAssets)
	}
	if !cap.Optional {
		t.Fatalf("cap.Optional = %v, want true", cap.Optional)
	}

	// Render with explicit component selection must produce command and prompt files.
	files, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetOpenCode,
		Assets:     agentpack.DefaultOperationalAssets(),
		Components: []ComponentID{ComponentCorePack, ComponentOpenCodeSDDAssets},
	})
	if err != nil {
		t.Fatalf("Render with ComponentOpenCodeSDDAssets error = %v, want nil", err)
	}

	// Phase 2 implemented: must produce command files.
	commandCount := 0
	promptCount := 0
	for _, f := range files {
		if strings.Contains(f.RelativePath, "commands/sdd-") {
			commandCount++
		}
		if strings.Contains(f.RelativePath, "prompts/sdd") {
			promptCount++
		}
	}
	if commandCount == 0 {
		t.Fatalf("Render with ComponentOpenCodeSDDAssets: no command files, want 9 sdd-*.md; got %v", sortedRenderedPaths(files))
	}
	if commandCount != 9 {
		t.Fatalf("Render command count = %d, want 9", commandCount)
	}
	if promptCount == 0 {
		t.Fatalf("Render with ComponentOpenCodeSDDAssets: no prompt files, want prompts/sdd/ content; got %v", sortedRenderedPaths(files))
	}
}

// --- Phase 2: SDD assets content rendering tests ---

func TestOpenCodeSDDAssetsRendersAllNineCommandFiles(t *testing.T) {
	// Phase 2: when ComponentOpenCodeSDDAssets is selected, all 9 canonical SDD
	// command files must be rendered at commands/sdd-{phase}.md.
	adapter := defaultOpenCodeAdapter()
	files, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetOpenCode,
		Assets:     agentpack.DefaultOperationalAssets(),
		Components: []ComponentID{ComponentCorePack, ComponentOpenCodeSDDAssets},
	})
	if err != nil {
		t.Fatalf("Render with ComponentOpenCodeSDDAssets error = %v, want nil", err)
	}

	// Collect command file paths.
	commandFiles := make(map[string]bool)
	for _, f := range files {
		if strings.Contains(f.RelativePath, "commands/") {
			commandFiles[f.RelativePath] = true
		}
	}

	// All 9 canonical SDD phases must be present.
	// Phase IDs from agentpack: init, explore, proposal, spec, design, tasks, apply, verify, archive
	// The approved canonical name for the proposal phase is "sdd-propose", not "sdd-proposal".
	wantPhases := []string{"sdd-init", "sdd-explore", "sdd-propose", "sdd-spec", "sdd-design", "sdd-tasks", "sdd-apply", "sdd-verify", "sdd-archive"}
	for _, phase := range wantPhases {
		wantPath := "commands/" + phase + ".md"
		if !commandFiles[wantPath] {
			t.Fatalf("rendered command files missing %q, got %v", wantPath, sortedRenderedPaths(files))
		}
	}

	// sdd-proposal.md must NOT exist (wrong name).
	if commandFiles["commands/sdd-proposal.md"] {
		t.Fatalf("rendered command files contain sdd-proposal.md (wrong name), want sdd-propose.md only")
	}
}

func TestOpenCodeSDDAssetsCommandFilesHavePhaseFrontmatter(t *testing.T) {
	// Each command file must have valid frontmatter with name, description, and trigger.
	// The canonical name for the proposal phase is "sdd-propose", not "sdd-proposal".
	adapter := defaultOpenCodeAdapter()
	files, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetOpenCode,
		Assets:     agentpack.DefaultOperationalAssets(),
		Components: []ComponentID{ComponentCorePack, ComponentOpenCodeSDDAssets},
	})
	if err != nil {
		t.Fatalf("Render error = %v, want nil", err)
	}

	for _, f := range files {
		if !strings.Contains(f.RelativePath, "commands/sdd-") {
			continue
		}
		content := string(f.Content)

		// Frontmatter must start with ---
		if !strings.HasPrefix(strings.TrimSpace(content), "---") {
			t.Fatalf("command file %q: content must start with frontmatter ---, got %q", f.RelativePath, content[:min(50, len(content))])
		}

		// Must have name field (canonical sdd-propose, not sdd-proposal)
		if !strings.Contains(content, "name: sdd-") {
			t.Fatalf("command file %q: missing 'name: sdd-*' frontmatter field", f.RelativePath)
		}

		// sdd-propose.md must have correct frontmatter name
		if strings.Contains(f.RelativePath, "sdd-propose.md") {
			if !strings.Contains(content, "name: sdd-propose") {
				t.Fatalf("command file %q: must have 'name: sdd-propose' frontmatter (canonical name)", f.RelativePath)
			}
		}

		// Must have description field
		if !strings.Contains(content, "description:") {
			t.Fatalf("command file %q: missing 'description:' frontmatter field", f.RelativePath)
		}

		// Must have trigger phrase in description
		if !strings.Contains(content, "Trigger:") {
			t.Fatalf("command file %q: missing 'Trigger:' phrase in description", f.RelativePath)
		}

		// Must reference SDD in the content (the command is for SDD)
		if !strings.Contains(content, "SDD") && !strings.Contains(content, "sdd-") {
			t.Fatalf("command file %q: content must reference SDD phase context", f.RelativePath)
		}
	}
}

func TestOpenCodeSDDAssetsCommandFilesNoBannedPhrases(t *testing.T) {
	// Command content must not contain banned phrases: gentle-orchestrator,
	// runtime claims, TUI, plugins, profiles, bootstrap, package-manager.
	adapter := defaultOpenCodeAdapter()
	files, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetOpenCode,
		Assets:     agentpack.DefaultOperationalAssets(),
		Components: []ComponentID{ComponentCorePack, ComponentOpenCodeSDDAssets},
	})
	if err != nil {
		t.Fatalf("Render error = %v, want nil", err)
	}

	bannedPhrases := []string{
		"gentle-orchestrator",
		"runtime subagent",
		"native subagent",
		"tui plugin",
		"profile variant",
		"bootstrap package-manager",
		"package manager behavior",
	}

	for _, f := range files {
		if !strings.Contains(f.RelativePath, "commands/sdd-") {
			continue
		}
		content := strings.ToLower(string(f.Content))
		for _, phrase := range bannedPhrases {
			if strings.Contains(content, phrase) {
				t.Fatalf("command file %q: banned phrase %q found in content", f.RelativePath, phrase)
			}
		}
	}
}

func TestOpenCodeSDDAssetsRendersPerPhasePromptFiles(t *testing.T) {
	// Phase 2 repair: SDD prompt assets must be rendered as per-phase files under
	// prompts/sdd/sdd-*.md, not a single system-prompt-guidance.md file.
	adapter := defaultOpenCodeAdapter()
	files, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetOpenCode,
		Assets:     agentpack.DefaultOperationalAssets(),
		Components: []ComponentID{ComponentCorePack, ComponentOpenCodeSDDAssets},
	})
	if err != nil {
		t.Fatalf("Render error = %v, want nil", err)
	}

	// Collect prompt file paths.
	promptFiles := make(map[string]bool)
	for _, f := range files {
		if strings.Contains(f.RelativePath, "prompts/sdd/") {
			promptFiles[f.RelativePath] = true
		}
	}

	// Must have 9 per-phase prompt files (sdd-init through sdd-archive).
	// The canonical name for the proposal phase is "sdd-propose", not "sdd-proposal".
	wantPromptPhases := []string{"sdd-init", "sdd-explore", "sdd-propose", "sdd-spec", "sdd-design", "sdd-tasks", "sdd-apply", "sdd-verify", "sdd-archive"}
	foundCount := 0
	for _, phase := range wantPromptPhases {
		wantPath := "prompts/sdd/" + phase + ".md"
		if promptFiles[wantPath] {
			foundCount++
		}
	}
	if foundCount == 0 {
		t.Fatalf("rendered files = %v, want per-phase prompt files under prompts/sdd/", sortedRenderedPaths(files))
	}
	if foundCount != 9 {
		t.Fatalf("per-phase prompt files found = %d, want 9 (sdd-init through sdd-archive); got %v", foundCount, sortedRenderedPaths(files))
	}

	// sdd-propose.md prompt must exist (canonical name).
	if !promptFiles["prompts/sdd/sdd-propose.md"] {
		t.Fatalf("rendered files = %v, want prompts/sdd/sdd-propose.md (canonical name)", sortedRenderedPaths(files))
	}

	// system-prompt-guidance.md must NOT exist (single file approach replaced by per-phase)
	if promptFiles["prompts/sdd/system-prompt-guidance.md"] {
		t.Fatalf("rendered files contain prompts/sdd/system-prompt-guidance.md (old single-file approach), want per-phase files only")
	}

	// Each per-phase prompt must have frontmatter and bounded content.
	for _, f := range files {
		if !strings.Contains(f.RelativePath, "prompts/sdd/sdd-") {
			continue
		}
		content := string(f.Content)

		// Must have frontmatter
		if !strings.HasPrefix(strings.TrimSpace(content), "---") {
			t.Fatalf("prompt file %q: content must start with frontmatter ---", f.RelativePath)
		}

		// Must have name field
		if !strings.Contains(content, "name: ") {
			t.Fatalf("prompt file %q: missing 'name:' frontmatter field", f.RelativePath)
		}

		// Must contain Key Boundaries section (bounded content check)
		if !strings.Contains(content, "Key Boundaries") {
			t.Fatalf("prompt file %q: missing 'Key Boundaries' section", f.RelativePath)
		}

		// Must reference OpenCode or Lore context
		if !strings.Contains(content, "OpenCode") && !strings.Contains(content, "Lore") {
			t.Fatalf("prompt file %q: content must reference OpenCode or Lore context", f.RelativePath)
		}

		// No banned phrases — "gentle-orchestrator" is the exact forbidden substring.
		// "orchestrator" alone is allowed; it appears in legitimate SDD phase references.
		// "gentle-orchestrator" is the specific banned phrase combining Gentle with orchestrator.
		safeContent := strings.ToLower(content)
		for _, phrase := range []string{"gentle-orchestrator", "runtime subagent", "native subagent"} {
			if strings.Contains(safeContent, phrase) {
				t.Fatalf("prompt file %q: banned phrase %q found", f.RelativePath, phrase)
			}
		}
	}
}

func TestOpenCodeSDDAssetsPromptsInertNoOpencodeJSONWiring(t *testing.T) {
	// Prompt assets are inert install-time content; no opencode.json wiring.
	adapter := defaultOpenCodeAdapter()
	files, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetOpenCode,
		Assets:     agentpack.DefaultOperationalAssets(),
		Components: []ComponentID{ComponentCorePack, ComponentOpenCodeSDDAssets},
	})
	if err != nil {
		t.Fatalf("Render error = %v, want nil", err)
	}

	// Prompt files must NOT modify opencode.json (no opencode.json in paths).
	for _, f := range files {
		if strings.Contains(f.RelativePath, "prompts/") {
			if f.RelativePath == "opencode.json" || strings.Contains(f.RelativePath, "opencode.json") {
				t.Fatalf("prompt file %q: prompts must not produce opencode.json", f.RelativePath)
			}
		}
	}
}

func TestOpenCodeSDDAssetsPromptsBoundedToOpenCodeLoreContext(t *testing.T) {
	// Prompt content must be bounded to OpenCode/Lore context, not generic.
	adapter := defaultOpenCodeAdapter()
	files, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetOpenCode,
		Assets:     agentpack.DefaultOperationalAssets(),
		Components: []ComponentID{ComponentCorePack, ComponentOpenCodeSDDAssets},
	})
	if err != nil {
		t.Fatalf("Render error = %v, want nil", err)
	}

	for _, f := range files {
		if !strings.Contains(f.RelativePath, "prompts/sdd") {
			continue
		}
		content := string(f.Content)

		// Prompt must mention OpenCode/Lore bounded context
		if !strings.Contains(content, "OpenCode") && !strings.Contains(content, "Lore") {
			t.Fatalf("prompt file %q: content must reference OpenCode or Lore context", f.RelativePath)
		}

		// Prompt must mention orchestrator
		if !strings.Contains(content, "orchestrator") && !strings.Contains(content, "Orchestrator") {
			t.Fatalf("prompt file %q: content must reference orchestrator behavior", f.RelativePath)
		}
	}
}
