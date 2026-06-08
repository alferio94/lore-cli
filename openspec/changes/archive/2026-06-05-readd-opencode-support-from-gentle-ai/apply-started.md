# Apply Started: readd-opencode-support-from-gentle-ai (cleanup slice)

## Slice
- Tasks in scope: copy/test-name polish only (no behavior changes)
  - A. Fix `internal/cli/app.go` install usage short target list line to include `opencode` consistently with the accepted behavior documented in the longer help copy.
  - B. Update the `--target` and `--component` flag description in `internal/cli/app.go` so the help-text target set matches the runtime-accepted target set (Pi, OpenCode, Codex, Antigravity).
  - C. Rename stale `TestInstallCommandRejectsOpenCodeTarget` in `internal/cli/install_flags_test.go` → `TestInstallCommandAcceptsOpenCodeTarget` and refresh the docstring + token (`secret-token=opencode-rejected` → `secret-token=opencode-supported`) so the test name and the focused assertion no longer imply that OpenCode is rejected.
  - D. Add focused assertions to `TestInstallUsageIncludesTargetAndComponentFlags` in `internal/cli/install_flags_test.go` that the install usage short target list now contains `opencode` AND that the `--target` flag description surfaces `OpenCode` as a supported target (consistent with the longer help copy).
- Tasks explicitly out of scope:
  - Any plugin/MCP runtime behavior change (the bounded `mcp.lore` ownership check, additive merge, backup-before-overwrite, idempotency, `opencode-plugins` default component, `sdd-engram` / `logo` exclusion list, and the `mcp.lore` ownership marker are all locked by prior slices and verified PASS WITH WARNINGS).
  - Any TUI menu / target-selection copy change (TUI already surfaces OpenCode and the bounded bundle correctly per the prior 3.x slice).
  - Any `README.md` wording change (the README already documents OpenCode as supported and is consistent with the bounded behavior).
  - Any new assets, new test files, or any new rendering path.
- Expected files (3):
  - `internal/cli/app.go`
  - `internal/cli/install_flags_test.go`
  - `openspec/changes/readd-opencode-support-from-gentle-ai/apply-{started,partial,progress,report}.md` (this slice's persistence artifacts)
- Validation planned (focused, per slice):
  - `go build ./internal/cli ./internal/install ./internal/tui` (fast, no external deps)
  - `go vet ./internal/cli ./internal/install ./internal/tui` (fast)
  - `go test ./internal/cli -run 'TestInstallCommand|TestInstallUsage' -count=1` (focused on the renamed test + the extended usage assertion)
  - `go test ./internal/cli ./internal/install ./internal/tui -count=1` (the full targeted slice per the orchestrator's explicit validation command)
- Risk budget: LOW — pure help-copy / test-name polish; no behavior, no new code paths, no new assets.

## Preconditions
- Proposal/spec/design/tasks context: read in full from the OpenSpec artifacts under `openspec/changes/readd-opencode-support-from-gentle-ai/` (apply-progress, apply-report, apply-partial, tasks, verify-report). No Lore memory artifacts are loadable in this session (no `lore_*` MCP tools exposed; `lore status` reports auth failure) so OpenSpec fallback is used per the verify-report protocol.
- Previous apply-progress merged: yes — prior 3.3 + 3.4 slices are complete (10/14 tasks). The two outstanding tasks (4.1, 4.2) are focused-verify tasks delegated to `sdd-verify`. This cleanup slice is an out-of-band copy/test-name polish that came out of the verify-report warning list.
- Strict TDD mode: not active for this change (per prior apply-progress).
- Verify report PASS WITH WARNINGS explicitly named two stale-copy issues (install usage short target list missing `opencode`; legacy `TestInstallCommandRejectsOpenCodeTarget` test name) — this slice resolves both, leaving the verify report at PASS.
