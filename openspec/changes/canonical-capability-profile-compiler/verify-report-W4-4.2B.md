# Verification Report: Canonical Capability Profile Compiler — W4.2B

## Scope and mode
- Change: `canonical-capability-profile-compiler`; bounded child task: `4.2B` only.
- Mode: standard, evidence-reconciliation verification. No Go command was rerun, no Git/task/worktree file was changed, and no network or remote action was performed.
- Authorities read: canonical tasks Lore `147c4a5d-f31f-4e00-836a-524940a307c7` (SHA-256 `6e7497261d71f80fad3807066bfb4748e1f5dafff3cbd987913573b321fb8f63`, equal to committed OpenSpec tasks); candidate Lore `21003b51-ed5e-4919-836a-8beb84a9e52f`; prior executable receipt `32da667b-ded3-47e0-a26c-bca0cb270f23`; and the supplied final post-governance authority `dg-f98e1bcb`. Failed auxiliary-harness lineage was retained as history and was not treated as product evidence.

## Commit and repository identity — PASS
| Check | Observed result |
|---|---|
| Worktree/branch/tip | `/private/tmp/lore-cli-w4-observable-routing-ccfc222c`, `feat/w4-observable-routing`, `ceaf414d7ace8b7c7c941a63758377e3fd61b968` |
| Parent/tree | `981c329a5730baf9587d8bebacc875a427ae3285` / `edc92346ff2e5b0adb791d2f8253de07f0c7a0ba` |
| Raw object | exactly one parent; no `gpgsig`, `sshsig`, trailers, or AI/Co-Authored-By attribution |
| Message | raw bytes are `feat(install): add W4 route policy` followed by exactly one LF |
| Delta | exactly two new `100644` paths: `internal/install/route_policy.go` and `internal/install/route_policy_test.go`; `+150/-0` (`89+61`), within the immutable 150/150 cap |
| Blobs | production `ee3b66099aa2cb65de893b8f66a7f422767b81fd`; test `19564369a9f355a78b017097e52590c166a5750c` |
| Candidate parity | both committed blobs equal candidate `21003b51…`; commit patch SHA-256 is exactly `bbf373c565800854081758a7440f86870c396bd62b530c4dfe64ed0ac0ec3908`; candidate payload/delta/result manifest evidence is unchanged (`32b5b36c…`, `ece072e0…`, `5f9d3ec5…`) |
| Current state | index delta, worktree tracked delta, and untracked list are empty; policy paths equal the committed tree |
| Publication boundary | branch has no upstream; no remote ref or tag contains candidate; only local ref/reflog evidence was read; no live network was used. This supports no local evidence of push/tag/remote/PR mutation, not a claim about inaccessible remote systems. |

## Contract, design, and task compliance — PASS
The committed OpenSpec proposal/spec/design bind W4.2B to independent four-target E/D/A gating, disabled canonical no-fallback behavior, explicit legacy routing, target-local reversible demotion, fixed/redacted errors, and a pure policy layer. The canonical task remains a zero-headroom two-file child and binds the same candidate. Parent `4.2`, `4.3`–`4.11`, and global W4 remain unchecked.

Static source inspection of the exact production blob confirms:
- `NewRoutePolicy` requires precisely the supported target set, rejects invalid/missing gates with `invalid_route_policy`, and copies caller map data.
- `Decide` gates every target independently: E admits explain, D admits explain/dry-run, A admits all canonical modes, off admits none. Disabled canonical decisions retain `RouteCanonical`, set `Admitted=false`, and return only `canonical_route_disabled`; there is no automatic legacy route.
- Only explicit `ModeLegacyDryRun`/`ModeLegacyApply` select `RouteLegacy`; no `LegacyAdapter`, `PrepareLegacy`, or `ExecuteLegacy` reference/invocation exists in either added file.
- `Demote` rejects unknown/invalid/non-decreasing gates, creates a fresh map, copies all source entries, changes only `next.gates[target]`, records a fixed `emergency-policy` demotion, and returns a fresh defensive `Demotions()` slice. It neither mutates the receiver map nor promotes a target.
- The added production file has no imports and only pure policy operations. It has no effect/network/server/profile/journal/transaction/finalizer/credential/publish/rollback surface or execution. Existing errors are exact fixed redacted mappings: `route_policy`, `route_policy.gate`, and `request`, without dynamic formatting.
- New declaration names are absent from the parent; full-package static inspection and the prior compile receipt establish resolved symbols/no collisions.

## Test and executable evidence reconciliation — PASS
No Go command was rerun by instruction. Receipt `32da667b…` ran against candidate bytes before staging and recorded: gofmt zero output, package compile-only PASS, and focused PASS for exactly `TestRoutePolicyEnforcesPerTargetCanonicalGates`, `TestRoutePolicyNeverFallsBackAndLegacyMustBeExplicit`, and `TestRoutePolicyDemotionIsTargetLocalAndRecorded`. Exact candidate-to-commit blob parity makes that evidence applicable to `ceaf414`.

| Scoped scenario | Meaningful test evidence | Result |
|---|---|---|
| D44–D47 / E-D-A-per-target matrix | `TestRoutePolicyEnforcesPerTargetCanonicalGates` iterates every supported target, four gates, and three canonical modes; asserts route, admission, and disabled error | PASS (reconciled) |
| D48 / no fallback and explicit legacy | `TestRoutePolicyNeverFallsBackAndLegacyMustBeExplicit` asserts disabled canonical apply remains canonical/non-admitted with `canonical_route_disabled`, while both explicit legacy modes admit `RouteLegacy` | PASS (reconciled) |
| D49 / target-local demotion | `TestRoutePolicyDemotionIsTargetLocalAndRecorded` asserts original isolation, Pi-only demotion, Codex unchanged, exact demotion record, and promotion rejection | PASS (reconciled) |

The tests meaningfully cover the task's route matrix, no-fallback/explicit-legacy behavior, and target-local demotion. Defensive caller-map and returned-demotion-slice copying are additionally confirmed by direct exact-blob source inspection; the focused tests do not mutate those returned/input collections.

## Corrected inline AST/static verifier — PASS, byte-reconciled
The supplied final verifier `/private/tmp/lore-cli-w42b-inline-guard.go` was read, not run. Its recorded PASS immediately before staging applies to the committed bytes because the candidate blobs and current committed blobs are identical. Source review confirms that it: (1) recognizes an AST array type conversion, therefore admits `append([]RouteDemotion(nil), ...)`; (2) rejects unapproved/dynamic call names and concurrency/control nodes, while only allowing the policy's finite pure call set; (3) rejects legacy identifiers and forbidden effect/network/authority substrings; (4) requires the fixed routing/demotion/error invariant strings and redacted error mappings; (5) requires a fresh map, exactly one loop-copy write `next.gates[id] = gate`, exactly one target write `next.gates[target] = to`, and zero receiver-map writes; and (6) parses all package files to reject declaration collisions and confirm required symbols. The earlier `dynamic call expression is forbidden` checker failure and subsequent auxiliary checker failures were harness defects/fail-stops before staging, not product failures; they remain immutable lineage only.

## Completeness and coherence
- Tasks: W4.2B implementation is complete and verified by this bounded report; parent 4.2 intentionally remains incomplete pending aggregate verification.
- Design coherence: the pure route-policy slice implements the design's per-target matrix and explicit legacy marker separation without prematurely wiring workflow, CLI/TUI, authority, or execution seams.
- Out of scope: no claim is made that future W4 routing/explain/dry-run/apply/presenter/CI scenarios are implemented or accepted.

## Issues
- Critical: none in the bounded W4.2B slice.
- Warning: no new concern. This report intentionally does not rerun Go commands; its runtime evidence is the exact-byte-reconciled prior receipt.
- Persistence note: the governing hybrid convention normally requires byte-identical Lore/OpenSpec copies. The explicit no-file-mutation prohibition prevents creating or changing an OpenSpec verify-report mirror in this run; this full Lore artifact is the authorized persisted record, and no filesystem parity claim is made.

## Verdict
**PASS.** Recommend marking only child **4.2B** complete. Keep parent **4.2** unchecked pending aggregate 4.2 verification; keep **4.3–4.11** and global W4 unchecked.