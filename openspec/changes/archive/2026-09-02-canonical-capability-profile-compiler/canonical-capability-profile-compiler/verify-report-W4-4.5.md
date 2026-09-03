# Verification Report: Canonical Capability Profile Compiler — W4.5

## Scope and verdict

**Verdict: PASS (scoped W4.5).** Commit `296e11b56b8d8acdda245948313fb7abf2fad124` implements only the bounded CLI canonical Explain presentation slice. It is a clean descendant of required W4.4 docs commit `2fccbac26ce29d455db7f11544abe4533469858f`; its subject is `feat(cli): add canonical install explain output` and its parent is exactly that W4.4 commit.

This is Standard verification: Strict TDD is inactive (no configured strict-TDD mode). This report does not accept W4 globally or tasks 4.6–4.11.

## Repository and cap gate

- Branch was `feat/w4-observable-routing`; `HEAD` was the required W4.5 commit and `git status --porcelain` was empty before governance persistence.
- `@{upstream}` is unset. `origin` exists as a configured remote, but this verification made no remote/upstream action.
- Commit scope is exactly the allowed six paths: `internal/install/explain.go`, `internal/cli/actions.go`, `internal/cli/app.go`, `internal/cli/app_test.go`, `internal/cli/install_presenter.go`, and `internal/cli/install_explain_test.go`.
- Canonical non-transferable accounting from `git diff --numstat 2fccbac..296e11b`: 392 additions + 8 deletions = **400 authored lines**. No transfer was used. `git diff --check` passed.

## Independent behavior review

- `runInstall` validates Explain conflicts and format before workflow dispatch, then calls exactly one injected/shared `install.Workflow.Prepare` through `installExplainAction`; focused spy tests prove one Prepare and zero Execute calls.
- `ExplainWorkflow.Prepare` delegates to the accepted `PrepareExplain` domain authority. Its `Execute` always returns `invalid_workflow_request`; it is not a planner, executor, transaction, or fallback path.
- `PrepareExplain` accepts only `ModeExplain`, invokes `RoutePolicy.Decide`, seals local facts through `SealTransactionPlan`, and returns a canonical `Prepared`/`Result`. It contains no effects interface or credential/network/finalizer/profile/manifest/journal/authority call. The reported plan has mutation count zero.
- Gate E/D/A admits canonical Explain; Gate Off produces `canonical_route_disabled` with exit 1. The CLI default policy sets all supported targets Off, which is the specified pre-rollout safe default. There is no implicit legacy fallback.
- Human success is stdout; human failure and warnings are stderr. JSON uses one newline-terminated `lore.install.result/v1` envelope on stdout with stable struct field order and no ANSI/progress output. Parser/conflict/format errors use stderr and exit 2.
- The presentation view omits `TransactionReport.ProvenancePath`; focused fixtures include sensitive paths and token-shaped strings and prove they do not appear. Repeated human/JSON/domain calls are deterministic.
- The W4.5 CLI Explain slice has no TTY prerequisite. Non-TTY refusal is specified for apply confirmation (D10, W4.8) and TUI startup (D35, W4.6), so neither was introduced or claimed by this slice.

## Scoped compliance matrix

| Scenarios | Evidence | Result |
|---|---|---|
| D1, D6 | `TestInstallExplainHumanUsesSharedWorkflowAndStableChannels`; `TestInstallExplainRejectsUsageBeforeWorkflow` | PASS |
| D11–D15 | CLI human/JSON success, admitted failure, singular-object, assigned-stream, and parser tests below | PASS for Explain scope |
| D17–D20 | Focused CLI tests prove exits 0/1/2 and residual-risk precedence 3 | PASS for represented Explain results |
| D21–D23 | Signal/authority behavior is canonical apply scope (4.8), not present in Explain | Deferred, not a W4.5 defect |
| D24–D27 | Exact W4.4 domain Explain suite below; sealed, deterministic, redacted, no-effect behavior | PASS |
| D44, D47–D48 | `TestW4ExplainEnforcesGateEAndCanonicalOnly` across all supported targets | PASS |

TUI (D16/D35+) and canonical dry-run/apply/legacy execution (D2–D5, D7–D10, D21–D23, D28+) remain intentionally deferred to tasks 4.6–4.11.

## Executed validation (verbose retained)

```text
$ gofmt -d internal/install/explain.go internal/cli/actions.go internal/cli/app.go internal/cli/app_test.go internal/cli/install_presenter.go internal/cli/install_explain_test.go
gofmt-bytes=0

$ go test -run '^$' ./internal/cli ./internal/output ./internal/install
ok github.com/alferio94/lore-cli/internal/cli (cached) [no tests to run]
ok github.com/alferio94/lore-cli/internal/output (cached) [no tests to run]
ok github.com/alferio94/lore-cli/internal/install (cached) [no tests to run]

$ go test -v ./internal/cli -run '^(TestInstallExplainHumanUsesSharedWorkflowAndStableChannels|TestInstallExplainJSONIsSingularVersionedAndDeterministic|TestInstallExplainFailuresStayOnAssignedStream|TestInstallExplainRejectsUsageBeforeWorkflow|TestInstallUsageIncludesPiFirstGuidance)$'
=== RUN TestInstallUsageIncludesPiFirstGuidance
--- PASS: TestInstallUsageIncludesPiFirstGuidance
=== RUN TestInstallExplainHumanUsesSharedWorkflowAndStableChannels
--- PASS: TestInstallExplainHumanUsesSharedWorkflowAndStableChannels
=== RUN TestInstallExplainJSONIsSingularVersionedAndDeterministic
--- PASS: TestInstallExplainJSONIsSingularVersionedAndDeterministic
=== RUN TestInstallExplainFailuresStayOnAssignedStream
=== RUN TestInstallExplainFailuresStayOnAssignedStream/human
=== RUN TestInstallExplainFailuresStayOnAssignedStream/json
--- PASS: TestInstallExplainFailuresStayOnAssignedStream
=== RUN TestInstallExplainRejectsUsageBeforeWorkflow
--- PASS: TestInstallExplainRejectsUsageBeforeWorkflow
PASS
ok github.com/alferio94/lore-cli/internal/cli 0.341s

$ go test -v ./internal/install -run '^TestW4Explain(SealsDeterministicOrderedLocalFacts|EnforcesGateEAndCanonicalOnly|RejectsUnsealedFactsRedactedAndRepeatable)$'
=== RUN TestW4ExplainSealsDeterministicOrderedLocalFacts
--- PASS: TestW4ExplainSealsDeterministicOrderedLocalFacts
=== RUN TestW4ExplainEnforcesGateEAndCanonicalOnly
--- PASS: TestW4ExplainEnforcesGateEAndCanonicalOnly
=== RUN TestW4ExplainRejectsUnsealedFactsRedactedAndRepeatable
--- PASS: TestW4ExplainRejectsUnsealedFactsRedactedAndRepeatable
PASS
ok github.com/alferio94/lore-cli/internal/install 0.424s
```

No full suite, race, vet, cross-build, or live-network validation was run, by explicit W4.5 scope.

## Closure

Task **4.5 only** is eligible for completion. Tasks 4.6–4.11 and global W4 remain unchecked. No code repair, Git commit, reset/clean/stash, or remote action occurred during verification.
