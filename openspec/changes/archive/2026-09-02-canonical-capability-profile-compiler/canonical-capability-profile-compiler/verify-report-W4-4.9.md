# Verification Report: Canonical Capability Profile Compiler — W4.9

## Verdict

**PASS — scoped W4.9 only.** Independent verification of `cc4b329b8d73a49d37fd16ed3741ab5e1f5a45f1` (`feat(install): govern explicit legacy routes`) passed. Its sole parent is the required W4.8 closure sync `7c88154d90eaa4189f4f27f74ecc81356fff5831`. HEAD is the verified commit, the worktree is clean, and branch `feat/w4-observable-routing` has no configured upstream. No remote operation occurred.

## Scope and accounting

- Exact scope is the 13 authorized install/CLI/TUI paths: `internal/install/{legacy_adapter,apply,error,route_policy_test}.go`, `internal/cli/{app,actions,install_explain_test}.go`, and `internal/tui/{root,install_model,install_cmd,install_update,install_view,install_test}.go`.
- `+327/-39 = 366/400`; 34 lines remain non-transferable. `gofmt -d` is empty and `git diff --check` passes.
- No OpenSpec, testdata/golden, CI, scripts, module, gate-default, rollout, 4.10, or 4.11 path changed. Production defaults set every supported target to `GateOff`.
- Diff review found no new network, credential/config/user-home, filesystem-effect, or secret-bearing production call. The only secret-shaped addition is a fake `/secret/config` non-disclosure assertion. Executed tests use spies and a temporary offline `TMPDIR` with `GOPROXY=off GOSUMDB=off`; no real user state or live service was used.

## Contract evidence

- CLI reaches legacy only when the parsed `--legacy` flag selects `legacy-apply` or `legacy-dry-run`; explain/legacy and yes/dry-run conflicts fail as usage. Canonical explain, dry-run, and apply retain one `CanonicalWorkflow` boundary.
- TUI exposes legacy only through the visible `l Legacy (advanced, deprecated)` selection from canonical Explain. It re-prepares on selection, displays `explicit-legacy` and a typed `legacy-deprecated` warning, requires Enter before execution, and Esc cancels without adapter execution.
- `LegacyAdapter` is narrow: `PrepareExplicitLegacy` validates explicit legacy modes/targets and produces a one-shot prepared value; `ConsumeExplicitLegacy` prevents reuse; completion maps only a fixed redacted `legacy_execution_failed` outcome. Adapter calls are absent from the install route policy and canonical workflow.
- Gate-off canonical failure, unsupported target, canonical preparation/application rejection, transaction/finalizer failure, cancellation, and presentation errors do not dispatch to legacy. Route selection is deterministic (`canonical-sealed` or `explicit-legacy`), warnings are typed, human/JSON/TUI stream behavior remains stable, and the focused tests assert redaction and JSON stderr silence.
- Retirement/removal is untouched; gates remain off. No automatic fallback or kill-switch shortcut was introduced.

## Executed validation

- `gofmt -d` over all 13 changed Go files; `git diff --check 7c88154 cc4b329` — PASS.
- `TMPDIR=<temporary> GOPROXY=off GOSUMDB=off go test -count=1 -run '^$' ./internal/install ./internal/cli ./internal/tui` — PASS.
- Exact W4.9 tests — PASS: `TestW49ExplicitLegacyPreparationIsGateIndependentOneShotAndRedacted`; `TestW49ExplicitLegacyCLIUsesOnlyAdapterWithStableRedactedParity`; `TestW49CanonicalFailuresNeverInvokeLegacyAdapter`; `TestW49TUIRequiresAdvancedLegacySelectionAndConfirmation`.
- Focused regressions (six) — PASS: the three W4.7 install dry-run contract/no-fallback tests; the two W4.7 CLI human/JSON and disabled-route tests; and the W4.7 TUI reprepare/route-parity test.
- Final static receipt — PASS: exact parent/subject/HEAD, clean worktree, no upstream, exact 13-path allowlist, `366 <= 400`, no 4.10/4.11 paths, all production gates off, and no install-policy/workflow legacy-adapter call.

## Scoped compliance matrix

| Scenarios | Evidence | Status |
|---|---|---|
| D4-D5 | Explicit CLI legacy apply/dry-run uses only the adapter; TUI advanced selection prepares and confirms before adapter execution. | COMPLIANT |
| D48-D49 | Gate-off/unsupported/canonical failures and W4.7 no-fallback regression prove no implicit adapter use; target-local policy remains unchanged. | COMPLIANT |
| D50-D51 | Stable human/JSON/TUI `explicit-legacy` marker and typed deprecation warning; JSON has no stderr chatter; redaction assertions pass. | COMPLIANT |
| D52-D55 | No retirement/removal code or rollout changed; proposal/design conditions remain deferred to a separate reviewed change. | COMPLIANT (preservation) |

## Task closure

Task **4.9** is independently verified and may be marked complete in Lore task authority. Only 4.9 changes status. Tasks **4.10-4.11** and global W4 remain pending. The OpenSpec ledger remains intentionally unmodified in Lore mode.

## Risks

No blocking W4.9 defect found. Deferred: W4.10 goldens/stream guards and W4.11 broad/full-suite, race, vet, cross-platform, and CI evidence. These were deliberately not run.