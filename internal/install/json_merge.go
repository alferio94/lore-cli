package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func mergeJSONObject(existing, desired []byte, existingLabel, desiredLabel, mergedLabel string) ([]byte, error) {
	base := map[string]any{}
	if len(strings.TrimSpace(string(existing))) > 0 {
		if err := json.Unmarshal(existing, &base); err != nil {
			return nil, fmt.Errorf("decode existing %s: %w", existingLabel, err)
		}
	}
	overlay := map[string]any{}
	if err := json.Unmarshal(desired, &overlay); err != nil {
		return nil, fmt.Errorf("decode rendered %s: %w", desiredLabel, err)
	}
	merged := mergeMaps(base, overlay)
	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode merged %s: %w", mergedLabel, err)
	}
	return append(data, '\n'), nil
}

func mergeAntigravityMCPConfig(existing, desired []byte) ([]byte, error) {
	return mergeJSONObject(existing, desired, "mcp_config.json", "mcp_config.json", "mcp_config.json")
}

// OpenCodeMCPConfigOwnershipError is the typed error returned by
// mergeOpenCodeConfigJSON when the existing opencode.json carries a
// non-Lore-owned `mcp.lore` block. It is a fail-closed conflict signal
// for the install pipeline: the existing user-owned or third-party
// MCP configuration would have been silently overwritten by an
// additive merge, so the installer refuses the merge and the install
// plan records a backup-before-abort action.
//
// The error fields are intentionally narrow and redacted:
//
//   - Path is the relative path of the file inside the opencode harness
//     (always "opencode.json" for this code path).
//   - ExistingManagedBy is the value of the conflicting block's
//     `managed_by` field, or "" if the field is missing.
//   - ExistingType / ExistingURL name the conflicting block's shape
//     (e.g. "remote", "stdio", the URL). The token is never surfaced.
//   - BackupPath is the absolute path of the backup written before
//     the installer aborts; "" if no backup was written (no existing
//     file or backup target could not be created).
//
// The Error() string is safe to print in CLI diagnostics: it names the
// path, the existing managed_by value, the existing type, the existing
// URL, the backup path, and the resolution guidance; it never embeds
// the saved token.
type OpenCodeMCPConfigOwnershipError struct {
	Path              string
	ExistingManagedBy string
	ExistingType      string
	ExistingURL       string
	BackupPath        string
}

func (e *OpenCodeMCPConfigOwnershipError) Error() string {
	owner := strings.TrimSpace(e.ExistingManagedBy)
	if owner == "" {
		owner = "<missing>"
	}
	existingType := strings.TrimSpace(e.ExistingType)
	if existingType == "" {
		existingType = "<unknown>"
	}
	existingURL := strings.TrimSpace(e.ExistingURL)
	if existingURL == "" {
		existingURL = "<unknown>"
	}
	backup := strings.TrimSpace(e.BackupPath)
	backupClause := ""
	if backup != "" {
		backupClause = " A backup of the existing file is at " + backup + "."
	}
	return fmt.Sprintf(
		"refusing to overwrite non-Lore-owned `mcp.lore` block in %s: existing managed_by=%q type=%q url=%q."+
			" The installer only overwrites the mcp.lore subtree when it is already recognizably Lore-owned (legacy managed_by=lore-cli, or remote /v1/mcp with Authorization)."+
			" Resolution: edit %s and either point mcp.lore at your Lore /v1/mcp endpoint with an Authorization header"+
			" or remove the mcp.lore subtree, then rerun `lore install --target opencode`.%s",
		e.Path, owner, existingType, existingURL,
		e.Path,
		backupClause,
	)
}

// IsOpenCodeMCPConfigOwnershipConflict reports whether err is an
// OpenCodeMCPConfigOwnershipError. The install pipeline uses this to
// distinguish a fail-closed ownership conflict from a generic JSON
// decode error and to surface a backup-before-abort action in the
// plan.
func IsOpenCodeMCPConfigOwnershipConflict(err error) bool {
	var ownership *OpenCodeMCPConfigOwnershipError
	return errors.As(err, &ownership)
}

// AsOpenCodeMCPConfigOwnershipConflict unwraps err into the typed
// OpenCodeMCPConfigOwnershipError, or returns nil when err is not a
// conflict error.
func AsOpenCodeMCPConfigOwnershipConflict(err error) *OpenCodeMCPConfigOwnershipError {
	var ownership *OpenCodeMCPConfigOwnershipError
	if errors.As(err, &ownership) {
		return ownership
	}
	return nil
}

// opencodeMCPLoreOwnership reports whether the given mcp-lore block is
// recognizably Lore-owned. Legacy installs carried a `managed_by:
// lore-cli` marker, but the current OpenCode MCP schema rejects
// additional fields inside remote MCP blocks. The native-schema-safe
// ownership signal is therefore a remote `/v1/mcp` endpoint with an
// Authorization header; foreign URLs or missing auth still fail closed.
func opencodeMCPLoreOwnership(block any, desiredURL string) bool {
	object, ok := block.(map[string]any)
	if !ok {
		return false
	}
	if raw, present := object["managed_by"]; present {
		value, ok := raw.(string)
		return ok && strings.TrimSpace(value) == "lore-cli"
	}
	blockType, _ := object["type"].(string)
	if strings.TrimSpace(blockType) != "remote" {
		return false
	}
	url, _ := object["url"].(string)
	normalizedURL := strings.TrimRight(strings.TrimSpace(url), "/")
	normalizedDesiredURL := strings.TrimRight(strings.TrimSpace(desiredURL), "/")
	if normalizedDesiredURL == "" || normalizedURL != normalizedDesiredURL || !strings.HasSuffix(normalizedURL, "/v1/mcp") {
		return false
	}
	headers, ok := object["headers"].(map[string]any)
	if !ok {
		return false
	}
	authorization, _ := headers["Authorization"].(string)
	return strings.HasPrefix(strings.TrimSpace(authorization), "Bearer ")
}

// inspectOpenCodeMCPConfigOwnership inspects the existing file for
// the opencode.json mcp.lore ownership marker. It returns:
//
//   - loreOwned:    true when the existing file has no mcp.lore block, OR
//     the mcp.lore block is Lore-owned.
//   - conflict:     a non-nil *OpenCodeMCPConfigOwnershipError when the
//     existing file carries a non-Lore-owned mcp.lore
//     block. The error is safe to surface in CLI output.
//   - err:          a JSON decode error when the existing file is not
//     valid JSON.
//
// The token (Authorization header) is never extracted into the
// returned struct, so the conflict is safe to log.
func inspectOpenCodeMCPConfigOwnership(existing []byte, relativePath string, desiredLoreMCPURL string) (loreOwned bool, conflict *OpenCodeMCPConfigOwnershipError, err error) {
	trimmed := strings.TrimSpace(string(existing))
	if trimmed == "" {
		return true, nil, nil
	}
	payload := map[string]any{}
	if decodeErr := json.Unmarshal(existing, &payload); decodeErr != nil {
		return false, nil, fmt.Errorf("decode existing %s: %w", relativePath, decodeErr)
	}
	mcp, ok := payload["mcp"].(map[string]any)
	if !ok {
		return true, nil, nil
	}
	raw, present := mcp["lore"]
	if !present {
		return true, nil, nil
	}
	if opencodeMCPLoreOwnership(raw, desiredLoreMCPURL) {
		return true, nil, nil
	}
	// Foreign mcp.lore block: extract the redacted conflict details.
	object, _ := raw.(map[string]any)
	managedBy := ""
	if value, ok := object["managed_by"].(string); ok {
		managedBy = value
	}
	existingType := ""
	if value, ok := object["type"].(string); ok {
		existingType = value
	}
	existingURL := ""
	if value, ok := object["url"].(string); ok {
		existingURL = value
	}
	return false, &OpenCodeMCPConfigOwnershipError{
		Path:              relativePath,
		ExistingManagedBy: managedBy,
		ExistingType:      existingType,
		ExistingURL:       existingURL,
	}, nil
}

// migrateOpenCodeLegacyStaleShape returns a copy of the parsed
// existing payload with the stale legacy shape silently removed.
// The post-repair shape is:
//
//   - For `opencode.json`: drop the top-level `lore` block (the
//     legacy `lore`-shaped renderer no longer produces this block;
//     the new renderer emits the native `agent` overlay).
//   - For `tui.json`: drop the top-level `lore` block AND drop the
//     plural `plugins` (an array of objects) key. The native
//     tui.json uses a singular `plugin` string array; the legacy
//     plural `plugins` array of objects is no longer the
//     documented shape and is replaced by the singular `plugin`
//     string array on every render.
//
// The migration is intentionally silent and additive: the user
// loses only the stale Lore-metadata fields, not any user-owned
// keys (e.g. `theme`, custom `mcp.<other>` entries, custom
// `agent.<other>` overrides). The migration runs on EVERY merge
// (idempotent), not only once: the drop is a no-op when the
// existing file is already on the native shape, so reruns stay
// safe. The function returns the migrated payload map (which is
// the same map as the input when no migration is needed) and a
// boolean reporting whether any stale fields were removed; the
// boolean is exposed for future instrumentation but is not
// required to drive the merge semantics.
func migrateOpenCodeLegacyStaleShape(payload map[string]any, relativePath string) (map[string]any, bool) {
	changed := false
	normalized := filepathToSlash(relativePath)
	if _, present := payload[opencodeLoreBlockKey]; present {
		// The legacy top-level `lore` block was the metadata-only
		// shape the previous renderer produced. It is no longer
		// emitted by the installer and is dropped during merge so
		// existing installs migrate to the native `agent` overlay
		// shape on the next run.
		delete(payload, opencodeLoreBlockKey)
		changed = true
	}
	if normalized == "tui.json" {
		// The legacy tui.json used a plural `plugins` array of
		// objects (e.g. `[{"id": "...", "owner": "community", ...}]`).
		// The native OpenCode tui.json uses a singular `plugin`
		// string array (e.g. `["opencode-subagent-statusline"]`).
		// The plural `plugins` key is dropped during merge so the
		// next render replaces it with the native singular form.
		// The values are intentionally NOT preserved: the new
		// shape uses bare plugin names with no per-entry metadata,
		// and the legacy per-entry fields (`owner`, `source`,
		// `enabled`) had no native equivalent in the new shape.
		if _, present := payload["plugins"]; present {
			delete(payload, "plugins")
			changed = true
		}
	}
	if normalized == "opencode.json" {
		if mcp, ok := payload["mcp"].(map[string]any); ok {
			if lore, ok := mcp["lore"].(map[string]any); ok {
				if value, ok := lore["managed_by"].(string); ok && strings.TrimSpace(value) == opencodeManagedByValue {
					delete(lore, "managed_by")
					changed = true
				}
			}
		}
	}
	return payload, changed
}

// mergeOpenCodeConfigJSON performs an additive merge for OpenCode
// JSON config files (currently `opencode.json` and `tui.json`). The
// merge:
//
//   - Treats a missing, empty, or whitespace-only existing file as a
//     fresh write (renders the desired payload verbatim).
//   - Silently migrates the legacy stale shape (top-level `lore`
//     block in opencode.json; top-level `lore` block + plural
//     `plugins` array in tui.json) by dropping the stale keys
//     before merging the new native shape on top. The migration is
//     idempotent: reruns on an already-migrated file are a no-op
//     for the migration step.
//   - Preserves user-owned top-level keys (e.g. `theme`, custom
//     `mcp.<other>` entries, custom `agent.<other>` overrides,
//     user-added keys) from the existing file.
//   - Writes the Lore-managed top-level keys (`$schema`, `agent`,
//     `skills` for opencode.json; `plugin` string array for
//     tui.json) from the desired payload via the existing
//     `mergeMaps` helper.
//   - Returns a typed *OpenCodeMCPConfigOwnershipError when the
//     existing `opencode.json` carries a non-Lore-owned `mcp.lore`
//     block. The conflict is detected before the merge runs so the
//     installer can fail closed with a backup-before-abort action
//     and never silently clobber a user-owned or third-party MCP
//     configuration. The `tui.json` file is fully Lore-owned and
//     does not carry an `mcp.lore` block, so the conflict path is
//     unreachable for `tui.json`; the helper still consults the
//     payload defensively in case the file was hand-edited.
//   - Returns an error when the existing file is not valid JSON, so
//     ambiguous user content is rejected rather than silently
//     dropped.
//
// The function is idempotent for the additive-merge path: applying
// it twice with the same input produces byte-identical output. The
// fail-closed path is intentionally NOT idempotent — a re-run after
// the user resolves the conflict proceeds with a normal merge.
func mergeOpenCodeConfigJSON(existing, desired []byte, relativePath string) ([]byte, error) {
	trimmedExisting := strings.TrimSpace(string(existing))
	if trimmedExisting == "" {
		return append([]byte(nil), desired...), nil
	}
	// Ownership check is scoped to opencode.json (where a foreign
	// mcp.lore block is possible). The tui.json payload does not
	// carry an mcp.lore block and is always fully Lore-owned, so
	// the conflict path is unreachable for tui.json in practice.
	if filepathToSlash(relativePath) == "opencode.json" {
		loreOwned, conflict, inspectErr := inspectOpenCodeMCPConfigOwnership(existing, relativePath, desiredOpenCodeLoreMCPURL(desired))
		if inspectErr != nil {
			return nil, inspectErr
		}
		if !loreOwned {
			return nil, conflict
		}
	}
	// Migration: silently drop the legacy top-level `lore` block
	// (and, for tui.json, the legacy plural `plugins` array) from
	// the existing payload before merging. The desired payload
	// always carries the new native shape (no top-level `lore`,
	// singular `plugin` string array for tui.json), so the merge
	// naturally replaces the stale fields. The function is safe
	// to call on already-migrated payloads (no-op) and on
	// hand-edited payloads that intentionally keep the legacy
	// shape (the migration is the repair path).
	parsed := map[string]any{}
	if err := json.Unmarshal(existing, &parsed); err != nil {
		return nil, fmt.Errorf("decode existing %s: %w", relativePath, err)
	}
	migrated, _ := migrateOpenCodeLegacyStaleShape(parsed, relativePath)
	data, err := json.MarshalIndent(migrated, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode migrated existing %s: %w", relativePath, err)
	}
	existing = append(data, '\n')
	return mergeJSONObject(existing, desired, relativePath, relativePath, relativePath)
}

func desiredOpenCodeLoreMCPURL(desired []byte) string {
	payload := map[string]any{}
	if err := json.Unmarshal(desired, &payload); err != nil {
		return ""
	}
	mcp, ok := payload["mcp"].(map[string]any)
	if !ok {
		return ""
	}
	lore, ok := mcp["lore"].(map[string]any)
	if !ok {
		return ""
	}
	url, _ := lore["url"].(string)
	return strings.TrimSpace(url)
}

// filepathToSlash is a tiny helper that mirrors path/filepath.ToSlash
// for the relativePath strings we get from the install pipeline
// (already forward-slash). Kept local so this file does not have to
// import path/filepath just for the comparison.
func filepathToSlash(relativePath string) string {
	return strings.ReplaceAll(relativePath, "\\", "/")
}
