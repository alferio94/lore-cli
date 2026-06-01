# Apply Progress: codex-install-config-adapter (Repair)

## Status
- Mode: Standard
- Current slice: **completed** (all 3 critical findings fixed)
- Completed tasks: 3/3 (repair tasks 4.1, 4.2, 4.3)
- Total tasks in change: 8 original + 3 repair = 11; 11 complete

## Critical Findings Fixed

### Finding 1: Codex dry-run/apply summaries falsely report Antigravity/MCP fields
**Root cause**: `formatSharedInstallPlanSummary` and `formatSharedInstallSummary` hardcoded `runtime=antigravity-prompt-skills`, `prompt=...`, `mcp_optional=true` for ALL non-Pi targets.

**Fix applied**:
- Added `formatCodexInstallPlanSummary` — outputs `install_target=codex scope=config-only auth_mode=config-only ... mcp=none runner=none bootstrap=none`. No Antigravity runtime, prompt, or MCP claims.
- Added `formatCodexInstallSummary` — outputs `install_target=codex scope=config-only auth_mode=config-only ... mcp=none runner=none bootstrap=none`.
- Added `formatAntigravityInstallPlanSummary` — retains correct Antigravity semantics: `runtime=antigravity-prompt-skills`, `prompt=...`, `mcp_optional=true`.
- Added `formatAntigravityInstallSummary` — retains correct Antigravity semantics with `conflicted` count.
- Updated `formatSharedInstallPlanSummary` and `formatSharedInstallSummary` to dispatch to target-specific formatters.

### Finding 2: Codex projection ignores custom agent-config.json models
**Root cause**: `PlanCodexInstall` called `EnsureDefault()` but never loaded the config to wire into the render pipeline. `renderCodexAgentsMD` fell back to `agentpack.DefaultSDDModel` for all agents.

**Fix applied**:
- Added `AgentConfig agentconfig.Config` field to `InstallRequest` in `internal/install/harness.go`.
- In `PlanCodexInstall`, after `EnsureDefault()` succeeds, load the config from the store and assign to `req.AgentConfig`.
- In `renderCodexFiles`, prefer `req.AgentConfig` if `SchemaVersion != 0`; only fall back to store lookup for callers that bypass `PlanCodexInstall`.
- `renderCodexAgentsMD` already uses `req.AgentConfig.SDDAgents` correctly — the fix ensures it's populated.

### Finding 3: Manifest validation fails open for config-only layout
**Root cause**: `Manifest.ValidateForLayout` returned `nil` immediately for `auth_mode=config-only`, skipping all structural checks.

**Fix applied**:
- Added config-only validation block in `ValidateForLayout` that checks:
  - `ManagedFiles` must be non-empty
  - Each `ManagedFileRecord` must have `Path`, `Component`, and `MergeMode` set
  - `BackupRoot` must be under the layout backups directory
  - `InstalledAt` must be a valid RFC3339 timestamp

## Completed Tasks (cumulative)
- [x] 1.1 Target registration and layout (original)
- [x] 1.2 Thread agentconfig through install preflight (original)
- [x] 2.1 Implement Codex adapter and rendering (original)
- [x] 2.2 Add backup/idempotency logic (original)
- [x] 3.1 Expose Codex in CLI/TUI (original)
- [x] 3.2 Update README and docs (original)
- [x] 4.1 (Repair) Fix Codex summary formatters — no Antigravity false claims
- [x] 4.2 (Repair) Fix Codex agent-config source of truth for custom models
- [x] 4.3 (Repair) Fix config-only manifest validation to fail closed

## Files Changed
| File | Action | Task(s) | Notes |
|------|--------|---------|-------|
| `internal/cli/actions.go` | Modified | 4.1 | Added target-specific summary formatters; updated dispatch functions |
| `internal/cli/actions_test.go` | Modified | 4.1 | Added 3 regression tests for Codex summary honesty |
| `internal/install/harness.go` | Modified | 4.2 | Added `AgentConfig agentconfig.Config` to `InstallRequest` |
| `internal/install/codex_install.go` | Modified | 4.2 | Load config after EnsureDefault; wire into render; prefer request config in render |
| `internal/install/adapter_codex_test.go` | Modified | 4.2 | Added `TestCodexInstallUsesCustomAgentConfigModels` regression test |
| `internal/install/manifest.go` | Modified | 4.3 | Added fail-closed validation for config-only auth_mode |
| `internal/install/manifest_test.go` | Modified | 4.3 | Added `TestManifestValidateForLayoutConfigOnlyFailsClosed` (7 subcases) and `TestManifestValidateForLayoutConfigOnlyPassesValid` |
| `openspec/changes/codex-install-config-adapter/tasks.md` | Modified | all | Updated Phase 4 with repair task details |

## Validation
| Command | Scope | Result | Notes |
|---------|-------|--------|-------|
| `go build ./...` | All | ✅ PASS | Clean build |
| `go test -count=1 ./internal/cli -run 'TestCodex\|TestInstall'` | CLI | ✅ PASS | All 3 new Codex summary tests + all Antigravity tests pass |
| `go test -count=1 ./internal/install -run 'TestCodex\|TestManifest'` | Install | ✅ PASS | 16 tests pass including new regression tests |
| `go test -count=1 ./...` | Broad | ✅ PASS | All packages pass |

## New Regression Tests Added
1. `TestCodexInstallPlanSummaryNoAntigravityRuntime` — verifies Codex dry-run/apply summaries contain no Antigravity runtime, prompt, or mcp_optional claims
2. `TestCodexInstallSummaryNoAntigravityRuntime` — verifies Codex apply summaries contain scope=config-only, mcp=none, runner=none, bootstrap=none
3. `TestCodexInstallPlanSummaryIncludesManagedActions` — verifies Codex plan summaries honestly list managed file actions
4. `TestCodexInstallUsesCustomAgentConfigModels` — verifies persisted agent-config.json custom model (gpt-4o for sdd-verify) appears in generated agents.md
5. `TestManifestValidateForLayoutConfigOnlyFailsClosed` — 7 subcases: missing managed_files, missing path/component/merge_mode, backup_root outside backups, invalid installed_at, wrong target
6. `TestManifestValidateForLayoutConfigOnlyPassesValid` — verifies well-formed config-only manifest passes validation

## Deviations and Risks
- None. All three critical findings fixed as specified.

## Next Recommendation
- Proceed to `sdd-verify` phase to validate the repaired implementation against the original spec.
- The three regression tests in `internal/cli/actions_test.go`, `internal/install/adapter_codex_test.go`, and `internal/install/manifest_test.go` provide coverage for all three critical findings.