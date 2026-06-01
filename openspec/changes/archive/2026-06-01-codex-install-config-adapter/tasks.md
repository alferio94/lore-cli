# Tasks: Codex Install Config Adapter

Out of scope: Lore MCP config, `config.toml` MCP blocks, Codex runner/live subagents, npm bootstrap, Claude/per-harness configurators.

## Phase 1: Target registration and layout
- [x] 1.1 Add Codex target metadata and registry wiring in `internal/install/adapter.go`, `harness.go`, `service.go`, and `components.go`; define `~/.codex` root, managed `agents.md`, `skills/*/SKILL.md`, and manifest/backup paths.
- [x] 1.2 Thread `agentconfig.Config` and `AgentConfigStore.EnsureDefault()` through install preflight in `internal/install/service.go` so Codex planning always starts from Lore-owned `agent-config.json`.
Validation: `go test ./internal/install -run 'Test(DefaultTargets|ResolveInstallTarget|FormatTargetSelection|CheckAgentConfig)' -v`.

## Phase 2: Codex rendering and plan/apply
- [x] 2.1 Implement `internal/install/adapter_codex.go` and `internal/install/codex_install.go` to render deterministic Lore-managed `agents.md` plus skills from `agentpack` + `agentconfig`, with replace semantics and manifest entries.
- [x] 2.2 Add backup/idempotency logic for existing `~/.codex/agents.md` and managed skills, including dry-run action classification and byte-stable reruns.
Validation: temp-home tests covering create/update/unchanged, backup creation, and no writes outside `~/.codex`.

## Phase 3: CLI/TUI exposure and docs
- [x] 3.1 Expose Codex as selectable in CLI help/flags and TUI target navigation via `internal/cli/actions.go`, `app.go`, `internal/tui/root.go`, and target-selection tests.
- [x] 3.2 Update `README.md` and install/status wording to describe Codex as config-only projection, with explicit no-MCP/no-runner/no-bootstrap language.
Validation: CLI/TUI tests for target visibility and summary text.

## Phase 4: Verification (repair completed)
- [x] 4.1 (Repair) Fix target-specific summary formatters so Codex diagnostics use config-only wording (no Antigravity runtime/prompt/MCP claims) and Antigravity diagnostics retain correct semantics. Added `formatCodexInstallSummary`, `formatCodexInstallPlanSummary`, `formatAntigravityInstallSummary`, `formatAntigravityInstallPlanSummary`. Added regression tests: `TestCodexInstallPlanSummaryNoAntigravityRuntime`, `TestCodexInstallSummaryNoAntigravityRuntime`, `TestCodexInstallPlanSummaryIncludesManagedActions`.
- [x] 4.2 (Repair) Fix Codex projection to use persisted `agent-config.json` custom models as source of truth. Added `AgentConfig` field to `InstallRequest`, wired `AgentConfigStore.Load()` after `EnsureDefault()` in `PlanCodexInstall`, updated `renderCodexFiles` to prefer `req.AgentConfig`. Added regression test: `TestCodexInstallUsesCustomAgentConfigModels`.
- [x] 4.3 (Repair) Fix `Manifest.ValidateForLayout` for config-only mode to fail closed on missing/invalid ManagedFiles, BackupRoot, and InstalledAt. Added config-only validation block before the cli-request checks. Added regression tests: `TestManifestValidateForLayoutConfigOnlyFailsClosed` (7 subcases), `TestManifestValidateForLayoutConfigOnlyPassesValid`.
Validation: focused `go test` on `internal/install`, `internal/cli`, and `internal/tui` — all passing. Broad `go test ./...` — all passing.