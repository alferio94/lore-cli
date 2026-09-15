# Lore CLI Roadmap

## Current position

The canonical capability-profile compiler is integrated and archived. Rollout implementation is also integrated; release promotion remains pending. The global W4 aggregate accepted W4.1–W4.11; the retained R16 baseline is recorded separately. Canonical routes remain **GateOff** by default: disabled routes fail closed and do not fall back to legacy behavior.

This status was checked on September 15, 2026. It is an integration record, not a release authorization. The latest public release observed was `v0.4.0` (May 28); no new release or production gate activation is implied here.

## Evidence

- Archived aggregate: [`verify-report-W4-global-aggregate.md`](openspec/changes/archive/2026-09-02-canonical-capability-profile-compiler/canonical-capability-profile-compiler/verify-report-W4-global-aggregate.md)
- Archived change record: [`archive-report.md`](openspec/changes/archive/2026-09-02-canonical-capability-profile-compiler/canonical-capability-profile-compiler/archive-report.md)
- Main integration: PRs #5 and #7–#17; `main` commit `3802716534b8372a814498e444c91c25b5fa5edd`.
- Historical hosted CI: GitHub Actions [run 34414467694](https://github.com/alferio94/lore-cli/actions/runs/34414467694) succeeded for that exact main SHA, including full/race platform coverage, cross-build/link coverage, vet, and release guards. This is recorded hosted evidence, not fresh local verification or native execution of all targets.

## Remaining release promotion

Before any promotion, obtain explicit authorization and record the nonsecret operational inputs: audience, rollout window, responsible owner, and target order.

1. Confirm the accepted recovery and reconciliation evidence for the intended candidate.
2. Select an annotated semantic-version tag and release profile under the repository release policy.
3. Obtain explicit approval for the candidate, tag, and rollout plan.
4. Promote in order: **E → D → A → stable**.
5. At each stage, assess the accepted recovery evidence and stop or roll back according to the approved plan before advancing.

## Maintainer guardrails

- Keep canonical routes GateOff until an approved promotion explicitly changes the gate.
- Do not restore disabled-route legacy fallback.
- Do not treat archived or hosted evidence as a substitute for the authorization and operational evidence required for a new release.
- Use the primary repository with ordinary short-lived task branches; do not require backup or auxiliary worktrees for routine work.
