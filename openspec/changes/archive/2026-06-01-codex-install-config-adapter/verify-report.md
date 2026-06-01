## Verification Report

**Change**: codex-install-config-adapter  
**Mode**: Standard  
**Artifact mode**: Hybrid (filesystem + Lore)  
**Date**: 2026-06-01

---

### Completeness
| Metric | Value |
|--------|-------|
| Tasks total | 11 |
| Tasks complete | 11 |
| Tasks incomplete | 0 |

Approved task checklist in `openspec/changes/codex-install-config-adapter/tasks.md` is fully checked off, including repair tasks 4.1–4.3 and the orchestrator copy-only repair recorded in `sdd/codex-install-config-adapter/apply-report-copy-repair` (`24db3de1-061e-4047-bcfe-bb3492679cd2`).

---

### Build & Tests Execution

**Focused verify reruns**: ✅ Passed

Commands executed in this verify rerun:
```bash
go test -count=1 ./internal/tui -run 'TestInstall'
go test -count=1 ./internal/cli -run 'TestInstall|TestCodex'
```

Results:
- `internal/tui`: PASS
- `internal/cli`: PASS

**Fresh orchestrator validation accepted as evidence**: ✅ Passed

Commands supplied in the handoff and treated as fresh runtime evidence for unchanged implementation behavior after the copy repair:
```bash
gofmt -w internal/tui/root.go
go test -count=1 ./internal/tui -run 'TestInstall'
go test -count=1 ./internal/cli -run 'TestInstall|TestCodex'
go build ./...
go test -count=1 ./...
```

Results:
- `gofmt`: applied successfully
- focused CLI/TUI tests: PASS
- build: PASS
- repository-wide tests: PASS

**Coverage**: ➖ Not available

---

### Copy Repair Verification

1. **Top-level TUI wording is repaired**
   - `internal/tui/root.go` now describes Codex as a supported config-only projection target.
   - The same string keeps Claude Code/OpenCode as `Coming soon` only.
   - Focused TUI tests still pass after the wording update.

2. **README wording is repaired**
   - The top-level install overview now states Codex is supported as a config-only target.
   - README install details explicitly preserve the hard boundaries: no MCP, no runner, no live subagents, no npm/bootstrap.
   - Agent-config diagnostics wording now says Codex consumes the contract for config-only projection only, without implying runtime support.

3. **No false Codex capability claims were introduced**
   - `internal/cli/actions.go` still emits Codex summaries with `scope=config-only`, `auth_mode=config-only`, `mcp=none`, `runner=none`, and `bootstrap=none`.
   - Static scan across Codex paths found no new Codex `config.toml`, no Lore MCP server block, and no `codex exec` path.
   - Existing Codex regression tests remain green.

---

### Spec Compliance Matrix

| Requirement | Scenario | Test / Evidence | Result |
|-------------|----------|-----------------|--------|
| Codex target is selectable | CLI exposes Codex help/target usage | `internal/cli/install_flags_test.go > TestInstallUsageIncludesTargetAndComponentFlags`; `internal/install/service_test.go > TestDefaultTargetsPreferPiAndMarkOthersComingSoon`; static review of `internal/cli/app.go` | ✅ COMPLIANT |
| Codex target is selectable | TUI selection can move to and execute supported Codex target set | `internal/tui/model_test.go > TestInstallTargetSelectionMovesBetweenSupportedTargetsOnly`; `internal/tui/model_test.go > TestNavigationAndInstallTargetSelectionMessage`; focused `go test -count=1 ./internal/tui -run 'TestInstall'` | ✅ COMPLIANT |
| Codex install uses Lore-owned agent config | Missing `agent-config.json` is created before planning | `internal/install/adapter_codex_test.go > TestPlanCodexInstallCreatesAgentConfig` | ✅ COMPLIANT |
| Codex install uses Lore-owned agent config | Persisted custom models drive `agents.md` projection | `internal/install/adapter_codex_test.go > TestCodexInstallUsesCustomAgentConfigModels` | ✅ COMPLIANT |
| Codex scope remains config-only | Managed output is limited to `~/.codex/agents.md`, `~/.codex/skills/*`, and manifest | `internal/install/adapter_codex_test.go > TestExecuteCodexInstallCreatesFiles`; static review of `ResolveCodexLayout`, `renderCodexFiles`, `buildCodexManifest` | ✅ COMPLIANT |
| Existing `agents.md` is backed up then overwritten | Existing file is preserved before replacement | `internal/install/adapter_codex_test.go > TestExecuteCodexInstallBackupExistingAgentsMD` | ✅ COMPLIANT |
| Manifest, idempotency, and dry-run stay safe | Reruns are stable and config-only manifests fail closed | `internal/install/adapter_codex_test.go > TestExecuteCodexInstallIdempotent`; `internal/install/manifest_test.go > TestManifestValidateForLayoutConfigOnlyFailsClosed`; `internal/install/manifest_test.go > TestManifestValidateForLayoutConfigOnlyPassesValid` | ✅ COMPLIANT |
| No-MCP guarantee is enforced | No Codex MCP/TOML/runner artifacts are written | `internal/install/adapter_codex_test.go > TestExecuteCodexInstallNoConfigToml`; `internal/install/adapter_codex_test.go > TestCodexAdapterRenderNoMCP`; static scan for `codex exec`, `config.toml`, `mcpServers`, `[mcp_servers]` | ✅ COMPLIANT |
| Codex diagnostics stay contract-focused | Dry-run/apply output stays Codex-specific and action-oriented | `internal/cli/actions_test.go > TestCodexInstallPlanSummaryNoAntigravityRuntime`; `internal/cli/actions_test.go > TestCodexInstallSummaryNoAntigravityRuntime`; `internal/cli/actions_test.go > TestCodexInstallPlanSummaryIncludesManagedActions`; focused `go test -count=1 ./internal/cli -run 'TestInstall|TestCodex'` | ✅ COMPLIANT |
| Hard boundaries stay intact | No Codex Lore MCP config, no `config.toml` MCP block, no `codex exec`, no live subagents, no npm bootstrap, no Claude/per-harness configurator | static review of `internal/install/adapter_codex.go`, `internal/install/codex_install.go`, `internal/cli/actions.go`, `README.md`, `internal/tui/root.go` | ✅ COMPLIANT |

**Compliance summary**: 10/10 scenarios compliant

---

### Correctness (Static — Structural Evidence)

| Requirement | Status | Notes |
|------------|--------|-------|
| Codex target wiring exists in shared install registry/service | ✅ Implemented | `defaultInstallRegistry`, `DefaultTargets`, `ResolveInstallTarget`, CLI/TUI routing all recognize Codex as supported. |
| Codex projection uses `agentpack` + `agent-config` as source of truth | ✅ Implemented | `PlanCodexInstall` loads persisted config; `renderCodexAgentsMD` maps SDD agent models from `req.AgentConfig`. |
| Codex install remains config-only under `~/.codex` | ✅ Implemented | Layout limits output to `agents.md`, `skills/*/SKILL.md`, `lore-install.json`, and backup paths. |
| Backup + overwrite semantics for `agents.md` | ✅ Implemented | Existing file plans `update` with `BackupPath` under `~/.codex/backups/<timestamp>/...`. |
| Idempotency and dry-run safety | ✅ Implemented | Planned actions classify `create/update/unchanged`; second install yields unchanged actions. |
| Manifest validation fails closed for config-only layout | ✅ Implemented | Config-only validation now checks required file metadata, backup root, and timestamp. |
| Codex diagnostics avoid false runtime claims | ✅ Implemented | Target-specific formatter split prevents inherited Antigravity wording and the repaired copy now matches that contract. |
| Hard no-MCP / no-runner / no-bootstrap boundaries | ✅ Implemented | No Codex `config.toml`, no MCP block, no runner/bootstrap/subagent wiring present. |

---

### Coherence (Design)

| Decision | Followed? | Notes |
|----------|-----------|-------|
| New `codexAdapter` on shared install path | ✅ Yes | Shared-harness registry and Codex-specific adapter/plan/apply path are used. |
| `~/.codex` layout with `agents.md`, `skills`, manifest, backups | ✅ Yes | `ResolveCodexLayout` and apply path match the intended layout. |
| `agents.md` replace semantics with backup-first updates | ✅ Yes | Existing managed content is backed up before overwrite. |
| `agent-config.json` is the model source of truth | ✅ Yes | Persisted config is loaded and used for rendering. |
| No Codex MCP/runtime config | ✅ Yes | No `config.toml`, MCP server block, runner, or bootstrap path exists. |
| Config-only manifests fail closed | ✅ Yes | Validation now blocks malformed config-only manifests. |
| TUI/README copy should not label Codex as roadmap-only | ✅ Yes | The warning-level stale wording is now repaired in both `internal/tui/root.go` and `README.md`. |

---

### Files Reviewed

- `internal/tui/root.go`
- `README.md`
- `internal/cli/actions.go`
- `internal/cli/actions_test.go`
- `internal/cli/app.go`
- `internal/cli/install_flags_test.go`
- `internal/install/adapter_codex.go`
- `internal/install/adapter_codex_test.go`
- `internal/install/codex_install.go`
- `internal/install/manifest.go`
- `internal/install/service.go`
- `internal/tui/model_test.go`
- `openspec/changes/codex-install-config-adapter/tasks.md`
- `openspec/changes/codex-install-config-adapter/verify-report.md` (updated by this rerun)

---

### Unrelated Dirty Tree Observations

Repository state is still dirty beyond this change. Notable unrelated or cross-change work present during verification includes:
- `docs/releases/v0.4.0.md`
- Pi hosted-MCP related files such as `internal/install/assets/pi/settings.json`, `internal/install/assets/pi/mcp.json`, `internal/install/pi.go`, `internal/install/mcp_config_test.go`
- other OpenSpec change directories under `openspec/changes/`

These were kept separate from the Codex verdict and do not independently fail this verification.

---

### Issues Found

**CRITICAL** (must fix before archive):
None.

**WARNING** (should fix):
None.

**SUGGESTION** (nice to have):
1. Add a small regression test covering the root TUI install menu copy so supported-target messaging cannot drift from actual target availability.

---

### Verdict
PASS

The warning-level stale wording is fixed, Codex still matches the approved config-only install contract without false MCP/runner claims, focused verify reruns passed, and the supplied fresh build/repository-wide test evidence remains green.