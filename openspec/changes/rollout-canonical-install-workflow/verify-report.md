# Aggregate Verification Report: rollout-canonical-install-workflow

## Verdict

**PASS WITH REMOTE-EXECUTION LIMITATIONS** at exact `ec66ac4fdcf2e7a60706c406306d138500db6128` from merged base `56406db776a4d89a5ed8753227fe7372942f5711`.

Standard verification; strict TDD was neither configured nor injected. Authority read in full: proposal `6deed242`, spec `5f78a65d` (8 requirements/11 scenarios), design `47ebaa5b`, corrected tasks `486c2465`, and final task closure `2ee5a2af`.

All 12 implementation tasks are closed. Local full/race/vet, six-target compile/link, release guard, snapshot, syntax, Make dry-runs, runtime identity, behavior-sensitive baseline, residue, and static-boundary checks pass. This is **PR-ready for review only**: it grants no archive, release, rollout, gate activation, hosted-CI success, dispatch, tag, or publication authority.

No code edit, commit, reset, clean/stash, push, PR, merge, tag, release, workflow dispatch, gate activation, or remote command occurred during verification.

## Reconciliation

The exact ordered chain is `2ebd47f` → `3ec6dad` → `236d7af` → `08bfbcc` → `8d00950` → `8cefc75` → `a01c49e` → `dec5a5c` → `b26d29c` → `ec66ac4`; every commit is parented to the previous commit, beginning at base.

| Closure | Commit | Diffstat | Cap/scope |
|---|---|---:|---|
| 1.1, 1.2A/B profile core/fixtures | `2ebd47f` | 223A/0D | PASS: combined cap 340A/100D. |
| 2.1 embedding | `3ec6dad` | 152A/1D | PASS: 260A/80D. |
| 2.2 fixture lock | `236d7af` | 33A/1D | PASS: 80A/20D. |
| 3.1 single route adapter | `08bfbcc` | 162A/2D | PASS: 220A/60D. |
| 4.1 safe diagnostics | `8d00950` | 257A/73D | PASS: 280A/80D. |
| 5.1 release evidence guard | `8cefc75` | 240A/77D | PASS: 240A/80D. |
| 5.2 recovery rehearsal | `a01c49e` | 185A/0D | PASS: 220A/60D. |
| 6.1 checkpoint | `dec5a5c` | 63A/2D | PASS: 120A/20D. |
| 6.2 promotion guard | `b26d29c` | 162A/3D | PASS: 180A/40D. |
| 7.1 guardrails | `ec66ac4` | 70A/10D | PASS: 200A/60D; six test paths only. |

`git diff --check` passed. The diff is limited to intended profile core, exactly one profile-to-`NewRoutePolicy` adapter, safe presentation, workflow/guards, recovery, and guardrail tests. There is no tip tag, release, or branch upstream. Only permitted untracked OpenSpec verification artifacts exist.

## Executed Evidence

Raw logs and exit statuses are retained under `openspec/changes/rollout-canonical-install-workflow/logs/`.

| Validation | Result | Evidence |
|---|---|---|
| `go test ./...` | PASS: 16 tested packages; command package has no tests. | `go-test-all-hermetic.*` |
| `go test -race ./...` | PASS: no race reports. | `go-test-race-all-hermetic.*` |
| `go vet ./...` | PASS: exit 0, no diagnostics. | `go-vet-all-hermetic.*` |
| Focused profile/route/presentation/recovery aggregate | PASS. | `release-profile-focused-and-syntax.log` |
| Local release workflow guard | PASS: E→D→A→stable and negative checkpoint/evidence/order/recovery/demotion/redaction cases. | same |
| Six supported cross builds | PASS: darwin amd64/arm64, linux amd64/arm64, windows amd64/arm64; compile/link plus static profile embedding. | `cross-build-six-targets.log` |
| Snapshot / Make dry-runs / YAML+Bash grammar | PASS: `make verify-release-profile-snapshots`, non-executed Make dry-runs, Ruby YAML parse, `sh -n`/`bash -n`. | `release-profile-focused-and-syntax.log` |
| Native profile identity and baseline behavior tests | PASS. | `runtime-baseline-and-static-audit.log`, `unprofiled-and-residue-final.log` |

All Go commands used scratch HOME/GOPATH/GOCACHE, read-only preexisting module cache, and `GOPROXY=off GOSUMDB=off`. A first literal-HOME-unset attempt failed before tests because Go had no module cache; retained as `go-test-all.log`. The succeeding hermetic tests passed and did not write user home/config.

Cross-built binaries were not executed. Host `darwin/arm64` runtime identity was exercised; other targets are truthfully compile/link-only.

## Requirements and Scenario Trace

| Requirement/scenario | Passing evidence | Result |
|---|---|---|
| R1 Validation | `TestProfileCore`; malformed base64/digest/provenance/rollback fail closed. | ✅ |
| R2 Matrix | `TestTask71DefaultOffFailClosedAndOpenCodeERequiresBuildSelection`; prerelease snapshot. Unprofiled native binary is all-off; explicit selected fixture is OpenCode=E only. | ✅ |
| R3 Skip | Workflow guard rejects absent, failed, stale, reordered, lineage-invalid predecessor evidence and reused authorization; accepts only E→D→A→stable. | ✅ |
| R3 Demotion | `TestTask52ReleaseProfileDemotionBlocksFutureDAndAWithoutUndo`; guard rejects `admission_blocked` and `undo_claimed`. | ✅ |
| R4 Parity | Version human/JSON parity, CLI/TUI W410 goldens, selected runtime JSON. | ✅ |
| R4 MCP secret | Version/CLI/TUI redaction plus unsafe bearer/header/path guard fixture yield stable redacted diagnostics. | ✅ |
| R5 Disabled route | Release-profile route tests and existing explicit-legacy/no-fallback test. | ✅ |
| R6 Incomplete canary | Guard rejects missing/invalid audience, window, owner, order, stage, evidence, authorization, and false authorization before effects. | ✅ |
| R7 D effect | Demotion/admission guard tests and full install dry-run/transaction suite. | ✅ |
| R7 A recovery | Recovery rehearsal and guard reject missing/failed recovery receipt, transaction recovery, restoration, or foreign-content preservation. | ✅ |
| R8 Red baseline | Local full/race pass; workflow runs both before build/publish and guard rejects `continue-on-error`. Hosted CI/R16 receipt intentionally not run. | ⚠️ PARTIAL |

**Summary: 10/11 fully runtime-compliant locally; R8 remains partial solely because local execution cannot truthfully assert hosted CI.**

## Coherence and Safety

- Strict canonical JSON, base64+SHA-256 ldflags, and one startup snapshot are implemented; missing/invalid/tampered data is all-off.
- `NewRoutePolicyFromReleaseProfile` is the only snapshot adapter; existing `NewRoutePolicy` remains route authority. Explicit legacy behavior remains available and never auto-falls back.
- Version/CLI/TUI expose only safe identity, digest, provenance, and gates; embedded payload is excluded.
- Workflow is dispatch-only and default authorization is false. Checkpoint/promotion guards precede fetch/build/attest/release; checksums and build provenance attestation are present. No `push` trigger exists.
- Checked test/golden sources have no profile/golden write/remove API. Temporary profiles/builds used scratch roots and were removed; post-validation profile/testdata diff is empty.
- Changed profile/route/presentation code adds no HOME lookup, HTTP client, local-config write, or direct network authority. Static matches for headers/tokens/paths are redaction predicates, guarded workflow wiring, or deliberately unsafe negative fixtures; no actual secret was printed.

## Limitations and Next Steps

1. Hosted CI and R16/remote evidence are deliberately unexecuted; local pass is not remote approval.
2. ShellCheck and `pwsh` are unavailable locally; neither result is claimed.
3. Linux/Windows native runtime/integration remain release-environment work; six-target results are compile/link/static embed only.

**Required next steps, not performed:** open/review the PR and obtain required hosted CI; then, only under separate authority, supply non-secret audience/window/owner/order/evidence/authorization, exact annotated tag/profile, and explicit release authorization. Do not activate E/D/A/stable or publish until guarded target-specific evidence passes. A additionally requires the accepted recovery/reconciliation receipt. Do not archive based on this local verification alone.
