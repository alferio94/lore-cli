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
	Path             string
	ExistingManagedBy string
	ExistingType     string
	ExistingURL      string
	BackupPath       string
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
			" The installer only overwrites the mcp.lore subtree when it is already Lore-owned (managed_by=lore-cli)."+
			" Resolution: edit %s and either set mcp.lore.managed_by to %q (only when the existing block is your own Lore-managed config)"+
			" or remove the mcp.lore subtree, then rerun `lore install --target opencode`.%s",
		e.Path, owner, existingType, existingURL,
		e.Path, "lore-cli",
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
// Lore-owned. A block is Lore-owned when it is a JSON object and
// carries a `managed_by` field whose trimmed value equals
// "lore-cli". Any other shape (missing, non-object, different
// managed_by) is treated as foreign and must fail closed.
func opencodeMCPLoreOwnership(block any) bool {
	object, ok := block.(map[string]any)
	if !ok {
		return false
	}
	raw, present := object["managed_by"]
	if !present {
		return false
	}
	value, ok := raw.(string)
	if !ok {
		return false
	}
	return strings.TrimSpace(value) == "lore-cli"
}

// inspectOpenCodeMCPConfigOwnership inspects the existing file for
// the opencode.json mcp.lore ownership marker. It returns:
//
//   - loreOwned:    true when the existing file has no mcp.lore block, OR
//                   the mcp.lore block is Lore-owned.
//   - conflict:     a non-nil *OpenCodeMCPConfigOwnershipError when the
//                   existing file carries a non-Lore-owned mcp.lore
//                   block. The error is safe to surface in CLI output.
//   - err:          a JSON decode error when the existing file is not
//                   valid JSON.
//
// The token (Authorization header) is never extracted into the
// returned struct, so the conflict is safe to log.
func inspectOpenCodeMCPConfigOwnership(existing []byte, relativePath string) (loreOwned bool, conflict *OpenCodeMCPConfigOwnershipError, err error) {
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
	if opencodeMCPLoreOwnership(raw) {
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

// mergeOpenCodeConfigJSON performs an additive merge for OpenCode
// JSON config files (currently `opencode.json` and `tui.json`). The
// merge:
//
//   - Treats a missing, empty, or whitespace-only existing file as a
//     fresh write (renders the desired payload verbatim).
//   - Preserves user-owned top-level keys (e.g. `theme`, custom
//     `mcp.<other>` entries, user-added keys) from the existing file.
//   - Writes the Lore-managed top-level keys (`lore`, `mcp.lore` for
//     opencode.json; the `plugins` array and `lore` block for tui.json)
//     from the desired payload via the existing `mergeMaps` helper.
//   - Returns a typed *OpenCodeMCPConfigOwnershipError when the
//     existing `opencode.json` carries a non-Lore-owned `mcp.lore`
//     block. The conflict is detected before the merge runs so the
//     installer can fail closed with a backup-before-abort action and
//     never silently clobber a user-owned or third-party MCP
//     configuration. The `tui.json` file is always fully Lore-owned
//     (the embedded asset carries the `lore.managed_by: lore-cli`
//     marker on every render), so the conflict path is effectively
//     unreachable for `tui.json`; the helper still consults the
//     payload defensively in case the file was hand-edited.
//   - Returns an error when the existing file is not valid JSON, so
//     ambiguous user content is rejected rather than silently dropped.
//
// The function is idempotent for the additive-merge path: applying it
// twice with the same input produces byte-identical output. The
// fail-closed path is intentionally NOT idempotent — a re-run after
// the user resolves the conflict proceeds with a normal merge.
func mergeOpenCodeConfigJSON(existing, desired []byte, relativePath string) ([]byte, error) {
	trimmedExisting := strings.TrimSpace(string(existing))
	if trimmedExisting == "" {
		return append([]byte(nil), desired...), nil
	}
	// Ownership check is scoped to opencode.json (where a foreign
	// mcp.lore block is possible). The tui.json payload is fully
	// Lore-owned and the embedded asset re-introduces the
	// `lore.managed_by: lore-cli` marker on every render, so the
	// conflict path is unreachable for tui.json in practice.
	if filepathToSlash(relativePath) == "opencode.json" {
		loreOwned, conflict, inspectErr := inspectOpenCodeMCPConfigOwnership(existing, relativePath)
		if inspectErr != nil {
			return nil, inspectErr
		}
		if !loreOwned {
			return nil, conflict
		}
	}
	return mergeJSONObject(existing, desired, relativePath, relativePath, relativePath)
}

// filepathToSlash is a tiny helper that mirrors path/filepath.ToSlash
// for the relativePath strings we get from the install pipeline
// (already forward-slash). Kept local so this file does not have to
// import path/filepath just for the comparison.
func filepathToSlash(relativePath string) string {
	return strings.ReplaceAll(relativePath, "\\", "/")
}
