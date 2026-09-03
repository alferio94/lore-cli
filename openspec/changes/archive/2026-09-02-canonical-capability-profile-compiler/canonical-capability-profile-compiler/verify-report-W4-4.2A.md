# Verification Report: Canonical Capability Profile Compiler — W4.2A Existing Commit

## Scope and mode

- Task: W4.2A only.
- Mode: independent, bounded, read-only local verification.
- Commit: `87c4fac789f6f99ceee9116b0b5cb1f284311ac0`.
- No Go command, test, build, gofmt, race, vet, runtime, CI, network/remote Git operation, task edit, or repository/file mutation was performed.
- Existing executable receipt was reconciled but not rerun: Lore `fc97bd42-7cbe-44ab-a5d7-247c43cd1431`.

## Verdict

**PASS for W4.2A commit identity, exact bytes, bounded shared-contract scope, prior immutable focused evidence, and local no-upstream/no-mutation evidence.**

This verifies only child 4.2A. It does not complete parent 4.2, child 4.2B, W4.3–W4.11, or global W4.

## Repository and commit identity

| Check | Expected | Observed | Result |
|---|---|---|---|
| Worktree / branch | `/private/tmp/lore-cli-w4-observable-routing-ccfc222c`, `feat/w4-observable-routing` | Exact | PASS |
| Tip | `87c4fac789f6f99ceee9116b0b5cb1f284311ac0` | Exact | PASS |
| Parent | `c863ecfa2598ad7c95c8cc6ea16fe8ac23821777` | Exact | PASS |
| Tree | `84b342e4f471e453c995c6c72e5cc46d543b8278` | Exact | PASS |
| Worktree/index | clean | zero status entries; worktree and index clean | PASS |
| Feature upstream | absent | no upstream configured; no `origin/feat/w4-observable-routing` local tracking ref | PASS |
| Ref/tag evidence | no local publication evidence | only local feature branch points at commit; no tag or remote-tracking ref contains it | PASS |

`origin` remains a pre-existing local remote configuration. No remote query, fetch, push, PR, tag, GitHub, or other live mutation was executed. Local evidence cannot assert remote-server state beyond absence of an upstream and local tracking/published refs.

## Raw commit-object message and signature

`git cat-file commit` was used directly, not formatted `git log` output or command-substitution framing. After the header separator, the raw body was byte-equal to:

```text
feat(install): add W4 workflow contracts\n
```

It has exactly the required canonical terminating LF and no additional paragraph, blank body, trailer, signature, AI attribution, or `Co-Authored-By` text. The raw header has no `gpgsig`. **PASS.**

## Exact delta and candidate parity

The delta contains exactly six added mode-`100644` paths and no others:

| Path | SHA-256 body | Git blob | Lines |
|---|---|---|---:|
| `internal/install/error.go` | `6b2739094506b66f032d89571724950ffb52e82d7a7b54fbd8f6a0e286d92774` | `722c478207577cccba32ef79da74038a3947b56e` | 42 |
| `internal/install/event.go` | `29648cd0ffa711ea9e6191b0b8445ab8053d0f3227f8e255513429eaa0dd6f5d` | `52da59ea9d7eac0def7f33d00824de4d1326aa2b` | 44 |
| `internal/install/legacy_adapter.go` | `8fac62c795c266c3302f406c6e38dfdfc14758da1ea0fc1709d795b1bcc3589b` | `f19c6b4abec834744a125760676a8969f1d857ec` | 10 |
| `internal/install/result.go` | `eec7fd9543008e72460ffde9fd3358d115c96db810d7f5f2aa5208d85542b34b` | `ae99d41d4f8039af4731c7da37b2b89f75bacdc6` | 51 |
| `internal/install/workflow_contract.go` | `8c8809ea123b5eb03593a4209b80cdfee54405e6449924dd3713cd0609afa137` | `f07d73324aaceef628ba5bd23db5608d7bd83963` | 66 |
| `internal/install/workflow_contract_test.go` | `19bae28e2364d6ea8ba8d8b731b1bf24d3a15ea935a79131b72b15e35c7aae15` | `5619be4e2210462d6c6b5b4abc4fdfb3d6b251c1` | 31 |

All six body hashes/blobs/modes match corrected candidate Lore `17165095-ee1d-41c3-95f3-0466540fa383` and its immutable external freeze. The immutable candidate manifest file is byte-recomputed as `830a4801a4008ae3f8383a5a535808b4e407e6e380a1e40f45e0cd7b01816789`; the committed binary/full-index patch is byte-identical to the freeze and hashes to `0b4b3d28773aefd381db426073475d605e390a9c691686df5fe95d7b3c0edd5a`; reconstructed ordered payload hashes to `002d399c0754447088552c6a8a246ff7befb2c63fadb420d9dc6e40541d3ad79`. Numstat is `+244/-0`, within immutable non-transferable cap 250. Result tree is exact.

No denied changed path exists. `internal/install/route_policy.go` and `internal/install/route_policy_test.go` are absent in both parent and commit. No CLI, TUI, output, server, profile, journal, transaction, authority, or route-policy behavior file is changed. **PASS.**

## Spec, design, task, and static coherence

The current committed/OpenSpec proposal, spec, design, and tasks SHA-256 values match the frozen authorities: proposal `b532e6eb70a43fa658bbcb60ae8a7c47e8d46ce76ae35ad892fa60dbe08aab94`, spec `7cb520ffdb11a7e8862013febdf3c1faa3c306d4db25763fd542df6110eb1a7e`, design `6caada4ed924fb29e2798a6a4ca6aae31f41e04835035bd27b01819b08e40202`, and tasks Lore `b99910a8-c76d-4fe7-9830-fa648c68fede` / SHA `2b6c71a1ee38a53f6f65c03a316da574c3b7488d23f50ce834fb3b967a7cb1fd`.

The source is a shared-contract slice only, as task 4.2A requires: Request/Prepared/Workflow, Result/Event/Error, Observer, and LegacyAdapter. Static source review verified defensive copying of request components, transaction report, result slices, and event operation; `Prepared` copies share one `atomic.Bool` state and `consume` is one-shot; errors use the fixed redacted messages/logical paths; LegacyAdapter remains an interface seam. The only imports are `context` and `sync/atomic`. There are no effectful implementations, network/server/profile/journal/transaction authority code, or B gate/RoutePolicy declarations.

The final external AST/static verifier source and its recorded PASS evidence were independently inspected; it accepts type conversions and ObserverFunc dispatch, rejects dynamic/effect-authority fixtures, checks imports/calls/collisions/unresolved references/B symbols, requires a single base `cloneTransactionReport`, and requires only fixed redacted logical errors. Its final receipt reports corrected guards and self-fixtures PASS. The code bytes now reviewed are exactly the guarded candidate bytes. **PASS.**

## Immutable executable evidence

Lore receipt `fc97bd42-7cbe-44ab-a5d7-247c43cd1431` records gofmt zero diff, compile-only package PASS, and focused `TestPreparedAndObservableContractsDefensivelyCopy` PASS for the exact corrected six-file hashes/blobs above. Identity reconciliation is exact, so the receipt remains bound; it was not rerun per the explicit prohibition. The focused test statically contains the intended assertions for sealed defensive copies, shared one-shot state, event non-aliasing, and result/error stability. **PASS (immutable receipt, not fresh execution).**

## Failure-lineage reconciliation

Prior SIGPIPE, mapfile/heredoc/path/tree/scratch/AST-framing, and duplicate-helper outcomes are retained as immutable harness/candidate lineage. The duplicate helper was removed in corrected `result.go`; the corrected candidate’s six hashes, patch, payload, manifest, tree, and prior focused receipt match the commit. The final apply receipt `dg-93fd0211` / Lore `7a54d50d-2a2d-46cd-8d65-671cf1543c90` shows all corrected AST/static, NUL-safe cached staging, commit parent/tree/unsigned checks passed; only a formatted `%B` message framing assertion failed after commit. Direct raw-object inspection above independently proves the exact message bytes, so that framing failure does not invalidate this commit. **PASS.**

## Completeness and recommendation

Current tasks deliberately still show 4.2A, 4.2B, parent 4.2, 4.3–4.11, and global W4 unchecked. No task file was edited.

**Recommendation:** mark only child **4.2A** complete after governance accepts this report. Keep **4.2B**, parent **4.2**, **4.3–4.11**, and global **W4** unchecked.

## Persistence boundary

This complete report is persisted in Lore under this topic and mirrored in OpenSpec at `openspec/changes/canonical-capability-profile-compiler/verify-report-W4-4.2A.md`. Cross-store parity is now closed.
