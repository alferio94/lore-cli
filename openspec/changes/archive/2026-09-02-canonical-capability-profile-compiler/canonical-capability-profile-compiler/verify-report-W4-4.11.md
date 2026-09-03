# Verification Report: Canonical Capability Profile Compiler — W4.11

**Verdict: FAIL — task W4.11 remains pending.**

## Scope, identity, and environment

- Verified before artifact persistence: `feat/w4-observable-routing` at exact requested tip `cfdb3c10cf74a631379dbbb5eadc19156235b3ea`; porcelain was empty and no upstream was configured. The only subsequent worktree change is this required verification artifact; no source, task-ledger, commit, reset/clean/stash, remote, CI, or service action occurred.
- Host: macOS 26.6.2, `darwin/arm64`, Go `go1.26.4`. Commands used `GOPROXY=off`, `GOSUMDB=off`, and a fresh temporary `TMPDIR`; no dependency fetch, live service, or user configuration was used.
- Raw command stdout/stderr and exit receipts remain at `/tmp/w4-11-verify.QB3rWC/` (preflight SHA-256 `21825de19ad3ba3214732cbafc91157ceb20fb3d7dfb5e0d6cc646c6ccf6e269`). Empty stderr files are retained as zero-byte evidence.

## Authorized local matrix

| Command | Result | Raw output SHA-256 |
|---|---|---|
| `go test ./...` | **FAIL** (exit 1, 15s) | `78cf9ee0cf43f609ee21d286d285b0059014416f0ae909e5b21193befcd00754` |
| `go test -race ./...` | **FAIL** (exit 1, 63s); no `WARNING: DATA RACE` emitted | `2c2a3c68ceea7a6974c8c7a837d736f7523ef40793d8d52c54b3c4875eeb38ac` |
| `go vet ./...` | **PASS** (exit 0) | stdout/stderr both empty |
| `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -exec=true ./...` | **PASS** (exit 0) | `6494fc3ad6edd177bb15a92aebed2181dd5b9b0d1de8f3ce1c0ad8af5c8e1e5f` |
| `GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go test -exec=true ./...` | **PASS** (exit 0) | `d3c081468b978e41660494c6139b1b0632b07af315439e01f89167357d91b6d7` |
| `go test ./internal/install ./internal/cli ./internal/tui -run '^(TestW4|TestW47|TestW48|TestW49|TestW410)'` | **PASS** (exit 0) | `abb239cd7aca7fe34a2d16e46237cc1fe7c41a61333b793ee289454bc6a3253d` |

`-exec=true` is explicitly compile/link-only evidence: it invokes `true` instead of executing target test binaries. Linux and Windows runtime tests were **not** executed locally. The native full and race runs are darwin/arm64 runtime evidence only. An additional native `go test -exec=true ./...` compile-only pre-step also passed; it adds no cross-platform or runtime claim.

## Full-suite blockers

1. **Known baseline, isolated:** `internal/cli.TestStatusAndDoctorActionsPreserveDiagnosticSemantics` still reports `len(checks) = 9, want 10`. This is the recorded Requirement 16 baseline and is not attributed to W4.
2. **W4 integration/acceptance regression (not a harness failure):** 16 existing CLI install tests expect the pre-W4 default installer to execute. With the intentionally default-off canonical gates, default canonical explain/dry-run/apply returns `canonical_route_disabled` (exit 1) before the old installer behavior. The affected tests include Pi, OpenCode, Antigravity, component, dry-run, apply, confirmation, and unsupported-target paths. They must be reconciled with the explicit-legacy/default-off contract; the full suite cannot be accepted as-is.
3. **Product boundary regression (not a harness failure):** `internal/install.TestW33EStaticGuardHasOneTransactionAndNoProfileAuthorityReuse` fails in both normal and race full suites. It finds two production transaction callers: the accepted `internal/install/transaction_fs.go:applyTransactionFSWithWait` and new `internal/install/apply.go:applyTransactionFS`. The W3.3 authority guard requires the former as the sole transaction entrypoint. This conflicts with the W4 design boundary that the W4 surface consumes the accepted W3.3 seam rather than owning transaction execution.

These deterministic failures reproduce in both full normal and full race execution; they are product/test-integration defects, not an offline toolchain or platform-harness block.

## Aggregate W4 behavior audit

| Area | Evidence | Result |
|---|---|---|
| Explain, dry-run, and sealed Prepared flow | `PrepareExplain`, `PrepareDryRun`, one-shot defensive `Prepared`, and focused W4 aggregate | PASS |
| Explicit legacy and no fallback | `RoutePolicy` only selects legacy for explicit modes; CLI/TUI adapter tests pass | PASS |
| E/D/A gates and defaults | Four-target golden matrix passes; production `defaultInstallWorkflow` sets every supported target to `GateOff` | PASS |
| CLI/TUI parity, streams, exits | Focused CLI/TUI/golden tests pass for human/JSON/TUI route markers, warnings, cancellation, and exit precedence | PASS in focused fixtures |
| Zero-effect explain/dry-run | W4 focused workflow tests pass and code stops before transaction/finalization | PASS |
| Apply transaction/manifest-last/rollback | Focused W4 apply tests pass, but direct `applyTransactionFS` caller violates the accepted W3.3 authority guard | **BLOCKED** |
| Redaction | Focused human/JSON/TUI goldens and legacy tests pass secret/path probes; no secret material appears in retained broad-test output | PASS within executed fixtures |

The accepted W3.3 native runtime/CI lineage remains historical W3.3 evidence only. It was reconciled as unchanged and is not presented as W4 Linux/Windows runtime evidence.

## Local CI parity review

Static review of `.github/workflows/{transaction-fs,profile-store,release}.yml` is retained at `/tmp/w4-11-verify.QB3rWC/ci-parity.stdout` (SHA-256 `40f0c730b052d2a0f9d5851513442fe69a23fcc4b71f497d05c701e83c7daa5f`). W4 touches transaction/finalizer-path-filtered install files, so `transaction-fs.yml` is relevant; it supplies prior W3.3 native/race and six-target `internal/install` cross-build coverage. `release.yml` has the repository-wide test gate. The local matrix exceeded the local portions with full vet and all-package Linux/Windows compile-only checks. No workflow/action/remote invocation occurred, so no new CI runtime result is claimed.

## Task disposition

Do **not** mark W4.11 complete. Do not change global W4 or archive state. The canonical task authority remains W4.1–W4.10 complete; W4.11 and global W4 pending. No code repair was performed.
