# Apply Progress: add-opencode-lore-models-plugin

## Status
- Mode: Standard (Strict TDD mode is not declared by `openspec/changes/add-opencode-lore-models-plugin/init.md`).
- Current slice: completed.
- Completed tasks: 22/23 in `tasks.md` (all in-scope tasks; 5.6 deferred to verify).
- Previous progress: none (no prior `apply-progress` was found in Lore memory for this change).

## Completed Tasks Cumulative
- [x] 0.1 Preflight git status (in-flight dirty work noted and preserved).
- [x] 0.2 Re-read all active change artifacts and reconfirmed the contract.
- [x] 0.3 Identified and planned merges for dirty OpenCode files.
- [x] 1.1 Renamed the managed plugin asset from `model-variants.ts` to `lore-models.ts`.
- [x] 1.2 Preserved the cache file contract at `~/.lore/cache/opencode-model-variants.json`.
- [x] 1.3 Updated OpenCode asset allowlist and static guards.
- [x] 1.4 Updated install summaries, help copy, and tests that named `model-variants`.
- [x] 2.1 Wired the `lore-models` in-OpenCode entrypoint with the documented fallback flow.
- [x] 2.2 Implemented the safe `opencode.json` hot-edit path (atomic, backed up, secret-redacting).
- [x] 2.3 Implemented fresh-vs-cached discovery distinction and explicit "no variant" removal.
- [x] 2.4 Kept the fallback interaction entirely inside OpenCode.
- [x] 3.1 Removed `agent.lore.permission`; added `mode: "subagent"` to non-lore agents.
- [x] 3.2 Threaded `effectiveOpenCodeExistingAgent` (existing `opencode.json` `agent.<name>.{model,variant}`) into the renderer for reinstall preservation.
- [x] 3.3 Included `lore-worker` in the managed `agent` overlay.
- [x] 3.4 Preserved unrelated user-owned config, foreign agents, and the foreign `mcp.lore` fail-closed boundary.
- [x] 4.1 Added a stale managed-file cleanup pass for OpenCode (`planOpenCodeStaleManagedPluginCleanup`).
- [x] 4.2 Delete is gated on previous manifest ownership; backup first, then delete.
- [x] 4.3 User-owned plugin files without prior manifest ownership are preserved.
- [x] 4.4 Fresh installs render only `lore-models.ts`.
- [x] 5.1-5.5 Updated tests and ran focused validations.
- [ ] 5.6 OpenCode plugin/runtime smoke test path is not present in this Go repo; deferred to verify.
- [x] 6.1-6.3 Updated user-facing copy; no token leakage in any surface.

## Files Changed Cumulative
| File | Action | Task(s) | Notes |
|------|--------|---------|-------|
| `internal/install/assets/opencode/plugins/lore-models.ts` | Created | 1.1, 2.1-2.4 | New managed plugin (preserves cache, adds hot-edit + tools). |
| `internal/install/assets/opencode/plugins/model-variants.ts` | Deleted | 1.1 | Renamed asset removed via `git rm -f`. |
| `internal/install/opencode_assets.go` | Modified | 1.3 | Allowlist updated to `lore-models.ts`. |
| `internal/install/adapter_opencode.go` | Modified | 1.3, 1.4, 3.1-3.4, 6.1-6.3 | Renderer + AGENTS.md copy + capability description updated. |
| `internal/install/opencode_install.go` | Modified | 4.1-4.3, 5.5 | Stale cleanup pass + apply-delete helper. |
| `internal/install/components.go` | Modified | 1.3, 1.4 | Component description updated. |
| `internal/install/service.go` | Modified | 1.4, 6.1-6.3 | OpenCode target description updated. |
| `internal/install/adapter_opencode_test.go` | Modified | 5.1, 5.2 | Overlay + agent overlay tests updated; new preservation test. |
| `internal/install/adapter_opencode_plugins_test.go` | Modified | 1.3, 5.1, 5.3, 5.4 | Plugin assets tests + new assertions. |
| `internal/install/opencode_install_test.go` | Modified | 4.2, 4.3, 5.3, 5.4 | New stale-cleanup tests added. |
| `internal/install/service_test.go` | Modified | 5.4, 6.1-6.3 | Description assertions updated. |
| `internal/cli/actions.go` | Modified | 1.4, 5.4 | Summary `plugins=` and `plugins_location=` lines updated. |
| `internal/cli/app.go` | Modified | 1.4, 5.4, 6.1-6.3 | Usage text updated. |
| `internal/cli/install_flags_test.go` | Modified | 1.4, 5.4 | All `model-variants` references updated; forbidden strings updated. |
| `internal/tui/root.go` | Modified | 1.4, 5.4, 6.1-6.3 | Menu description + target selection prompt updated. |
| `internal/tui/model_test.go` | Modified | 5.4, 6.1-6.3 | Description assertions updated. |
| `openspec/changes/add-opencode-lore-models-plugin/tasks.md` | Modified | All | Checklist progress updated. |
| `openspec/changes/add-opencode-lore-models-plugin/apply-started.md` | Created | Slice contract | Pre-mutation checkpoint. |
| `openspec/changes/add-opencode-lore-models-plugin/apply-partial.md` | Created | Slice contract | Per-task checkpoint per SDD apply contract. |

## Validation Cumulative
| Command | Scope | Result | Notes |
|---------|-------|--------|-------|
| `go build ./...` | Whole repo | passed | No compile errors. |
| `go vet ./...` | Whole repo | passed | No vet diagnostics. |
| `go test ./internal/install -run 'TestOpenCode\|Test.*Plugin\|Test.*Manifest\|Test.*Merge' -count=1` | Focused install regression | passed | All 30+ targeted tests green, including the new `TestOpenCodeStaleManagedPluginCleanup*` and `TestOpenCodeAgentOverlayPreservesExistingModelAndVariant` tests. |
| `go test ./internal/install ./internal/cli ./internal/tui -count=1` | Broader OpenCode surface | passed | All install/CLI/TUI tests green. |
| `go test ./internal/agentconfig ./internal/install` | Cross-target agent-config | passed | No regression. |
| `go test ./... -count=1` | Whole repo | passed | All packages green. |

## Deviations and Risks
- **Deviation:** `permission: "allow"` was present in the dirty worktree from a prior SDD change and was removed in this apply per the spec for this change. The remove is intentional and is documented in the AGENTS.md managed surface copy.
- **Deviation:** The `model-variants.ts` cache path (`~/.lore/cache/opencode-model-variants.json`) is intentionally preserved. The cache identity is metadata-only and the spec/design call for keeping the cache file path stable across the rename.
- **Risk:** Task 5.6 (OpenCode runtime smoke test) is intentionally deferred; there is no Go-side smoke test path for the TypeScript plugin in this repository. The plugin's hot-edit helper is exercised through the static source and the install-side renderer/test coverage. End-to-end OpenCode runtime testing is a verify-phase follow-up.
- **Risk:** The plugin's preferred floating-selector UX was not implemented because the opencode docs and schema review (per exploration.md) could not verify a safe public OpenCode dialog API to plugins. The implementation ships the documented fallback command/tool flow (`lore_models_set_agent`, `lore_models_list_agents`) inside OpenCode and explicitly avoids unsafe undocumented UI calls as the only configuration path.
- **Risk:** Reinstall preservation reads the pre-install `opencode.json` best-effort; a parse failure or missing file falls back to managed defaults. The additive merge in `mergeOpenCodeConfigJSON` still rejects malformed `opencode.json` upstream with a JSON-decode error, so a double-report condition is not introduced.

## Next Slice Recommendation
- Tasks: 5.6 only.
- Why this next: the verify phase owns the end-to-end OpenCode runtime smoke test; this apply slice has done everything in its bounded scope and the remaining 5.6 is explicitly out-of-slice for apply.
- Recommended next phase: `sdd-verify`.
