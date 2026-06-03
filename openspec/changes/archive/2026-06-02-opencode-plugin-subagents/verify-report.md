## Verification Report

**Change**: opencode-plugin-subagents  
**Version**: N/A  
**Mode**: Standard  
**Verification pass**: rerun after repair `dg-1aa9825c`; note `dg-a70a4a19` failed envelope parsing only and may have applied code.  
**Artifact retrieval**: Lore direct `lore_get_observation(id)` is degraded in this runtime (`project_id is required`); full artifacts were retrieved via Lore search results for project `86836476-c996-4c1a-b21e-361caab8b8d4` plus filesystem fallback for current apply report.

---

### Completeness

| Metric | Value |
|--------|-------|
| Canonical tasks total | 9 |
| Canonical tasks checked complete | 0 |
| Tasks evidenced by apply/code/tests | 9 |
| Tasks incomplete by implementation evidence | 0 |

The canonical Lore tasks artifact remains unchecked/stale, but apply progress, repair report, code, and test evidence support completion of detector foundation, probes, UX integration, regression tests, and validation.

---

### Build & Tests Execution

**Focused detector tests**: ✅ Passed  
Command: `go test ./internal/opencodeready -v -count=1`  
Result: PASS.

**Focused CLI tests**: ✅ Passed  
Command: `go test ./internal/cli -run 'TestDoctor|TestInstall|TestOpenCode' -v -count=1`  
Result: PASS.

**Broad repository tests**: ✅ Passed  
Command: `go test ./... -count=1`  
Result: all packages passed.

**Build / type-check**: ✅ Passed via `go test ./... -count=1` package compilation.  
**Coverage**: ➖ Not collected.

---

### Repaired Findings Check

| Prior finding | Result | Evidence |
|---|---|---|
| Overall readiness must not be `ready` when plugin/runtime/native-agent findings are `unknown` | ✅ Fixed for those findings | `aggregate()` now gates `FindingIDOpenCodePluginAPI`, `FindingIDOpenCodeRuntime`, and `FindingIDOpenCodeAgents`; `TestProbe_Aggregation_PluginRuntimeAgentsUnknown_FailClosed`, `TestAggregate_CLIReady_PluginRuntimeAgentsUnknown_Unknown`, and focused tests pass. |
| `AllowTempProbe=false` must prevent `TempWritableProbe` | ✅ Fixed at finding level | `probeConfigDir()` and `probePluginsDir()` call `TempWritableProbe` only inside `if opts.AllowTempProbe`; `TestProbe_ConfigDir_Missing_AllowTempProbeFalse_Unknown` asserts no probe calls. |
| Regression tests cover the repaired cases | ✅ Present | Detector tests include fail-closed aggregate and no-temp-probe cases; focused detector and CLI tests pass. |

---

### Spec Compliance Matrix

| Requirement | Scenario | Test / Evidence | Result |
|-------------|----------|-----------------|--------|
| Readiness detection is non-installing and fail-closed | Fresh install with no OpenCode config dir | `TestProbe_FreshInstall_NoConfigDir_MissingButCreatable`; `probeConfigDir` / `probePluginsDir`; no persistent plugin/config write found | ⚠️ PARTIAL |
| Readiness detection is non-installing and fail-closed | Probe would require unsafe mutation | `TestProbe_Aggregation_PluginRuntimeAgentsUnknown_FailClosed`; static review of `aggregate` | ✅ COMPLIANT for plugin/runtime/native unknowns |
| CLI, version, and directory states are distinguished | CLI present with parseable version | `TestProbe_CLIPresent_VersionParseable`; version extraction in `probeVersion` | ✅ COMPLIANT |
| CLI, version, and directory states are distinguished | Missing CLI | `TestProbe_MissingCLI_Blocking` | ✅ COMPLIANT |
| CLI, version, and directory states are distinguished | Existing vs creatable vs blocked directories | Existing and missing-creatable tests pass; blocked/inaccessible paths now report `unknown`, not `blocking`, after repair | ⚠️ PARTIAL |
| Plugin, runtime, and native-agent evidence stays evidence-based | Read-only evidence confirms plugin loading | `TestProbe_PluginAPI_HelpWithPluginOutput_Ready`; `TestProbe_PluginAPI_DebugInfo_Ready` | ✅ COMPLIANT |
| Plugin, runtime, and native-agent evidence stays evidence-based | Runtime prerequisite/native support is unverifiable | `TestProbe_Runtime_BunNotAvailable_Unknown`; `TestProbe_NativeAgents_Unknown`; fail-closed aggregate tests | ✅ COMPLIANT |
| Doctor reports bounded readiness findings only | Doctor reports safe readiness summary | `TestDoctorActionIncludesOpenCodeReadinessCheck`; `formatOpenCodeReadinessDetail` | ⚠️ PARTIAL |
| Unsupported capability claims are excluded | Preflight summary stays readiness-only | CLI tests and static review: detail includes `plugins=none runtime-subagents=none command-routing=none`; install preflight is informational | ✅ COMPLIANT |

**Compliance summary**: 6/9 compliant, 3 partial, 0 failing in executed tests; 1 critical semantic issue remains by static/spec review.

---

### Correctness (Static — Structural Evidence)

| Requirement | Status | Notes |
|------------|--------|-------|
| Readiness detector with statuses/findings/report/evidence/remediation/missing-but-creatable | ✅ Implemented | `internal/opencodeready/types.go` defines the model; detector emits evidence/remediation and `NewlyInstalled`. |
| Command runner/filesystem abstractions deterministic/testable | ✅ Implemented | `CommandRunner`, `FS`, fake runner/FS, and extensive package tests exist. |
| CLI/version/config/plugin dir detection and no user config writes | ✅ Mostly | CLI/version and path probes exist. Temp probe is now gated by `AllowTempProbe`; no persistent writes found in detector. |
| Conservative plugin/runtime/native evidence | ✅ Improved | Unknown plugin/runtime/native findings now prevent overall ready. |
| UX integration in doctor/install preflight readiness-only | ⚠️ Partial | Bounded wording and non-claims exist. However `openCodeReadinessCheck` maps `Overall=unknown` to `output.StatusOK`, so CLI status can look OK while detail says unknown. |
| Ordinary OpenCode installs not blocked by plugin readiness | ✅ Implemented | Install preflight downgrades readiness fail to warn and still proceeds. |
| Explicit non-goals preserved | ✅ Implemented | No plugin writes/executable assets/package bootstrap/agent definitions/routing/runtime-native claims/MCP-token changes were introduced by this readiness slice. |

---

### Coherence (Design)

| Decision | Followed? | Notes |
|----------|-----------|-------|
| `internal/opencodeready` package | ✅ Yes | Package exists with detector/types/helpers/tests. |
| Probe abstractions | ✅ Yes | Interfaces and fakes are present. |
| Status model with `ready|warn|blocking|unknown` and `NewlyInstalled` | ✅ Yes | Implemented and tested. |
| Config/plugin paths via read-only/temp-safe inspection only | ✅ Mostly | `AllowTempProbe` gate is now enforced. |
| Plugin API/load/runtime/native support unknown unless safely verified | ✅ Yes for finding and aggregate semantics | Repaired prior critical issue. |
| Doctor and OpenCode install preflight summaries | ⚠️ Partial | Summaries are bounded, but CLI status mapping does not preserve unknown as non-ready. |

---

### Issues Found

**CRITICAL** (must fix before archive)
1. `aggregate()` still only treats plugin API/runtime/native-agent `unknown` as fail-closed. Other unknown/warn findings such as config/plugin directory readiness can be present while overall becomes `ready` if CLI/version and plugin/runtime/agent evidence are ready. This conflicts with the approved spec's broader “MUST treat unknown as not ready” and fresh-install “result is warn or unknown” contract.
2. `openCodeReadinessCheck()` maps `report.Overall == StatusUnknown` to `output.StatusOK` instead of warning/non-ready. This can make doctor/preflight status appear OK while detail says `overall=unknown`, weakening the required classification preservation.

**WARNING** (should fix)
1. Regression coverage proves `AllowTempProbe=false` for `probeConfigDir`, but does not include a full `Probe`/overall regression for both config and plugin dirs missing with temp probes disabled and all other signals ready.
2. CLI/action tests still use the real `opencodeready.Probe`, which makes several tests host-state dependent rather than fully deterministic.
3. Canonical tasks artifact is stale/unchecked relative to apply and verification evidence.

**SUGGESTION** (nice to have)
1. Add table tests for aggregate behavior with any `unknown` and with non-blocking `warn` findings.
2. Add an injected detector seam for CLI/action tests to assert unknown/warn/fail status mapping without relying on local OpenCode/Bun.

---

### Files Reviewed

- `internal/opencodeready/types.go`
- `internal/opencodeready/detector.go`
- `internal/opencodeready/helpers.go`
- `internal/opencodeready/detector_test.go`
- `internal/cli/actions.go`
- `internal/cli/actions_test.go`
- `internal/cli/actions_opencode_test.go`
- `internal/cli/app.go`
- `internal/install/adapter_opencode.go`
- `README.md`
- Lore explore/proposal/spec/design/tasks/previous verify/apply artifacts for `opencode-plugin-subagents`
- `openspec/changes/opencode-plugin-subagents/apply-report.md`

---

### Verdict

FAIL

The specific repair targets from the prior verify report are fixed and all focused/broad tests pass. Final archive readiness is still blocked because overall and CLI status classification can still imply readiness/OK in the presence of non-plugin unknown or warn findings, contrary to the approved fail-closed readiness contract.
