# Design: Canonical Capability Profile Compiler — W4 Observable Routing

## Authority and approach

Supersedes design `a6e719f1-4f73-470c-a004-b973a138edf7` (SHA-256 `36e17e8e0bda5629143fe0524dd4bd25eaed3b8b78abb9b19f66d844aee4f3ec`) under recovered proposal `370522aa-580d-4906-ad8d-dac6f8f41e69` (`b532e6eb…ab94`) and resolves the design details requested by spec needs-input `4722fcd3-c100-4339-8987-76da7c555765`. Accepted W1–W3.3/C1–C46 and the shared Workflow/Prepared/Result/Event architecture remain unchanged; W4 is still pending specification/tasks/apply.

```text
CLI parser ─┐
            ├─> Workflow.Prepare ─> compiler.Explain/Compile ─> adapters ─> sealed Prepared
TUI Cmd  ───┘                                                        │
 human/json/TUI presenters <── ordered Events + Result <── Workflow.Execute ─> accepted W3.3 transaction
```

`runInstall → installActionWithOptions → Plan*Install → Execute*Install` is replaced at the presentation boundary, not duplicated. One injected `install.Workflow` owns admission, preparation, execution, and `RoutePolicy`; target `Plan*/Execute*` paths remain only behind `LegacyAdapter`. Existing compiler, projector, `SealTransactionPlan`, transaction journal/authority, hosted-MCP finalizer, and profile-store authority remain the sole canonical seams.

## Decisions and interfaces

| Decision | Rejected | Why |
|---|---|---|
| Opaque, sealed, single-consumption `Prepared` | reconstruct/reuse after confirmation or retry | Prevents drift and substitution. |
| Typed `Result`/ordered `Event`; separate presenters | presenter-owned domain state | Gives CLI/TUI parity and centralized redaction. |
| Per-target policy with route markers | global gate or fallback | Supports observable, reversible rollout. |

```go
type Mode string // explain, dry-run, apply, legacy-dry-run, legacy-apply
type Workflow interface {
  Prepare(context.Context, Request) (Prepared, Result)
  Execute(context.Context, Prepared, Observer) Result
}
type Result struct { SchemaVersion int; Mode Mode; Route string; Status Status; Report TransactionReport; Operations []Operation; Guidance []Guidance; Rollback RollbackResult; Error *InstallError }
type Event struct { Seq uint64; Phase Phase; Kind EventKind; Progress Progress; Operation *Operation; Error *InstallError }
```

Collections stay copied/ordered and observers cannot affect execution. Errors expose fixed codes and logical/redacted paths only. Server identity/authorization remains server-owned; explain performs no network or hosted effect. Credential resolution occurs once after sealing and only during apply. Target authority, rollback, v3-manifest-last/v2 preservation, Unix no-follow/mode/flock, and Windows reparse/ACL/mutex/owner-thread rules are unchanged.

## CLI and presentation matrix

| Invocation | Mode |
|---|---|
| `install --explain`; `install --dry-run`; `install` | canonical explain; dry-run; apply |
| `install --legacy --dry-run`; `install --legacy` | legacy dry-run; apply |

`--explain` conflicts with `--dry-run`, `--legacy`, and `--yes`. `--yes` with either dry-run is usage error; it is valid only for canonical/legacy apply. Apply prompts on a TTY unless `--yes`; non-TTY apply without it returns typed `confirmation_required` and never reads stdin.

All modes accept `--format human|json` (human default). Human final success/reports use stdout; progress, warnings, deprecation, and errors use stderr. JSON writes exactly one `{"schema_version":1,"result":...}` object to stdout, with no ANSI, spinner, progress, or chatter; admitted domain/apply failures are structured there. Parser, unknown-flag, combination, and invalid-format errors use stderr only.

Exit precedence is: residual risk **3**; otherwise usage **2**; signal interruption **130**; domain/non-admission/disabled/confirmation-required or failure with complete rollback **1**; success or voluntary pre-execution decline/cancel **0**. A signal before authority yields 130; after authority it cancels execution, blocks return, waits for rollback/result, then yields 130 only if restoration is complete, otherwise 3.

## TUI sequencing

TUI is a separate presenter over the same typed data and rejects non-TTY launch with usage guidance. Back/Esc before `Execute` discards `Prepared` with zero effects. During execution, cancel requests context cancellation while navigation remains blocked until rollback/result. Retry always re-Prepares; residual risk disables retry. Resize only reflows the view. Phase and numeric/text progress are always rendered; animation is decorative, and `LORE_NO_ANIMATION=1` disables it.

## Files, rollout, and verification

| Boundary | Files |
|---|---|
| Workflow/policy/legacy | create `internal/install/workflow_{contract,prepare,execute,legacy}.go`; extend `projector.go`, `hosted_mcp_finalizer.go`, `transaction*.go`, `profile_store*.go`, four adapters |
| Presenters | modify `internal/cli/{app,actions}.go`; create `internal/cli/install_presenter.go`; split `internal/tui/install_{model,update,view,cmd}.go`; extend `internal/output/` |

Each Pi/OpenCode/Codex/Antigravity target advances **E→D→A**: E enables explain; D adds dry-run; A adds apply and alone permits canonical default. Disabled modes return `canonical_route_disabled`, never legacy fallback. Results/tests expose `canonical-sealed` or `explicit-legacy`. Emergency demotion is A→D→E→off; legacy stays explicit.

Legacy retirement requires all four targets at A, compatibility/golden parity, no unresolved rollback regressions, adoption review, and at least one published deprecation version plus 30 days. A later separately reviewed compatibility change removes the flag, TUI choice, adapter, and old callers.

Verification uses table tests, mutation/credential/network spies, exact flag/TTY/format/exit matrices, JSON/human goldens and sensitive-output scans, four-target route/parity tests, rollback/signal/no-residue tests, deterministic model/teatest cancel/back/retry/resize/reduced-motion tests, race/vet/full suite, and native/cross-platform CI. Stack review slices by contracts, explain, dry-run, apply, presenters, adapters, and goldens; each keeps tests with behavior, is ≤400 authored lines, independently reversible, and names prior/follow-up dependencies.
