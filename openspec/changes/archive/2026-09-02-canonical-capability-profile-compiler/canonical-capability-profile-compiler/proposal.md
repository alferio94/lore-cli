# Proposal: Canonical Capability Profile Compiler — W4 Observable Routing

## Authority and Intent

Supersedes `83017d07-8bbc-4b1b-b65c-17fc63a44755`, preserving baseline `0c395cd8-ac3d-47ae-a8f8-194c4add6f48` (SHA-256 `0996cda4cc7ccce045cf62bd46f6e26146e704ac8f2165c435a91520c27530d3`); links needs-input `sdd/canonical-capability-profile-compiler/spec-W4-observable-routing-needs-input` (`4722fcd3-c100-4339-8987-76da7c555765`) and current W4 design `sdd/canonical-capability-profile-compiler/design` (`a6e719f1-4f73-470c-a004-b973a138edf7`; normalize later). Retains accepted W1–W3.3 intent/lineage; W3.3 complete, W4 pending. Spec unchanged: 16 requirements/123 scenarios.

## Scope and CLI Policy

Shared typed Result/Event workflow; CLI/TUI render it:

| Invocation | Route |
|---|---|
| `install --explain` / `--dry-run` / `install` | canonical W3.3 explain / dry-run / apply |
| `install --legacy` / `--legacy --dry-run` | legacy apply / dry-run |

`--explain` conflicts with `--dry-run`, `--legacy`, and `--yes`; `--yes` with dry-run is usage error and valid only for canonical/legacy apply. Interactive apply prompts; non-TTY without `--yes` is `confirmation_required`, never stdin.

## Capability

Modified `canonical-capability-profile-compiler`: W4 policy; new none.

## Output and Exit Policy

Modes accept `--format human|json` (human default). Human success/final reports go to stdout; progress/warnings/deprecation/errors go to stderr. JSON emits exactly one versioned final object on stdout, no ANSI/spinner/progress; admitted domain/apply failures emit structured JSON. Parser/unknown-flag/invalid-format: stderr/2.

| Exit | Meaning |
|---|---|
| 0 | success or voluntary pre-execution decline/cancel |
| 1 | non-admission, disabled route, confirmation required, or failure with complete rollback |
| 2 | usage/flag/format error |
| 3 | residual risk, overriding any primary outcome |
| 130 | signal interruption; after authority, only after complete rollback; residual risk is 3 |

## TUI, Cancellation, and Rollback

TUI shares typed semantics, not `--format`; it requires a TTY or returns usage guidance. Back/Esc pre-Execute discards Prepared with zero effects. Execute cancellation waits for rollback/result; navigation stays blocked. Retry always re-Prepares, never reuses/reconstructs Prepared; residual risk disables retry. Resize only reflows; text phase/progress always exists; animation is decorative and `LORE_NO_ANIMATION=1` disables it.

## Rollout, Compatibility, and Boundaries

Per-target gates: E (explain), D (explain+dry-run), A (explain+dry-run+apply). Disabled canonical modes fail `canonical_route_disabled`, never fall back. Per-target emergency policy may demote A→D→E→off; legacy remains explicit. Canonical default needs A; E/D are gates, never silent default switches. Legacy removal is a separately reviewed compatibility change, never automatic, after all four targets A, compatibility/golden parity, no rollback regressions, adoption review, and a published-version plus 30-day deprecation window; it deletes flag/TUI choice/adapter/old callers.

In: routing/output/cancellation/parity/redaction/staged acceptance. Out: spec scenarios, design, tasks, source/tests/workflows/Git/CI, server/API, v2 migration, or a second transaction.

## Risks, Rollback, and Success

| Risk | Mitigation |
|---|---|
| Unsafe route or interruption | gates; authority-aware rollback; residual-risk precedence |
| Output/secret drift | typed results; JSON/human/TUI goldens |
| Legacy persistence | explicit opt-in and retirement evidence |

Rollback: gate demotion, no legacy fallback; preserve W3.3 semantics. Success: matrix-conformant CLI/TUI parity, no prohibited disclosure/effects, and focused/cross-platform/golden/accessibility/noninteractive/full-suite/CI evidence before acceptance.
