# Apply Report: Canonical Capability Profile Compiler — W4.11 Integration Repair

## Latest Slice Result
- Status: completed repair; acceptance remains pending.
- Task attempted: bounded W4.11 integration repair.
- Task completed: none; W4.11 and global W4 remain unchecked.
- Failed verification lineage commit: `1f0785b5efcea0d9a2a295a3d85d2250f84f87bd`.
- Repair commit: `1c822fb34f553cdacdd72d3d5d5131c2dbe42b1e`.
- Parent chain: `cfdb3c10` → `1f0785b` → `1c822fb`.

## Repair
- Restored unsupported/roadmap target prevalidation before canonical route-gate evaluation; no gate or fallback changed.
- Updated the 15 supported legacy cases to explicit legacy invocations, including effect-free explicit-legacy confirmation decline.
- Kept the W3.3 AST/profile-authority scan and allowed only `internal/install/apply.go:applyTransactionFS` plus the exclusive `transaction_fs.go:applyTransactionFSWithWait` authority chain.
- Source/test scope is exactly four authorized paths at 89/90 authored changed lines.

## Validation
- Exact diagnosed W4 failing tests by name with verbose output: PASS; `/tmp/w4-11-repair/targeted.verbose.log`.
- `go test ./internal/cli ./internal/install`: expected FAIL solely from accepted Requirement 16 baseline `TestStatusAndDoctorActionsPreserveDiagnosticSemantics`; `internal/install` passed and no repaired W4 case failed. Log: `/tmp/w4-11-repair/packages.log`.
- gofmt, diff-check, exact scope, cap, no gate/fallback addition, high-confidence secret scan, exact commit paths/messages/bodies, clean pre-report tree, no upstream, parent chain, and task-pending checks: PASS.
- Full `./...`, race, vet, and cross-platform validation were intentionally deferred to the independent W4.11 rerun.

## Repository State and Handoff
- The committed repair tree was verified clean with no upstream before this required OpenSpec report was persisted.
- Dirty tree expected now: yes — only this untracked repair report.
- Required next action: independently rerun W4.11 verification; do not mark task 4.11 from this apply report.
