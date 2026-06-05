# Verification Report: readd-opencode-support-from-gentle-ai

**Change**: readd-opencode-support-from-gentle-ai
**Version**: N/A (spec/design supplied as Lore memory IDs in prompt; Lore MCP tools unavailable in this harness)
**Mode**: Standard
**Date**: 2026-06-05
**Persistence**: OpenSpec fallback (`openspec/changes/readd-opencode-support-from-gentle-ai/verify-report.md`). Lore-first persistence could not be used because no `lore_*` MCP tools are exposed and `lore status` reports auth failure for the local CLI session.

---

## Inputs Reviewed

- User correction: in the gentle-ai pattern, `background-agents.ts` and `model-variants.ts` are copied to `~/.config/opencode/plugins/` and are **not** registered in `tui.json`; `tui.json` registers only the community `opencode-subagent-statusline`.
- OpenSpec apply artifacts:
  - `openspec/changes/readd-opencode-support-from-gentle-ai/tasks.md`
  - `openspec/changes/readd-opencode-support-from-gentle-ai/apply-report.md`
  - `openspec/changes/readd-opencode-support-from-gentle-ai/apply-progress.md`
  - `openspec/changes/readd-opencode-support-from-gentle-ai/apply-partial.md`
- Source/tests inspected:
  - `internal/install/assets/opencode/tui.json`
  - `internal/install/assets/opencode/plugins/background-agents.ts`
  - `internal/install/assets/opencode/plugins/model-variants.ts`
  - `internal/install/assets/opencode/plugins/opencode-subagent-statusline.ts`
  - `internal/install/adapter_opencode.go`
  - `internal/install/json_merge.go`
  - `internal/install/opencode_install.go`
  - `internal/install/opencode_install_test.go`
  - `internal/install/adapter_opencode_plugins_test.go`
  - `internal/cli/install_flags_test.go`
  - `internal/tui/model_test.go`
  - `README.md`

---

## Completeness

| Metric | Value |
|--------|-------|
| Tasks total | 14 |
| Tasks complete | 12 |
| Tasks incomplete | 2 |

Incomplete tasks are verification tasks only:

- 4.1 Verify `lore install --target opencode` dry-run/apply paths create/update expected files, preserve unrelated user content, and exercise foreign/resolved `mcp.lore` behavior end-to-end.
- 4.2 Run broader install/CLI tests after the OpenCode slice is green.

Verification performed in this pass satisfies 4.1/4.2 evidence expectations using unit/integration tests and source inspection. The task checklist remains unchecked because verify does not mutate task state.

---

## Build & Tests Execution

**Build**: Not run separately in this verify pass. Prior apply report recorded `go build ./...` and `go vet ./...` as clean. Current verify executed the full Go test suite.

**Tests**: Passed.

Commands executed:

```bash
go test ./...
go test ./internal/install -run 'TestOpenCode|Test.*Target|Test.*Render|Test.*Plugin|Test.*Leak|Test.*Redact' -count=1 -v
go test ./internal/cli ./internal/tui -run 'Test.*OpenCode|Test.*Install|Test.*Copy' -count=1 -v
```

Results:

- `go test ./...` passed for all packages, including `cmd/lore`, `internal/cli`, `internal/install`, `internal/tui`, and supporting packages.
- Focused install/OpenCode tests passed, including:
  - `TestOpenCodeConfigJSONMergeFailsClosedOnForeignMcpLoreBlock`
  - `TestOpenCodeConfigJSONMergeAllowsLoreOwnedMcpLoreBlock`
  - `TestOpenCodeConfigJSONMergeIgnoresOwnershipForTUIJSON`
  - `TestOpenCodePlanOpenCodeInstallFailsClosedOnForeignMcpLore`
  - `TestOpenCodePlanOpenCodeInstallFailsClosedAndRecordsConflictSummary`
  - `TestOpenCodeAdapterRenderWithPluginsIncludesTUISettingsAndPluginFiles`
  - `TestOpenCodeRenderFullSurfaceNoGentleLeakageAcrossAllRenderedFiles`
  - `TestOpenCodePluginAssetsExcludeSddEngramAndLogo`
  - `TestOpenCodePluginAssetsNoGentleWordingLeakage`
  - CLI/TUI install smoke tests for Pi, Codex, Antigravity, and OpenCode install surfaces.

**Coverage**: Not available/configured for this repository.

---

## Focused End-to-End Smoke Evidence

A live external `lore install --target opencode --dry-run --yes` command could not complete because the saved local CLI session failed preflight network/auth (`lore status` reports `[FAIL] auth: network request failed`). Instead, verification used the repository's integration-style install tests with fake stores/clients and temp homes, which exercise plan/apply behavior without touching the user's real config.

Evidence confirmed by tests:

- Pi install dry-run/apply smoke: `TestInstallCommandRunsPiInstallAndPrintsSummary`, `TestInstallCommandDryRunReportsPlanWithoutMutation`, `TestInstallCommandApplyRemovesAndBacksUpPreExistingLoreMemoryExtension`, `TestInstallCommandPiMCPConfigMaterializesBearerTokenPlaintext`.
- Codex install dry-run/apply smoke: `TestCodexInstallPlanSummaryIncludesManagedActions`, `TestCodexInstallSummaryNoAntigravityRuntime`.
- Antigravity install dry-run/apply smoke: `TestInstallCommandSupportsAntigravityDryRunAndApply`.
- OpenCode dry-run/apply/conflict smoke: `TestInstallCommandRejectsOpenCodeTarget` (legacy name; currently asserts accepted OpenCode dry-run semantics), `TestOpenCodePlanOpenCodeInstallBacksUpAndUpdatesExistingOpenCodeJSON`, `TestOpenCodePlanOpenCodeInstallFailsClosedOnForeignMcpLore`, `TestOpenCodePlanOpenCodeInstallFailsClosedAndRecordsConflictSummary`, and `TestOpenCodePlanOpenCodeInstallIsIdempotent`.

---

## Spec Compliance Matrix

| Requirement | Scenario | Evidence | Result |
|-------------|----------|----------|--------|
| Re-add OpenCode target | Target is registered and accepted by install registry/CLI | `TestSupportedTargetsIncludesOpenCode`, `TestInstallAcceptsOpenCodeTarget`, `TestRegistryResolveReturnsTargetAdapterAndCapabilities`, focused tests passed | COMPLIANT |
| Render bounded OpenCode managed surface | Renders `AGENTS.md`, skills, `opencode.json`, plugins, `tui.json`, manifest | `adapter_opencode.go`, `opencode_install.go`, `TestDefaultOpenCodeAdapterRenderProducesAGENTSAndSkills`, `TestOpenCodeAdapterRenderWithPluginsIncludesTUISettingsAndPluginFiles` | COMPLIANT |
| Correct plugin registration semantics | `background-agents.ts` and `model-variants.ts` copied to plugins dir; only community statusline registered in `tui.json` | `internal/install/assets/opencode/tui.json` contains only `opencode-subagent-statusline` under `plugins`; plugin files exist under `assets/opencode/plugins/`; tests assert local plugin TS files are not in `tui.json` | COMPLIANT |
| Explicit plugin exclusions | No `sdd-engram` or `logo` plugin asset is bundled or registered | `managedOpenCodePluginAssetNames` excludes them; `tui.json` only records them as `lore.plugins_excluded`; static guards passed | COMPLIANT |
| No Gentle leakage | OpenCode assets/rendered surfaces do not include Gentle-authored copy | `TestOpenCodePluginAssetsNoGentleWordingLeakage`, `TestOpenCodeBundledPluginAssetsNoGentleWordingLeakage`, `TestOpenCodeRenderFullSurfaceNoGentleLeakageAcrossAllRenderedFiles` passed | COMPLIANT |
| MCP ownership marker | Rendered `mcp.lore` carries `managed_by: lore-cli` | `renderOpenCodeMCPConfig` writes `managed_by`; `TestOpenCodeMCPConfigRendersRemoteMCPBlock` asserts it | COMPLIANT |
| Foreign `mcp.lore` fail-closed | Existing non-Lore-owned or missing-marker `mcp.lore` fails with typed conflict, backup on disk, no overwrite | `OpenCodeMCPConfigOwnershipError`, `inspectOpenCodeMCPConfigOwnership`, `planOpenCodeRenderedFileAction`; `TestOpenCodeConfigJSONMergeFailsClosedOnForeignMcpLoreBlock`, `TestOpenCodePlanOpenCodeInstallFailsClosedOnForeignMcpLore` passed | COMPLIANT |
| Resolved/absent/Lore-owned `mcp.lore` additive merge | Missing or Lore-owned `mcp.lore` proceeds, preserves unrelated user content, replaces Lore-owned subtree | `mergeOpenCodeConfigJSON`; `TestOpenCodeConfigJSONMergeFreshWriteProducesManagedBlock`, `TestOpenCodeConfigJSONMergePreservesExistingUserContent`, `TestOpenCodeConfigJSONMergeAllowsLoreOwnedMcpLoreBlock`, `TestOpenCodePlanOpenCodeInstallFailsClosedAndRecordsConflictSummary` passed | COMPLIANT |
| Scope ownership check to `opencode.json` | `tui.json` is unaffected by MCP ownership conflict logic | `mergeOpenCodeConfigJSON` checks `filepathToSlash(relativePath) == "opencode.json"`; `TestOpenCodeConfigJSONMergeIgnoresOwnershipForTUIJSON` passed | COMPLIANT |
| MCP redaction | Conflict/summary output does not leak existing or rendered token | Error type records type/url/managed_by only; tests assert no foreign/rendered token in conflict and no saved token in install summary | COMPLIANT |

**Compliance summary**: 10/10 scenarios compliant with runtime test evidence.

---

## Correctness (Static — Structural Evidence)

| Requirement | Status | Notes |
|------------|--------|-------|
| OpenCode plugin files | Implemented | `background-agents.ts`, `model-variants.ts`, and `opencode-subagent-statusline.ts` are bundled under `internal/install/assets/opencode/plugins/`. |
| `tui.json` statusline-only registration | Implemented | `plugins` array includes only `opencode-subagent-statusline`; local TS stubs are copied files, not `tui.json` registrations. |
| `mcp.lore.managed_by` marker | Implemented | `renderOpenCodeMCPConfig` writes `managed_by: lore-cli` inside `mcp.lore`. |
| Foreign MCP fail-closed conflict | Implemented | `*OpenCodeMCPConfigOwnershipError` and helpers are present; conflict path writes backup before abort. |
| Additive merge preservation | Implemented | `mergeOpenCodeConfigJSON` preserves user top-level keys and other `mcp` entries. |
| Token redaction | Implemented | Conflict inspector does not extract `headers.Authorization`; error string names only path/type/url/managed_by/backup. |
| No deprecated Pi `lore-memory.ts` | Implemented/guarded | Existing Pi tests still reject deprecated memory extension. |

---

## Coherence (Design)

| Decision | Followed? | Notes |
|----------|-----------|-------|
| Config-only bounded OpenCode projection | Yes | No runtime subagent/autostart/bootstrap behavior was added. |
| Correct gentle plugin semantics | Yes | Local TS files are copied under plugins; only community statusline is in `tui.json`. |
| Fail closed on foreign `mcp.lore` | Yes | Typed conflict and backup-before-abort are implemented and tested. |
| Preserve user content via additive merge | Yes | Existing unrelated `opencode.json` content and other MCP entries are preserved. |
| No Gentle leakage/excluded plugins | Yes | Static guards and asset tests passed; only explicit exclusion metadata contains `sdd-engram`/`logo`. |
| Lore-first persistence | Deviated due unavailable tooling | No `lore_*` tools are exposed in this harness and local `lore` auth is unhealthy, so report persisted to OpenSpec fallback. |

---

## Issues Found

**CRITICAL**: None.

**WARNING**:
- Live external CLI smoke against a real `lore install` command was not feasible because the local saved Lore CLI auth/network preflight is unhealthy. Repository integration tests cover dry-run/apply behavior with temp homes and fake clients.
- The install usage first line/target flag help still lists `pi|codex|antigravity` and omits `opencode`, although the longer help text documents OpenCode and tests confirm OpenCode is accepted. This is a copy inconsistency, not a behavior failure.

**SUGGESTION**:
- Rename legacy test `TestInstallCommandRejectsOpenCodeTarget`; it now asserts OpenCode support rather than rejection.
- In a future cleanup slice, update the install usage target list to include `opencode`.

---

## Verdict

PASS WITH WARNINGS

The repair is behaviorally compliant: `mcp.lore` ownership is marked and fail-closed, resolved/absent/Lore-owned configs merge additively, plugin registration matches the corrected expectation, token output is redacted, and the full Go test suite plus focused install/OpenCode tests pass.
