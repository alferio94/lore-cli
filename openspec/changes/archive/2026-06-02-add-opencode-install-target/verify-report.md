## Verification Report

**Change**: add-opencode-install-target  
**Mode**: Standard  
**Artifact mode**: Hybrid fallback (filesystem persisted; Lore read path degraded, Lore save attempted)  
**Date**: 2026-06-02

---

### Completeness

| Metric | Value |
|--------|-------|
| Tasks total | 10 |
| Tasks complete | 10 |
| Tasks incomplete | 0 |

Evidence sources:
- Canonical current tasks observation preview: `sdd/add-opencode-install-target/tasks` (`ba28235b-8918-49bb-8ad2-e0772a5d11bc`) shows all 10 tasks checked complete.
- Filesystem fallback checkpoint `openspec/changes/add-opencode-install-target/tasks.md` also shows all 10 tasks complete.
- Apply handoff evidence shows final focused + broad validation completed in the last apply slice.

---

### Build & Tests Execution

**Focused install verification rerun**: ✅ Passed
```bash
go test -count=1 ./internal/install -run 'TestOpenCode|TestManifest|TestInstall' -v
```

**Focused CLI verification rerun**: ✅ Passed
```bash
go test -count=1 ./internal/cli -run 'TestInstall|TestOpenCode' -v
```

**Focused TUI verification rerun**: ✅ Passed
```bash
go test -count=1 ./internal/tui -run 'TestInstall' -v
```

**Broad repository-wide verification rerun**: ✅ Passed
```bash
go test -count=1 ./...
```

**Build**: ✅ Passed
```bash
go build ./...
```

**Coverage**: ➖ Not available in this verify run

Notes:
- Fresh verify reruns were executed for the OpenCode-focused install/CLI/TUI surface.
- Broad `go test -count=1 ./...` was rerun and passed, so apply evidence did not need to be treated as stale.

---

### Spec Compliance Matrix

| Requirement | Scenario | Test / Evidence | Result |
|-------------|----------|-----------------|--------|
| OpenCode target is selectable | CLI and TUI show OpenCode as supported | `internal/install/service_test.go > TestDefaultTargetsPreferPiAndMarkOthersComingSoon`; `internal/install/service_test.go > TestResolveInstallTargetKeepsPiDefaultAndRejectsRoadmapTargets`; `internal/cli/install_flags_test.go > TestInstallUsageIncludesTargetAndComponentFlags`; `internal/tui/model_test.go > TestInstallTargetSelectionMovesBetweenSupportedTargetsOnly`; `internal/tui/model_test.go > TestInstallTargetSelectionSurfacesPiDefaultAndAntigravityMVPGuidance` | ✅ COMPLIANT |
| OpenCode install uses Lore-owned source inputs | Generated OpenCode files follow Lore-owned inputs | `internal/install/adapter_opencode_test.go > TestOpenCodeRenderProducesAgentsAndManagedSkills`; `internal/install/adapter_opencode_test.go > TestOpenCodeAgentConfigUsesCustomModels`; static review of `renderOpenCodeAgentsMD`, `renderOpenCodeManagedSkills`, `renderOpenCodeLoreBlock` | ✅ COMPLIANT |
| Managed OpenCode files stay within approved surface | Apply writes only approved files | `internal/install/opencode_install_test.go > TestOpenCodeExecuteWritesFilesAndManifest`; `internal/cli/install_flags_test.go > TestOpenCodeInstallDryRunAndApply/apply_writes_bounded_managed_surface_only`; static review of `ResolveOpenCodeLayout` and `renderOpenCodeFiles` | ✅ COMPLIANT |
| OpenCode settings merge preserves user-owned config | Unrelated settings survive a Lore update | `internal/install/adapter_opencode_test.go > TestOpenCodeMergePreservesUserJSON`; `internal/install/opencode_install_test.go > TestOpenCodeManifestUsesMergedJSONHash` | ✅ COMPLIANT |
| OpenCode settings merge preserves user-owned config | Ambiguous ownership stops the install | `internal/install/adapter_opencode_test.go > TestOpenCodeMergeRejectsAmbiguousLoreOwnership`; static review of `mergeOpenCodeJSON` fail-closed guards | ✅ COMPLIANT |
| Optional commands are explicit | Commands are omitted unless selected | `internal/install/adapter_opencode_test.go > TestOpenCodeCommandsOmittedWithoutApprovedBoundary`; `internal/install/adapter_opencode_test.go > TestOpenCodeCommandsFailClosedWithoutApprovedBoundary`; `internal/cli/actions_test.go > TestOpenCodeInstallPlanSummaryUsesBoundedTargetSpecificCopy` | ✅ COMPLIANT |
| Manifest, backups, dry-run, and reruns stay safe | Dry-run and rerun are idempotent | `internal/install/opencode_install_test.go > TestOpenCodePlanCreatesManagedActions`; `internal/install/opencode_install_test.go > TestOpenCodeBackupBeforeOverwrite`; `internal/install/opencode_install_test.go > TestOpenCodeIdempotentRerunKeepsFilesUnchanged`; `internal/cli/install_flags_test.go > TestOpenCodeInstallDryRunAndApply/dry-run_stays_non-mutating` | ✅ COMPLIANT |
| Unsupported capability claims are excluded | Generated output stays within bounded non-goals | `internal/cli/actions_test.go > TestOpenCodeInstallSummaryUsesBoundedTargetSpecificCopy`; `internal/install/adapter_opencode_test.go > TestOpenCodeRenderProducesAgentsAndManagedSkills`; static review of `renderOpenCodeAgentsMD`, `renderOpenCodeLoreBlock`, README OpenCode section | ✅ COMPLIANT |

**Compliance summary**: 8/8 scenarios compliant

---

### Correctness (Static — Structural Evidence)

| Requirement | Status | Notes |
|------------|--------|-------|
| OpenCode is a real supported target, not roadmap-only | ✅ Implemented | Registry now resolves `TargetOpenCode`; service/CLI/TUI expose it as available and selectable. |
| OpenCode uses `agentpack` + `agentconfig` as source of truth | ✅ Implemented | Prompt/skills render from `agentpack`; model declarations in AGENTS/opencode lore block derive from `req.AgentConfig` / ensured default `agent-config.json`. |
| Managed surface is bounded to approved files | ✅ Implemented | Managed files are `AGENTS.md`, `skills/*/SKILL.md`, optional extended skills in the same skills tree, `opencode.json`, `lore-install.json`, and backups. Commands directory is intentionally omitted in this slice. |
| `opencode.json` merge preserves unrelated config and fails closed | ✅ Implemented | Existing unrelated top-level keys are preserved; invalid JSON, non-object roots, non-object `lore`, and foreign/missing ownership markers fail closed before write. |
| Manifest / backups / dry-run / idempotency are safe | ✅ Implemented | Plan/apply classify create/update/unchanged deterministically, back up before overwrite, store merged-content hashes, and reruns converge unchanged. |
| Antigravity marker-merge compatibility is preserved | ✅ Implemented | Shared manifest validation now explicitly accepts `marker-merge`; regression test proves Antigravity prompt manifests still validate. |
| Unsupported scope remains excluded | ✅ Implemented | No plugins, profiles, TUI plugins, bootstrap/package-manager flows, MCP token persistence, or native/runtime subagent claims are rendered for OpenCode. |

---

### Coherence (Design)

| Decision | Followed? | Notes |
|----------|-----------|-------|
| New OpenCode adapter on shared install path | ✅ Yes | `defaultOpenCodeAdapter`, `ResolveOpenCodeLayout`, and OpenCode plan/apply path exist as specified. |
| `~/.config/opencode` layout with AGENTS, skills, opencode.json, manifest, backups | ✅ Yes | `ResolveOpenCodeLayout` and apply path match the approved bounded filesystem surface. |
| Prompt/skills sourced from portable assets | ✅ Yes | AGENTS + managed skills project from `agentpack`; model declarations use `agentconfig`. |
| Commands remain explicit/omitted by default | ✅ Yes | No command files are created unless a future approved boundary exists; current implementation fails closed on attempted explicit render. |
| Lore owns only the top-level `lore` block in `opencode.json` | ✅ Yes | Merge logic preserves unrelated keys and only replaces the approved Lore-owned block. |
| Fail-closed validation on ambiguous ownership and invalid content | ✅ Yes | Merge rejects malformed JSON, non-object roots, and ambiguous/foreign Lore ownership. |
| No plugin/profile/runtime-subagent/token-persistence expansion | ✅ Yes | Output and docs keep the slice bounded to config-only projection. |

---

### Files Reviewed

- `internal/install/adapter.go`
- `internal/install/service.go`
- `internal/install/adapter_opencode.go`
- `internal/install/opencode_install.go`
- `internal/install/manifest.go`
- `internal/install/adapter_antigravity.go`
- `internal/cli/actions.go`
- `internal/cli/app.go`
- `internal/tui/root.go`
- `README.md`
- `internal/install/adapter_test.go`
- `internal/install/service_test.go`
- `internal/install/adapter_opencode_test.go`
- `internal/install/opencode_install_test.go`
- `internal/install/manifest_test.go`
- `internal/cli/actions_test.go`
- `internal/cli/install_flags_test.go`
- `internal/cli/app_test.go`
- `internal/tui/model_test.go`
- `openspec/changes/add-opencode-install-target/tasks.md`
- `openspec/changes/add-opencode-install-target/apply-progress.md`
- `openspec/changes/add-opencode-install-target/apply-report.md`

---

### Unrelated Dirty Tree Observations

The repository is still dirty beyond this approved change. Current unrelated or adjacent worktree items include:
- `docs/releases/v0.4.2.md`
- Codex-related files: `internal/install/adapter_codex.go`, `internal/install/adapter_codex_test.go`, `internal/install/codex_install.go`
- Shared files with changes not traced in the OpenCode apply artifact list: `internal/install/components.go`, `internal/install/harness.go`

These were distinguished from the OpenCode verdict and did not cause the focused or broad Go validation to fail, but they remain a traceability risk for archive/review.

---

### Issues Found

**CRITICAL**
- None.

**WARNING**
- Lore artifact retrieval is partially degraded in this worker context: direct `lore_get_observation(id)` returned `project_id is required`, so verification relied on Lore search previews plus filesystem fallback artifacts rather than full direct observation reads.
- The worktree contains unrelated dirty files outside the approved OpenCode change surface, including Codex and release-note edits; archive/review should stay scoped carefully.

**SUGGESTION**
- Add a future focused test for any eventual approved OpenCode command-surface slice so the current omit-by-default boundary cannot drift when command rendering is introduced.

---

### Verdict
PASS WITH WARNINGS

The OpenCode install target is implemented as an actual supported bounded target; CLI/TUI selection, agentpack+agentconfig sourcing, approved managed surface, fail-closed `opencode.json` merge behavior, manifest/backup/idempotency safety, and Antigravity marker-merge compatibility all match the approved explore/proposal/spec/design/tasks. Fresh focused verify reruns and a fresh broad `go test -count=1 ./...` both passed. Remaining concerns are traceability/tooling warnings only, not implementation contract failures.
