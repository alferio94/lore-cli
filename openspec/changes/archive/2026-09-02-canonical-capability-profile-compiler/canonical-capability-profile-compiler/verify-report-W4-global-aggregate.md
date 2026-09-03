# Global W4 Aggregate Verification — Canonical Capability Profile Compiler

## Verdict

**PASS WITH RECORDED R16 BASELINE.** This read-only aggregate accepts **only global W4** at `1c822fb34f553cdacdd72d3d5d5131c2dbe42b1e`. W4.1–W4.11 retain their accepted closures. No archive, merge, push, production-gate enablement, or unrelated task state advances.

## Authority and method

- Reconciled canonical proposal/spec/design/task lineage, scoped apply/verify reports, current source/Git objects, W4.11 report index `1b74d400-4d78-42d3-8c9a-c360eae1c24e`, and raw receipts in `/tmp/w4-11-verify-rerun.iB4y1k`.
- No broad Go command was rerun.
- Active accounting is **26 requirements / 192 scenarios**: preserved R1–R16 / 123 scenarios plus W4 R17–R26 / D1–D69. Earlier W1–W3.3 acceptance remains immutable and is not reaccepted.
- Historical failures remain intact: W4.4 fail-stops/needs-input and W4.11 failed report `83fb8cb5-0bdb-469d-b78e-4a20c4ece778` at `1f0785b` are not PASS evidence. W4.11 uses only its superseding rerun Lore parts `f1dd0060-9947-4349-8d21-76eafa788382`, `b46cc4aa-ad9d-4ff2-8bae-ff352074f28d`, and `72bcae38-6e89-447d-b98a-f6ebd99d7a12`.

## Task, topology, scope, and governance reconciliation

| Closure | Commit(s) | Bound scope/cap and accepted result |
|---|---|---|
| W4.1 | `30e86dc` → `95e0c97` | Canonical W4 authority checkpoint and verification: PASS |
| W4.2 | `87c4fac`, `ceaf414` | 4.2A 244/250 + 4.2B 150/150 = 394/400 contracts/policy: PASS |
| W4.3 | `0a7a2db` | Three server-scope projection paths, 156/400: PASS |
| W4.4 | `751e40a` | Sealed Explain, 207/400: PASS with accepted procedure deviation |
| W4.5 | `296e11b` | Six CLI Explain/presenter paths, 400/400: PASS |
| W4.6 | `31790f2` | Ten CLI/TUI Explain paths, 387/400: PASS |
| W4.7 | `cca5eb5` | Eleven canonical dry-run paths, 347/400: PASS |
| W4.8 | `38bc253` | Twelve canonical apply paths, 399/400: PASS |
| W4.9 | `cc4b329` | Thirteen explicit-legacy governance paths, 366/400: PASS |
| W4.10 | `267a510` | Seven presentation/golden paths, 358/400: PASS |
| W4.11 | `1c822fb` parent `1f0785b` | Four repair paths, 89/90, independent rerun: PASS with recorded baseline |

The local first-parent chain is continuous from W4 checkpoint through every implementation/closure commit to `1c822fb`. Each closure bound its path allowlist, non-transferable cap, clean/no-upstream state, and governance sync. No cap was transferred.

## Requirement and scenario traceability

| Requirement / scenarios | Implementation and passing evidence | Result |
|---|---|---|
| R17 D1–D10 modes/conflicts/confirmation | workflow contracts and CLI paths; W4.5/7/8 tests, exact17 | PASS |
| R18 D11–D16 human/JSON/TUI streams | CLI/TUI presenters; W4.5/6/10 goldens | PASS |
| R19 D17–D23 exit/cancel precedence | result/apply/presenters; W4.8 and W4.10 guards | PASS |
| R20 D24–D27 sealed deterministic Explain | Explain + sealed plan; W4.4/5 suite | PASS |
| R21 D28–D34 dry-run/apply/transaction | dry-run/apply + accepted W3.3 seams; W4.7/8 + static guard | PASS |
| R22 D35–D43 TUI state/accessibility | TUI command/model/update/view; W4.6/7 tests | PASS |
| R23 D44–D49 gates/no fallback | route policy; W4.2B/7/10 route evidence | PASS |
| R24 D50–D55 explicit legacy/retirement | adapter and explicit CLI/TUI selection; W4.9/goldens | PASS; retirement deliberately unperformed |
| R25 D56–D62 targets/server boundary | projector/adapters; W4.3 scope suite | PASS |
| R26 D63–D69 acceptance evidence | W4.10 fixtures and W4.11 receipts/CI static audit | PASS within stated limitations |

All D1–D69 map to the accepted task evidence. No W4 change weakens the preserved R1–R16 lineage.

## Coherence and safety

Current source has one shared `CanonicalWorkflow` Prepare/Execute boundary for canonical Explain, dry-run, and apply. Apply seals/reseals the accepted W3.3 plan and invokes the existing `applyTransactionFS` chain. The passing W3.3 static guard permits only `internal/install/apply.go:applyTransactionFS` and `internal/install/transaction_fs.go:applyTransactionFSWithWait`; no second planner, executor, transaction, journal, profile-store authority, or server transaction participant exists.

`defaultInstallWorkflow` sets every supported target to `GateOff`. Disabled canonical routes return `canonical_route_disabled` without legacy dispatch. Legacy requires explicit `--legacy` or advanced TUI selection through its narrow adapter. No production gate is enabled.

Canonical apply retains selected-target authority: backup before writes, finalization, profile completion, v3 manifest-last publication, rollback, and residual-risk precedence. Explain/dry-run use sealed plans without mutation authority. CLI/TUI tests/goldens cover streams, singular versioned JSON, redaction, cancellation, exits, and route markers.

## W4.11 raw evidence and limitations

The rerun ledger records exact repaired regressions **17/17 PASS**, W4 aggregate **22 parents PASS**, `go vet ./...` exit 0, and successful Linux/Windows arm64 compile/link reruns using `CGO_ENABLED=0`, `GOOS`, `GOARCH`, and `go test -exec=true ./...`. Initial cross attempts exited 127 and remain retained history; only their exact reruns are accepting evidence.

`go test ./...` and `go test -race ./...` each exit 1 solely on accepted unrelated R16 baseline `internal/cli.TestStatusAndDoctorActionsPreserveDiagnosticSemantics` (`len(checks) = 9, want 10`). All W4-relevant packages pass and race output contains no race report. Full/race are therefore **baseline-bearing, not globally green**; any extra failure would block PASS.

Linux/Windows arm64 evidence is compile/link only, not foreign-platform runtime execution. No foreign-platform runtime claim is made. CI workflow evidence is static; no remote CI was invoked.

## Git, publication, and artifacts

- HEAD is `1c822fb` with sole parent `1f0785b`; repair scope is exactly `internal/cli/app.go`, `internal/cli/app_test.go`, `internal/cli/install_flags_test.go`, and `internal/install/static_guards_test.go`.
- Tracked source/index is clean. The branch has no upstream. `origin` is configured only; no remote action occurred, and local evidence cannot claim inaccessible remote state.
- Before this phase, only authorized untracked W4.11 parity files existed: `apply-report-W4-4.11-repair.md` and `verify-report-W4-4.11-rerun.md`. This file is the sole additional exact Lore/OpenSpec parity artifact. No code/Git mutation occurred.

## Disposition

**PASS.** Mark only global W4 complete in canonical Lore task authority. Keep gates off; do not archive, merge, push, publish, retire legacy, or advance unrelated archive work.
