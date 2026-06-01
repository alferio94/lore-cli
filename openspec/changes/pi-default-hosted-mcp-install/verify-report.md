## Verification Report

**Change**: pi-default-hosted-mcp-install  
**Artifacts reviewed**: proposal `77e706e8-3812-4b5b-be09-109400167256`, spec `ce93887b-f6ec-4e1b-921d-d97cad44d2ab`, design `3362165d-4df5-4d2e-823c-8cfa55ea849b`, tasks `openspec/changes/pi-default-hosted-mcp-install/tasks.md`, apply artifacts `openspec/changes/pi-default-hosted-mcp-install/{apply-started.md,apply-progress.md,apply-report.md}`, repair handoff `dg-630bbe1e`  
**Mode**: Standard (strict TDD not active; no `openspec/config.yaml` or testing-capabilities artifact found)

---

### Completeness

| Metric | Value |
|--------|-------|
| Tasks total | 18 |
| Tasks complete | 18 |
| Tasks incomplete | 0 |

All 18 tasks are checked in `openspec/changes/pi-default-hosted-mcp-install/tasks.md`.

---

### Build & Tests Execution

**Build**: `go build ./...`  
**Result**: ✅ Passed

**Tests**: `go test -count=1 ./...`  
**Result**: ✅ Passed

Focused uncached verification runs:
- `go test -count=1 ./internal/install -run 'Test(PiAdapterRenderMaterializesBearerTokenPlaintext|PiAdapterRenderRedactsTokenInOtherFiles|DefaultPiAdapterRenderUsesDefinitionAndPiAssets|InstallPiWritesManagedFilesBackupsAndManifest|ExecutePiInstallDryRunDoesNotMutateFilesystem|ExecutePiInstallRerunDoesNotDriftWhenManagedOverlaysAreUnchanged|DefaultComponentSelectionUsesHostedMCPForPiAndMCPForAntigravity|PlanPiInstallReportsManagedFileActions)$'` → ✅ Passed
- `go test -count=1 ./internal/cli -run 'Test(InstallCommandPiMCPConfigMaterializesBearerTokenPlaintext|InstallCommandDryRunSurfacesManagedFileActions|InstallCommandDryRunReportsPlanWithoutMutation|InstallCommandAcceptsLoreServerMCPWithPiTarget|InstallUsageIncludesTargetAndComponentFlags)$'` → ✅ Passed

**Coverage**: `go test -cover ./...`  
**Result**: ✅ Passed

Relevant package coverage from the repo-wide run:
- `internal/cli`: 68.5%
- `internal/install`: 81.2%
- `internal/update`: 75.3%
- `internal/agentpack`: 91.1%

**Behavioral token probe**  
Result: ✅ Passed
- `internal/install/mcp_config_test.go:TestPiAdapterRenderMaterializesBearerTokenPlaintext` proves rendered Pi `mcp.json` contains `Authorization: Bearer <saved-token>` plaintext
- `internal/cli/app_test.go:TestInstallCommandPiMCPConfigMaterializesBearerTokenPlaintext` proves applied `~/.pi/agent/mcp.json` contains the saved token plaintext and contains neither `${LORE_API_TOKEN}` nor unrendered template placeholders
- `internal/cli/app_test.go:TestInstallCommandDryRunReportsPlanWithoutMutation` and `TestInstallCommandDryRunSurfacesManagedFileActions` prove dry-run stdout/stderr stay redacted and do not mutate files

---

### Spec Compliance Matrix

| Requirement | Scenario | Test / Evidence | Result |
|-------------|----------|-----------------|--------|
| Default Pi install writes hosted MCP package and config | Default plan targets hosted MCP | `internal/install.TestDefaultComponentSelectionUsesHostedMCPForPiAndMCPForAntigravity`; `internal/install.TestPlanPiInstallReportsManagedFileActions`; `internal/cli.TestInstallCommandDryRunSurfacesManagedFileActions` | ✅ COMPLIANT |
| Default Pi install writes hosted MCP package and config | Apply writes MCP config from saved auth context | `internal/install.TestPiAdapterRenderMaterializesBearerTokenPlaintext`; `internal/cli.TestInstallCommandPiMCPConfigMaterializesBearerTokenPlaintext`; static evidence in `internal/install/pi.go` and `internal/install/adapter_pi.go` | ✅ COMPLIANT |
| Hosted MCP package source is HTTPS and pinned | Default Pi package entry is canonical and stable | Runtime/tests prove the package is pinned and stable as `git:github.com/nicobailon/pi-mcp-adapter@1091b34da83d58bd2d9fcaff2dc31f449a94bf1f`; this satisfies current tasks/docs but not the older spec wording that still says HTTPS | ⚠️ PARTIAL |
| Dry-run, manifest, and rerun behavior stay safe | Dry-run reports hosted MCP without mutations | `internal/install.TestExecutePiInstallDryRunDoesNotMutateFilesystem`; `internal/cli.TestInstallCommandDryRunReportsPlanWithoutMutation`; `internal/cli.TestInstallCommandDryRunSurfacesManagedFileActions` | ✅ COMPLIANT |
| Dry-run, manifest, and rerun behavior stay safe | Rerun validates new defaults | `internal/install.TestExecutePiInstallRerunDoesNotDriftWhenManagedOverlaysAreUnchanged`; `internal/install.TestInstallPiWritesManagedFilesBackupsAndManifest` | ✅ COMPLIANT |
| lore-memory remains available but dormant | Default install leaves rollback assets dormant | `internal/install.TestDefaultPiAdapterRenderUsesDefinitionAndPiAssets`; `internal/install.TestPlanPiInstallReportsManagedFileActions`; `internal/install.TestInstallPiWritesManagedFilesBackupsAndManifest` | ✅ COMPLIANT |
| Docs, tests, and secret handling match the new default | Documentation and CLI summaries reflect the new contract | Static review of `README.md`, `docs/releases/v0.4.0.md`, `internal/install/service.go`, `internal/cli/app.go`; `internal/cli.TestInstallUsageIncludesTargetAndComponentFlags` | ✅ COMPLIANT |
| Docs, tests, and secret handling match the new default | Secrets stay redacted through plan and apply | `internal/install.TestPiAdapterRenderRedactsTokenInOtherFiles`; `internal/cli.TestInstallCommandPiMCPConfigMaterializesBearerTokenPlaintext`; `internal/cli.TestInstallCommandDryRunReportsPlanWithoutMutation`; `internal/cli.TestInstallCommandDryRunSurfacesManagedFileActions` | ✅ COMPLIANT |

**Compliance summary**: 7/8 scenarios compliant, 1 partial due to stale spec wording about HTTPS package source.

---

### Correctness (Static — Structural Evidence)

| Requirement | Status | Notes |
|------------|--------|-------|
| Pi defaults to hosted MCP instead of default lore-memory install | ✅ Implemented | `internal/install/components.go` defaults Pi to `core-pack + lore-server-mcp + extended-skills`; `pi-extensions` is optional/non-default. |
| Pi renders hosted MCP settings and `mcp.json` while leaving lore-memory dormant by default | ✅ Implemented | `internal/install/adapter_pi.go` renders `settings.json` + `mcp.json` by default and only renders `lore-memory` assets when `pi-extensions` is explicitly selected. |
| Generated Pi MCP config materializes the saved token in plaintext and does not keep a runtime env placeholder | ✅ Implemented | Source template is `Bearer {{LORE_API_TOKEN}}`; `renderRequestReplacements()` fills `{{LORE_API_TOKEN}}` from `SavedToken`; `PiInstallRequest.renderRequest()` now forwards `SavedToken`; tests verify installed output contains the actual token and no `${LORE_API_TOKEN}`. |
| Dry-run/stdout/stderr stay token-safe | ✅ Implemented | CLI dry-run/apply tests use `assertNoTokenLeak(...)` on stdout/stderr. |
| Managed-file inventory, dry-run, manifest, and rerun semantics accept the new default file set | ✅ Implemented | `ResolvePiLayout()` and Pi service tests cover the 5-file default managed set plus overlays. |
| Hosted package source matches the repaired repository contract | ✅ Implemented | `PiHostedMCPPackageSource()` emits `git:github.com/nicobailon/pi-mcp-adapter@1091b34da83d58bd2d9fcaff2dc31f449a94bf1f`; README/release docs/tests align with that exact source. |
| Docs and help reflect hosted MCP default and the plaintext-token tradeoff | ✅ Implemented | `README.md`, `docs/releases/v0.4.0.md`, `internal/install/service.go`, and `internal/cli/app.go` all describe hosted MCP default, dormant lore-memory, and plaintext bearer token in runtime MCP config. |

---

### Coherence (Design)

| Decision | Followed? | Notes |
|----------|-----------|-------|
| Pi default component model flips to hosted MCP | ✅ Yes | Implemented through component defaults and Pi adapter capability changes. |
| Package action stays additive/idempotent in `settings.json` | ✅ Yes | Existing additive merge behavior remains and rerun tests pass. |
| MCP config is a separate Pi-managed file merged additively | ✅ Yes | `mcp.json` is rendered and managed separately with additive JSON merge semantics. |
| Dormant rollback assets remain available | ✅ Yes | `lore-memory.ts` and `lore-footer.ts` remain in assets and are only opt-in via `pi-extensions`. |
| MCP config uses stdio `lore mcp serve` with no token in file | ❌ No | Current implementation intentionally uses hosted HTTP `/v1/mcp` plus plaintext bearer token in `mcp.json`, matching the repair and Antigravity behavior, not the stale original design text. |
| Package source pinning remains HTTPS-based | ❌ No | Current implementation intentionally uses canonical `git:github.com/...@sha`, matching tasks/docs/tests, not the stale proposal/spec/design wording. |

---

### Issues Found

**CRITICAL**
- None.

**WARNING**
- Lore proposal/spec/design artifacts are stale in two places: they still describe an HTTPS package source and a stdio/no-token Pi MCP design, while the repaired repository contract is `git:github.com/...@sha` plus plaintext `Authorization: Bearer <saved-token>` in `~/.pi/agent/mcp.json`.

**SUGGESTION**
- Refresh proposal/spec/design before archive so the persisted SDD record matches the repaired repository contract exactly.

---

### Verdict
PASS WITH WARNINGS

Repository behavior now matches the repair request: Pi materializes the saved Lore API token as plaintext `Authorization: Bearer <saved-token>` in `~/.pi/agent/mcp.json`, no `${LORE_API_TOKEN}` runtime placeholder survives rendering, dry-run/stdout/stderr stay redacted, lore-memory remains dormant by default, docs/tests/build pass, and the only remaining drift is stale wording in earlier SDD artifacts.