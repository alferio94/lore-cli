# Verification Report: add-opencode-lore-models-plugin

**Change**: `add-opencode-lore-models-plugin`
**Date**: 2026-06-09
**Repository**: `/Users/alfonsocarmona/personal/lore2/lore-cli`
**Persistence**: OpenSpec filesystem plus Lore MCP memory
**Verdict**: PASS WITH WARNINGS

---

## Inputs Reviewed

- `openspec/changes/add-opencode-lore-models-plugin/init.md`
- `openspec/changes/add-opencode-lore-models-plugin/exploration.md`
- `openspec/changes/add-opencode-lore-models-plugin/proposal.md`
- `openspec/changes/add-opencode-lore-models-plugin/specs/opencode-lore-models/spec.md`
- `openspec/changes/add-opencode-lore-models-plugin/design.md`
- `openspec/changes/add-opencode-lore-models-plugin/tasks.md`
- `openspec/changes/add-opencode-lore-models-plugin/apply-report.md`
- `openspec/changes/add-opencode-lore-models-plugin/state.json`

Implementation and doc surfaces re-inspected:

- `internal/install/assets/opencode/plugins/lore-models.ts`
- `internal/install/opencode_assets.go`
- `internal/install/adapter_opencode.go`
- `internal/install/opencode_install.go`
- `internal/install/json_merge.go`
- `internal/install/components.go`
- `internal/install/service.go`
- `internal/install/adapter_opencode_test.go`
- `internal/install/adapter_opencode_plugins_test.go`
- `internal/install/opencode_install_test.go`
- `internal/cli/install_flags_test.go`
- `internal/tui/model_test.go`
- `README.md`

---

## Validation Commands

```bash
go build ./...
go test ./internal/install -run 'TestOpenCode|Test.*Plugin|Test.*Manifest|Test.*Merge' -count=1
go test ./... -count=1
```

Results:

- `go build ./...` → passed.
- `go test ./internal/install -run 'TestOpenCode|Test.*Plugin|Test.*Manifest|Test.*Merge' -count=1` → passed.
- `go test ./... -count=1` → passed.

The previously observed concurrent verify-only `internal/cli` flake did not reproduce in this rerun. Because this verify executed commands independently rather than in a parallel batch, there is still mild uncertainty around that older concurrency-sensitive failure mode, but no deterministic product failure is currently evidenced.

No repository OpenCode runtime smoke harness exists for the TypeScript plugin, so task 5.6 remains a verify-phase limitation rather than an implementation miss.

---

## README Follow-up Review

- The stale README bundle wording naming managed OpenCode plugin `model-variants.ts` as a current asset is resolved.
- `README.md` now names `lore-models.ts` in the OpenCode managed-plugin bundle description.
- The remaining README reference to `model-variants.ts` is intentional historical migration context only: it explains that `lore-models.ts` preserves the discovery-cache behavior of the previous asset.
- README wording now also reflects the in-OpenCode hot-edit behavior and mentions `lore-worker` in the Lore-managed agent set.
- No new secret-bearing examples, token values, or `Authorization` headers were introduced by the follow-up.

---

## Compliance Matrix

| Goal | Evidence | Result |
|---|---|---|
| 1. `model-variants.ts` replaced by `lore-models.ts` while preserving cache behavior | Managed plugin asset registry and tests reference `lore-models.ts`; the legacy asset was removed; the cache file remains `~/.lore/cache/opencode-model-variants.json`. | COMPLIANT |
| 2. Direct in-OpenCode model/variant configuration exists; floating selector fallback acceptable only inside OpenCode | `lore-models.ts` provides in-OpenCode fallback tools (`lore_models_set_agent`, `lore_models_list_agents`) and safe `opencode.json` hot-edit behavior. | COMPLIANT |
| 3. Hot-edit of `opencode.json` safely updates only allowed fields, preserves unrelated keys, and avoids secret leakage | Source and tests still show targeted agent-field updates, backup-first atomic write flow, reparsing, and secret redaction protections. | COMPLIANT |
| 4. `lore install --target opencode` preserves user-chosen model/variant values | Renderer/install logic and focused tests still preserve existing `agent.<name>.model` and `agent.<name>.variant` values for Lore-managed agents. | COMPLIANT |
| 5. Renderer changes: `lore` primary; non-lore managed agents `mode:"subagent"`; `agent.lore.permission` removed; `lore-worker` added; exclusions preserved | Renderer/tests continue to assert `default_agent: "lore"`, `mode: "primary"` only for `lore`, `mode: "subagent"` for non-lore managed agents including `lore-worker`, and preserved exclusions. | COMPLIANT |
| 6. Stale cleanup deletes old managed `model-variants.ts` only with ownership/manifest proof | Cleanup planning remains manifest-scoped and tested against stale managed and unowned similarly named files. | COMPLIANT |
| 7. Tests/docs updated appropriately | Focused tests pass, full Go suite passes, README follow-up resolves stale current-state wording, and the only remaining old plugin reference is intentional migration history. | COMPLIANT |

---

## Findings

### Follow-up status

- The bounded README follow-up is correct and converged with the implemented OpenCode bundle naming.
- No additional code repair was required during this verify rerun.

### Remaining warnings

1. **No OpenCode runtime smoke harness**: the repository still lacks automated runtime validation for the TypeScript plugin flow, so evidence remains source-and-Go-test heavy.
2. **Prior concurrency-sensitive test signal**: an earlier verify recorded a flaky `internal/cli` failure when broader `go test` commands were launched concurrently. This rerun did not reproduce it, but the historical note remains worth carrying until intentionally stress-tested or root-caused.

---

## Conclusion

After the README follow-up, the change now satisfies the approved spec, design, and task set. The stale README `model-variants.ts` wording is resolved except for one intentional historical note, focused and broad validations pass on rerun, and the change remains ready for `sdd-archive` with only the standing warnings about missing OpenCode runtime smoke coverage and the previously observed but unreproduced concurrent-test flake.
