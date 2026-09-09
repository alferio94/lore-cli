# Tasks: Roll Out Canonical Install Workflow

> Canonical task closure after independent scoped verification at `ec66ac4fdcf2e7a60706c406306d138500db6128`. Relative to the prior closure, **only task 7.1 is newly marked complete**. Authority: proposal `6deed242`, spec `5f78a65d`, design `47ebaa5b`, corrected tasks `486c2465-db5a-4098-aacb-4cef2c2f6cbc`; verify evidence `sdd/rollout-canonical-install-workflow/verify-report`.

## Phase 1: Branch Isolation + Releaseprofile Core
- [x] 1.1 Isolate the apply branch from merged `main56406db`; record clean-tree/base receipts and recommend commit `chore: isolate rollout-apply branch`. Cap: 0A/0D; files: none; validate `git status` + `git rev-parse`.
- [x] 1.2A Add `internal/releaseprofile/profile.go` + `profile_test.go` for schema/types/strict validation and all-off fallback. Cap: 220A/60D max, no transfer; tests: malformed/tampered/rollback-incompatible inputs.
- [x] 1.2B Add canonical JSON + digest helpers and authored fixtures under `internal/releaseprofile/testdata/`; generated profile JSON stays snapshot-accounted, not authored. Cap: 120A/40D max; tests: round-trip/golden parity.

## Phase 2: Build Embedding + Immutable Fixtures
- [x] 2.1 Embed base64+SHA-256 through the existing ldflags build path and resolve the immutable snapshot once at process start. Files: `.github/release-profiles/*.json`, `scripts/*`, `Makefile`, `internal/releaseprofile/*`. Cap: 260A/80D max; tests: bad-payload fail-closed, good-payload identity.
- [x] 2.2 Lock prerelease fixture immutability and authored/snapshot accounting; snapshots may regenerate, but never hand-edit. Cap: 80A/20D max; tests: fixture diff/golden.

## Phase 3: Single Install Route-Policy Integration
- [x] 3.1 Wire exactly one `install.NewRoutePolicy` adapter to the immutable snapshot; keep explicit legacy behavior, no automatic fallback or retirement. Cap: 220A/60D max; files: `internal/install/*`, `cmd/lore/*`; tests: gates-off and explicit-legacy cases.

## Phase 4: Safe Version/CLI/TUI Diagnostics
- [x] 4.1 Thread safe profile identity into `internal/version/*`, `internal/cli/*`, and `internal/tui/*` without changing route/result semantics; redact secrets, full paths, and user content. Cap: 280A/80D max; tests: human/JSON/TUI parity and redaction.

## Phase 5: Release Workflow + Evidence + Recovery
- [x] 5.1 Update release workflow/scripts for authenticated remote actions, checksums, attestations, and truthful OS-vs-target evidence. Cap: 240A/80D max; tests: workflow/evidence fixtures.
- [x] 5.2 Add demotion/recovery evidence for D/A; keep A blocked until recovery rehearsal proves foreign-content preservation and user restoration. Cap: 220A/60D max; tests: recovery/failure rehearsal.

## Phase 6: Operator Checkpoints + Promotions
- [x] 6.1 Stop prerelease publication until audience, window, owner, and target-order inputs are present. Cap: 120A/20D max; tests: missing-input stop.
- [x] 6.2 Encode the later promotion order E→D→A→stable with explicit remote authorization gates before each publication. Cap: 180A/40D max; tests: stage-order gate matrix.

## Phase 7: Verification + Legacy Guardrails
- [x] 7.1 Add focused tests for default-off, legacy explicit/no fallback, redaction, and promotion-block conditions. Cap: 200A/60D max; validate with targeted `go test` packages.
