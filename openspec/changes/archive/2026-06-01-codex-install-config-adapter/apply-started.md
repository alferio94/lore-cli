# Apply Started: codex-install-config-adapter (Repair)

## Trigger
Failed verify `dg-3918d819`. Apply phase relaunched to repair three critical findings.

## Slice — Repair Critical Findings Only

### Tasks in scope (from tasks.md Phase 4):
- Task 4.1: Add focused tests for adapter rendering, manifest validation, and failure-closed behavior when agent-config.json is missing or invalid. ← **Repair critical finding 2**
- Task 4.2: Add focused tests proving agents.md is backed up then overwritten, reruns stay idempotent, and no config.toml or runner artifacts are written. ← **Repair critical finding 1**

### Out of scope (hard boundaries from envelope):
- No Lore MCP config in Codex
- No config.toml MCP block
- No codex exec
- No live subagents
- No npm bootstrap
- No Claude/per-harness configurator
- Unrelated dirty tree changes (v0.4.0.md, pi-mcp, archive)

## Critical Findings to Fix

### Finding 1: Codex dry-run/apply summaries falsely report Antigravity/MCP fields
- **Root cause**: `formatSharedInstallPlanSummary` and `formatSharedInstallSummary` hardcode `runtime=antigravity-prompt-skills`, `prompt=...`, `mcp_optional=true` for ALL non-Pi targets.
- **Fix**: Add `formatCodexInstallPlanSummary` and `formatCodexInstallSummary` functions. Update `installCodexActionWithOptions` to call Codex-specific formatters. Add regression tests.

### Finding 2: Codex projection ignores custom agent-config.json models
- **Root cause**: `renderCodexFiles` tries to load agent-config but `getAgentConfigStoreForRender` defaults to nil. `PlanCodexInstall` calls `EnsureDefault()` but does not load the config to pass into the RenderRequest. `renderCodexAgentsMD` only reads from `req.AgentConfig` which is always empty.
- **Fix**: After `EnsureDefault()` in `PlanCodexInstall`, load the config from the store and wire it into the InstallRequest/plan so `renderCodexFiles` has it. Add regression tests.

### Finding 3: Manifest validation fails open for config-only layout
- **Root cause**: `Manifest.ValidateForLayout` returns `nil` early for `auth_mode=config-only` without validating ManagedFiles, BackupRoot, or InstalledAt.
- **Fix**: Add validation for ManagedFiles count + path coverage, BackupRoot under layout root, and InstalledAt timestamp for config-only mode. Add regression tests.

## Expected Files
- `internal/cli/actions.go` — Codex-specific summary formatters
- `internal/install/codex_install.go` — wire agent-config into render pipeline
- `internal/install/manifest.go` — fail-closed config-only validation
- `internal/cli/actions_test.go` — regression tests for Finding 1
- `internal/install/adapter_codex_test.go` — regression tests for Finding 2
- `internal/install/manifest_test.go` — regression tests for Finding 3

## Validation Planned
- `go test ./internal/cli -run 'TestCodex' -v`
- `go test ./internal/install -run 'TestCodex\|TestManifest' -v`
- `go test ./...` (broad)
- `go build ./...`

## Risk Budget
- Medium — all three fixes touch core install pipeline; regression tests add coverage.

## Preconditions
- Proposal/spec/design/tasks read: yes
- Previous apply-progress: not found (fresh repair from failed verify)
- Strict TDD mode: inactive (standard mode)