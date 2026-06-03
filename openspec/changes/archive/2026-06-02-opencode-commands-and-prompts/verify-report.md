## Verification Report

**Change**: opencode-commands-and-prompts  
**Version**: N/A  
**Mode**: Standard  
**Verification pass**: rerun after repair `dg-1ef0a899`  
**Artifact retrieval**: Lore tool retrieval was degraded (`lore_get_observation` required unavailable project context). Approved spec/design/tasks and prior verify failure were retrieved via `lore recall --project-id 86836476-c996-4c1a-b21e-361caab8b8d4 --type architecture --json` fallback plus filesystem repair artifacts.

---

### Completeness

| Metric | Value |
|--------|-------|
| Tasks total | 12 |
| Tasks checked in `tasks.md` | 10 |
| Tasks unchecked in `tasks.md` | 2 |
| Tasks evidenced complete now | 12 |

**Unchecked in artifact but evidenced by this rerun**
- 5.1 Run focused install, CLI, and TUI tests for the touched paths
- 5.2 Run broader package tests only after focused slices pass

**Task artifact note**
- `openspec/changes/opencode-commands-and-prompts/tasks.md` still leaves Phase 5 unchecked even though repair evidence plus this verify run complete those validations.

---

### Build & Tests Execution

**Focused install tests**: ✅ Passed  
Command: `go test ./internal/install -run 'TestOpenCodeSDD|TestOpenCode.*Command|TestOpenCode.*Prompt' -v`

Result:
- 13 passed
- 0 failed
- 0 skipped

**Focused CLI tests**: ✅ Passed  
Command: `go test ./internal/cli -run 'TestInstall|TestOpenCode' -v`

Result:
- 22 passed
- 0 failed
- 0 skipped

**Focused TUI tests**: ✅ Passed  
Command: `go test ./internal/tui -run 'TestInstall' -v`

Result:
- 9 passed
- 0 failed
- 0 skipped

**Broader repo tests**: ✅ Passed  
Command: `go test ./...`

Result:
- all packages passed

**Build / type-check**: ➖ No separate build command configured; `go test ./...` compiled the repository successfully.

**Coverage**: ➖ Not available

---

### Spec Compliance Matrix

| Requirement | Scenario | Test / Evidence | Result |
|-------------|----------|-----------------|--------|
| Optional OpenCode SDD assets component | Opt-in installs only approved files | `TestOpenCodeSDDAssetsComponentExplicitlySelectable`; `TestOpenCodeSDDAssetsRendersAllNineCommandFiles`; `TestOpenCodeSDDAssetsRendersPerPhasePromptFiles`; static review of `internal/install/adapter_opencode.go` | ✅ COMPLIANT |
| Command content stays within the Lore/OpenCode boundary | Command asset content is bounded | `TestOpenCodeSDDAssetsCommandFilesHavePhaseFrontmatter`; `TestOpenCodeSDDAssetsCommandFilesNoBannedPhrases` | ✅ COMPLIANT |
| Prompt assets are staged only | Prompt assets remain non-runtime claims | `TestOpenCodeSDDAssetsRendersPerPhasePromptFiles`; `TestOpenCodeSDDAssetsPromptsInertNoOpencodeJSONWiring`; `TestOpenCodeSDDAssetsPromptsBoundedToOpenCodeLoreContext` | ✅ COMPLIANT |
| Managed OpenCode files stay within the approved surface | Apply writes only approved files | `TestOpenCodeSDDAssetsPlanApplyBackupManifest`; static review of managed paths in `internal/install/opencode_install.go` | ✅ COMPLIANT |
| Optional SDD assets are explicit and safely tracked | Assets are omitted unless selected | `TestOpenCodeSDDAssetsComponentOmittedByDefault` | ✅ COMPLIANT |
| Optional SDD assets are explicit and safely tracked | Managed asset lifecycle stays deterministic | `TestOpenCodeSDDAssetsPlanApplyBackupManifest`; `TestOpenCodeSDDAssetsBackupBeforeOverwrite`; `TestOpenCodeSDDAssetsIdempotentRerun` | ✅ COMPLIANT |
| Unsupported capability claims are excluded | Generated output stays within bounded non-goals | Static search across `internal/install`, `internal/cli`, `internal/tui`, `README.md`; focused CLI/TUI tests passed; no runtime/native subagent, plugin/profile/bootstrap, token-behavior, MCP-scope, or `gentle-orchestrator` implementation changes found | ✅ COMPLIANT |

**Compliance summary**: 7/7 scenarios compliant

---

### Correctness (Static — Structural Evidence)

| Requirement | Status | Notes |
|------------|--------|-------|
| Non-default OpenCode-only component | ✅ Implemented | `ComponentOpenCodeSDDAssets` exists, is optional, and is not in default OpenCode selection |
| Canonical command surface | ✅ Implemented | `renderOpenCodeSDDCommands` emits `commands/sdd-propose.md`; no `sdd-proposal` asset remains |
| Per-phase prompt assets | ✅ Implemented | `renderOpenCodeSDDPrompts` emits `prompts/sdd/sdd-*.md` for the 9 canonical phases; no `system-prompt-guidance.md` output remains |
| Component-selected lifecycle behavior | ✅ Implemented | Plan/apply/backup/manifest/idempotent tests now exercise the optional asset component |
| Bounded CLI/TUI/README messaging | ⚠️ Partial | README and TUI copy correctly describe optional assets; CLI install help also does so, but one later help sentence still says “Commands remain out of scope in this slice,” which is stale/inconsistent |
| Existing boundaries unchanged | ✅ Implemented | No plugin/profile/bootstrap/TUI-plugin/MCP-scope/token-behavior expansion; no `gentle-orchestrator`; assets remain optional and OpenCode-only |

---

### Coherence (Design)

| Decision | Followed? | Notes |
|----------|-----------|-------|
| Optional component model | ✅ Yes | Implemented as `opencode-sdd-assets`, OpenCode-only, not defaulted |
| File layout | ✅ Yes | Commands render under `commands/sdd-*.md`; prompts render under `prompts/sdd/sdd-*.md`; proposal phase uses canonical `sdd-propose` |
| Asset source | ✅ Yes | Static Go render helpers are used for deterministic assets |
| Runtime claims remain bounded | ✅ Yes | No runtime wiring for prompts, no native/runtime subagent claims, no boundary expansion |
| UX/docs updates | ⚠️ Mostly | README/TUI/copy tests align; CLI help text retains one stale sentence that should be reconciled for full consistency |

---

### Files Reviewed

- `internal/install/components.go`
- `internal/install/adapter_opencode.go`
- `internal/install/adapter_opencode_test.go`
- `internal/install/opencode_install.go`
- `internal/install/opencode_install_test.go`
- `internal/cli/app.go`
- `internal/cli/install_flags_test.go`
- `internal/tui/root.go`
- `README.md`
- `openspec/changes/opencode-commands-and-prompts/tasks.md`
- `openspec/changes/opencode-commands-and-prompts/apply-repair-report.md`
- prior failed verify artifact `1e05a73a-28a2-4be9-9d88-14c236da4aee`
- Lore spec/design/tasks via CLI recall fallback (`5d70e3b5-6f02-4bf3-959e-fb37fda340ef`, `e133c997-1835-47f0-902d-792d1b4b8285`, `78106802-2021-492c-8f06-3a8028ced762`)

---

### Issues Found

**CRITICAL** (must fix before archive)
- None

**WARNING** (should fix)
1. `internal/cli/app.go` install help contains one stale sentence: “Commands remain out of scope in this slice.” The rest of the copy correctly describes optional command/prompt assets, so this is a consistency issue rather than a behavior or boundary violation.
2. `openspec/changes/opencode-commands-and-prompts/tasks.md` still leaves Phase 5 unchecked even though focused and broad validations are now complete, which weakens traceability.

**SUGGESTION** (nice to have)
1. Add a focused CLI help copy assertion for the optional OpenCode SDD assets wording so the stale sentence cannot regress silently.

---

### Verdict

PASS WITH WARNINGS

The repair fixes the prior blocking findings: canonical `sdd-propose` naming is in place, per-phase prompt assets are rendered, lifecycle tests now prove plan/apply/backup/manifest/idempotent behavior, and bounded OpenCode-only asset behavior is preserved. Remaining issues are limited to traceability/copy consistency and do not block archive readiness.
