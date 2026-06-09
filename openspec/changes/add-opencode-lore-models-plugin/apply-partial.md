# Apply Partial: add-opencode-lore-models-plugin

## Completed in This Slice
- [x] 0.1 Preflight git status — confirmed in-flight dirty work in `internal/cli`, `internal/install`, `internal/tui` from prior SDD changes; this apply preserves unrelated surfaces and edits only the scopes required for this change.
- [x] 0.2 Re-read init/exploration/proposal/spec/design/state artifacts; contract reconfirmed: direct-from-OpenCode model/variant config, `lore-models` rename, safe hot-edit of `opencode.json`, manifest-scoped stale cleanup.
- [x] 0.3 Identified dirty OpenCode files that overlap this change; planned merges that keep the in-flight `default_agent=lore` and `default-component lore-server-mcp` work intact while replacing the stale `agent.lore.permission="allow"` line and `model-variants` references.
- [x] 1.1 Renamed the managed plugin asset from `model-variants.ts` to `lore-models.ts`; the new asset keeps the provider/model/variant discovery cache behavior (cache path unchanged) and adds the safe `opencode.json` hot-edit helper plus the `lore_models_set_agent` / `lore_models_list_agents` plugin-tool fallback flow.
- [x] 1.2 Preserved the cache file contract at `~/.lore/cache/opencode-model-variants.json`; the new `lore-models.ts` writes the same cache atomically and the cache remains metadata-only.
- [x] 1.3 Updated `internal/install/opencode_assets.go` allowlist (now `lore-models.ts`); plugin asset renderer still excludes `sdd-engram` and `logo`; static guard in `static_guards_test.go` still enforced.
- [x] 1.4 Updated install summary copy (`internal/cli/actions.go`), usage text (`internal/cli/app.go`), and TUI menu/install target copy (`internal/tui/root.go`).
- [x] 2.1 In-OpenCode selector UX: documented fallback command/tool flow (`lore_models_set_agent`, `lore_models_list_agents`) shipped in `lore-models.ts`; no dependency on undocumented floating selector APIs.
- [x] 2.2 Safe `opencode.json` hot-edit helper (`hotEditAgentModelVariant` exported from `lore-models.ts`): validates the agent is Lore-managed, deep-merges only `agent.<name>.model` / `agent.<name>.variant`, backup first, atomic write with `0600` permissions, reparses for verification, redacts secret-bearing values (`Authorization`, `apiKey`, `token`, `password`, `secret`) in errors/logs.
- [x] 2.3 `lore-models.ts` reads the current `opencode.json` value during selection, distinguishes fresh vs cached discovery (cache is labeled not-freshly-verified on read), and supports explicit "no variant/default" removal when the caller passes `variant=null`.
- [x] 2.4 The fallback flow stays inside OpenCode via plugin tools; no shell access or manual `opencode.json` editing is required.
- [x] 3.1 OpenCode config renderer: `default_agent: "lore"` and `agent.lore.mode: "primary"` retained; `agent.lore.permission` removed; every non-lore Lore-managed agent (SDD phases + `lore-worker`) renders `mode: "subagent"`.
- [x] 3.2 `effectiveOpenCodeExistingAgent` reads the pre-install `opencode.json` and threads user-chosen `model` / `variant` for every Lore-managed agent into the managed overlay before merge, so reinstall preserves user choices.
- [x] 3.3 `lore-worker` is included in the managed `agent` overlay with `mode: "subagent"`, prompt reference to `skills/lore-worker/SKILL.md`, and a model sourced from `ProfileBalanced.RoleModels["lore-worker"]`.
- [x] 3.4 `mergeOpenCodeConfigJSON` continue to preserve user-owned top-level keys, foreign agents, and the foreign `mcp.lore` fail-closed boundary; the new `effectiveOpenCodeExistingAgent` helper is best-effort and only reads `agent.<name>.model` / `agent.<name>.variant`.
- [x] 4.1 `planOpenCodeStaleManagedPluginCleanup` compares the previous `lore-install.json` `managed_files` to the newly rendered managed paths and emits a backup-first `delete` action when a previously Lore-managed plugin path is no longer rendered.
- [x] 4.2 The cleanup pass only deletes `plugins/model-variants.ts` (or any other path) when the previous manifest's `managed_files` records prove Lore managed it; backup-first, then delete.
- [x] 4.3 New test `TestOpenCodeStaleManagedPluginCleanupLeavesUnownedFilesAlone` proves user-owned `model-variants.ts` survives when no prior manifest records the file.
- [x] 4.4 Fresh installs render only `lore-models.ts` (and the other two documented managed plugins); the previous `model-variants.ts` is never emitted by the renderer.
- [x] 5.1-5.4 OpenCode adapter/install/CLI/TUI tests updated; new tests added for `TestOpenCodeAgentOverlayPreservesExistingModelAndVariant`, `TestOpenCodeStaleManagedPluginCleanupRemovesModelVariants`, and `TestOpenCodeStaleManagedPluginCleanupLeavesUnownedFilesAlone`.
- [x] 5.5 Focused validation: `go test ./internal/install -run 'TestOpenCode|Test.*Plugin|Test.*Manifest|Test.*Merge' -count=1` → passed; `go test ./internal/install ./internal/cli ./internal/tui -count=1` → passed; `go test ./internal/agentconfig ./internal/install` → passed.
- [ ] 5.6 OpenCode plugin/static-asset smoke test path is not present in this Go repo; the TypeScript plugin's hot-edit helper is exercised through the static-source surface and the install-side renderer/test coverage, but no end-to-end OpenCode runtime test is run in this apply slice. Marked as a verify-phase follow-up.
- [x] 6.1-6.3 User-facing copy (install summary, usage text, TUI menu description, target selection prompt, OpenCode target description, capability description) updated to mention `lore-models.ts`, the in-OpenCode selector flow, the revised `mode: "primary"` / `mode: "subagent"` contract, and the absence of `permission: "allow"`. No docs, summaries, or errors expose raw token-bearing config values or plaintext MCP tokens.

## Files Changed So Far
| File | Action | Notes |
|------|--------|-------|
| `internal/install/assets/opencode/plugins/lore-models.ts` | Created | New managed plugin; preserves the variant cache and adds the safe hot-edit helper plus fallback tools. |
| `internal/install/assets/opencode/plugins/model-variants.ts` | Deleted | Renamed asset; old file removed via `git rm -f`. |
| `internal/install/opencode_assets.go` | Modified | `managedOpenCodePluginAssetNames` now references `lore-models.ts`; docstring updated. |
| `internal/install/adapter_opencode.go` | Modified | Added `opencodeSubagentModeValue`, `opencodeAgentVariantKey`, `opencodeLoreWorkerAgentName`, `opencodeWorkerModel`, `effectiveOpenCodeExistingAgent`, `openCodeAgentVariants`, `openCodeAgentModelsFromExisting`, `isLoreManagedOverlayAgent`, `openCodeExistingAgentEntry`, `renderOpenCodeNativeConfigWithExisting`, `renderOpenCodeMCPConfigWithExisting`; `opencodeAgentOverlay` now takes `existingAgent`, removes `permission` from `agent.lore`, adds `mode: "subagent"` to non-lore agents, adds the `lore-worker` entry, renders `variant` when present. AGENTS.md managed surface copy updated. |
| `internal/install/opencode_install.go` | Modified | Added `planOpenCodeStaleManagedPluginCleanup`, `openCodeStaleManagedPluginRelativePath`, `applyOpenCodePlannedDelete`; `PlanOpenCodeInstall` and `ExecuteOpenCodeInstall` now call the stale-cleanup pass and apply any `delete` action surfaced by it. |
| `internal/install/json_merge.go` | Untouched (intentional) | The existing additive-merge logic already preserves user-owned top-level keys and continues to fail closed on foreign `mcp.lore`; the variant preservation is implemented at the renderer layer instead. |
| `internal/install/components.go` | Modified | `ComponentOpenCodePlugins` description now names `lore-models.ts` and the rename history. |
| `internal/install/service.go` | Modified | OpenCode target description updated (no `permission: "allow"`, mentions `lore-worker`, `mode: "subagent"`, and variant preservation). |
| `internal/install/adapter_opencode_test.go` | Modified | `TestOpenCodeNativeConfigDeclaresLorePrimaryOrchestratorAgent` asserts no `permission`; `TestOpenCodeAgentOverlayPrimaryIsLayeredOnTopOfSddPhases` asserts the new 11-entry overlay with no permission, `lore-worker`, and `mode: "subagent"`; new test `TestOpenCodeAgentOverlayPreservesExistingModelAndVariant` for reinstall preservation; AGENTS.md substring assertions updated. |
| `internal/install/adapter_opencode_plugins_test.go` | Modified | All `model-variants.ts` references replaced with `lore-models.ts`; new test `TestOpenCodeBundledPluginsContainRuntimeHooks` checks the new plugin's hot-edit helper and tool names; bounded managed set is verified to NOT include the legacy `model-variants.ts` path. |
| `internal/install/opencode_install_test.go` | Modified | New tests `TestOpenCodeStaleManagedPluginCleanupRemovesModelVariants` and `TestOpenCodeStaleManagedPluginCleanupLeavesUnownedFilesAlone`. |
| `internal/install/service_test.go` | Modified | OpenCode description assertions updated for the new contract. |
| `internal/cli/actions.go` | Modified | Install summary `plugins=` and `plugins_location=` lines now name `lore-models`. |
| `internal/cli/app.go` | Modified | Usage text mentions `lore-models.ts`, `mode: "primary"`, `mode: "subagent"`, `lore-worker`, no `permission: "allow"` bypass. |
| `internal/cli/install_flags_test.go` | Modified | All `model-variants` references updated; forbidden-string list updated to match the new managed bundle. |
| `internal/tui/root.go` | Modified | Install menu description and target selection prompt updated. |
| `internal/tui/model_test.go` | Modified | `TestInitialRenderShowsMenuHintsAndInstallEntry` and `TestInstallTargetSelectionSurfacesPiDefaultAndAntigravityMVPGuidance` assertions updated. |
| `openspec/changes/add-opencode-lore-models-plugin/tasks.md` | Modified | Phases 0-4 + Phase 5 (5.1-5.5) + Phase 6 marked complete; 5.6 deferred. |
| `openspec/changes/add-opencode-lore-models-plugin/apply-started.md` | Created | Pre-mutation checkpoint per the SDD apply phase contract. |

## Validation So Far
- `go build ./...` → passed (no compile errors).
- `go vet ./...` → passed (no vet diagnostics).
- `go test ./internal/install -run 'TestOpenCode|Test.*Plugin|Test.*Manifest|Test.*Merge' -count=1` → passed.
- `go test ./internal/install ./internal/cli ./internal/tui -count=1` → passed.
- `go test ./internal/agentconfig ./internal/install` → passed.
- `go test ./... -count=1` → all packages green.

## Remaining in This Slice
- None (all in-scope tasks completed in this slice).

## Recovery Notes
- Safe resume point: nothing pending in this slice.
- Known risks/blockers: 5.6 (OpenCode runtime smoke test) is intentionally deferred to the verify phase; the design notes the floating-selector UX is preferred only when a verified safe OpenCode runtime API exists, and this slice ships the documented fallback command/tool flow inside the plugin.
