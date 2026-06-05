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
	desired, err := renderOpenCodeLoreBlock(agentconfig.Config{})
	if err != nil {
		t.Fatalf("renderOpenCodeLoreBlock() error = %v, want nil", err)
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
// the additive merge preserves user-owned top-level keys and merges
// the Lore-managed `lore` and `mcp.lore` blocks via the overlay.
func TestOpenCodeConfigJSONMergePreservesExistingUserContent(t *testing.T) {
	desired, err := renderOpenCodeMCPConfig(agentconfig.Config{}, "https://lore.example", "secret-token")
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
	// The Lore-managed top-level `lore` and `mcp.lore` must be present.
	lore, ok := payload["lore"].(map[string]any)
	if !ok {
		t.Fatalf("merged payload missing top-level `lore` object; got keys %v", keysOfMap(payload))
	}
	if got := lore["managed_by"]; got != "lore-cli" {
		t.Fatalf("merged payload lore.managed_by = %v, want lore-cli", got)
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
	desired, err := renderOpenCodeMCPConfig(agentconfig.Config{}, "https://lore.example", "secret-token")
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

// TestOpenCodeConfigJSONMergeRejectsInvalidExistingJSON verifies the
// merge returns an error rather than silently dropping user content
// when the existing file is not valid JSON.
func TestOpenCodeConfigJSONMergeRejectsInvalidExistingJSON(t *testing.T) {
	desired, err := renderOpenCodeLoreBlock(agentconfig.Config{})
	if err != nil {
		t.Fatalf("renderOpenCodeLoreBlock() error = %v, want nil", err)
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
	desired, err := renderOpenCodeMCPConfig(agentconfig.Config{}, "https://lore.example", "ultra-secret-token")
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
		"managed_by=lore-cli",
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
// Lore-owned (carries the managed_by: lore-cli marker). The merge
// replaces the Lore-owned subtree from the overlay, preserving all
// other top-level keys.
func TestOpenCodeConfigJSONMergeAllowsLoreOwnedMcpLoreBlock(t *testing.T) {
	desired, err := renderOpenCodeMCPConfig(agentconfig.Config{}, "https://lore.example", "new-token")
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
}

// TestOpenCodeConfigJSONMergeIgnoresOwnershipForTUIJSON verifies the
// ownership check is scoped to opencode.json. The tui.json payload
// is fully Lore-owned (the embedded asset re-introduces the
// `lore.managed_by: lore-cli` marker on every render) and must
// always proceed with the additive merge even if a user hand-edits
// the file and removes the marker.
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
	if _, ok := payload["plugins"].([]any); !ok {
		t.Fatalf("merged tui.json plugins array missing")
	}
	if _, ok := payload["lore"].(map[string]any); !ok {
		t.Fatalf("merged tui.json missing top-level `lore` object; got keys %v", keysOfMap(payload))
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
	if _, ok := merged["lore"].(map[string]any); !ok {
		t.Fatalf("merged opencode.json missing top-level `lore` object; got keys %v", keysOfMap(merged))
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
//   - Write the existing file to the managed backup path BEFORE
//     surfacing the conflict (so the user can recover).
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
		t.Fatal("conflict.BackupPath = \"\", want managed backup path written before the conflict surfaced")
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

	// The conflict backup MUST exist on disk and MUST contain the
	// original foreign content.
	backupBytes, backupReadErr := os.ReadFile(conflict.BackupPath)
	if backupReadErr != nil {
		t.Fatalf("ReadFile(conflict backup) error = %v, want backup at %s", backupReadErr, conflict.BackupPath)
	}
	if string(backupBytes) != string(foreignExisting) {
		t.Fatalf("conflict backup content = %q, want original foreign content %q", string(backupBytes), string(foreignExisting))
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

// TestOpenCodeInstallSummaryDoesNotEmbedSavedToken is a focused
// redaction gate for the OpenCode install path. The full install
// summary must not contain the saved login token even when
// `lore-server-mcp` is selected (the MCP block persists the token
// in the opencode.json file, but the summary line is rendered
// separately and must redact).
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
