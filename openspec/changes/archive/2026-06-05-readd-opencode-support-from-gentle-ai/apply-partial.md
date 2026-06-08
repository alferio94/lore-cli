# Apply Partial: readd-opencode-support-from-gentle-ai (cleanup slice)

## Completed in This Slice
- [x] A. Fixed `internal/cli/app.go` install usage short target list line — `Usage: lore install [--dry-run] [--yes] [--target pi|codex|antigravity] [--component <id>]` → `Usage: lore install [--dry-run] [--yes] [--target pi|opencode|codex|antigravity] [--component <id>]`. Now consistent with the accepted runtime behavior (Pi, OpenCode, Codex, Antigravity are all routable install targets) and the longer help copy that already documents OpenCode in detail.
- [x] B. Updated the `--target` flag description in `internal/cli/app.go` to mention OpenCode — `Install target (Pi stays the default recommended target; OpenCode, Codex, and Antigravity are supported managed targets)`. Updated the `--component` flag description to include OpenCode in the supported target set AND to call out that OpenCode also supports `opencode-plugins` — `Optional component override; repeat or use a comma-separated list (Pi, OpenCode, Codex, and Antigravity support core-pack; Pi/Codex/Antigravity also support lore-server-mcp; OpenCode also supports opencode-plugins)`.
- [x] C. Renamed stale `TestInstallCommandRejectsOpenCodeTarget` → `TestInstallCommandAcceptsOpenCodeTarget` in `internal/cli/install_flags_test.go`. Refreshed the docstring to match the new semantics (OpenCode is supported again; the bounded foundation slice is exercised). Renamed the focused test token from `secret-token=opencode-rejected` → `secret-token=opencode-supported` so the test surface no longer implies rejection. Test body assertions (dry-run summary tokens, defensive negatives for the local plugin .ts files NOT in `tui.json`, no-on-disk side-effects checks) were already updated by the prior 3.3 slice and are unchanged.
- [x] D. Added focused assertions to `TestInstallUsageIncludesTargetAndComponentFlags` in `internal/cli/install_flags_test.go` that lock the cleanup-slice fix:
  - Positive: install usage line `Usage: lore install [--dry-run] [--yes] [--target pi|opencode|codex|antigravity] [--component <id>]` appears in `--help` stderr.
  - Positive: `--target` flag description `Pi stays the default recommended target; OpenCode, Codex, and Antigravity are supported managed targets` appears in `--help` stderr.
  - Positive: `--component` flag description `Pi, OpenCode, Codex, and Antigravity support core-pack; Pi/Codex/Antigravity also support lore-server-mcp; OpenCode also supports opencode-plugins` appears in `--help` stderr.
  - Defensive negative: the install usage short target list must NOT regress to `[--target pi|codex|antigravity]` (the verify-report warning list specifically called out `pi|codex|antigravity` as stale copy; this assertion locks the regression out of the test surface).

## Files Changed So Far
| File | Action | Notes |
|------|--------|-------|
| `internal/cli/app.go` | Modified | 3-line copy polish in `runInstall`: `--target` flag description, `--component` flag description, and the install usage short target list line. All other help paragraphs (the longer OpenCode help text, Antigravity help text, Codex help text, the Ownership contract paragraph, etc.) were already updated by the prior 3.3 slice and are unchanged. |
| `internal/cli/install_flags_test.go` | Modified | Renamed `TestInstallCommandRejectsOpenCodeTarget` → `TestInstallCommandAcceptsOpenCodeTarget`; refreshed the docstring; renamed the focused test token from `secret-token=opencode-rejected` → `secret-token=opencode-supported`. Extended `TestInstallUsageIncludesTargetAndComponentFlags` with 3 new positive assertions (lock the new copy) plus 1 defensive negative assertion (lock out the regression to `pi|codex|antigravity`). |
| `openspec/changes/readd-opencode-support-from-gentle-ai/apply-started.md` | Created | Slice definition + preconditions for this cleanup slice. |
| `openspec/changes/readd-opencode-support-from-gentle-ai/apply-partial.md` | Created | This file. |
| `openspec/changes/readd-opencode-support-from-gentle-ai/apply-progress.md` | Created | Cumulative progress artifact (this slice + the prior 3.3 / 3.4 slices already on disk). |
| `openspec/changes/readd-opencode-support-from-gentle-ai/apply-report.md` | Created | Slice report for this cleanup slice. |

## Validation So Far
- `go build ./internal/cli ./internal/install ./internal/tui` → passed (clean, no output).
- `go vet ./internal/cli ./internal/install ./internal/tui` → passed (clean, no output).
- `go test ./internal/cli -run 'TestInstallCommand|TestInstallUsage' -count=1 -v` → passed. All focused tests green, including:
  - `TestInstallCommandAcceptsOpenCodeTarget` (renamed, with new `secret-token=opencode-supported` token name) → PASS.
  - `TestInstallUsageIncludesTargetAndComponentFlags` (with 3 new positive assertions + 1 defensive negative assertion locking the install usage short target list) → PASS.
  - `TestInstallCommandRejectsUnsupportedInstallTarget`, `TestInstallCommandSupportsAntigravityDryRunAndApply`, `TestInstallCommandAcceptsLoreServerMCPWithPiTarget`, `TestInstallCommandDryRunAcceptsExplicitPiTargetAndComponents`, plus all other Pi install command tests → PASS.
- `go test ./internal/cli ./internal/install ./internal/tui -count=1` → passed. All three packages green:
  - `internal/cli` → ok (1.835s).
  - `internal/install` → ok (0.931s).
  - `internal/tui` → ok (2.300s).

## Remaining in Current Slice
- None. The bounded slice is complete. No follow-up apply slices are required for this change.

## Recovery Notes
- Safe resume point: the bounded slice is complete. The next agent (or the orchestrator) should re-run `go test ./internal/cli ./internal/install ./internal/tui -count=1` to confirm a clean green, then re-delegate to `sdd-verify` to upgrade the verify report from `PASS WITH WARNINGS` to `PASS`. No new code paths were added; the slice is purely copy/test-name polish.
- Known risks/blockers: None. The only risk category was "regress the install usage short target list" and the defensive negative assertion in `TestInstallUsageIncludesTargetAndComponentFlags` locks that regression out of the test surface. The other risk category was "re-introduce the legacy `RejectsOpenCodeTarget` test name" — the test rename is bounded to a single test function and the prior 3.3 slice's defensive negative assertions in the test body remain in place.
- Lore memory note: this session has no `lore_*` MCP tools and `lore status` reports auth failure. Per the verify-report protocol and the launch-prompt fallback policy, all apply artifacts for this slice are persisted to the OpenSpec fallback store at `openspec/changes/readd-opencode-support-from-gentle-ai/`. If Lore memory becomes available, the next slice (or the next `sdd-archive` run) should sync these artifacts back into Lore under `sdd/readd-opencode-support-from-gentle-ai/apply-{started,partial,progress,report}` topic keys.
