# Tasks: Canonical Capability Profile Compiler — W3.3/W4 Contract Gate

## State
- Active contract: 16 requirements / 123 scenarios.
- W1 1.1-1.12 accepted.
- W2A 2A.1-2A.4 accepted.
- W2B 2B.1-2B.4 accepted; rejected/candidate/transport lineage preserved.
- W2C 2C.1-2C.4 accepted; failed/rejected/candidate lineage preserved.
- W3.1 accepted; immutable authored ledger remains 636/800.
- W3.2 accepted at 463/650; lineage preserved.
- W3.3-B and W3.3-B/C1-A remain accepted; W3.3-C option 1 PASS lineage is retained only as superseded history.
- W3.3-D option 1 is now scoped accepted/verified; current C→D correction/reverification is closed.
- W3.3 is accepted/verified by the canonical aggregate report; W4 remains blocked; legacy `lore-install.json` v2 stays preserved.
- Historical D cap 875 is planning history only and must not be reused as an active transfer budget.
- W4.2 split authorized: 4.2A (244/250 final; historical pre-correction candidate lineage 248/250) is complete and bound to commit `87c4fac789f6f99ceee9116b0b5cb1f284311ac0` / verify report `f673d9c1-9141-4bdc-8971-1fd2a065327c`; 4.2B (150/150, zero headroom) is complete and bound to commit `ceaf414d7ace8b7c7c941a63758377e3fd61b968` / verify report `cac136ae-a353-4508-a6cc-56b29d101609`; aggregate 394/400 is final accepted and bound to aggregate verify `e0a3064b-61b0-4d8c-937a-5b6413dd42d9` / OpenSpec `82cb19ebb8e987f22738e550dd288d05d86b7c306e4207737236523cbff9f1a5`; historical split-preflight aggregate 398/400 remains lineage only, parent 4.2 is complete, and original/gofmt/SIGPIPE/cap-failure lineages are preserved.

## Checklist
- [x] 1.1-1.4 W1a schema/identity/purity.
- [x] 1.5-1.8 W1b provenance/immutability.
- [x] 1.9-1.12 W1c registry/admission/sensitive seam.
- [x] 2A.1-2A.4 W2A selection, scope filtering, intent, gate.
- [x] 2B.1-2B.4 W2B ownership/reconcile gate.
- [x] 2C.1-2C.4 W2C manifest codec/migrations gate.
- [x] 3.1 RED→GREEN R6-7/R13 projectors; accepted, 636/800 immutable.
- [x] 3.2 RED→GREEN R3.2/R12.1-3 create/store, success-only persistence, rollback; accepted via B13 commit `1fd4dd2d2a44b709d9c28e494c2a5fcb3a42a766`, direct CI run `32196708787`, fresh verify `a774564a0791283a1aa4d43cc08d1247bcb2b0fab0d917ec7dbafcf4eca5469e`, and acceptance review `cb6a3ad3d3dccb57110e7d60860b0ec2614bf63b8e5581f935cb701bf3d47c50`; immutable ledger remains 463/650.
- [x] 3.3 W3.3-D option 1 C→D transfer correction/reverification. Historical W3.3-C option 1 PASS lineage is retained below only; current normative work starts at 3.3.5. Canonical aggregate W3.3 A–E PASS is recorded in `sdd/canonical-capability-profile-compiler/verify-report`. Serialize edits so the two implementation units never touch the same file concurrently, and never treat an intermediate ownership API as accepted.
  - [x] 3.3.1 Historical preflight for W3.3-C option 1: read-only cap freeze and slice boundary capture (superseded lineage only).
  - [x] 3.3.2 Historical C-only finalizer slice in `internal/install/hosted_mcp_finalizer.go` / `internal/install/hosted_mcp_finalizer_test.go` (superseded lineage only).
  - [x] 3.3.3 Historical C-only redaction/no-server evidence in `internal/install/hosted_mcp_finalizer_test.go` (superseded lineage only).
  - [x] 3.3.4 Historical two-file slice verification for W3.3-C option 1 (superseded lineage only).
  - [x] 3.3.5 Read-only sdd-apply preflight: freeze the exact two-unit boundary within the fresh exact eight-file universe, per-unit ledgers, and a newly measured non-transferable cap; confirm serialized ownership of `transaction.go`/`transaction_test.go`, capture the frozen Unit-1 hashes Unit 2 must inherit, and invalidate the historical D cap 875 and failed six-file preflight.
  - [x] 3.3.6 Unit 1 — `internal/install/hosted_mcp_finalizer.go`, `internal/install/hosted_mcp_finalizer_test.go`, and `internal/install/transaction.go`: return the opaque C→D handoff, prove provisional C success and the single-CAS ownership seam, and keep D-only completion out of scope.
  - [x] 3.3.7 Focused validation for Unit 1 in `internal/install/transaction_test.go`: prove handoff ownership, provisional C success, stale/foreign/reused handoff rejection, and the absence of profile/v3 completion before the Unit 1 hashes freeze.
  - [x] 3.3.8 Unit 2 — `internal/install/profile_store_authority.go`, `internal/install/profile_store.go`, `internal/install/profile_store_unix.go`, `internal/install/profile_store_windows.go`, and `internal/install/transaction.go`: start only from the frozen exact Unit-1 hashes, add the profile lease/rebase/apply/restore path through the held-authority adapter seam, and complete canonical v3 manifest-last publication with D-exclusive commit/release.
  - [x] 3.3.9 Focused validation for Unit 2 in `internal/install/transaction_test.go`: cover lease ordering, exact v3 publication, manifest-last durability, rollback/restoration, and residual-risk/cleanup behavior while preserving the Unit 1 seam.
    - [x] 3.3.9.1 Native-adapter reverification on Unix and Windows: prove absent-state safe restore without raw-path deletion, present-state exact bytes/mode restoration, canonical identity/no-follow/reparse safety, authority ownership/durability/residue cleanup, and no exported/general delete API.
  - [x] 3.3.10 Scoped reverification of the corrected W3.3-C acceptance surface: re-run the C21-C30 expectations against the new C→D behavior from the frozen Unit-1 hashes and keep the prior PASS as superseded lineage only.
  - [x] 3.3.11 Governance-only W3.3-E corrected primary-outcome validation for the fixed two-file identity `e42aa4e57a5ae5e150bd265cbf5845c97cd18020af57e4212634829c2a7cc61a`; preserve the original +592 lineage and the +13/-1 correction, the normal/race/static/AST/cross-build receipts, and the verify memory/report lineage while keeping the historical failed verify as superseded history.
- [x] 3.4 Read-only sdd-apply preflight: measured the candidate cap and froze the W3.3-C option 1 slice; no writes or `profile_store*` calls.
- [x] 3.5 Canonical root identity + process-local guard in `internal/install/transaction_fs.go` and `transaction_authority.go`; add tests for alias convergence, same-root exclusion, and unrelated-root overlap. Out: `profile_store*`, W4, server APIs.
- [x] 3.6 Unix authority/owner-death in `transaction_authority_unix.go`; add tests for `O_DIRECTORY|O_NOFOLLOW`, `flock`, zero-wait busy, five-second timeout, redacted `target_authority_busy` / `target_authority_timeout` at `selected_target.authority`, and orphan recovery before mutation.
- [x] 3.7 Windows authority/owner-death in `transaction_authority_windows.go`; add Windows CI/runtime tests for `FILE_FLAG_OPEN_REPARSE_POINT`, mutex abandonment, case/volume/reparse aliases, ACL retention, and redacted outcomes.
- [x] 3.8 Durable orphan journal recovery + fail-closed rollback in `transaction_fs.go`/tests; cover crash/rollback/idempotent completion, residue cleanup, and independent-root concurrency. Out: profile-store authority and W4.

## W4 Ordered Checklist (supersedes stale 4.1-4.4)
- [x] 4.1 Read-only preflight from clean W3.3 checkpoint `ccfc222ce854349e86577d7bcee888949e773e12`; PASS verified in Lore `0510017a-8785-4aac-82fb-852192eaad44` for commit `30e86dcd1121ee2040a53d01961efe42b75aef36`; freeze the W4 slice cap, confirm no W4 branch/worktree mutation, and stop on dirty base or hash drift.
- [x] 4.2 Split W4 task into child tasks 4.2A and 4.2B; parent complete after aggregate verification; aggregate 394/400 is final accepted and bound to aggregate verify `e0a3064b-61b0-4d8c-937a-5b6413dd42d9` / OpenSpec `82cb19ebb8e987f22738e550dd288d05d86b7c306e4207737236523cbff9f1a5`, historical split-preflight aggregate 398/400 remains lineage only, and 4.2B has zero spare lines.
  - [x] 4.2A Add `internal/install/{workflow_contract,result,event,error,legacy_adapter}.go` plus `workflow_contract_test.go` for Request/Prepared/Result/Event/Error, Observer, defensive-copy, one-shot, and error coverage; cap250; authority from candidate `454b65f5-c0a7-4c0c-aa1c-6567f8312e12` and report `05991af6-00f9-48f3-a276-b99fee18a821`; final accepted ledger 244/250, with historical pre-correction candidate lineage 248/250, completed by independent PASS on commit `87c4fac789f6f99ceee9116b0b5cb1f284311ac0` and verify report `f673d9c1-9141-4bdc-8971-1fd2a065327c`.
  - [x] 4.2B Add `internal/install/route_policy.go` plus `route_policy_test.go` for per-target Gate/RoutePolicy demotion, E→D→A gating, explicit legacy/no-fallback behavior, and table-driven route coverage; cap150, zero headroom; completed by independent PASS on commit `ceaf414d7ace8b7c7c941a63758377e3fd61b968` and verify report `cac136ae-a353-4508-a6cc-56b29d101609`.
- [x] 4.3 Extend `internal/install/projector.go` and adapter seams so local `ProjectID`, server UUID/key, and explicit `repository_id` stay distinct; cover C1-C4 and D56-D62; completed by independent PASS on commit `0a7a2dbba4a363a92800ef68f23347466109ddc1` and verify report `11a65f29-ded8-49e7-971c-65168f7f9cd6`.
- [x] 4.4 Implement sealed `internal/install/explain.go` with zero mutation/network/credential/authority effect, stable ordering, and redaction; verify D24-D27 and D1-D10.
- [x] 4.5 Wire `install --explain` in `internal/cli/{app,actions}.go`, `internal/cli/install_presenter.go`, and `internal/output/*` for human/json stdout/stderr, schema-v1 JSON, conflict/usage exits, and non-TTY refusal; test D11-D23.
- [x] 4.6 Add TUI explain parity in `internal/tui/install_{model,update,view,cmd}.go` with no-TTY rejection, reduced-motion, navigation/cancel/retry/resize, and typed route/status parity; test D35-D43 and D65; completed by independent PASS on commit `31790f27e53ad2a1cf7fbb7153e512b875d04b64` and verify report `sdd/canonical-capability-profile-compiler/verify-report-W4-4.6`.
- [x] 4.7 Route canonical dry-run through the shared Workflow using sealed Prepare/Result only, with zero side effects, typed phase/event ordering, and canonical-vs-legacy route markers; test D2, D28-D34, D44-D49.
- [ ] 4.8 Route canonical apply through the same Workflow into accepted W3.3 seams with confirmation, signal 130 precedence, residual-risk code 3, rollback/recovery, and `transaction*`/`profile_store*` authority boundaries; test D3, D9-D10, D17-D23, D29-D34.
- [ ] 4.9 Keep explicit legacy dry-run/apply behind `LegacyAdapter` only; emit warnings/route markers, block fallback/kill-switch shortcuts, and keep retirement/removal out of W4; test D4-D5, D48-D55.
- [ ] 4.10 Add goldens and stream guards in `internal/{cli,tui,install}/testdata` for human/json/TUI outputs, redaction, path/no-effect checks, and four-target E→D→A matrices; cover D63-D69.
- [ ] 4.11 Run focused race/vet/full-suite and cross-platform CI checks with an independent verify slice before closing any W4 task; isolate the known baseline and keep W3.2/B13 and W3.3 lineage untouched.

## W3.3/W4 Contract Gate
- W3.3-D option 1 is bounded; the preflight-derived cap is authoritative.
- W4 remains pending until 4.1-4.11 are independently verified in order; 4.2 must land as 4.2A/4.2B under the same canonical gate, each slice stays ≤400 authored lines including tests/docs, and any 4.2B drift requires fail-stop/new preflight before apply if canonical artifacts or the clean base are not ready.
- Rollback/hybrid checkpoint: preserve `tasks-pre-amendment-W3.3-W4`, `tasks-validation-W3.3-W4`, and W3.3 design/pre-snapshot/validation hash parity; do not unlock W4.
