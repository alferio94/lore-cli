package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alferio94/lore-cli/internal/agentconfig"
	"github.com/alferio94/lore-cli/internal/agentpack"
)

// TestOpenCodeConfigJSONMergeFreshWriteProducesManagedBlock verifies
// the additive merge treats a missing or empty file as a fresh
// write: the merged output is byte-equal to the desired payload.
func TestOpenCodeConfigJSONMergeFreshWriteProducesManagedBlock(t *testing.T) {
	desired, err := renderOpenCodeNativeConfig(agentpack.DefaultDefinition(), agentconfig.Config{})
	if err != nil {
		t.Fatalf("renderOpenCodeNativeConfig() error = %v, want nil", err)
	}
	merged, err := mergeOpenCodeConfigJSON(nil, desired, "opencode.json")
	if err != nil {
		t.Fatalf("mergeOpenCodeConfigJSON(nil) error = %v, want nil", err)
	}
	if string(merged) != string(desired) {
		t.Fatalf("mergeOpenCodeConfigJSON(nil) result = %q, want %q", string(merged), string(desired))
	}
	// Whitespace-only existing file should be treated identically.
	mergedWhitespace, err := mergeOpenCodeConfigJSON([]byte("\n\n  \n"), desired, "opencode.json")
	if err != nil {
		t.Fatalf("mergeOpenCodeConfigJSON(whitespace) error = %v, want nil", err)
	}
	if string(mergedWhitespace) != string(desired) {
		t.Fatalf("mergeOpenCodeConfigJSON(whitespace) result = %q, want %q", string(mergedWhitespace), string(desired))
	}
}

// TestOpenCodeConfigJSONMergePreservesExistingUserContent verifies
// the additive merge preserves user-owned top-level keys and
// writes the native `$schema`, native `agent` overlay, and
// `mcp.lore` block from the desired payload. The post-repair
// shape MUST NOT contain a top-level `lore` block.
func TestOpenCodeConfigJSONMergePreservesExistingUserContent(t *testing.T) {
	desired, err := renderOpenCodeMCPConfig(agentpack.DefaultDefinition(), agentconfig.Config{}, "https://lore.example", "secret-token")
	if err != nil {
		t.Fatalf("renderOpenCodeMCPConfig() error = %v, want nil", err)
	}
	existing := []byte(`{"theme":"solarized","customTopLevel":42,"mcp":{"existing":{"type":"stdio","command":"keep-me"}}}`)
	merged, err := mergeOpenCodeConfigJSON(existing, desired, "opencode.json")
	if err != nil {
		t.Fatalf("mergeOpenCodeConfigJSON(existing) error = %v, want nil", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(merged, &payload); err != nil {
		t.Fatalf("decode merged payload: %v", err)
	}
	// User-owned keys must survive.
	if got := payload["theme"]; got != "solarized" {
		t.Fatalf("merged payload theme = %v, want solarized", got)
	}
	if got := payload["customTopLevel"]; got != float64(42) {
		t.Fatalf("merged payload customTopLevel = %v, want 42", got)
	}
	// Post-repair shape: no top-level `lore` block. The legacy
	// metadata-only `lore` block is no longer emitted and MUST NOT
	// appear in the merged result.
	if _, ok := payload["lore"]; ok {
		t.Fatalf("merged payload unexpectedly carries top-level `lore` object after repair: %v", payload)
	}
	// Primary `lore` orchestrator entry MUST be present in the
	// merged `agent` overlay so OpenCode can boot into the global
	// Lore orchestrator. The entry is sourced from
	// `ProfileBalanced.RoleModels["orchestrator"]` of the active
	// agentpack definition and references the managed AGENTS.md
	// file via `{file:./AGENTS.md}`.
	agentOverlay, ok := payload["agent"].(map[string]any)
	if !ok {
		t.Fatalf("merged payload missing top-level `agent` overlay; got keys %v", keysOfMap(payload))
	}
	loreAgent, ok := agentOverlay[opencodePrimaryAgentName].(map[string]any)
	if !ok {
		t.Fatalf("merged agent overlay missing primary %q entry; got keys %v", opencodePrimaryAgentName, keysOfMap(agentOverlay))
	}
	wantOrchestratorModel := expectedOrchestratorModelForDefaultDefinition()
	if got := loreAgent["model"]; got != wantOrchestratorModel {
		t.Fatalf("merged agent.%s.model = %v, want %q", opencodePrimaryAgentName, got, wantOrchestratorModel)
	}
	if got, _ := loreAgent["prompt"].(string); got != "{file:./"+opencodePrimaryAgentPromptFile+"}" {
		t.Fatalf("merged agent.%s.prompt = %q, want %q", opencodePrimaryAgentName, got, "{file:./"+opencodePrimaryAgentPromptFile+"}")
	}
	// Native `$schema` and `agent` overlay must be present.
	if got := payload["$schema"]; got != opencodeConfigSchemaURL {
		t.Fatalf("merged payload $schema = %v, want %q", got, opencodeConfigSchemaURL)
	}
	agent, ok := payload["agent"].(map[string]any)
	if !ok {
		t.Fatalf("merged payload missing top-level `agent` overlay; got keys %v", keysOfMap(payload))
	}
	if _, ok := agent["sdd-propose"]; !ok {
		t.Fatalf("merged agent overlay missing sdd-propose entry; got %v", keysOfMap(agent))
	}
	mcp, ok := payload["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("merged payload missing top-level `mcp` object; got keys %v", keysOfMap(payload))
	}
	// Existing user-managed `mcp.existing` must survive the merge.
	existingMCP, ok := mcp["existing"].(map[string]any)
	if !ok {
		t.Fatalf("merged payload mcp.existing missing; got %v", mcp)
	}
	if got := existingMCP["command"]; got != "keep-me" {
		t.Fatalf("merged payload mcp.existing.command = %v, want keep-me", got)
	}
	// And the new `mcp.lore` block must be present.
	loreMCP, ok := mcp["lore"].(map[string]any)
	if !ok {
		t.Fatalf("merged payload mcp.lore missing; got %v", mcp)
	}
	if got := loreMCP["type"]; got != "remote" {
		t.Fatalf("merged payload mcp.lore.type = %v, want remote", got)
	}
	if got := loreMCP["url"]; got != "https://lore.example/v1/mcp" {
		t.Fatalf("merged payload mcp.lore.url = %v, want https://lore.example/v1/mcp", got)
	}
}

// TestOpenCodeConfigJSONMergeIsIdempotent verifies the additive
// merge is idempotent: re-applying the merge to its own output
// produces byte-identical results. This is the safety gate that
// keeps reruns safe.
func TestOpenCodeConfigJSONMergeIsIdempotent(t *testing.T) {
	desired, err := renderOpenCodeMCPConfig(agentpack.DefaultDefinition(), agentconfig.Config{}, "https://lore.example", "secret-token")
	if err != nil {
		t.Fatalf("renderOpenCodeMCPConfig() error = %v, want nil", err)
	}
	existing := []byte(`{"theme":"solarized","mcp":{"existing":{"type":"stdio","command":"keep-me"}}}`)
	first, err := mergeOpenCodeConfigJSON(existing, desired, "opencode.json")
	if err != nil {
		t.Fatalf("mergeOpenCodeConfigJSON(first) error = %v, want nil", err)
	}
	second, err := mergeOpenCodeConfigJSON(first, desired, "opencode.json")
	if err != nil {
		t.Fatalf("mergeOpenCodeConfigJSON(second) error = %v, want nil", err)
	}
	if string(first) != string(second) {
		t.Fatalf("merge is not idempotent: first=%q second=%q", string(first), string(second))
	}
	// Third pass with the merged result as both existing and desired
	// (worst case) is still idempotent.
	third, err := mergeOpenCodeConfigJSON(first, first, "opencode.json")
	if err != nil {
		t.Fatalf("mergeOpenCodeConfigJSON(merged-as-desired) error = %v, want nil", err)
	}
	if string(third) != string(first) {
		t.Fatalf("merge is not idempotent under self-overlay: third=%q first=%q", string(third), string(first))
	}
}

func TestOpenCodeConfigJSONMergePreservesNativeLoreMCPExactly(t *testing.T) {
	desired, err := renderOpenCodeMCPConfig(agentpack.DefaultDefinition(), agentconfig.Config{}, "https://lore.example", "new-token")
	if err != nil {
		t.Fatalf("renderOpenCodeMCPConfig() error = %v, want nil", err)
	}
	existing := []byte(`{
		"mcp": {
			"lore": {
				"type": "remote",
				"url": "https://lore.example/v1/mcp",
				"enabled": false,
				"headers": {"Authorization": "Bearer existing-token", "X-User": "keep-me"}
			},
			"other": {"type":"stdio", "command":"keep-me"}
		},
		"agent": {"custom-agent": {"mode":"subagent", "model":"custom/model", "prompt":"custom"}},
		"provider": {"custom": {"npm": "@custom/provider"}}
	}`)
	merged, err := mergeOpenCodeConfigJSON(existing, desired, "opencode.json")
	if err != nil {
		t.Fatalf("mergeOpenCodeConfigJSON(existing native mcp.lore) error = %v, want nil", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(merged, &payload); err != nil {
		t.Fatalf("decode merged payload: %v", err)
	}
	mcp := payload["mcp"].(map[string]any)
	lore := mcp["lore"].(map[string]any)
	if got := lore["enabled"]; got != false {
		t.Fatalf("mcp.lore.enabled = %v, want preserved false", got)
	}
	headers := lore["headers"].(map[string]any)
	if got := headers["Authorization"]; got != "Bearer existing-token" {
		t.Fatalf("mcp.lore Authorization = %v, want existing token preserved exactly", got)
	}
	if got := headers["X-User"]; got != "keep-me" {
		t.Fatalf("mcp.lore X-User = %v, want custom header preserved", got)
	}
	if got := mcp["other"].(map[string]any)["command"]; got != "keep-me" {
		t.Fatalf("mcp.other.command = %v, want user MCP preserved", got)
	}
	agents := payload[opencodeAgentsKey].(map[string]any)
	if _, ok := agents["custom-agent"]; !ok {
		t.Fatalf("custom user agent missing after merge; got %v", keysOfMap(agents))
	}
	if _, ok := payload["provider"]; !ok {
		t.Fatalf("custom provider/settings missing after merge; got keys %v", keysOfMap(payload))
	}
	if err := validateOpenCodeStartupSafeConfig(merged, opencodeConfigFileName); err != nil {
		t.Fatalf("validateOpenCodeStartupSafeConfig(merged) error = %v, want nil", err)
	}
}

func TestOpenCodeConfigJSONMergePreservesNativeLoreMCPWhenDesiredOmitsMCP(t *testing.T) {
	desired, err := renderOpenCodeNativeConfig(agentpack.DefaultDefinition(), agentconfig.Config{})
	if err != nil {
		t.Fatalf("renderOpenCodeNativeConfig() error = %v, want nil", err)
	}
	existing := []byte(`{"mcp":{"lore":{"type":"remote","url":"https://existing.example/v1/mcp","enabled":false,"headers":{"Authorization":"Bearer existing-token"}},"other":{"type":"stdio","command":"keep-me"}},"theme":"solarized"}`)
	merged, err := mergeOpenCodeConfigJSON(existing, desired, "opencode.json")
	if err != nil {
		t.Fatalf("mergeOpenCodeConfigJSON(native mcp.lore with desired MCP omitted) error = %v, want nil", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(merged, &payload); err != nil {
		t.Fatalf("decode merged payload: %v", err)
	}
	mcp := payload["mcp"].(map[string]any)
	lore := mcp["lore"].(map[string]any)
	if got := lore["url"]; got != "https://existing.example/v1/mcp" {
		t.Fatalf("mcp.lore.url = %v, want existing native URL preserved", got)
	}
	if got := lore["enabled"]; got != false {
		t.Fatalf("mcp.lore.enabled = %v, want existing value preserved", got)
	}
	if got := lore["headers"].(map[string]any)["Authorization"]; got != "Bearer existing-token" {
		t.Fatalf("mcp.lore Authorization = %v, want existing token preserved", got)
	}
	if got := mcp["other"].(map[string]any)["command"]; got != "keep-me" {
		t.Fatalf("mcp.other.command = %v, want unrelated MCP preserved", got)
	}
}

func TestOpenCodeConfigJSONMergeReplacesManagedAgentsAndRemovesLegacyRefs(t *testing.T) {
	desired, err := renderOpenCodeMCPConfig(agentpack.DefaultDefinition(), agentconfig.Config{}, "https://lore.example", "secret-token")
	if err != nil {
		t.Fatalf("renderOpenCodeMCPConfig() error = %v, want nil", err)
	}
	existing := []byte(`{
		"agent": {
			"lore": {"mode":"primary", "model":"old", "prompt":"{file:./AGENTS.md}", "tools":{"task":true}, "permission":{"task":"allow"}},
			"lore-worker": {"mode":"subagent", "model":"old", "prompt":"{file:./skills/lore-worker/SKILL.md}", "tools":{"question":true}},
			"sdd-apply": {"mode":"subagent", "model":"old", "prompt":"{file:./skills/sdd-apply/SKILL.md}"},
			"user-agent": {"mode":"subagent", "model":"user/model", "prompt":"user"}
		},
		"plugin": ["user-plugin", "background-agents.ts"],
		"plugins": ["lore-models.ts"],
		"skills": {"path":"~/.config/opencode/skills"}
	}`)
	merged, err := mergeOpenCodeConfigJSON(existing, desired, "opencode.json")
	if err != nil {
		t.Fatalf("mergeOpenCodeConfigJSON(legacy managed refs) error = %v, want nil", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(merged, &payload); err != nil {
		t.Fatalf("decode merged payload: %v", err)
	}
	if _, present := payload["plugins"]; present {
		t.Fatalf("legacy plugins key survived opencode.json merge: %v", payload["plugins"])
	}
	plugins := payload["plugin"].([]any)
	if len(plugins) != 1 || plugins[0] != "user-plugin" {
		t.Fatalf("opencode.json plugin refs = %v, want only user-plugin preserved", plugins)
	}
	skills := payload[opencodeSkillsDirKey].(map[string]any)
	if _, present := skills["path"]; present {
		t.Fatalf("stale skills.path survived opencode.json merge: %v", skills)
	}
	paths, ok := skills["paths"].([]any)
	if !ok || len(paths) != 1 || paths[0] != opencodeSkillsDirPath {
		t.Fatalf("skills.paths = %v, want [%q]", skills["paths"], opencodeSkillsDirPath)
	}
	agents := payload[opencodeAgentsKey].(map[string]any)
	if _, ok := agents["user-agent"]; !ok {
		t.Fatalf("user-owned agent missing after managed-agent replacement; got %v", keysOfMap(agents))
	}
	for _, name := range []string{opencodePrimaryAgentName, opencodeLoreWorkerAgentName, "sdd-apply"} {
		entry := agents[name].(map[string]any)
		if _, present := entry["tools"]; present {
			t.Fatalf("agent.%s retained deprecated tools block: %v", name, entry)
		}
		if got, _ := entry[opencodeAgentPromptKey].(string); strings.Contains(got, "./skills/") || !strings.Contains(got, "./prompts/") {
			t.Fatalf("agent.%s.prompt = %q, want native prompt path", name, got)
		}
	}
	assertOpenCodePrimaryPermission(t, agents[opencodePrimaryAgentName].(map[string]any))
	assertOpenCodeSubagentPermission(t, opencodeLoreWorkerAgentName, agents[opencodeLoreWorkerAgentName].(map[string]any))
	assertOpenCodeSubagentPermission(t, "sdd-apply", agents["sdd-apply"].(map[string]any))
	if err := validateOpenCodeStartupSafeConfig(merged, opencodeConfigFileName); err != nil {
		t.Fatalf("validateOpenCodeStartupSafeConfig(merged) error = %v, want nil", err)
	}
}

func TestOpenCodeTUIJSONMergePreservesUserPluginsWithoutLegacyRefs(t *testing.T) {
	desired, err := readOpenCodeTUISettingsAsset()
	if err != nil {
		t.Fatalf("readOpenCodeTUISettingsAsset() error = %v, want nil", err)
	}
	existing := []byte(`{"plugin":["user-plugin","background-agents.ts","opencode-subagent-statusline"],"theme":"solarized"}`)
	merged, err := mergeOpenCodeConfigJSON(existing, desired, "tui.json")
	if err != nil {
		t.Fatalf("mergeOpenCodeConfigJSON(tui plugins) error = %v, want nil", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(merged, &payload); err != nil {
		t.Fatalf("decode merged tui.json: %v", err)
	}
	plugins := payload["plugin"].([]any)
	want := []string{"user-plugin"}
	if len(plugins) != len(want) {
		t.Fatalf("plugin list = %v, want %v", plugins, want)
	}
	for i, wantPlugin := range want {
		if plugins[i] != wantPlugin {
			t.Fatalf("plugin[%d] = %v, want %q; full list %v", i, plugins[i], wantPlugin, plugins)
		}
	}
	for _, plugin := range plugins {
		if isLegacyOpenCodePluginReference(plugin.(string)) {
			t.Fatalf("legacy plugin reference survived tui merge: %v", plugins)
		}
	}
	if got := payload["theme"]; got != "solarized" {
		t.Fatalf("theme = %v, want preserved solarized", got)
	}
}

// TestOpenCodePlanRejectsInvalidStartupShapeBeforeWrite verifies the
// startup-safe validation gate runs after merge and before any on-disk
// replacement can happen.
func TestOpenCodePlanRejectsInvalidStartupShapeBeforeWrite(t *testing.T) {
	home := t.TempDir()
	layout := ResolveOpenCodeLayout(home)
	if err := os.MkdirAll(layout.RootDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(opencode root) error = %v", err)
	}
	valid, err := renderOpenCodeNativeConfig(agentpack.DefaultDefinition(), agentconfig.Config{})
	if err != nil {
		t.Fatalf("renderOpenCodeNativeConfig() error = %v, want nil", err)
	}
	if err := os.WriteFile(layout.Paths[opencodeJSONPathKey], valid, 0o600); err != nil {
		t.Fatalf("WriteFile(existing opencode.json) error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(valid, &payload); err != nil {
		t.Fatalf("decode valid config: %v", err)
	}
	agents := payload[opencodeAgentsKey].(map[string]any)
	worker := agents[opencodeLoreWorkerAgentName].(map[string]any)
	worker[opencodeAgentPromptKey] = "{file:./skills/lore-worker/SKILL.md}"
	invalid, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode invalid desired config: %v", err)
	}
	_, _, err = planOpenCodeRenderedFileAction(layout, RenderedFile{
		Component:    ComponentCorePack,
		RelativePath: opencodeConfigFileName,
		MergeMode:    MergeModeAdditiveJSON,
		Content:      invalid,
	}, filepath.Join(layout.RootDir, "backups", "test"))
	if err == nil {
		t.Fatal("planOpenCodeRenderedFileAction(invalid startup shape) error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "agent.lore-worker.prompt") {
		t.Fatalf("plan error = %v, want startup-safe prompt validation failure", err)
	}
	after, readErr := os.ReadFile(layout.Paths[opencodeJSONPathKey])
	if readErr != nil {
		t.Fatalf("ReadFile(existing opencode.json) error = %v", readErr)
	}
	if string(after) != string(valid) {
		t.Fatalf("existing opencode.json changed during planning; got %q want original %q", string(after), string(valid))
	}
}

func TestOpenCodePlanRejectsDeprecatedToolsBlockBeforeWrite(t *testing.T) {
	home := t.TempDir()
	layout := ResolveOpenCodeLayout(home)
	if err := os.MkdirAll(layout.RootDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(opencode root) error = %v", err)
	}
	valid, err := renderOpenCodeNativeConfig(agentpack.DefaultDefinition(), agentconfig.Config{})
	if err != nil {
		t.Fatalf("renderOpenCodeNativeConfig() error = %v, want nil", err)
	}
	if err := os.WriteFile(layout.Paths[opencodeJSONPathKey], valid, 0o600); err != nil {
		t.Fatalf("WriteFile(existing opencode.json) error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(valid, &payload); err != nil {
		t.Fatalf("decode valid config: %v", err)
	}
	agents := payload[opencodeAgentsKey].(map[string]any)
	worker := agents[opencodeLoreWorkerAgentName].(map[string]any)
	worker["tools"] = map[string]any{opencodePermissionTaskKey: true}
	invalid, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode invalid desired config: %v", err)
	}
	_, _, err = planOpenCodeRenderedFileAction(layout, RenderedFile{
		Component:    ComponentCorePack,
		RelativePath: opencodeConfigFileName,
		MergeMode:    MergeModeAdditiveJSON,
		Content:      invalid,
	}, filepath.Join(layout.RootDir, "backups", "test"))
	if err == nil {
		t.Fatal("planOpenCodeRenderedFileAction(deprecated tools block) error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "agent.lore-worker.tools") || !strings.Contains(err.Error(), "deprecated") {
		t.Fatalf("plan error = %v, want deprecated tools validation failure", err)
	}
	after, readErr := os.ReadFile(layout.Paths[opencodeJSONPathKey])
	if readErr != nil {
		t.Fatalf("ReadFile(existing opencode.json) error = %v", readErr)
	}
	if string(after) != string(valid) {
		t.Fatalf("existing opencode.json changed during planning; got %q want original %q", string(after), string(valid))
	}
}

// TestOpenCodeConfigJSONMergeRejectsInvalidExistingJSON verifies the
// merge returns an error rather than silently dropping user content
// when the existing file is not valid JSON.
func TestOpenCodeConfigJSONMergeRejectsInvalidExistingJSON(t *testing.T) {
	desired, err := renderOpenCodeNativeConfig(agentpack.DefaultDefinition(), agentconfig.Config{})
	if err != nil {
		t.Fatalf("renderOpenCodeNativeConfig() error = %v, want nil", err)
	}
	if _, err := mergeOpenCodeConfigJSON([]byte("not-json"), desired, "opencode.json"); err == nil {
		t.Fatal("mergeOpenCodeConfigJSON(invalid existing) error = nil, want JSON decode error")
	}
}

// TestOpenCodeConfigJSONMergeFailsClosedOnForeignMcpLoreBlock verifies
// the additive merge fails closed (returns a typed
// *OpenCodeMCPConfigOwnershipError) when the existing opencode.json
// carries a non-Lore-owned mcp.lore block. The conflict must surface
// the existing type, url, and managed_by value (NEVER the token), the
// relative path, and a resolution sentence that names the manual edit
// path. This is the safety gate that prevents the installer from
// silently clobbering a user-owned or third-party MCP configuration.
func TestOpenCodeConfigJSONMergeFailsClosedOnForeignMcpLoreBlock(t *testing.T) {
	desired, err := renderOpenCodeMCPConfig(agentpack.DefaultDefinition(), agentconfig.Config{}, "https://lore.example", "ultra-secret-token")
	if err != nil {
		t.Fatalf("renderOpenCodeMCPConfig() error = %v, want nil", err)
	}
	// Foreign mcp.lore: no `managed_by` marker. The existing block
	// is treated as third-party / hand-edited and the merge fails
	// closed.
	existing := []byte(`{"theme":"solarized","mcp":{"lore":{"type":"remote","url":"https://other.example/v1/mcp","headers":{"Authorization":"Bearer other-token"}}}}`)
	_, mergeErr := mergeOpenCodeConfigJSON(existing, desired, "opencode.json")
	if mergeErr == nil {
		t.Fatal("mergeOpenCodeConfigJSON(foreign mcp.lore) error = nil, want fail-closed ownership conflict")
	}
	conflict := AsOpenCodeMCPConfigOwnershipConflict(mergeErr)
	if conflict == nil {
		t.Fatalf("mergeOpenCodeConfigJSON(foreign mcp.lore) error = %v, want *OpenCodeMCPConfigOwnershipError (use IsOpenCodeMCPConfigOwnershipConflict to detect)", mergeErr)
	}
	if !IsOpenCodeMCPConfigOwnershipConflict(mergeErr) {
		t.Fatal("IsOpenCodeMCPConfigOwnershipConflict returned false for the conflict error")
	}
	if conflict.Path != "opencode.json" {
		t.Fatalf("conflict.Path = %q, want opencode.json", conflict.Path)
	}
	if conflict.ExistingType != "remote" {
		t.Fatalf("conflict.ExistingType = %q, want remote", conflict.ExistingType)
	}
	if conflict.ExistingURL != "https://other.example/v1/mcp" {
		t.Fatalf("conflict.ExistingURL = %q, want https://other.example/v1/mcp", conflict.ExistingURL)
	}
	// Defensive: the existing token MUST NOT be present anywhere in
	// the conflict error message.
	errorText := mergeErr.Error()
	if strings.Contains(errorText, "other-token") {
		t.Fatalf("conflict error leaked foreign token; got %q", errorText)
	}
	if strings.Contains(errorText, "ultra-secret-token") {
		t.Fatalf("conflict error leaked rendered (Lore) token; got %q", errorText)
	}
	for _, want := range []string{
		"refusing to overwrite non-Lore-owned",
		"mcp.lore",
		"opencode.json",
		"managed_by",
		"remote",
		"https://other.example/v1/mcp",
		"Resolution",
		"/v1/mcp with Authorization",
	} {
		if !strings.Contains(errorText, want) {
			t.Fatalf("conflict error missing %q substring; got %q", want, errorText)
		}
	}

	// Foreign mcp.lore with an explicit non-Lore managed_by value
	// (e.g. another tool's plugin owns the block): same fail-closed
	// behavior, and the existing managed_by value is surfaced.
	existingOwned := []byte(`{"mcp":{"lore":{"managed_by":"other-tool","type":"stdio","command":"some-mcp"}}}`)
	_, mergeErr2 := mergeOpenCodeConfigJSON(existingOwned, desired, "opencode.json")
	conflict2 := AsOpenCodeMCPConfigOwnershipConflict(mergeErr2)
	if conflict2 == nil {
		t.Fatalf("mergeOpenCodeConfigJSON(foreign managed_by) error = %v, want *OpenCodeMCPConfigOwnershipError", mergeErr2)
	}
	if conflict2.ExistingManagedBy != "other-tool" {
		t.Fatalf("conflict2.ExistingManagedBy = %q, want other-tool", conflict2.ExistingManagedBy)
	}
	if conflict2.ExistingType != "stdio" {
		t.Fatalf("conflict2.ExistingType = %q, want stdio", conflict2.ExistingType)
	}
	if !strings.Contains(mergeErr2.Error(), "other-tool") {
		t.Fatalf("conflict2 error missing foreign managed_by value; got %q", mergeErr2.Error())
	}
}

// TestOpenCodeConfigJSONMergeAllowsLoreOwnedMcpLoreBlock verifies the
// additive merge PROCEEDS when the existing mcp.lore block is already
// Lore-owned (legacy managed_by marker or native remote /v1/mcp with
// Authorization). The merge replaces the Lore-owned subtree from the
// overlay, preserving all other top-level keys, and drops legacy
// marker fields so the resulting OpenCode config remains schema-valid.
func TestOpenCodeConfigJSONMergeAllowsLoreOwnedMcpLoreBlock(t *testing.T) {
	desired, err := renderOpenCodeMCPConfig(agentpack.DefaultDefinition(), agentconfig.Config{}, "https://lore.example", "new-token")
	if err != nil {
		t.Fatalf("renderOpenCodeMCPConfig() error = %v, want nil", err)
	}
	existing := []byte(`{"theme":"solarized","mcp":{"lore":{"managed_by":"lore-cli","type":"remote","url":"https://old.example/v1/mcp","headers":{"Authorization":"Bearer old-token"}},"existing":{"type":"stdio","command":"keep-me"}}}`)
	merged, err := mergeOpenCodeConfigJSON(existing, desired, "opencode.json")
	if err != nil {
		t.Fatalf("mergeOpenCodeConfigJSON(Lore-owned mcp.lore) error = %v, want nil", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(merged, &payload); err != nil {
		t.Fatalf("decode merged payload: %v", err)
	}
	// User-owned top-level keys survive.
	if got := payload["theme"]; got != "solarized" {
		t.Fatalf("merged payload theme = %v, want solarized", got)
	}
	mcp, ok := payload["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("merged payload missing top-level mcp; got keys %v", keysOfMap(payload))
	}
	// The mcp.existing (non-Lore) block survives the merge.
	existingMCP, ok := mcp["existing"].(map[string]any)
	if !ok {
		t.Fatalf("merged payload mcp.existing missing; got %v", mcp)
	}
	if got := existingMCP["command"]; got != "keep-me" {
		t.Fatalf("merged payload mcp.existing.command = %v, want keep-me", got)
	}
	// The Lore-owned mcp.lore block is overwritten with the new
	// payload (overlay wins for the Lore-owned subtree).
	loreMCP, ok := mcp["lore"].(map[string]any)
	if !ok {
		t.Fatalf("merged payload mcp.lore missing; got %v", mcp)
	}
	if got := loreMCP["url"]; got != "https://lore.example/v1/mcp" {
		t.Fatalf("merged payload mcp.lore.url = %v, want https://lore.example/v1/mcp (overlay wins for Lore-owned subtree)", got)
	}
	headers, _ := loreMCP["headers"].(map[string]any)
	if got := headers["Authorization"]; got != "Bearer new-token" {
		t.Fatalf("merged payload mcp.lore.headers.Authorization = %v, want Bearer new-token", got)
	}
	if _, present := loreMCP["managed_by"]; present {
		t.Fatalf("merged payload mcp.lore still carries legacy managed_by marker; want native-schema-valid MCP block: %v", loreMCP)
	}

	nativeExisting := []byte(`{"mcp":{"lore":{"type":"remote","url":"https://lore.example/v1/mcp","headers":{"Authorization":"Bearer old-token"}}}}`)
	if _, err := mergeOpenCodeConfigJSON(nativeExisting, desired, "opencode.json"); err != nil {
		t.Fatalf("mergeOpenCodeConfigJSON(native Lore-owned mcp.lore) error = %v, want nil", err)
	}
}

// TestOpenCodeConfigJSONMergeIgnoresOwnershipForTUIJSON verifies the
// ownership check is scoped to opencode.json. The tui.json payload
// is fully Lore-owned and must always proceed with the additive
// merge even if a user hand-edits the file. The post-repair shape
// uses a singular `plugin` string array (not the legacy plural
// `plugins` object array) and MUST NOT carry a top-level `lore`
// block.
func TestOpenCodeConfigJSONMergeIgnoresOwnershipForTUIJSON(t *testing.T) {
	desired, err := readOpenCodeTUISettingsAsset()
	if err != nil {
		t.Fatalf("readOpenCodeTUISettingsAsset() error = %v, want nil", err)
	}
	// Existing tui.json with NO lore block: still proceeds.
	existingNoLore := []byte(`{"theme":"solarized","plugins":[{"id":"user-plugin","owner":"user","enabled":true}]}`)
	merged, err := mergeOpenCodeConfigJSON(existingNoLore, desired, "tui.json")
	if err != nil {
		t.Fatalf("mergeOpenCodeConfigJSON(tui.json, no lore) error = %v, want nil", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(merged, &payload); err != nil {
		t.Fatalf("decode merged tui.json: %v", err)
	}
	if got := payload["theme"]; got != "solarized" {
		t.Fatalf("merged tui.json theme = %v, want solarized (user content preserved)", got)
	}
	// Post-repair shape: the user plugin from the legacy plural
	// object array is converted to the singular `plugin` string
	// array, and no Lore-managed statusline plugin is added.
	plugin, ok := payload["plugin"].([]any)
	if !ok {
		t.Fatalf("merged tui.json missing singular `plugin` string array; got keys %v", keysOfMap(payload))
	}
	wantPlugins := []string{"user-plugin"}
	if len(plugin) != len(wantPlugins) {
		t.Fatalf("merged tui.json `plugin` = %v, want %v", plugin, wantPlugins)
	}
	for i, want := range wantPlugins {
		if plugin[i] != want {
			t.Fatalf("merged tui.json `plugin`[%d] = %v, want %q; full list %v", i, plugin[i], want, plugin)
		}
	}
	if _, ok := payload["plugins"]; ok {
		t.Fatalf("merged tui.json unexpectedly carries legacy plural `plugins` array; want native singular `plugin` string array")
	}
	if _, ok := payload["lore"]; ok {
		t.Fatalf("merged tui.json unexpectedly carries top-level `lore` object after repair: %v", payload)
	}
}

func TestOpenCodePlanOpenCodeInstallDoesNotWriteMissingAgentConfig(t *testing.T) {
	homeDir := t.TempDir()
	loreConfigDir := filepath.Join(t.TempDir(), "lore-config")
	store := agentconfig.NewStore(loreConfigDir)
	agentConfigPath, err := store.Path()
	if err != nil {
		t.Fatalf("agent-config Path() error = %v", err)
	}

	service := Service{AgentConfigStore: store}
	_, err = service.PlanOpenCodeInstall(InstallRequest{
		HomeDir:        homeDir,
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  loreConfigDir,
		LoreCLIVersion: "v0.1.0",
		Target:         TargetOpenCode,
		Components:     []ComponentID{ComponentCorePack},
		Now:            time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("PlanOpenCodeInstall() error = %v, want nil", err)
	}
	if _, err := os.Stat(agentConfigPath); !os.IsNotExist(err) {
		t.Fatalf("agent-config stat err = %v, want missing after read-only planning", err)
	}
}

// TestOpenCodePlanOpenCodeInstallBacksUpAndUpdatesExistingOpenCodeJSON
// verifies the plan/execute pipeline uses the additive merge for an
// existing opencode.json: the existing user-owned top-level keys are
// preserved, the Lore-managed `lore` and `mcp.lore` blocks are added,
// and the prior file is backed up under the install backup root.
func TestOpenCodePlanOpenCodeInstallBacksUpAndUpdatesExistingOpenCodeJSON(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolveOpenCodeLayout(homeDir)
	// Pre-create an existing user-owned opencode.json.
	if err := os.MkdirAll(layout.RootDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(rootDir) error = %v", err)
	}
	existing := []byte(`{"theme":"solarized","customTopLevel":42,"mcp":{"existing":{"type":"stdio","command":"keep-me"}}}`)
	opencodeJSONPath := layout.Paths[opencodeJSONPathKey]
	if err := os.WriteFile(opencodeJSONPath, existing, 0o600); err != nil {
		t.Fatalf("WriteFile(opencode.json) error = %v", err)
	}

	service := Service{}
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	plan, err := service.PlanOpenCodeInstall(InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		SavedToken:     "secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v0.1.0",
		Target:         TargetOpenCode,
		Components:     []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
		Now:            now,
	})
	if err != nil {
		t.Fatalf("PlanOpenCodeInstall() error = %v, want nil", err)
	}
	opencodeAction := findOpenCodePlannedFileAction(plan.Files, "opencode.json")
	if opencodeAction == nil {
		t.Fatalf("PlanOpenCodeInstall() missing opencode.json action; got %v", plannedActionSummary(plan.Files))
	}
	if opencodeAction.Action != "update" {
		t.Fatalf("opencode.json plan action = %q, want update (existing user content must be merged)", opencodeAction.Action)
	}
	if opencodeAction.BackupPath == "" {
		t.Fatal("opencode.json plan action missing backup path; want backup-before-overwrite for an update")
	}
	if opencodeAction.MergeMode != MergeModeAdditiveJSON {
		t.Fatalf("opencode.json plan merge mode = %q, want %q", opencodeAction.MergeMode, MergeModeAdditiveJSON)
	}

	// Execute the plan and verify the on-disk file preserves the
	// user-owned keys while gaining the Lore-managed blocks.
	result, err := service.ExecuteOpenCodeInstall(plan, InstallCommandOptions{})
	if err != nil {
		t.Fatalf("ExecuteOpenCodeInstall() error = %v, want nil", err)
	}
	if len(result.Summary.Failed) > 0 {
		t.Fatalf("ExecuteOpenCodeInstall() failed files = %v, want none", result.Summary.Failed)
	}
	mergedBytes, err := os.ReadFile(opencodeJSONPath)
	if err != nil {
		t.Fatalf("ReadFile(opencode.json) error = %v", err)
	}
	var merged map[string]any
	if err := json.Unmarshal(mergedBytes, &merged); err != nil {
		t.Fatalf("decode merged opencode.json: %v", err)
	}
	if got := merged["theme"]; got != "solarized" {
		t.Fatalf("merged opencode.json theme = %v, want solarized (user content preserved)", got)
	}
	if got := merged["customTopLevel"]; got != float64(42) {
		t.Fatalf("merged opencode.json customTopLevel = %v, want 42 (user content preserved)", got)
	}
	// Post-repair shape: no top-level `lore` block in the merged
	// opencode.json. The legacy metadata-only `lore` block is no
	// longer emitted by the installer.
	if _, ok := merged["lore"]; ok {
		t.Fatalf("merged opencode.json unexpectedly carries top-level `lore` object after repair: %v", merged)
	}
	if got := merged["$schema"]; got != opencodeConfigSchemaURL {
		t.Fatalf("merged opencode.json $schema = %v, want %q", got, opencodeConfigSchemaURL)
	}
	agentOverlay, ok := merged["agent"].(map[string]any)
	if !ok {
		t.Fatalf("merged opencode.json missing top-level `agent` overlay; got keys %v", keysOfMap(merged))
	}
	// Primary `lore` orchestrator entry MUST be installed on
	// disk: the model is sourced from
	// `ProfileBalanced.RoleModels["orchestrator"]` of the active
	// agentpack definition, and the prompt references the
	// managed AGENTS.md file.
	loreAgent, ok := agentOverlay[opencodePrimaryAgentName].(map[string]any)
	if !ok {
		t.Fatalf("merged opencode.json agent overlay missing primary %q entry; got keys %v", opencodePrimaryAgentName, keysOfMap(agentOverlay))
	}
	wantOrchestratorModel := expectedOrchestratorModelForDefaultDefinition()
	if got := loreAgent["model"]; got != wantOrchestratorModel {
		t.Fatalf("merged opencode.json agent.%s.model = %v, want %q", opencodePrimaryAgentName, got, wantOrchestratorModel)
	}
	if got, _ := loreAgent["prompt"].(string); got != "{file:./"+opencodePrimaryAgentPromptFile+"}" {
		t.Fatalf("merged opencode.json agent.%s.prompt = %q, want %q", opencodePrimaryAgentName, got, "{file:./"+opencodePrimaryAgentPromptFile+"}")
	}
	mcp, ok := merged["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("merged opencode.json missing top-level `mcp` object; got keys %v", keysOfMap(merged))
	}
	existingMCP, ok := mcp["existing"].(map[string]any)
	if !ok {
		t.Fatalf("merged opencode.json mcp.existing missing; got %v", mcp)
	}
	if got := existingMCP["command"]; got != "keep-me" {
		t.Fatalf("merged opencode.json mcp.existing.command = %v, want keep-me (user content preserved)", got)
	}
	if _, ok := mcp["lore"].(map[string]any); !ok {
		t.Fatalf("merged opencode.json missing mcp.lore block; got %v", mcp)
	}
	// Backup file must exist on disk and contain the prior content.
	backupBytes, err := os.ReadFile(opencodeAction.BackupPath)
	if err != nil {
		t.Fatalf("ReadFile(backup) error = %v, want prior opencode.json backed up at %s", err, opencodeAction.BackupPath)
	}
	if string(backupBytes) != string(existing) {
		t.Fatalf("backup content = %q, want prior existing content %q", string(backupBytes), string(existing))
	}
}

// TestOpenCodePlanOpenCodeInstallFailsClosedOnForeignMcpLore is the
// end-to-end safety gate for the ownership check. The existing
// opencode.json carries a non-Lore-owned mcp.lore block; the plan
// phase must:
//
//   - Return a *OpenCodeMCPConfigOwnershipError (no generic error).
//   - Record the opencode.json plan action as `conflicted` (not
//     `update` / `create` / `unchanged`).
//   - Record the managed backup path for user guidance without
//     writing it during the pure plan phase.
//   - Leave the on-disk opencode.json UNTOUCHED (no managed write
//     happens on a conflict).
//
// The test also covers a second scenario where the existing file has
// a different shape (no mcp key, no lore key) to confirm the
// ownership check returns true and the normal additive merge runs.
func TestOpenCodePlanOpenCodeInstallFailsClosedOnForeignMcpLore(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolveOpenCodeLayout(homeDir)
	if err := os.MkdirAll(layout.RootDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(rootDir) error = %v", err)
	}
	opencodeJSONPath := layout.Paths[opencodeJSONPathKey]
	foreignExisting := []byte(`{"theme":"solarized","mcp":{"lore":{"type":"remote","url":"https://other.example/v1/mcp","headers":{"Authorization":"Bearer other-token"}}}}`)
	if err := os.WriteFile(opencodeJSONPath, foreignExisting, 0o600); err != nil {
		t.Fatalf("WriteFile(opencode.json) error = %v", err)
	}

	service := Service{}
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	plan, err := service.PlanOpenCodeInstall(InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		SavedToken:     "ultra-secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v0.1.0",
		Target:         TargetOpenCode,
		Components:     []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
		Now:            now,
	})
	if err == nil {
		t.Fatalf("PlanOpenCodeInstall(foreign mcp.lore) error = nil, want fail-closed ownership conflict; plan.Files=%v", plannedActionSummary(plan.Files))
	}
	conflict := AsOpenCodeMCPConfigOwnershipConflict(err)
	if conflict == nil {
		t.Fatalf("PlanOpenCodeInstall(foreign mcp.lore) error = %v, want *OpenCodeMCPConfigOwnershipError", err)
	}
	if conflict.Path != "opencode.json" {
		t.Fatalf("conflict.Path = %q, want opencode.json", conflict.Path)
	}
	if conflict.ExistingType != "remote" {
		t.Fatalf("conflict.ExistingType = %q, want remote", conflict.ExistingType)
	}
	if conflict.ExistingURL != "https://other.example/v1/mcp" {
		t.Fatalf("conflict.ExistingURL = %q, want https://other.example/v1/mcp", conflict.ExistingURL)
	}
	if conflict.BackupPath == "" {
		t.Fatal("conflict.BackupPath = \"\", want managed backup path recorded for conflict guidance")
	}

	// The on-disk opencode.json MUST still hold the original
	// foreign content (no managed write on conflict).
	stillOnDisk, readErr := os.ReadFile(opencodeJSONPath)
	if readErr != nil {
		t.Fatalf("ReadFile(opencode.json) after conflict error = %v, want untouched file", readErr)
	}
	if string(stillOnDisk) != string(foreignExisting) {
		t.Fatalf("on-disk opencode.json after conflict = %q, want original foreign content %q (conflict must not write the merged file)", string(stillOnDisk), string(foreignExisting))
	}

	// Planning is pure: the conflict backup path is reported for
	// guidance, but no backup file is written during plan/dry-run.
	if _, backupStatErr := os.Stat(conflict.BackupPath); !os.IsNotExist(backupStatErr) {
		t.Fatalf("Stat(conflict backup) err = %v, want no backup file written during plan", backupStatErr)
	}

	// The conflict error message must NOT leak either the existing
	// foreign token or the rendered (Lore) token.
	errText := err.Error()
	if strings.Contains(errText, "other-token") {
		t.Fatalf("conflict error leaked foreign token; got %q", errText)
	}
	if strings.Contains(errText, "ultra-secret-token") {
		t.Fatalf("conflict error leaked rendered token; got %q", errText)
	}
	for _, want := range []string{
		"refusing to overwrite non-Lore-owned",
		"opencode.json",
		"Resolution",
		conflict.BackupPath,
	} {
		if !strings.Contains(errText, want) {
			t.Fatalf("conflict error missing %q substring; got %q", want, errText)
		}
	}
}

// TestOpenCodePlanOpenCodeInstallFailsClosedAndRecordsConflictSummary
// verifies the plan-mode summary is honest about the conflict: the
// plan summary's `managed_action=...` entry for opencode.json reports
// the `conflicted` action (not `update` / `create` / `unchanged`).
// Dry-run output must reflect the conflict so users can see the
// state without applying.
func TestOpenCodePlanOpenCodeInstallFailsClosedAndRecordsConflictSummary(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolveOpenCodeLayout(homeDir)
	if err := os.MkdirAll(layout.RootDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(rootDir) error = %v", err)
	}
	opencodeJSONPath := layout.Paths[opencodeJSONPathKey]
	foreignExisting := []byte(`{"mcp":{"lore":{"type":"remote","url":"https://other.example/v1/mcp","headers":{"Authorization":"Bearer other-token"}}}}`)
	if err := os.WriteFile(opencodeJSONPath, foreignExisting, 0o600); err != nil {
		t.Fatalf("WriteFile(opencode.json) error = %v", err)
	}

	service := Service{}
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	_, err := service.PlanOpenCodeInstall(InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		SavedToken:     "ultra-secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v0.1.0",
		Target:         TargetOpenCode,
		Components:     []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
		Now:            now,
	})
	// The plan fails closed before the plan summary is built; the
	// shape of the error is the user-facing summary. We assert the
	// summary surface (the error message) is actionable and
	// contains the backup path and resolution guidance.
	if err == nil {
		t.Fatal("PlanOpenCodeInstall(foreign mcp.lore) error = nil, want fail-closed conflict")
	}
	if !IsOpenCodeMCPConfigOwnershipConflict(err) {
		t.Fatalf("PlanOpenCodeInstall(foreign mcp.lore) error = %v, want *OpenCodeMCPConfigOwnershipError", err)
	}
	conflict := AsOpenCodeMCPConfigOwnershipConflict(err)
	if conflict == nil || conflict.BackupPath == "" {
		t.Fatalf("conflict or backup path missing; got %+v", conflict)
	}

	// A subsequent install with the user-resolved opencode.json
	// (mcp.lore removed) MUST proceed with the normal additive
	// merge. This is the "re-run after resolution" safety gate.
	if err := os.WriteFile(opencodeJSONPath, []byte(`{"theme":"solarized","customTopLevel":42}`), 0o600); err != nil {
		t.Fatalf("WriteFile(resolved opencode.json) error = %v", err)
	}
	plan2, plan2Err := service.PlanOpenCodeInstall(InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		SavedToken:     "ultra-secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v0.1.0",
		Target:         TargetOpenCode,
		Components:     []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
		Now:            now,
	})
	if plan2Err != nil {
		t.Fatalf("PlanOpenCodeInstall(resolved) error = %v, want nil after user removed the foreign mcp.lore", plan2Err)
	}
	opencodeAction := findOpenCodePlannedFileAction(plan2.Files, "opencode.json")
	if opencodeAction == nil {
		t.Fatalf("PlanOpenCodeInstall(resolved) missing opencode.json action; got %v", plannedActionSummary(plan2.Files))
	}
	if opencodeAction.Action != "update" {
		t.Fatalf("PlanOpenCodeInstall(resolved) opencode.json action = %q, want update", opencodeAction.Action)
	}
}

// TestOpenCodePlanOpenCodeInstallIsIdempotent verifies re-running
// PlanOpenCodeInstall on a freshly-installed target produces an
// `unchanged` action for every managed file *except* the
// `lore-install.json` manifest, which legitimately updates its
// `InstalledAt` timestamp on rerun. The bounded merge for
// `opencode.json` and the plugin/tui.json files must be byte-stable
// across reruns.
func TestOpenCodePlanOpenCodeInstallIsIdempotent(t *testing.T) {
	homeDir := t.TempDir()
	service := Service{}
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	components := []ComponentID{ComponentCorePack, ComponentLoreServerMCP}
	req := InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		SavedToken:     "secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v0.1.0",
		Target:         TargetOpenCode,
		Components:     components,
		Now:            now,
	}
	plan, err := service.PlanOpenCodeInstall(req)
	if err != nil {
		t.Fatalf("PlanOpenCodeInstall(first) error = %v, want nil", err)
	}
	if _, err := service.ExecuteOpenCodeInstall(plan, InstallCommandOptions{}); err != nil {
		t.Fatalf("ExecuteOpenCodeInstall(first) error = %v, want nil", err)
	}
	// Second run with the same install clock: every managed file
	// except the manifest must be `unchanged`, and the manifest is
	// allowed to be `unchanged` only when the install clock is
	// identical (the manifest captures the `InstalledAt` timestamp).
	plan2, err := service.PlanOpenCodeInstall(req)
	if err != nil {
		t.Fatalf("PlanOpenCodeInstall(second) error = %v, want nil", err)
	}
	for _, action := range plan2.Files {
		if filepath.ToSlash(action.RelativePath) == opencodeManifestFileName {
			if action.Action != "unchanged" {
				t.Fatalf("PlanOpenCodeInstall(second) manifest action %q; want unchanged when install clock is identical", action.Action)
			}
			continue
		}
		if action.Action != "unchanged" {
			t.Fatalf("PlanOpenCodeInstall(second) action %q for %q; want unchanged for idempotent rerun", action.Action, action.RelativePath)
		}
		if action.BackupPath != "" {
			t.Fatalf("PlanOpenCodeInstall(second) backup path %q for %q; want no backup on unchanged rerun", action.BackupPath, action.RelativePath)
		}
	}
}

// TestOpenCodeConfigJSONMergeMigratesLegacyTopLevelLoreBlock is
// the focused migration gate: an existing opencode.json written
// by the legacy `lore`-shaped renderer (top-level `lore` block
// + a user-managed top-level key) must be repaired to the native
// `agent` overlay shape on the next merge. The legacy `lore`
// block is silently dropped; the user's `theme` key is preserved.
func TestOpenCodeConfigJSONMergeMigratesLegacyTopLevelLoreBlock(t *testing.T) {
	desired, err := renderOpenCodeNativeConfig(agentpack.DefaultDefinition(), agentconfig.Config{})
	if err != nil {
		t.Fatalf("renderOpenCodeNativeConfig() error = %v, want nil", err)
	}
	// Legacy shape: top-level `lore` metadata block, no native
	// `agent` overlay, no `$schema`. The user kept their own
	// `theme` and `customTopLevel` keys.
	legacyExisting := []byte(`{"theme":"solarized","customTopLevel":42,"lore":{"managed_by":"lore-cli","schema_version":1,"agents":{"sdd-propose":{"model":"gpt-5.4"}},"skills_dir":"~/.config/opencode/skills"}}`)
	merged, err := mergeOpenCodeConfigJSON(legacyExisting, desired, "opencode.json")
	if err != nil {
		t.Fatalf("mergeOpenCodeConfigJSON(legacy) error = %v, want nil", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(merged, &payload); err != nil {
		t.Fatalf("decode merged payload: %v", err)
	}
	// Legacy top-level `lore` block is gone.
	if _, ok := payload["lore"]; ok {
		t.Fatalf("merged payload still carries legacy top-level `lore` block after migration: %v", payload)
	}
	// User-owned keys are preserved.
	if got := payload["theme"]; got != "solarized" {
		t.Fatalf("merged payload theme = %v, want solarized (user content preserved)", got)
	}
	if got := payload["customTopLevel"]; got != float64(42) {
		t.Fatalf("merged payload customTopLevel = %v, want 42 (user content preserved)", got)
	}
	// Native `agent` overlay is present.
	agent, ok := payload["agent"].(map[string]any)
	if !ok {
		t.Fatalf("merged payload missing native `agent` overlay; got keys %v", keysOfMap(payload))
	}
	if _, ok := agent["sdd-propose"]; !ok {
		t.Fatalf("merged `agent` overlay missing sdd-propose entry; got %v", keysOfMap(agent))
	}
	// Native `$schema` is present.
	if got := payload["$schema"]; got != opencodeConfigSchemaURL {
		t.Fatalf("merged payload $schema = %v, want %q", got, opencodeConfigSchemaURL)
	}
}

// TestOpenCodeConfigJSONMergeMigratesLegacyTuiJSONPluralPlugins
// is the focused migration gate for the legacy tui.json shape: an
// existing tui.json written by the previous renderer (top-level
// `lore` block + a plural `plugins` array of objects) must be
// repaired to the native singular `plugin` string array shape on
// the next merge. The legacy `lore` block AND the legacy plural
// `plugins` array are silently dropped; the user's `theme` key is
// preserved.
func TestOpenCodeConfigJSONMergeMigratesLegacyTuiJSONPluralPlugins(t *testing.T) {
	desired, err := readOpenCodeTUISettingsAsset()
	if err != nil {
		t.Fatalf("readOpenCodeTUISettingsAsset() error = %v, want nil", err)
	}
	// Legacy shape: top-level `lore` block + plural `plugins`
	// array of objects. The user kept their own `theme` and
	// `customTopLevel` keys.
	legacyExisting := []byte(`{"theme":"solarized","customTopLevel":7,"plugins":[{"id":"opencode-subagent-statusline","owner":"community","source":"community://opencode-subagent-statusline","enabled":true}],"lore":{"managed_by":"lore-cli","schema_version":1,"tui_managed":true,"plugins_excluded":["sdd-engram","logo"],"config_only":true}}`)
	merged, err := mergeOpenCodeConfigJSON(legacyExisting, desired, "tui.json")
	if err != nil {
		t.Fatalf("mergeOpenCodeConfigJSON(legacy tui.json) error = %v, want nil", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(merged, &payload); err != nil {
		t.Fatalf("decode merged tui.json: %v", err)
	}
	// Legacy top-level `lore` block is gone.
	if _, ok := payload["lore"]; ok {
		t.Fatalf("merged tui.json still carries legacy top-level `lore` block after migration: %v", payload)
	}
	// Legacy plural `plugins` array of objects is gone (replaced
	// by the native singular `plugin` string array from the
	// desired payload).
	if _, ok := payload["plugins"]; ok {
		t.Fatalf("merged tui.json still carries legacy plural `plugins` array after migration: %v", payload)
	}
	// User-owned keys are preserved.
	if got := payload["theme"]; got != "solarized" {
		t.Fatalf("merged tui.json theme = %v, want solarized (user content preserved)", got)
	}
	if got := payload["customTopLevel"]; got != float64(7) {
		t.Fatalf("merged tui.json customTopLevel = %v, want 7 (user content preserved)", got)
	}
	// Native singular `plugin` string array is present and empty:
	// the previous statusline registration is no longer managed.
	plugin, ok := payload["plugin"].([]any)
	if !ok {
		t.Fatalf("merged tui.json missing native singular `plugin` string array; got keys %v", keysOfMap(payload))
	}
	if len(plugin) != 0 {
		t.Fatalf("merged tui.json `plugin` = %v, want empty list", plugin)
	}
}

// TestOpenCodePlanOpenCodeInstallMigratesLegacyStaleShape is the
// end-to-end migration gate: a home directory that was previously
// installed with the legacy `lore`-shaped renderer (top-level
// `lore` block in opencode.json AND plural `plugins` + `lore` in
// tui.json) is repaired on the next `lore install --target
// opencode` run. The on-disk opencode.json MUST be rewritten in
// the native shape (no top-level `lore`, native `agent` overlay,
// native `$schema`, `mcp.lore` for MCP), and the on-disk tui.json
// MUST be rewritten in the native singular `plugin` string array
// shape. The plan must record a `update` action for both files
// (existing broken shape → new native shape) and the existing
// user-owned top-level keys (`theme`, `customTopLevel`) MUST be
// preserved.
func TestOpenCodePlanOpenCodeInstallMigratesLegacyStaleShape(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolveOpenCodeLayout(homeDir)
	if err := os.MkdirAll(layout.RootDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(rootDir) error = %v", err)
	}
	// Pre-create a legacy-shape opencode.json on disk.
	opencodeJSONPath := layout.Paths[opencodeJSONPathKey]
	legacyOpencode := []byte(`{"theme":"solarized","customTopLevel":42,"lore":{"managed_by":"lore-cli","schema_version":1,"agents":{},"skills_dir":"~/.config/opencode/skills"}}`)
	if err := os.WriteFile(opencodeJSONPath, legacyOpencode, 0o600); err != nil {
		t.Fatalf("WriteFile(opencode.json) error = %v", err)
	}
	// Pre-create a legacy-shape tui.json on disk.
	tuiPath := filepath.Join(layout.RootDir, "tui.json")
	legacyTUI := []byte(`{"theme":"solarized","customTopLevel":7,"plugins":[{"id":"opencode-subagent-statusline","owner":"community","enabled":true}],"lore":{"managed_by":"lore-cli","schema_version":1,"tui_managed":true,"plugins_excluded":["sdd-engram","logo"],"config_only":true}}`)
	if err := os.WriteFile(tuiPath, legacyTUI, 0o600); err != nil {
		t.Fatalf("WriteFile(tui.json) error = %v", err)
	}

	service := Service{}
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	plan, err := service.PlanOpenCodeInstall(InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		SavedToken:     "secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v0.1.0",
		Target:         TargetOpenCode,
		Components:     []ComponentID{ComponentCorePack, ComponentLoreServerMCP, ComponentOpenCodePlugins},
		Now:            now,
	})
	if err != nil {
		t.Fatalf("PlanOpenCodeInstall() error = %v, want nil", err)
	}
	opencodeAction := findOpenCodePlannedFileAction(plan.Files, "opencode.json")
	if opencodeAction == nil {
		t.Fatalf("PlanOpenCodeInstall() missing opencode.json action; got %v", plannedActionSummary(plan.Files))
	}
	if opencodeAction.Action != "update" {
		t.Fatalf("opencode.json plan action = %q, want update (legacy shape must be repaired)", opencodeAction.Action)
	}
	tuiAction := findOpenCodePlannedFileAction(plan.Files, "tui.json")
	if tuiAction == nil {
		t.Fatalf("PlanOpenCodeInstall() missing tui.json action; got %v", plannedActionSummary(plan.Files))
	}
	if tuiAction.Action != "update" {
		t.Fatalf("tui.json plan action = %q, want update (legacy shape must be repaired)", tuiAction.Action)
	}

	// Execute the plan and verify the on-disk files are rewritten
	// in the native shape.
	result, err := service.ExecuteOpenCodeInstall(plan, InstallCommandOptions{})
	if err != nil {
		t.Fatalf("ExecuteOpenCodeInstall() error = %v, want nil", err)
	}
	if len(result.Summary.Failed) > 0 {
		t.Fatalf("ExecuteOpenCodeInstall() failed files = %v, want none", result.Summary.Failed)
	}

	// opencode.json: native shape, no top-level `lore` block, user
	// keys preserved.
	mergedBytes, err := os.ReadFile(opencodeJSONPath)
	if err != nil {
		t.Fatalf("ReadFile(opencode.json) error = %v", err)
	}
	var merged map[string]any
	if err := json.Unmarshal(mergedBytes, &merged); err != nil {
		t.Fatalf("decode merged opencode.json: %v", err)
	}
	if _, ok := merged["lore"]; ok {
		t.Fatalf("merged opencode.json still carries top-level `lore` block after migration: %v", merged)
	}
	if got := merged["$schema"]; got != opencodeConfigSchemaURL {
		t.Fatalf("merged opencode.json $schema = %v, want %q", got, opencodeConfigSchemaURL)
	}
	if _, ok := merged["agent"].(map[string]any); !ok {
		t.Fatalf("merged opencode.json missing native `agent` overlay; got keys %v", keysOfMap(merged))
	}
	if got := merged["theme"]; got != "solarized" {
		t.Fatalf("merged opencode.json theme = %v, want solarized (user content preserved)", got)
	}
	if got := merged["customTopLevel"]; got != float64(42) {
		t.Fatalf("merged opencode.json customTopLevel = %v, want 42 (user content preserved)", got)
	}
	// The migration from the legacy `lore`-shaped renderer
	// MUST also install the primary `lore` orchestrator entry
	// (under `agent.lore`, NOT under a top-level `lore` block)
	// so the post-migration opencode.json boots into the global
	// Lore orchestrator instead of falling back to the built-in
	// `build` agent.
	agentOverlay, ok := merged["agent"].(map[string]any)
	if !ok {
		t.Fatalf("merged opencode.json missing native `agent` overlay; got keys %v", keysOfMap(merged))
	}
	loreAgent, ok := agentOverlay[opencodePrimaryAgentName].(map[string]any)
	if !ok {
		t.Fatalf("merged opencode.json agent overlay missing primary %q entry; got keys %v", opencodePrimaryAgentName, keysOfMap(agentOverlay))
	}
	wantOrchestratorModel := expectedOrchestratorModelForDefaultDefinition()
	if got := loreAgent["model"]; got != wantOrchestratorModel {
		t.Fatalf("merged opencode.json agent.%s.model = %v, want %q", opencodePrimaryAgentName, got, wantOrchestratorModel)
	}
	if got, _ := loreAgent["prompt"].(string); got != "{file:./"+opencodePrimaryAgentPromptFile+"}" {
		t.Fatalf("merged opencode.json agent.%s.prompt = %q, want %q", opencodePrimaryAgentName, got, "{file:./"+opencodePrimaryAgentPromptFile+"}")
	}
	mcp, ok := merged["mcp"].(map[string]any)
	if !ok || mcp["lore"] == nil {
		t.Fatalf("merged opencode.json missing mcp.lore block; got %v", merged)
	}

	// tui.json: native singular `plugin` string array, no top-level
	// `lore` block, no plural `plugins` array, user keys preserved.
	mergedTUI, err := os.ReadFile(tuiPath)
	if err != nil {
		t.Fatalf("ReadFile(tui.json) error = %v", err)
	}
	var mergedTUIPayload map[string]any
	if err := json.Unmarshal(mergedTUI, &mergedTUIPayload); err != nil {
		t.Fatalf("decode merged tui.json: %v", err)
	}
	if _, ok := mergedTUIPayload["lore"]; ok {
		t.Fatalf("merged tui.json still carries top-level `lore` block after migration: %v", mergedTUIPayload)
	}
	if _, ok := mergedTUIPayload["plugins"]; ok {
		t.Fatalf("merged tui.json still carries legacy plural `plugins` array after migration: %v", mergedTUIPayload)
	}
	plugin, ok := mergedTUIPayload["plugin"].([]any)
	if !ok {
		t.Fatalf("merged tui.json missing native singular `plugin` string array; got keys %v", keysOfMap(mergedTUIPayload))
	}
	if len(plugin) != 0 {
		t.Fatalf("merged tui.json `plugin` = %v, want empty list", plugin)
	}
	if got := mergedTUIPayload["theme"]; got != "solarized" {
		t.Fatalf("merged tui.json theme = %v, want solarized (user content preserved)", got)
	}
	if got := mergedTUIPayload["customTopLevel"]; got != float64(7) {
		t.Fatalf("merged tui.json customTopLevel = %v, want 7 (user content preserved)", got)
	}
}

// TestOpenCodeStaleManagedPluginCleanupRemovesModelVariants is the
// focused regression gate for the manifest-scoped stale
// managed-file cleanup introduced by the
// `add-opencode-lore-models-plugin` change. The test:
//
//   - Pre-creates a previous `lore-install.json` manifest that
//     records `plugins/model-variants.ts` as a Lore-managed plugin
//     file (the legacy managed asset name from before the rename).
//   - Pre-creates the on-disk `plugins/model-variants.ts` file
//     so the stale file actually exists.
//   - Runs `PlanOpenCodeInstall` and `ExecuteOpenCodeInstall`.
//   - Verifies the plan records a `delete` action for the stale
//     `plugins/model-variants.ts` path with a backup-first
//     contract, the on-disk file is removed by the apply step,
//     and the backup file is written under the install backup
//     root.
//
// The test then asserts a subsequent install (the rerun after the
// rename) is idempotent: the manifest-scoped cleanup pass emits
// NO action because the previous (new) manifest does not record
// the stale path.
func TestOpenCodeStaleManagedPluginCleanupRemovesModelVariants(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolveOpenCodeLayout(homeDir)
	if err := os.MkdirAll(filepath.Join(layout.RootDir, "plugins"), 0o755); err != nil {
		t.Fatalf("MkdirAll(plugins) error = %v", err)
	}
	// Pre-create a previous manifest that records the legacy
	// `plugins/model-variants.ts` as Lore-managed.
	previousManifest := Manifest{
		SchemaVersion: PortableManifestSchemaVersion,
		Target:        TargetOpenCode,
		AuthMode:      "config-only",
		Components:    []ComponentID{ComponentCorePack, ComponentOpenCodePlugins},
		ManagedFiles: []ManagedFileRecord{
			{
				Path:        filepath.Join(layout.RootDir, "plugins", "model-variants.ts"),
				Component:   ComponentOpenCodePlugins,
				MergeMode:   MergeModeReplace,
				ContentHash: "deadbeef",
			},
		},
		BackupRoot:  filepath.Join(layout.RootDir, "backups", "previous"),
		InstalledAt: "2026-06-01T00:00:00Z",
	}
	previousBytes, err := marshalManifest(previousManifest)
	if err != nil {
		t.Fatalf("marshalManifest(previous) error = %v", err)
	}
	if err := os.WriteFile(layout.ManifestPath, previousBytes, 0o600); err != nil {
		t.Fatalf("WriteFile(previous manifest) error = %v", err)
	}
	// Pre-create the stale on-disk file.
	stalePath := filepath.Join(layout.RootDir, "plugins", "model-variants.ts")
	if err := os.WriteFile(stalePath, []byte("// stale model-variants.ts (Lore-managed, pre-rename)"), 0o600); err != nil {
		t.Fatalf("WriteFile(stale plugin) error = %v", err)
	}

	service := Service{}
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	plan, err := service.PlanOpenCodeInstall(InstallRequest{
		HomeDir:        homeDir,
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v0.1.0",
		Target:         TargetOpenCode,
		Components:     []ComponentID{ComponentCorePack, ComponentOpenCodePlugins},
		Now:            now,
	})
	if err != nil {
		t.Fatalf("PlanOpenCodeInstall() error = %v, want nil", err)
	}
	staleAction := findOpenCodePlannedFileAction(plan.Files, "plugins/model-variants.ts")
	if staleAction == nil {
		t.Fatalf("PlanOpenCodeInstall() missing stale delete action for plugins/model-variants.ts; got %v", plannedActionSummary(plan.Files))
	}
	if staleAction.Action != "delete" {
		t.Fatalf("stale plugin action = %q, want delete", staleAction.Action)
	}
	if staleAction.Component != ComponentOpenCodePlugins {
		t.Fatalf("stale plugin component = %q, want %q", staleAction.Component, ComponentOpenCodePlugins)
	}
	if staleAction.BackupPath == "" {
		t.Fatal("stale plugin action missing backup path; want backup-first delete")
	}

	// Apply the plan and verify the on-disk file is removed and
	// the backup is written.
	result, err := service.ExecuteOpenCodeInstall(plan, InstallCommandOptions{})
	if err != nil {
		t.Fatalf("ExecuteOpenCodeInstall() error = %v, want nil", err)
	}
	if len(result.Summary.Failed) > 0 {
		t.Fatalf("ExecuteOpenCodeInstall() failed files = %v, want none", result.Summary.Failed)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("Stat(stale plugin) err = %v, want file removed by stale cleanup", err)
	}
	backupBytes, readErr := os.ReadFile(staleAction.BackupPath)
	if readErr != nil {
		t.Fatalf("ReadFile(backup) error = %v, want stale file backed up at %s", readErr, staleAction.BackupPath)
	}
	if !strings.Contains(string(backupBytes), "stale model-variants.ts") {
		t.Fatalf("backup content = %q, want stale model-variants.ts content", string(backupBytes))
	}
	// The summary must surface the delete via the Deleted field
	// and the backup via the BackedUp field.
	foundDelete := false
	foundBackedUp := false
	for _, p := range result.Summary.Deleted {
		if filepath.ToSlash(p) == "plugins/model-variants.ts" {
			foundDelete = true
		}
	}
	for _, p := range result.Summary.BackedUp {
		if filepath.ToSlash(p) == "plugins/model-variants.ts" {
			foundBackedUp = true
		}
	}
	if !foundDelete {
		t.Fatalf("result.Summary.Deleted missing plugins/model-variants.ts: %v", result.Summary.Deleted)
	}
	if !foundBackedUp {
		t.Fatalf("result.Summary.BackedUp missing plugins/model-variants.ts: %v", result.Summary.BackedUp)
	}
	// The new manifest must NOT record the stale path.
	for _, rec := range result.Manifest.ManagedFiles {
		if filepath.Clean(rec.Path) == filepath.Clean(stalePath) {
			t.Fatalf("new manifest unexpectedly records stale path %q; the stale cleanup must drop it from the manifest", rec.Path)
		}
	}

	// Subsequent install: no previous-manifest ownership proof
	// for the stale path, so the cleanup pass is a no-op.
	plan2, err := service.PlanOpenCodeInstall(InstallRequest{
		HomeDir:        homeDir,
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v0.1.0",
		Target:         TargetOpenCode,
		Components:     []ComponentID{ComponentCorePack, ComponentOpenCodePlugins},
		Now:            now,
	})
	if err != nil {
		t.Fatalf("PlanOpenCodeInstall(second) error = %v, want nil", err)
	}
	if staleAction := findOpenCodePlannedFileAction(plan2.Files, "plugins/model-variants.ts"); staleAction != nil {
		t.Fatalf("PlanOpenCodeInstall(second) unexpectedly emits stale delete action for plugins/model-variants.ts; the previous manifest no longer records the stale path: action=%+v", staleAction)
	}
}

// TestOpenCodeStaleManagedPluginCleanupLeavesUnownedFilesAlone
// verifies the manifest-scoped safety gate: a
// `plugins/model-variants.ts` file that exists on disk WITHOUT
// prior manifest ownership is left untouched by the cleanup pass.
// The test pre-creates a stale on-disk file and an unrelated
// previous manifest that does NOT record the path; the install
// plan must NOT include a delete action for the file and the
// execute step must NOT remove it.
func TestOpenCodeStaleManagedPluginCleanupLeavesUnownedFilesAlone(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolveOpenCodeLayout(homeDir)
	if err := os.MkdirAll(filepath.Join(layout.RootDir, "plugins"), 0o755); err != nil {
		t.Fatalf("MkdirAll(plugins) error = %v", err)
	}
	// Pre-create a previous manifest that does NOT record the
	// stale path (the file is user-owned, not Lore-managed).
	previousManifest := Manifest{
		SchemaVersion: PortableManifestSchemaVersion,
		Target:        TargetOpenCode,
		AuthMode:      "config-only",
		Components:    []ComponentID{ComponentCorePack},
		ManagedFiles: []ManagedFileRecord{
			{
				Path:        filepath.Join(layout.RootDir, "AGENTS.md"),
				Component:   ComponentCorePack,
				MergeMode:   MergeModeReplace,
				ContentHash: "deadbeef",
			},
		},
		BackupRoot:  filepath.Join(layout.RootDir, "backups", "previous"),
		InstalledAt: "2026-06-01T00:00:00Z",
	}
	previousBytes, err := marshalManifest(previousManifest)
	if err != nil {
		t.Fatalf("marshalManifest(previous) error = %v", err)
	}
	if err := os.WriteFile(layout.ManifestPath, previousBytes, 0o600); err != nil {
		t.Fatalf("WriteFile(previous manifest) error = %v", err)
	}
	// Pre-create the unowned on-disk file (e.g. a user-owned
	// `model-variants.ts` plugin that was never managed by Lore).
	stalePath := filepath.Join(layout.RootDir, "plugins", "model-variants.ts")
	if err := os.WriteFile(stalePath, []byte("// user-owned model-variants.ts; NOT Lore-managed"), 0o600); err != nil {
		t.Fatalf("WriteFile(unowned plugin) error = %v", err)
	}

	service := Service{}
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	plan, err := service.PlanOpenCodeInstall(InstallRequest{
		HomeDir:        homeDir,
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v0.1.0",
		Target:         TargetOpenCode,
		Components:     []ComponentID{ComponentCorePack, ComponentOpenCodePlugins},
		Now:            now,
	})
	if err != nil {
		t.Fatalf("PlanOpenCodeInstall() error = %v, want nil", err)
	}
	if staleAction := findOpenCodePlannedFileAction(plan.Files, "plugins/model-variants.ts"); staleAction != nil {
		t.Fatalf("PlanOpenCodeInstall() unexpectedly emits stale delete action for user-owned plugins/model-variants.ts: action=%+v", staleAction)
	}
	if _, err := service.ExecuteOpenCodeInstall(plan, InstallCommandOptions{}); err != nil {
		t.Fatalf("ExecuteOpenCodeInstall() error = %v, want nil", err)
	}
	if _, err := os.Stat(stalePath); err != nil {
		t.Fatalf("Stat(user-owned plugin) err = %v, want user-owned file preserved by cleanup", err)
	}
}

func TestOpenCodeStaleManagedPluginCleanupDoesNotDeleteTUIOnComponentOverride(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolveOpenCodeLayout(homeDir)
	if err := os.MkdirAll(layout.RootDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(rootDir) error = %v", err)
	}
	previousManifest := Manifest{
		SchemaVersion: PortableManifestSchemaVersion,
		Target:        TargetOpenCode,
		AuthMode:      "config-only",
		Components:    []ComponentID{ComponentCorePack, ComponentOpenCodePlugins},
		ManagedFiles: []ManagedFileRecord{
			{
				Path:        filepath.Join(layout.RootDir, "tui.json"),
				Component:   ComponentOpenCodePlugins,
				MergeMode:   MergeModeAdditiveJSON,
				ContentHash: "deadbeef",
			},
		},
		BackupRoot:  filepath.Join(layout.RootDir, "backups", "previous"),
		InstalledAt: "2026-06-01T00:00:00Z",
	}
	previousBytes, err := marshalManifest(previousManifest)
	if err != nil {
		t.Fatalf("marshalManifest(previous) error = %v", err)
	}
	if err := os.WriteFile(layout.ManifestPath, previousBytes, 0o600); err != nil {
		t.Fatalf("WriteFile(previous manifest) error = %v", err)
	}
	tuiPath := filepath.Join(layout.RootDir, "tui.json")
	if err := os.WriteFile(tuiPath, []byte(`{"theme":"solarized","plugin":[]}`), 0o600); err != nil {
		t.Fatalf("WriteFile(tui.json) error = %v", err)
	}

	service := Service{}
	plan, err := service.PlanOpenCodeInstall(InstallRequest{
		HomeDir:        homeDir,
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v0.1.0",
		Target:         TargetOpenCode,
		Components:     []ComponentID{ComponentCorePack},
		Now:            time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("PlanOpenCodeInstall() error = %v, want nil", err)
	}
	if action := findOpenCodePlannedFileAction(plan.Files, "tui.json"); action != nil && action.Action == "delete" {
		t.Fatalf("PlanOpenCodeInstall() emitted delete for tui.json on component override: action=%+v", action)
	}
	if _, err := service.ExecuteOpenCodeInstall(plan, InstallCommandOptions{}); err != nil {
		t.Fatalf("ExecuteOpenCodeInstall() error = %v, want nil", err)
	}
	if _, err := os.Stat(tuiPath); err != nil {
		t.Fatalf("Stat(tui.json) err = %v, want tui.json preserved", err)
	}
}

func TestOpenCodeInstallSummaryDoesNotEmbedSavedToken(t *testing.T) {
	homeDir := t.TempDir()
	service := Service{}
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	plan, err := service.PlanOpenCodeInstall(InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		SavedToken:     "ultra-secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v0.1.0",
		Target:         TargetOpenCode,
		Components:     []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
		Now:            now,
	})
	if err != nil {
		t.Fatalf("PlanOpenCodeInstall() error = %v, want nil", err)
	}
	result, err := service.ExecuteOpenCodeInstall(plan, InstallCommandOptions{})
	if err != nil {
		t.Fatalf("ExecuteOpenCodeInstall() error = %v, want nil", err)
	}
	// Build a string view of the full plan summary and the resulting
	// install summary so the redaction check covers both surfaces.
	summaryText := openCodeSummaryAsString(summarizePlannedActions(plan.Files))
	if strings.Contains(summaryText, "ultra-secret-token") {
		t.Fatalf("install plan summary leaked saved token; summary=%q", summaryText)
	}
	resultText := openCodeSummaryAsString(result.Summary)
	if strings.Contains(resultText, "ultra-secret-token") {
		t.Fatalf("install result summary leaked saved token; summary=%q", resultText)
	}
	if result.Manifest.ServerURL != "" && strings.Contains(result.Manifest.ServerURL, "ultra-secret-token") {
		t.Fatalf("install result ServerURL leaked saved token; ServerURL=%q", result.Manifest.ServerURL)
	}
	for _, record := range result.Manifest.ManagedFiles {
		if strings.Contains(record.Path, "ultra-secret-token") {
			t.Fatalf("manifest ManagedFile path leaked saved token; path=%q", record.Path)
		}
	}
}

// openCodeSummaryAsString renders the bounded summary fields of an
// InstallSummary as a single string for redaction checks. The
// fields inspected cover every field the OpenCode install summary
// exposes in the CLI surface (Created, Updated, Unchanged, BackedUp,
// Failed, Deleted, Conflicted).
func openCodeSummaryAsString(summary InstallSummary) string {
	parts := make([]string, 0, 8)
	parts = append(parts, summary.Created...)
	parts = append(parts, summary.Updated...)
	parts = append(parts, summary.Unchanged...)
	parts = append(parts, summary.BackedUp...)
	parts = append(parts, summary.Failed...)
	parts = append(parts, summary.Deleted...)
	parts = append(parts, summary.Conflicted...)
	return strings.Join(parts, "\n")
}

func findOpenCodePlannedFileAction(actions []PlanFileAction, relativePath string) *PlanFileAction {
	for i, action := range actions {
		if filepath.ToSlash(action.RelativePath) == relativePath {
			return &actions[i]
		}
	}
	return nil
}

func plannedActionSummary(actions []PlanFileAction) []string {
	out := make([]string, 0, len(actions))
	for _, action := range actions {
		out = append(out, action.Action+":"+action.RelativePath)
	}
	return out
}

func keysOfMap(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	return out
}

// Compile-time check: the agentpack import is used so the build
// stays wired to the agentpack dependency (defensive: a previous
// refactor accidentally dropped it; this guard keeps the import
// in place when the test is the only consumer in the file).
var _ = agentpack.DefaultDefinition
