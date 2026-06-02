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
