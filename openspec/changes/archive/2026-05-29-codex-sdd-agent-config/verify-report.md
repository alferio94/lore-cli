## Verification Report

**Change**: codex-sdd-agent-config  
**Artifacts reviewed**: explore `1f4378f7-2e07-4331-a2ed-f9ede3930082`, proposal `cd947c4b-8892-4a51-8e54-f667a545493f`, spec `f822d8b7-8fa3-43ab-b357-d29f99775f65`, design `596edca3-8d84-4004-9920-a545d7964cf6`, tasks `76c81e9f-d20d-4f11-a071-d8ba4e5c8683`, repaired tasks artifact `4435a338-3a10-4eb0-8499-6ebacbec6b40`, prior verify `224f5eb2-809c-4c03-82be-70c127c1b9dc`, repair apply artifacts `openspec/changes/codex-sdd-agent-config/{apply-progress.md,apply-report.md}`  
**Mode**: Standard (strict TDD not active; no `openspec/config.yaml` or testing-capabilities artifact found)

---

### Completeness

| Metric | Value |
|--------|-------|
| Tasks total | 8 |
| Tasks complete | 8 |
| Tasks incomplete | 0 |

Implementation now matches the repaired task checklist persisted under the same topic key (`4435a338-3a10-4eb0-8499-6ebacbec6b40`). The original tasks observation id supplied in the handoff (`76c81e9f-d20d-4f11-a071-d8ba4e5c8683`) still shows unchecked boxes, so Lore history contains stale duplicate task observations, but the latest durable task artifact is complete.

---

### Build & Tests Execution

**Focused agent-config package**: `go test -count=1 ./internal/agentconfig/...`  
**Result**: ✅ Passed

**Focused agent-config scenarios**: `go test -count=1 ./internal/agentconfig -run 'Test(StorePathWithEnvOverride|StoreEnsureDefaultCreatesFile|StoreEnsureDefaultRejectsInvalidFile|StoreSaveIdempotent|ConfigValidateUnknownAgent|ConfigValidateBlankModel|ToJSONIsCanonical)' -v`  
**Result**: ✅ Passed

**Focused install touchpoints**: `go test -count=1 ./internal/install -run 'Test(CheckAgentConfigValid|CheckAgentConfigNotFound|CheckAgentConfigInvalid|CheckAgentConfigNilStoreSkipped|DefaultComponentSelectionUsesHostedMCPForPiAndMCPForAntigravity|ResolveInstallTargetKeepsPiDefaultAndRejectsRoadmapTargets|FormatTargetSelectionExplainsPiNativePathAndMCPDeferral)' -v`  
**Result**: ✅ Passed

**Focused CLI touchpoints**: `go test -count=1 ./internal/cli -run 'Test(StatusAndDoctorActionsPreserveDiagnosticSemantics|InstallUsageIncludesTargetAndComponentFlags|InstallCommandAcceptsLoreServerMCPWithPiTarget)' -v`  
**Result**: ✅ Passed

**Focused agentpack contract**: `go test -count=1 ./internal/agentpack -run 'Test(DefaultDefinitionProvidesPortableLorePack|DefaultDefinitionIncludesCanonicalRolesAndPhaseOrder|DefaultDefinitionIncludesManagedAgentOverlays|DefinitionValidateRejectsInvalidShape|PhaseAgentNameMapsProposalToSddPropose)' -v`  
**Result**: ✅ Passed

**Repository-wide tests**: `go test -count=1 ./...`  
**Result**: ✅ Passed

**Build**: `go build ./...`  
**Result**: ✅ Passed

**Ad hoc behavioral probe**: temporary Go program exercising `agentconfig.Store` plus `auth.Manager.Save/Logout` with a real temp config directory  
**Result**: ✅ Passed
- `agent-config.json` was created once with defaults
- `auth.Manager.Save()` rewrote `config.json` only and left `agent-config.json` byte-for-byte unchanged
- `auth.Manager.Logout()` removed `config.json` only and left `agent-config.json` byte-for-byte unchanged

**Important validation note**:
- `internal/agentpack/slice2_tests.go` is gone; only real `*_test.go` files remain for this contract.

---

### Spec Compliance Matrix

| Requirement | Scenario | Test / Evidence | Result |
|-------------|----------|-----------------|--------|
| Separate, durable agent-config file | Default location is deterministic | `internal/agentconfig.TestStorePathDefaultDir`; `internal/agentconfig.TestStorePathWithEnvOverride`; static pathing in `internal/agentconfig/store.go` | ✅ COMPLIANT |
| Separate, durable agent-config file | Auth flows do not clobber agent config | Ad hoc behavioral probe of `auth.Manager.Save/Logout` with sibling `agent-config.json`; structural isolation in `internal/auth/manager.go` and `internal/config/store.go` | ✅ COMPLIANT |
| Versioned declarative SDD schema | Default generation writes complete SDD declarations | `internal/agentconfig.TestDefaultConfig`; `internal/agentconfig.TestStoreEnsureDefaultCreatesFile`; `internal/agentconfig.TestConfigIsSecretFree`; static schema in `internal/agentconfig/config.go` | ✅ COMPLIANT |
| Initial model policy is explicit | Generated defaults use gpt-5.4 everywhere | `internal/agentconfig.TestDefaultSDDModel`; `internal/agentconfig.TestDefaultConfig`; codex profile and exported constants in `internal/agentpack/{definition.go,defaults.go}` | ✅ COMPLIANT |
| Persistence is deterministic and idempotent | Equivalent rewrites are stable | `internal/agentconfig.TestToJSONIsCanonical`; `internal/agentconfig.TestStoreSaveIdempotent`; `internal/agentconfig.TestStoreSaveIdempotentFromDifferentDefaultCalls` | ✅ COMPLIANT |
| Validation fails closed on invalid contracts | Unknown or invalid agent declarations are rejected | `internal/agentconfig.TestConfigValidateWrongSchemaVersion`; `TestConfigValidateMissingAgent`; `TestConfigValidateBlankModel`; `TestConfigValidateUnknownAgent`; `TestStoreEnsureDefaultRejectsInvalidFile`; `TestStoreLoadMalformedJSON` | ✅ COMPLIANT |
| Config-only scope stays explicit | Inspection remains non-executing | `internal/cli.TestStatusAndDoctorActionsPreserveDiagnosticSemantics`; `internal/cli.TestInstallUsageIncludesTargetAndComponentFlags`; static wording in `README.md` and `internal/cli/actions.go` keeps Codex as config-only / coming-soon | ✅ COMPLIANT |

**Compliance summary**: 7/7 scenarios compliant

---

### Correctness (Static — Structural Evidence)

| Requirement | Status | Notes |
|------------|--------|-------|
| Separate sibling `agent-config.json` outside auth `config.json` | ✅ Implemented | New `internal/agentconfig` package provides `FileName`, schema, and store path resolution. |
| Canonical SDD declarations persisted with schema version | ✅ Implemented | `Config{schema_version,sdd_agents}` schema v1 is present and default generation emits all 9 canonical phase agents. |
| All canonical SDD agents default to `gpt-5.4` | ✅ Implemented | `internal/agentconfig.DefaultSDDModel`, `internal/agentpack.DefaultSDDModel`, and codex profile all align on `gpt-5.4`. |
| Deterministic/idempotent file persistence | ✅ Implemented | `ToJSON()` canonicalizes output and `Store.Save()` writes atomically with `0600`/`0700`. |
| Validation rejects malformed/unknown/blank contracts | ✅ Implemented | `Validate()` rejects wrong schema, missing canonical phases, unknown keys, and blank models. |
| Status/doctor/install contract-only touchpoints | ⚠️ Partial | Status/doctor are wired directly in production. `install.Service` has a read-only agent-config check, but the production CLI install path does not currently inject `AgentConfigStore`, so install summaries do not yet surface this check end-to-end. |
| Secret safety | ✅ Implemented | The schema is secret-free; auth remains in `config.json` + keychain; diagnostics/tests assert no token leakage. |

---

### Coherence (Design)

| Decision | Followed? | Notes |
|----------|-----------|-------|
| File ownership = sibling `agent-config.json` under Lore config dir | ✅ Yes | Implemented as a separate store and validated by save/logout probe. |
| Package boundary = new `internal/agentconfig` package | ✅ Yes | New package created; prior divergent `internal/agentpack/agentconfig.go` path removed. |
| Canonical phases from `agentpack` + `DefaultSDDModel = "gpt-5.4"` | ⚠️ Partial | `agentpack` exports `SDDPhaseAgentNames()` and `DefaultSDDModel`, but `internal/agentconfig` still duplicates the canonical phase list and default-model constant instead of consuming the exported helpers directly. |
| Schema strictness for known version / known SDD agents / full phase coverage | ✅ Yes | Validation is fail-closed and test-covered. |
| No persisted harness/runtime configurator in v1 | ✅ Yes | No Codex runner, live subagent invocation, per-harness config writer, Claude adapter, or Lore MCP runtime was added. |
| Planned file changes match design table | ⚠️ Minor drift | README/status/doctor/install service were touched, but production install wiring stops short of surfacing the read-only agent-config check because `App` does not inject `AgentConfigStore` into `install.Service`. |
| Reuse `config.ResolveDir` for path resolution | ⚠️ Minor drift | `internal/agentconfig/store.go` reimplements the same env/user-config-dir resolution instead of calling `internal/config.ResolveDir()`. Behavior matches, but the design intent to reuse the helper was not followed literally. |

---

### Issues Found

**CRITICAL**
- None.

**WARNING**
- Production install summaries do not currently surface `agent-config` diagnostics end-to-end because `internal/cli/actions.go` constructs `install.Service` without wiring `AgentConfigStore`, even though `install.Service.Preflight()` supports it.
- `internal/agentconfig` duplicates canonical SDD phase names and the default-model constant instead of consuming `agentpack.SDDPhaseAgentNames()` / `agentpack.DefaultSDDModel`, leaving a future drift risk.
- Lore contains duplicate task observations for this change; the original supplied tasks id remains unchecked while a newer task artifact under the same topic key is checked complete.

**SUGGESTION**
- Before archive, refresh the task artifact history or archive notes so the stale unchecked tasks id does not confuse later traceability review.
- In a follow-up cleanup, wire `AgentConfigStore` through the production install app path and replace duplicated phase/default definitions in `internal/agentconfig` with the exported `agentpack` helpers.

---

### Verdict
PASS WITH WARNINGS

The repair resolves the prior verification failures. The repository now implements the approved `agent-config.json` sibling contract in `internal/agentconfig`, removes the divergent agentpack-only path and non-test `slice2_tests.go`, passes focused and broad validation, preserves auth/config separation, keeps all canonical SDD agent defaults at `gpt-5.4`, and maintains the config-only/non-runner scope. Remaining gaps are minor coherence/wiring issues, not contract failures.
