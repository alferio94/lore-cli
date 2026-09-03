# Verification Report: Canonical Capability Profile Compiler — W4.8

## Verdict

**PASS — scoped W4.8 only.** Commit `38bc25327edb72a99d8c2d5359dd97af969b8926` (`feat(install): execute canonical apply workflow`) is clean and is the exact child of accepted W4.7 closure `f42262e39e9bdb59d6c5fe5ae8d65744933f9077`. The local branch has no configured upstream; no remote operation occurred.

## Scope and accounting

- Exact committed scope: 12 authorized install/CLI/TUI paths: `internal/install/{apply.go,apply_test.go,explain.go,result.go}`, `internal/cli/{actions.go,app.go,install_presenter.go,install_explain_test.go}`, and `internal/tui/{install_cmd.go,install_update.go,install_view.go,install_test.go}`.
- Accounting: `+383/-16 = 399/400`; one line remains non-transferable. `gofmt -d` is empty and `git diff --check` passes.
- Changed-path, diff-denylist, secret-marker, and unsafe-path inspection found no credentials, network client/dialer, user config/credential access, or rollout/4.9+ additions. The runtime fixtures are `t.TempDir`-backed; execution used an isolated `TMPDIR` and `GOPROXY=off GOSUMDB=off`.
- Production route policy remains `GateOff` for every target. The candidate is not wired into production defaults; no rollout was enabled. Explicit legacy remains unavailable and no implicit fallback path exists.

## Contract evidence

- `CanonicalWorkflow.Prepare/Execute` is the sole shared CLI/TUI domain boundary. Apply admits only `ModeApply` at Gate A and seals a W3.3 `TransactionPlan`; execution consumes `Prepared` once, repeats Gate A and sealed-plan equivalence checks, and rejects target/profile/store/plan drift before authority or mutation.
- The apply path invokes the accepted W3.3 transaction authority/journal, hosted-MCP finalizer, opaque one-CAS handoff, held profile completion, and v3 manifest-last completion seams. Surface code only prepares, confirms, observes, and presents typed results; it owns no planner, transaction, journal, profile authority, or mutation implementation.
- W3.3 sealing validates admitted/sealed IR, exact target/semantic identity, profile completion, reconcile/permit, drift, provenance path, and canonical manifest binding. Transaction authority recovers before admission, validates safe paths, backs up all resources before ordered writes, and holds the journal through finalization/completion. The completion seam owns profile then v3 publication then one commit/release; rollback restores profile/target/v3 coherently and residual risk overrides primary failures.
- Apply resolves the credential only in accepted finalization, clears it in the established finalizer, emits cloned deterministic typed events, shields execution from observer panics, and converts failures to redacted logical codes/paths. Legacy route markers are absent from canonical results.
- CLI confirms only after admission; a non-TTY without `--yes` returns `confirmation_required` without reading stdin. Human progress is stderr; JSON is one final object on stdout; exit precedence is residual risk `3`, interruption `130`, cancellation-before-execute `0`, and other refusal/recovered failure `1`. TUI uses the same workflow, blocks navigation during execute/cancel, waits for final typed results, re-prepares on retry, and disables retry for residual risk.

## Executed validation

- `gofmt -d` on all 12 changed Go files; `git diff --check <W4.7> <W4.8>` — PASS.
- `TMPDIR=<temporary> GOPROXY=off GOSUMDB=off go test -count=1 -run '^$' ./internal/install ./internal/cli ./internal/tui` — PASS (all three packages).
- Exact W4.8 tests — PASS: `TestW48CanonicalApplyUsesOneSealedW33TransactionAndPublishesManifestLast`, `TestW48CanonicalApplyRejectsMismatchWithoutEffectsAndRollsBackFinalizerBoundary`, and `TestW48InstallApplyConfirmsThenUsesOneSharedWorkflowWithExitPrecedence`.
- Focused W4.7 CLI/TUI/domain regressions — PASS: three canonical dry-run contract tests, both CLI dry-run tests, and the TUI canonical dry-run/reprepare parity test.
- Accepted W3.3 regressions — PASS: identity/effect-free rejection matrix; handoff misuse/stale/CAS tests; completion manifest-last/one-commit test; W3.3 success and full failure matrix; v2 preservation/v3/profile restoration; pre-commit rollback and residual/post-commit cleanup tests.
- Focused test receipt: 2 W4.8 domain + 1 W4.8 CLI + 3 W4.7 domain + 2 W4.7 CLI + 1 W4.7 TUI + 5 TUI cancel/retry/no-fallback/non-TTY checks + 10 accepted W3.3 top-level tests (including their named submatrices), all PASS. No full suite, race, vet, cross-build, CI, goldens, or live network was run.

## Scoped compliance matrix

| Scenarios | Evidence | Status |
|---|---|---|
| D3, D9-D10 | Apply CLI uses shared workflow after confirmation; fake/non-TTY tests prove confirmation and no stdin read. | COMPLIANT |
| D17-D23 | Typed exit precedence and cancellation UI behavior; W3.3 rollback/residual matrices. | COMPLIANT |
| D29-D34 | W4.8 apply tests plus W3.3 sealing/handoff/completion/failure/recovery matrices prove admission-before-effects, finalization-only credential, ordered events, rollback, residual risk, v2 preservation, manifest-last, and residue cleanup. | COMPLIANT |

## Task closure

Task **4.8** is independently verified and may be marked complete in Lore task authority. Only 4.8 changes status. Tasks **4.9–4.11** and global W4 remain pending; production gates remain off. The repository task ledger is intentionally left clean/unmodified by this read-only verification; its pre-closure SHA-256 is `f9cdaea8e6913e49b0a7a6f97a1b8be621fd70f3de7b4a517077d23397b5efe9`.

## Risks

No blocking W4.8 defect found. Deferred, not accepted: explicit legacy work (4.9), goldens/stream guards (4.10), and broad/race/vet/cross-platform/CI validation (4.11).