# Verification Report: Canonical Capability Profile Compiler — W4.7

## Verdict

**PASS — scoped W4.7 only.** Commit `cca5eb5d4b7bbeca5bd8a1bb03f4d5edcd3cdc40` is the clean, local, no-upstream successor of W4.6 closure `87dee1c383426e7cdb8e5b4294d83702c03ed606`. Its subject is `feat(install): route canonical dry-run workflow`; it has no body/trailers.

## Scope and accounting

- Exact parent: `87dee1c383426e7cdb8e5b4294d83702c03ed606`.
- Exact committed scope: the eleven authorized install/CLI/TUI dry-run paths; no W4.8 apply, W4.9 legacy governance, W4.10 goldens, or W4.11 broad-validation path was added.
- Accounting: `+311/-36 = 347/400` authored lines; 53 lines unused and not transferable.
- Initial post-commit worktree was clean; branch `feat/w4-observable-routing` has no configured upstream. No remote operation occurred.
- `git diff --check`, changed-diff secret/denylist scan, and path allowlist review passed.

## Contract evidence

- `CanonicalWorkflow` now prepares canonical dry-run through `PrepareDryRun`, seals `TransactionPlan`, and consumes that same one-shot sealed plan in `ExecuteDryRun`.
- Dry-run uses only `TransactionPlan.DryRun(nil)`: no confirmation, filesystem, network, credential, authority, backup/journal, finalizer, profile, manifest-publication, or mutation effect seam is invoked. Report mutation count remains zero.
- Execution rejects consumed, mismatched, non-dry-run, assume-yes, non-canonical, or zero-plan `Prepared` values. Route-policy D/A admission succeeds; E/off returns exact `canonical_route_disabled` without legacy fallback.
- `Prepared`, reports, results, operations, guidance, and observed events are copied defensively. Events are deterministic: prepare started/completed then seal started/completed. Observer panic cannot control the dry-run result.
- CLI invokes exactly one shared `Prepare` and one `Execute` for canonical dry-run. Human/JSON success uses stable stdout; admitted JSON rejection is one structured stdout object with exit 1; parser/combination errors remain stderr/2. TUI re-prepares from Explain, executes the shared workflow once, renders canonical route/result parity, and retains existing cancellation/navigation behavior.

## Executed validation

- `gofmt -d` on all 11 changed Go files and `git diff --check` — PASS.
- `go test -run '^$' ./internal/install ./internal/cli ./internal/tui` — PASS.
- Six W4.7 top-level tests plus human/JSON subtests — PASS:
  - `TestW47DryRunConsumesSameSealedPlanWithOrderedEventsAndNoEffects`
  - `TestW47DryRunRejectsMismatchReuseAndObserverInterference`
  - `TestW47DryRunEnforcesIndependentGatesWithoutLegacyFallback`
  - `TestW47InstallDryRunHumanAndJSONUseOneSharedWorkflow` (`human`, `json`)
  - `TestW47InstallDryRunDisabledFailsClosedWithoutExecute`
  - `TestW47TUIDryRunRepreparesAndRendersCanonicalResultParity`
- Ten focused accepted contract regressions — PASS: three W4 Explain, two RoutePolicy, Prepared defensive-copy, and four CLI Explain/format/stream/usage tests.

## Task closure

Only checklist item **4.7** is now marked complete. Items 4.8–4.11 and the global W4 gate remain pending. No broad/full suite, race, vet, cross-build, CI, goldens, legacy execution, or apply behavior was run or absorbed.

OpenSpec canonical tasks SHA-256 after this closure: `f9cdaea8e6913e49b0a7a6f97a1b8be621fd70f3de7b4a517077d23397b5efe9`.
