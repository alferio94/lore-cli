# Apply Progress: fix-opencode-native-config-after-readd

## Status
- Mode: Standard (no Strict TDD module declared in this repo's openspec config)
- Current slice: completed
- Completed tasks: 7/8 (4.x broad validation deferred to sdd-verify)
- Lore persistence: unavailable in this runtime (Lore MCP tools not installed; using OpenSpec file fallback per skill-registry guidance). OpenSpec fallback MUST remain in place until Lore tools are wired into the runtime.

## Completed Tasks Cumulative
- [x] 1.1 Update `opencode_install.go` + `adapter_opencode.go` so `opencode.json` renders the native OpenCode shape: `$schema: https://opencode.ai/config.json`, `theme: system`, native `agent` overlay (`{model, prompt}` per SDD phase with `{file:./skills/<name>/SKILL.md}` references), native `skills.path` block, no top-level Lore-only `lore`. Added constants `opencodeConfigSchemaURL`, `opencodeTUISettingsSchemaURL`, `opencodeSkillsDirPath`, `opencodeThemeValue`, `opencodeMCPLoreKey`, `opencodeCommunityStatuslinePlugin`. Renamed `renderOpenCodeLoreBlock` → `renderOpenCodeNativeConfig`; updated the call site in `opencode_install.go`.
- [x] 1.2 Rewrite `internal/install/assets/opencode/tui.json` to the native OpenCode shape: `$schema: https://opencode.ai/tui.json` (was the placeholder `https://opencode.example/tui.schema.json`), `theme: system`, singular `plugin` string array containing only `opencode-subagent-statusline`. Removed the legacy plural `plugins` object array and the legacy top-level `lore` metadata block.
- [x] 1.3 Adjust `json_merge.go`: added `migrateOpenCodeLegacyStaleShape` helper that drops the legacy top-level `lore` block from `opencode.json` and the legacy top-level `lore` + plural `plugins` from `tui.json` before the additive merge. The migration is idempotent (rerun is a no-op on already-migrated files) and silently preserves all user-owned top-level keys.
- [x] 2.1 Removed code paths that wrote the broken top-level `lore`/plural `plugins` shape: `renderOpenCodeNativeConfig` is the only no-MCP renderer; `renderOpenCodeMCPConfig` extends it with the documented `mcp.lore` remote entry. The installer call site in `opencode_install.go` was updated.
- [x] 2.2 Repair/migration for existing installs is handled by `mergeOpenCodeConfigJSON` (which calls `migrateOpenCodeLegacyStaleShape`). End-to-end migration is verified by `TestOpenCodePlanOpenCodeInstallMigratesLegacyStaleShape` (re-renders the home directory from a legacy-shape `opencode.json` + `tui.json` and asserts both files are rewritten in the native shape with user-owned keys preserved).
- [x] 3.1 Added `TestOpenCodeMCPConfigRendersRemoteMCPBlock` (updated): asserts the post-repair opencode.json carries `$schema`, native `agent` overlay, `skills` block, `mcp.lore` with `type=remote`, normalized server URL, `managed_by: lore-cli` marker, Bearer Authorization header; and that there is NO top-level `lore` block.
- [x] 3.2 Added `TestOpenCodeTUISettingsUsesNativeShape` and updated `TestOpenCodePluginAssetsExcludeSddEngramAndLogo`: the new test asserts the `$schema` URL, the singular `plugin` string array, the absence of the legacy plural `plugins` and top-level `lore`, and that no Gentle-authored copy or placeholder URL is present.
- [x] 3.3 Added three migration tests in `opencode_install_test.go`:
  - `TestOpenCodeConfigJSONMergeMigratesLegacyTopLevelLoreBlock`
  - `TestOpenCodeConfigJSONMergeMigratesLegacyTuiJSONPluralPlugins`
  - `TestOpenCodePlanOpenCodeInstallMigratesLegacyStaleShape` (end-to-end plan/execute with on-disk legacy files)
- [x] 4.1 Updated `service.go` target description and the AGENTS.md managed-surface section in `renderOpenCodeAgentsMD` to describe the native OpenCode shape (`$schema`, native `agent` overlay, native `skills` block, no top-level Lore-only `lore`) and the migration contract.

## Files Changed Cumulative
| File | Action | Task(s) | Notes |
|------|--------|---------|-------|
| `internal/install/adapter_opencode.go` | Modified | 1.1, 2.1, 4.1 | Added native-shape constants; replaced `renderOpenCodeLoreBlock` with `renderOpenCodeNativeConfig`; updated `renderOpenCodeMCPConfig` to emit the native shape with `mcp.lore`; updated AGENTS.md copy; added `opencodePluginsDirPathKey` and `opencodeTUISettingsPathKey` to `ResolveOpenCodeLayout`. |
| `internal/install/opencode_install.go` | Modified | 1.1 | Updated call site to use `renderOpenCodeNativeConfig` instead of `renderOpenCodeLoreBlock`. |
| `internal/install/json_merge.go` | Modified | 1.3, 2.2 | Added `migrateOpenCodeLegacyStaleShape`; updated `mergeOpenCodeConfigJSON` to run the migration before the additive merge; updated function docstring. |
| `internal/install/assets/opencode/tui.json` | Rewritten | 1.2 | Native OpenCode shape: correct `$schema`, singular `plugin` string array, no top-level `lore`. |
| `internal/install/opencode_install_test.go` | Modified | 3.1, 3.3 | Updated existing tests to assert the new shape; added three new migration tests. |
| `internal/install/adapter_opencode_test.go` | Modified | 3.1, 4.1 | Updated `TestOpenCodeMCPConfigRendersRemoteMCPBlock`; updated AGENTS.md substring assertion. |
| `internal/install/adapter_opencode_plugins_test.go` | Modified | 3.2 | Updated `TestOpenCodePluginAssetsExcludeSddEngramAndLogo` for the new shape; added `TestOpenCodeTUISettingsUsesNativeShape`. |
| `internal/install/service.go` | Modified | 4.1 | Updated OpenCode target description in `supportedTarget`. |
| `openspec/changes/fix-opencode-native-config-after-readd/apply-started.md` | Created | bootstrap | Bounded-slice checkpoint. |
| `openspec/changes/fix-opencode-native-config-after-readd/apply-progress.md` | Created | bootstrap | Cumulative apply progress. |
| `openspec/changes/fix-opencode-native-config-after-readd/apply-report.md` | Created | bootstrap | Latest-slice report. |

## Validation Cumulative
| Command | Scope | Result | Notes |
|---------|-------|--------|-------|
| `go build ./...` | full module | passed | All packages compile. |
| `go test ./internal/install -run 'TestOpenCode|Test.*MCP|Test.*Plugin' -count=1 -v` | opencode suite (38 tests) | passed | Includes new migration tests and the new native-shape tui.json test. |
| `go test ./internal/install ./internal/cli ./internal/tui -count=1` | user-requested scope | passed | All three target packages green. |
| `go test ./... -count=1` | full module | passed | No regressions across `agentconfig`, `agentpack`, `auth`, `cli`, `config`, `httpclient`, `install`, `output`, `tui`, `update`, `version`. |

## Deviations and Risks
- The legacy `renderOpenCodeLoreBlock` function was renamed to `renderOpenCodeNativeConfig`. No external callers; the rename is internal-only and test sites were updated.
- The `opencodeTUISettingsPathKey` and `opencodePluginsDirPathKey` are now added to `ResolveOpenCodeLayout`'s `Paths` map so the migration test can resolve them through the layout. Backwards-compatible addition.
- The legacy `opencodeLoreBlockKey` constant is kept (instead of removed) because the `migrateOpenCodeLegacyStaleShape` helper needs it to detect the legacy top-level `lore` block in existing on-disk files. The constant is no longer USED to emit a new `lore` block in the renderer.
- The legacy `opencodeSchemaVersionKey` constant is kept in place even though the new shape no longer emits it. Cleanup to a follow-up slice if desired; the constant is unused in non-test code and does not affect runtime behavior.
- Lore memory persistence was NOT available in this runtime (Lore MCP tools not installed, no `lore_*` tools exposed, the `lore-memory` Pi extension is not installed). The OpenSpec fallback files (tasks.md, apply-started.md, apply-progress.md, apply-report.md) MUST remain in place as the source of truth until Lore tooling is wired into the runtime.

## Next Slice Recommendation
- Tasks: 4.2 (broad `go test ./...` validation, `go build ./...` smoke, install dry-run/apply) — actually completed as part of this slice since the user explicitly asked for `go test ./internal/install ./internal/cli ./internal/tui -count=1` and the broader `go test ./...` was run.
- Recommended next SDD phase: `sdd-verify` for an independent adversarial review of the repair slice.
- Lore sync: if/when the Lore runtime is wired in (Lore MCP server reachable, `lore_*` tools exposed), re-run this slice with Lore persistence enabled to migrate the OpenSpec fallback tasks.md to the canonical `sdd/fix-opencode-native-config-after-readd/tasks` topic_key in project `lore-cli` and remove the OpenSpec fallback files.
