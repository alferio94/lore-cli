# Scoped Verification Report: W4.3 Server Scope Projection

## Verdict

PASS, scoped to W4.3 only. Commit `0a7a2dbba4a363a92800ef68f23347466109ddc1` is the exact authorized three-file candidate. Recommend marking only task 4.3 complete; leave 4.4-4.11 and global W4 unchecked. No Go/build/test/race/vet/CI/network/remote command was run in this verification.

## Scope and task state

- Change/task: `canonical-capability-profile-compiler` / 4.3.
- Authority worktree/branch: `/private/tmp/lore-cli-w4-observable-routing-ccfc222c` / `feat/w4-observable-routing`.
- The canonical ledger remains pending for 4.3 and 4.4-4.11; it was not modified.
- Scope is C1-C4 and D56-D62 at the projector/adapter seam. Explain, CLI, TUI, workflow execution, legacy execution, goldens, broad validation, and CI remain later work.

## Commit and state identity

| Check | Observed | Result |
|---|---|---|
| Tip / parent / tree | `0a7a2dbba4a363a92800ef68f23347466109ddc1` / `1133ff60688b0c0b6c874e83ca121ca51a6f4347` / `6c9d737cf969509e23c99808d86a13d36f25ba9d` | PASS |
| Branch / index / worktree | exact W4 branch; index tree equals HEAD tree; zero tracked or untracked status entries | PASS |
| Upstream / W4 remote ref | none / no `refs/remotes/origin/feat/w4-observable-routing` | PASS |
| Commit object | sole parent; LF-only exact logical message; no body, trailers, attribution, CR, or `gpgsig`; unsigned status `N` | PASS |
| Local branch lineage | normal commit entry after W4.2 closure; no amend, reset, rebase, merge, or push reflog entry | PASS |

## Candidate, patch, payload, and bytes

Full preflight authority was read from Lore `64487c01-b68b-427a-9e27-20afcad4ac39`; full candidate ledger was read from `98cd7b72-7aed-4380-a476-0ed1f7cc2902`.

| Check | Observed | Result |
|---|---|---|
| Full-index no-renames binary patch SHA-256 | `aaf1d9cd8a8012f1918b89ba0a555047fe677aa41f735b6b2f47621993d98e53` | PASS |
| Ordered payload SHA-256 | `2142ec6bc2accbfa4ccb9b13d3f0c1d0fa22a21be56c77dee2b5787512c3fd7d` | PASS |
| Result tree | `6c9d737cf969509e23c99808d86a13d36f25ba9d` | PASS |
| Delta and budget | `+142/-14`; authored `156/400` | PASS |
| Paths | exactly adapter.go, projector.go, projector_test.go; no denied path | PASS |

Payload was independently recomputed as ordered logical path plus NUL plus each committed raw result blob.

| Path | Mode | Blob | Bytes | SHA-256 |
|---|---:|---|---:|---|
| `internal/install/adapter.go` | `100644` | `838b62e94ce8d1512eb54db3d619d72918dc1970` | 5381 | `c1566300dcce7735f671165ff1e9dae4ff573c996cc74a940e092522bddff9d2` |
| `internal/install/projector.go` | `100644` | `74d6dc80784cab0e3d8d5e6cc9b9385f6c2182f5` | 17516 | `a704ac6bf7012c6d58d0f1660d8d3f92dfd92939e7e717c663d7909dff0408ca` |
| `internal/install/projector_test.go` | `100644` | `e986531deac9913a38b9ef0cd0c0b5952b61e809` | 15312 | `b9f38fcbbddbbe43bc78f46066f63b799e44d1b3f76c703aefa44ed462538fcc` |

## Validation evidence and guard lineage

Lore apply records `03a6e284-b60d-4037-8cba-f80a15a2e1e3` and `581d12b0-db47-46f1-ab88-779e5fa39531`, with apply receipt `dg-64502f9b`, record PASS for the frozen patch, gofmt-zero, package compile-only, and the four focused tests. Ledger hashes/blobs/modes/tree equal the independently read commit objects, so the receipts apply to these committed bytes. The tests are statically present as `TestW4ProjectSemanticPlanNormalizesFourTargetFacts`, `TestW4ProjectSemanticPlanValidatesOnlyExplicitServerScope`, `TestW3ProjectSemanticPlanDefensiveCopiesAndSecretRejection`, and `TestW33ATransactionRejectsEveryMismatchWithZeroOutputAndEffects`. They were not rerun by explicit prohibition.

Corrected guard `/private/tmp/lore-cli-w43-inline-guard.go` has SHA-256 `0157cda9db2777fadb43c212b64a40f432b9aaf47d780ca6edccd5010e8508e2`. Its direct form accepts exactly one worktree argument, parses all three files, and was recorded PASS. Its initial `--` usage failure is harness lineage only: `--` becomes an extra argv item and fails before source inspection. The guard meaningfully checks import inventories, no goroutines/effects, denied boundary calls/symbols, unique named types and fields, UUID and optional repository validation, fixed/redacted logical errors, four target adapters, agentpack guidance, defensive guidance copying, and projector-test declarations.

## Semantic compliance

| Scope | Evidence | Result |
|---|---|---|
| C1/D57 local-server separation | Local `compiler.ProjectID` remains in profile/persistence facts. Distinct `ServerProjectID`, `ServerProjectKey`, `ServerRepositoryID`, and `ServerScope` prevent conversion/substitution; local `project:alpha` is rejected as server UUID. | PASS |
| C2/D58 explicit repository UUID | Exactly one project UUID or key is required when scope is present; repository UUID is optional but must be an unpadded UUID. No path, remote, URL, title, metadata, or text inference exists. | PASS |
| C3/D62 authority/redaction | No server/repository model, HTTP client, authorization, binding, filtering, audit, or local authority was added. Errors are fixed/redacted and expose logical paths only. | PASS |
| C4 no transaction participation | `RenderRequest` carries only validated scope; no credential, transaction, profile, finalizer, manifest, or server call appears. | PASS |
| D56 targets | Pi, OpenCode, Codex, Antigravity each map to its own default adapter and focused test coverage. | PASS |
| D59-D61 guidance | Nonzero scope calls deterministic `agentpack.LoreMCPGuidance`: activity first, context or filter-only search, then scoped full get; no query-text, full-discovery-body, or generated-summary claim. | PASS |
| copies/secrets | Guidance and returned facts/intents are defensively copied; focused tests retain secret-rejection coverage. | PASS |

Scope is required only for `ComponentLoreServerMCP`; unrelated resources do not acquire fabricated server requirements. `RenderRequest.Validate` validates an explicit scope but does not require one outside an MCP-resource projection.

## No-effect and local-action review

Changed production imports are only standard pure packages plus existing `agentconfig`, `agentpack`, `compiler`, and `reconcile`. Direct inspection and guard evidence found no filesystem/process/network/RPC/database/credential resolver/server call/goroutine/channel send/transaction/profile/finalizer/manifest/CLI/TUI/Explain/Legacy path. New names occur once; no unresolved or colliding symbol was found.

No local evidence of unauthorized amend, reset, upstream attachment, W4 remote ref, push, PR, tag creation, or other remote action was found. This statement is limited to local commit objects, refs, reflogs, and worktree state; verification performed no remote action.

## Residual risk

No critical W4.3 issue. This is not global W4 acceptance: broad suite, race, vet, cross-platform/CI, presentation/execution paths, goldens, and remaining scenarios remain reserved for 4.4-4.11, especially 4.11.
