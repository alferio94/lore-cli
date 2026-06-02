## Verification Report

**Change**: opencode-mcp-config-support  
**Mode**: Standard  
**Artifact mode**: Hybrid fallback (Lore save attempted; filesystem copy written)  
**Date**: 2026-06-02

---

### Completeness

| Metric | Value |
|--------|-------|
| Filesystem tasks total | 14 |
| Filesystem tasks complete | 14 |
| Filesystem tasks incomplete | 0 |
| Canonical Lore tasks artifact | Stale (original 10 unchecked items) |

Notes:
- The filesystem checkpoint `openspec/changes/opencode-mcp-config-support/tasks.md` reflects the repair completion state and shows all original + repair tasks complete.
- The canonical Lore tasks artifact (`fed5aea0-9fc1-434e-985f-538fa899e3a6`) still shows the pre-repair unchecked checklist, so Lore task traceability remains stale.

---

### Build & Tests Execution

**Build**: ✅ Passed
```bash
go build ./...
```

**Focused install verification rerun**: ✅ Passed
```bash
go test -count=1 ./internal/install -run 'TestOpenCode|TestManifest|TestInstall' -v
```

Key passing evidence from the rerun:
- `TestOpenCodePlanWithMCPSelectsLoreAndMCPLoreBlocks`
- `TestOpenCodePlanWithoutMCPDoesNotRenderMCPLore`
- `TestOpenCodeMCPMergePreservesExistingLoreBlock`
- `TestOpenCodeBackupBeforeOverwrite`
- `TestOpenCodeManifestUsesMergedJSONHash`
- `TestOpenCodeIdempotentRerunKeepsFilesUnchanged`
- `TestManifestValidateAllowsAntigravityMarkerMerge`

**Focused CLI verification rerun**: ✅ Passed
```bash
go test -count=1 ./internal/cli -run 'TestInstall|TestOpenCode' -v
```

Key passing evidence from the rerun:
- `TestOpenCodeInstallDryRunPassesServerURLAndTokenToPlan`
- `TestOpenCodeInstallPlanSummaryReportsMCPRemote`
- `TestOpenCodeInstallPlanSummaryReportsMCPNone`
- `TestOpenCodeInstallDryRunAndApply`
- `TestInstallCommandPassesSavedTokenToValidationWithoutLeakingIt`

**Focused TUI verification rerun**: ✅ Passed
```bash
go test -count=1 ./internal/tui -run 'TestInstall' -v
```

**Broad repository-wide verification rerun**: ✅ Passed
```bash
go test -count=1 ./...
```

**Additional runtime probes (temporary files only, no repo edits)**: ✅ Passed
1. CLI dry-run probe:
```text
lore install --dry-run --target opencode --component lore-server-mcp
→ exit 0; output contains install_target=opencode and mcp=remote; token string not present
```
2. Service plan/apply probe:
```text
PlanOpenCodeInstall + ExecuteOpenCodeInstall with direct ServerURL/SavedToken
→ ~/.config/opencode/opencode.json contains both top-level lore and mcp.lore
```

**Coverage**: ➖ Not collected in this verify run

---

### Spec Compliance Matrix

| Requirement | Scenario | Test / Evidence | Result |
|-------------|----------|-----------------|--------|
| Verified OpenCode MCP gating | Verified authenticated shape enables MCP support | `internal/install/adapter_opencode_test.go > TestOpenCodeMCPContractTopLevelShape`; `internal/install/adapter_opencode_test.go > TestOpenCodeMCPRendersWhenServerURLAndTokenProvided`; `internal/install/opencode_install_test.go > TestOpenCodePlanWithMCPSelectsLoreAndMCPLoreBlocks`; CLI dry-run runtime probe | ✅ COMPLIANT |
| Verified OpenCode MCP gating | Unverified shape blocks token config | `internal/install/adapter_opencode_test.go > TestOpenCodeMCPContractFailClosedForEmptyToken`; `internal/install/adapter_opencode_test.go > TestOpenCodeMCPContractFailClosedForEmptyURL`; `internal/install/adapter_opencode_test.go > TestOpenCodeMCPFailClosedWhenComponentSelectedButNoAuth` | ✅ COMPLIANT |
| Existing Lore auth source only | Saved auth is reused | `internal/cli/actions_test.go > TestOpenCodeInstallDryRunPassesServerURLAndTokenToPlan`; static review of `internal/cli/actions.go` (`installOpenCodeActionWithOptions` threads `preflight.ServerURL` / `preflight.Token`) | ✅ COMPLIANT |
| Existing Lore auth source only | Missing saved auth fails closed | Existing preflight flow in `internal/install/service.go`; focused CLI install tests pass with preflight gating intact | ✅ COMPLIANT |
| OpenCode MCP path and shape | Lore MCP is placed under top-level `mcp.lore` and never `mcpServers` | `internal/install/adapter_opencode_test.go > TestOpenCodeMCPContractTopLevelShape`; `internal/install/opencode_install_test.go > TestOpenCodePlanWithMCPSelectsLoreAndMCPLoreBlocks` | ✅ COMPLIANT |
| Safe merge and fail-closed writes | Unrelated user settings are preserved | `internal/install/adapter_opencode_test.go > TestOpenCodeMergePreservesUnrelatedMCPEntries`; `internal/install/adapter_opencode_test.go > TestOpenCodeMergePreservesExistingLoreBlock`; `internal/install/opencode_install_test.go > TestOpenCodeMCPMergePreservesExistingLoreBlock` | ✅ COMPLIANT |
| Safe merge and fail-closed writes | Invalid / ambiguous JSON prevents partial update | `internal/install/adapter_opencode_test.go > TestOpenCodeMergeFailsClosedForAmbiguousMCPLoreOwnership`; `internal/install/adapter_opencode_test.go > TestOpenCodeMCPFailClosedWhenComponentSelectedButNoAuth` | ✅ COMPLIANT |
| Managed change accounting | Existing file update is backed up and tracked | `internal/install/opencode_install_test.go > TestOpenCodeBackupBeforeOverwrite`; `internal/install/opencode_install_test.go > TestOpenCodeManifestUsesMergedJSONHash` | ✅ COMPLIANT |
| Managed change accounting | Dry-run and repeated apply stay stable | `internal/install/opencode_install_test.go > TestOpenCodeIdempotentRerunKeepsFilesUnchanged`; `internal/cli/install_flags_test.go > TestOpenCodeInstallDryRunAndApply/dry-run_stays_non-mutating`; broad rerun `go test -count=1 ./...` | ✅ COMPLIANT |
| Redacted warnings and bounded claims | Plaintext token warning is explicit and redacted | Static review of `internal/cli/actions.go`, `internal/cli/app.go`, `internal/tui/root.go`, `internal/install/service.go`, `internal/install/adapter_opencode.go`, and `README.md`; CLI dry-run runtime probe confirms token absence from output; `internal/install/adapter_opencode_test.go > TestOpenCodeMCPContractNoTokenLeakInSummaryOrLog` | ✅ COMPLIANT |
| Redacted warnings and bounded claims | Scope messaging stays bounded | Static review of `README.md`, `internal/cli/app.go`, `internal/tui/root.go`, `internal/install/service.go`; no plugin/command/profile/bootstrap/runtime-subagent claims introduced | ✅ COMPLIANT |

**Compliance summary**: 11/11 scenarios compliant

---

### Correctness (Static — Structural Evidence)

| Requirement | Status | Notes |
|------------|--------|-------|
| OpenCode supports `lore-server-mcp` with top-level `mcp.lore` | ✅ Implemented | `renderOpenCodeMCPConfig` produces the combined payload; `renderOpenCodeFiles` + plan/apply keep both `lore` and `mcp.lore`. |
| URL/token come only from existing preflight + saved auth flow | ✅ Implemented | `installOpenCodeActionWithOptions` passes `preflight.ServerURL` and `preflight.Token` into `PlanOpenCodeInstall`; no alternative auth path was introduced. |
| Merge preserves user keys and unrelated `mcp.*`, owns only `mcp.lore`, fails closed on ambiguity | ✅ Implemented | `mergeOpenCodeJSON` preserves unrelated keys and rejects ambiguous `mcp.lore` ownership. |
| Manifest/backups/dry-run/idempotency cover MCP changes | ✅ Implemented | Plan/apply/manifest code paths and focused tests cover backup, merged hashes, dry-run, and unchanged reruns. |
| CLI/TUI/docs warn about plaintext token persistence without leaking token values | ✅ Implemented | User-facing OpenCode copy now warns about plaintext token persistence while keeping actual token values out of summaries and diagnostics. |
| No plugins/commands/prompts/profiles/bootstrap/runtime-subagent claims introduced | ✅ Implemented | OpenCode wording remains bounded to config-only support. |
| Existing bounded OpenCode install remains compatible | ✅ Implemented | Non-MCP OpenCode tests still pass, including plan/apply and bounded managed surface coverage. |

---

### Coherence (Design)

| Decision | Followed? | Notes |
|----------|-----------|-------|
| Auth shape gate before enablement | ✅ Yes | Contract tests enforce top-level `mcp.lore`, remote shape, and fail-closed validation. |
| Preflight data threaded into OpenCode plan/render path | ✅ Yes | CLI path now threads preflight server URL and token into OpenCode planning. |
| Merge only `mcp.lore` while preserving unrelated `mcp.*` | ✅ Yes | Implemented in `mergeOpenCodeJSON`. |
| Secret UX warns in summaries/docs while redacting token values | ✅ Yes | Warnings are explicit; runtime probe confirmed no token leak in CLI output. |
| Manifest/backups/idempotency remain stable for MCP changes | ✅ Yes | Focused install tests passed after the repair. |

---

### Files Reviewed

- `internal/cli/actions.go`
- `internal/cli/actions_test.go`
- `internal/cli/app.go`
- `internal/cli/install_flags_test.go`
- `internal/install/adapter.go`
- `internal/install/adapter_opencode.go`
- `internal/install/adapter_opencode_test.go`
- `internal/install/components.go`
- `internal/install/harness.go`
- `internal/install/opencode_install.go`
- `internal/install/opencode_install_test.go`
- `internal/install/service.go`
- `internal/tui/root.go`
- `README.md`
- `openspec/changes/opencode-mcp-config-support/tasks.md`
- `openspec/changes/opencode-mcp-config-support/apply-repair-started.md`
- `openspec/changes/opencode-mcp-config-support/apply-progress.md`
- `openspec/changes/opencode-mcp-config-support/apply-report.md`
- `openspec/changes/opencode-mcp-config-support/verify-report.md` (prior failed report reviewed for regression closure)

---

### Issues Found

**CRITICAL**
- None.

**WARNING**
1. Lore artifact retrieval remains degraded in this worker context: `lore_get_observation(id)` returned `project_id is required`, so artifact content had to be read from Lore search previews and filesystem fallbacks instead of full direct observation reads.
2. The canonical Lore tasks artifact is stale relative to the repair-complete filesystem checkpoint.

**SUGGESTION**
1. Sync the repaired tasks checkpoint back into the canonical Lore tasks artifact so Lore-only traceability matches the filesystem fallback.
2. Add a permanent repo test that combines OpenCode MCP selection with explicit no-token-leak assertions on the CLI stdout/stderr path, mirroring the temporary runtime probe used here.

---

### Verdict
PASS WITH WARNINGS

The repaired OpenCode MCP config support now passes focused install/CLI/TUI reruns, a fresh repository-wide `go test -count=1 ./...`, and targeted runtime probes. The two prior critical failures are fixed: the CLI OpenCode MCP dry-run no longer fails on missing auth threading, and the service plan/apply path now writes `opencode.json` with both top-level `lore` and `mcp.lore`. Remaining concerns are traceability/tooling warnings only.