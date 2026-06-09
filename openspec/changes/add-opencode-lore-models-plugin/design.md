# Design: Add OpenCode Lore Models Plugin

## Status
- Phase: design
- Change: `add-opencode-lore-models-plugin`
- Date: 2026-06-09
- Repository: `/Users/alfonsocarmona/personal/lore2/lore-cli`
- Persistence: OpenSpec filesystem plus Lore MCP memory
- Scope: technical design only; no implementation code changed

## Source Artifacts
- `openspec/changes/add-opencode-lore-models-plugin/init.md`
- `openspec/changes/add-opencode-lore-models-plugin/exploration.md`
- `openspec/changes/add-opencode-lore-models-plugin/proposal.md`
- `openspec/changes/add-opencode-lore-models-plugin/specs/opencode-lore-models/spec.md`
- `openspec/changes/add-opencode-lore-models-plugin/state.json`
- Lore decision memory `sdd/add-opencode-lore-models-plugin/decision-hot-edit-opencode-json`

## Design Summary
Replace the managed OpenCode `model-variants.ts` asset with `lore-models.ts`. The new plugin keeps provider/model/variant discovery cache behavior and adds an in-OpenCode model-selection flow for Lore-managed agents. The primary persistence path is a safe hot edit of `~/.config/opencode/opencode.json`, updating only `agent.<name>.model` and `agent.<name>.variant`. The installer then treats existing OpenCode agent model/variant values as user choices and preserves them on reinstall while still rendering defaults for missing values.

The implementation must stay inside documented/safe OpenCode surfaces by default. A floating/dialog selector may be used only when the active OpenCode runtime exposes a supported safe API. Otherwise, `lore-models.ts` must provide a fallback interaction inside OpenCode, such as a plugin command/tool-driven flow that reports choices/status through OpenCode and persists the selected values without requiring manual file edits.

## Architecture

### Components
| Component | Responsibility |
|---|---|
| `internal/install/assets/opencode/plugins/lore-models.ts` | OpenCode plugin for provider/model/variant discovery, in-OpenCode selection UX, status/error reporting, and safe `opencode.json` hot edits. |
| `internal/install/opencode_assets.go` | Bounded managed plugin asset allowlist; replace `model-variants.ts` with `lore-models.ts`, keep `background-agents.ts` and `opencode-subagent-statusline.ts`, preserve `sdd-engram`/`logo` exclusions. |
| `internal/install/adapter_opencode.go` | Render revised OpenCode agent overlay: `lore` primary, `lore-worker` plus SDD phase agents as subagents, no `agent.lore.permission`, render model/variant values. |
| `internal/install/json_merge.go` | Preserve existing hot-edited `agent.<name>.model` and `agent.<name>.variant` during additive reinstall merges while continuing to preserve unrelated keys and fail closed on foreign `mcp.lore`. |
| `internal/install/opencode_install.go` | Add manifest-scoped stale managed-file cleanup for old `plugins/model-variants.ts` and apply backup-first delete actions. |
| `internal/agentpack` | Source of managed agent definitions, including `lore-worker` and SDD agents. |
| `internal/agentconfig` | Secondary/fallback durable source only if needed for recovery/default seeding; not the primary persistence path for OpenCode selections. |

### Data Stores
1. **Primary preference store:** `~/.config/opencode/opencode.json`
   - Native OpenCode fields: `agent.<name>.model` and `agent.<name>.variant`.
   - Written directly by `lore-models.ts` using safe parse/merge/backup/atomic replacement.
   - Read by the installer before/while merging so user choices survive reinstall.
2. **Discovery cache:** `~/.lore/cache/opencode-model-variants.json`
   - Metadata-only provider/model/variant cache derived from OpenCode runtime/provider data.
   - Preserves current behavior from `model-variants.ts`.
   - Never authoritative for user-selected model/variant values.
3. **Optional safety fallback:** Lore-owned override/state file under a Lore-owned path, only for recovery if hot editing fails or is impossible.
   - It must not supersede `opencode.json` as the primary behavior.
   - If implemented, it must be secret-free, clearly marked fallback-only, and reconciled into `opencode.json` when safe.

## Managed Agent Contract

### Managed agents
The OpenCode agent overlay must include:
- `lore` as primary orchestrator.
- All canonical `sdd-*` phase agents.
- `lore-worker` as a managed subagent.

### Rendered shape
Fresh render and reinstall target shape:

```json
{
  "default_agent": "lore",
  "agent": {
    "lore": {
      "description": "...",
      "model": "<effective-orchestrator-model>",
      "variant": "<optional-user-or-default-variant>",
      "mode": "primary",
      "prompt": "{file:./AGENTS.md}"
    },
    "sdd-design": {
      "model": "<effective-model>",
      "variant": "<optional-user-variant>",
      "mode": "subagent",
      "prompt": "{file:./skills/sdd-design/SKILL.md}"
    },
    "lore-worker": {
      "model": "<effective-worker-model>",
      "variant": "<optional-user-variant>",
      "mode": "subagent",
      "prompt": "{file:./skills/lore-worker/SKILL.md}"
    }
  }
}
```

Rules:
- `agent.lore.permission = "allow"` must not be rendered.
- No global/default permissions are broadened.
- Non-lore managed agents must render `mode: "subagent"`.
- `variant` must be omitted when no effective variant exists; do not invent provider-specific variants.
- Existing user-owned/foreign agents must remain preserved by additive merge.
- Explicit exclusions `sdd-engram` and `logo` remain excluded from embed assets, rendered assets, TUI registration, and static guards.

## OpenCode Plugin Design

### Plugin identity
- Rename the managed asset from `model-variants.ts` to `lore-models.ts`.
- Export a plugin name aligned with `LoreModelsPlugin` and update logs from `[model-variants]` to `[lore-models]`.
- Keep the cache file path `~/.lore/cache/opencode-model-variants.json` unless implementation discovers a strong reason to migrate it; the cache is provider metadata, not a plugin identity contract.

### Runtime discovery flow
1. On plugin activation and on explicit refresh, call OpenCode runtime/provider APIs such as `client.provider.list()` when available.
2. Normalize provider data into a stable internal structure:
   - provider id
   - model id
   - display label if available
   - sorted variant keys when available
3. Write the cache atomically to `~/.lore/cache/opencode-model-variants.json`.
4. If runtime discovery fails, report the failure inside OpenCode and use cache only as stale/recovery context. The plugin must label cached data as not freshly verified.

### Lore-managed agent discovery
The plugin should discover configurable agents from the active `opencode.json` first, not from hardcoded assumptions alone:
1. Read `~/.config/opencode/opencode.json`.
2. Parse `agent` entries.
3. Select Lore-managed agents by prompt/path convention:
   - `lore` with `prompt: "{file:./AGENTS.md}"` and `mode: "primary"`.
   - SDD agents and `lore-worker` with prompt references under `./skills/<name>/SKILL.md` and names known to Lore.
4. Sort agents deterministically: `lore`, `lore-worker`, then canonical SDD phase order.
5. If the file is missing or malformed, report a recoverable error and suggest rerunning `lore install --target opencode`.

The implementation may also embed the known Lore agent names as a validation allowlist so it never edits arbitrary `agent.<name>` entries.

### User interaction
Preferred path:
- Use a floating/dialog selector only if OpenCode exposes a supported safe selector/dialog API to plugins in the active runtime.
- Flow: select agent -> select provider/model -> select variant or no variant -> preview -> confirm -> persist -> toast/status.

Fallback path:
- Must remain inside OpenCode.
- May use documented plugin command/tool/event surfaces to accept structured choices and report options/status.
- Must not require the user to leave OpenCode or manually edit `opencode.json`.
- Must show current value, proposed value, whether runtime data is fresh or cached, and final success/failure.

Status and errors:
- Success: identify agent and fields changed without printing secret-bearing config.
- Runtime data unavailable: explain that provider/model/variant availability could not be verified.
- Malformed config: abort without write and report the path plus parse class only.
- Write failure: report that the previous config remains recoverable and name backup/temp paths only when safe.

## Safe Hot-Edit Algorithm for `opencode.json`

The plugin's persistence operation must be deterministic and recoverable.

1. Resolve path:
   - Default to `~/.config/opencode/opencode.json`.
   - Do not follow arbitrary user-supplied paths.
2. Read existing bytes.
3. Parse JSON into an object.
   - If empty, missing, or malformed, abort unless the implementation has a verified fresh-render path; do not create a partial config from plugin guesses.
4. Validate target agent:
   - `agent` is an object.
   - selected agent exists and is Lore-managed by name and prompt/mode convention.
   - target `model` is nonblank.
   - target `variant`, if provided, is nonblank and was discovered from current runtime data or explicitly selected through an accepted fallback path.
5. Preserve a deep copy for rollback/backup.
6. Merge only the allowed fields:
   - Set `agent.<selected>.model = <model>`.
   - Set `agent.<selected>.variant = <variant>` when a variant is selected.
   - Remove `agent.<selected>.variant` only if the user explicitly chooses a "no variant/default" option.
   - Do not modify any other top-level key, `mcp`, command, plugin, theme, foreign agent, prompt, mode, or permission field.
7. Validate merged JSON:
   - Reparse serialized bytes.
   - Confirm the selected fields match the intended values.
   - Confirm unchanged protected subtrees such as `mcp` are byte/structure-equivalent except for JSON formatting.
8. Backup:
   - Create `~/.config/opencode/backups/<timestamp>/opencode.json` or a plugin-specific backup path under the existing OpenCode backup root convention.
   - Use restrictive permissions (`0600`) for files because `opencode.json` may contain bearer tokens.
9. Atomic write:
   - Write temp file in the same directory with `0600`.
   - `fsync`/close where practical in Node.
   - Rename temp over `opencode.json`.
   - Clean temp file best-effort on failure.
10. Post-write verification:
   - Read and parse the final file.
   - Confirm selected fields are present.
11. Redacted errors:
   - Never log raw file contents.
   - Redact `Authorization`, `Bearer ...`, `apiKey`, `token`, `password`, and headers-like secret fields from thrown/logged error objects.
   - Prefer messages naming path, phase, and field class only.

Failure behavior:
- Any failure before atomic rename leaves the original file authoritative.
- If backup succeeds but write fails, report backup path.
- If backup fails, abort before modifying the config.
- If final verification fails, report failure and backup path; do not attempt complex auto-repair in the plugin.

## Installer Reinstall Preservation

`lore install --target opencode` must preserve user-chosen values already present in `opencode.json`.

### Effective model/variant resolution
For each Lore-managed agent:
1. Start with installer defaults from agentpack/profile:
   - `lore`: balanced profile orchestrator model fallback.
   - SDD phase agents: default SDD model or existing `agentconfig` SDD model override.
   - `lore-worker`: role/default mapping from managed agent definitions; if unavailable, use the SDD default model.
2. Read existing `opencode.json` when present and valid.
3. If `agent.<managed>.model` is nonblank in existing config, treat it as user-chosen and preserve it.
4. If `agent.<managed>.variant` is nonblank in existing config, treat it as user-chosen and preserve it.
5. Render defaults only for missing/blank values.
6. Omit variants when neither existing config nor an explicit default provides one.

### Merge behavior
The current recursive merge overwrites the `agent` subtree with desired managed values. This change must adjust render/merge sequencing so preserved values are included in the desired overlay before merge, or the merge function must specifically preserve selected `agent.<managed>.model`/`variant` fields from existing config. The preferred implementation is to compute an effective managed-agent overlay before calling `mergeOpenCodeConfigJSON`, because it keeps the merge helper generic and makes tests straightforward.

### Reset semantics
Normal reinstall must not reset values. Default-restoring behavior requires an explicit reset semantic. If no reset CLI flag exists in this change, document that reset remains future work and can be performed by removing the specific `agent.<name>.model`/`variant` fields before reinstall.

## Config Renderer Changes

Update `opencodeAgentOverlay` and related documentation strings:
- Remove `opencodePermissionKey: "allow"` from `agent.lore`.
- Add `opencodeSubagentModeValue = "subagent"`.
- Include `lore-worker` in the overlay.
- Add `variant` rendering support.
- Update `renderOpenCodeAgentsMD` managed surface copy so it no longer claims `permission: "allow"` or `model-variants.ts`.
- Update capability description for `opencode-plugins` to name `lore-models.ts`.
- Keep `tui.json` registration limited to `opencode-subagent-statusline`; local plugin `.ts` files continue to be discovered from the plugins directory.

## Stale Asset Cleanup

Add an OpenCode stale managed-file cleanup pass analogous to the existing managed overlay cleanup pattern:
1. Load previous `~/.config/opencode/lore-install.json` if present and valid.
2. Build the set of newly rendered managed absolute paths.
3. For each previous manifest managed file:
   - If path is absent from the new rendered set, component is Lore-managed, and path is under the OpenCode root, schedule a backup-first delete action.
   - The known rename case is `plugins/model-variants.ts` -> `plugins/lore-models.ts`.
4. Do not delete files when no previous manifest proves ownership.
5. Do not scan the plugins directory by filename.
6. Record cleanup in install summary and write the new manifest without stale entries.

The delete operation must back up the stale file under the normal backup root before removal.

## Migration Plan
1. Fresh installs render `lore-models.ts`, revised agent overlay, and no old plugin asset.
2. Upgrade installs with a previous manifest delete only manifest-owned `plugins/model-variants.ts` after backup.
3. Upgrade installs without a previous manifest leave any existing `model-variants.ts` untouched.
4. Existing hot-edited `agent.<name>.model`/`variant` values are read from `opencode.json` and preserved in the rendered overlay.
5. Existing `agent.lore.permission = "allow"` is removed only from the managed `agent.lore` overlay during merge; foreign/user agents are unaffected.
6. Existing foreign `mcp.lore` conflict handling remains fail-closed and token-redacted.

## Alternatives Considered
- **Agent-config as primary persistence:** rejected for this change because the user explicitly requires direct-from-OpenCode configuration and hot-editing `opencode.json` as primary behavior. It remains useful as fallback/default seed only.
- **Plugin-local preference file as primary persistence:** rejected because it would require reinstall to project values later and would not make OpenCode config immediately authoritative.
- **Force undocumented floating selector APIs:** rejected as the only path. Selector UX is preferred, but the implementation must fall back to a safe in-OpenCode workflow when the runtime lacks a supported dialog API.
- **Directory-scan cleanup of `model-variants.ts`:** rejected because it risks deleting user-owned plugin files. Manifest ownership is required.

## Impacted Files and Tests

### Implementation files
- `internal/install/assets/opencode/plugins/model-variants.ts` -> replace/rename to `lore-models.ts`.
- `internal/install/opencode_assets.go` for asset allowlist, descriptions, and exclusions.
- `internal/install/adapter_opencode.go` for agent overlay, `lore-worker`, subagent mode, permission removal, variants, AGENTS.md copy.
- `internal/install/opencode_install.go` for effective existing config preservation and stale managed-file cleanup planning/apply.
- `internal/install/json_merge.go` if preservation is implemented in merge rather than precomputed overlay, and for tests around agent-field preservation.
- `internal/install/assets/opencode/tui.json` only if metadata/copy must change; do not register local plugin `.ts` files there.
- `internal/agentconfig/config.go`, `internal/agentconfig/store.go` only if fallback/default seed state is added; not required for primary hot-edit behavior.
- CLI/TUI files only if install summaries or reset semantics are exposed in this change: `internal/cli/actions.go`, `internal/cli/app.go`, `internal/tui/root.go`.

### Tests
- `internal/install/adapter_opencode_test.go`: revised `agent` overlay, no `permission`, subagent modes, `lore-worker`, variants omitted/preserved.
- `internal/install/adapter_opencode_plugins_test.go`: asset list contains `lore-models.ts`, not `model-variants.ts`; exclusions still enforced.
- `internal/install/opencode_install_test.go`: reinstall preserves existing model/variant values, stale manifest-owned plugin cleanup, no cleanup without manifest ownership.
- `internal/install/json_merge_test.go`: malformed config failure, foreign `mcp.lore` redaction remains, user keys/foreign agents preserved.
- Plugin tests or fixture validation for `lore-models.ts`: provider parsing, cache write shape, safe hot-edit helper, redaction, backup/atomic-write failure behavior.
- Existing static guards: update expected managed plugin names and continue rejecting `sdd-engram`/`logo`.

## Verification Plan
1. Run focused installer tests:
   - `go test ./internal/install -run 'TestOpenCode|Test.*Plugin|Test.*Manifest|Test.*Merge'`
2. Run agent config tests if fallback/default seed schema changes:
   - `go test ./internal/agentconfig ./internal/install`
3. Run broader relevant tests:
   - `go test ./internal/install ./internal/cli ./internal/tui`
4. Manually inspect rendered fresh `opencode.json` fixture for:
   - `default_agent: "lore"`
   - `agent.lore.mode: "primary"`
   - no `agent.lore.permission`
   - non-lore `mode: "subagent"`
   - `lore-worker` present
5. Simulate upgrade with previous manifest containing `plugins/model-variants.ts`; verify backup-first delete and no arbitrary plugin deletion.
6. Simulate hot-edited `opencode.json` with secret-bearing `mcp.lore.headers.Authorization`; verify reinstall preserves model/variant and errors do not expose token values.
7. Validate plugin TypeScript syntax and, where available, run OpenCode plugin runtime smoke testing.

## Rollback and Safety Notes
- Because stale cleanup backs up files before deletion, rollback can restore `plugins/model-variants.ts` from the install backup root if needed.
- Because plugin hot edits back up `opencode.json` before atomic replacement, users can restore the prior config after write or validation failures.
- Reverting the code change and rerunning install should restore the old renderer behavior, but user-selected model/variant fields in `opencode.json` should remain harmless native OpenCode fields.
- Never print raw `opencode.json` contents in logs or test failure messages when fixtures include authorization headers.

## Open Questions
No blocking product decision remains for task breakdown. The floating selector remains conditional on verified safe OpenCode runtime support; the fallback in-OpenCode UX is part of the accepted design.
