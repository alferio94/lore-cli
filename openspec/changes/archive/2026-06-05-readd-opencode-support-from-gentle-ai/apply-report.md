# Apply Report: readd-opencode-support-from-gentle-ai

## Latest Slice Result
- Status: completed
- Tasks attempted: cleanup A (install usage short target list line), cleanup B (`--target` and `--component` flag descriptions), cleanup C (rename `TestInstallCommandRejectsOpenCodeTarget` → `TestInstallCommandAcceptsOpenCodeTarget` + refresh docstring + rename focused test token from `secret-token=opencode-rejected` to `secret-token=opencode-supported`), cleanup D (3 new positive assertions + 1 defensive negative assertion in `TestInstallUsageIncludesTargetAndComponentFlags`)
- Tasks completed: A, B, C, D
- Tasks remaining (from the original change): 4.1, 4.2 (focused-verify tasks delegated to `sdd-verify`). No further apply slices are required — this cleanup slice resolves the verify-report PASS WITH WARNINGS warnings and the bounded surface is in its final state.

## Repository State Summary
- Files changed (this slice, copy/test-name polish only):
  - `internal/cli/app.go` — 3 lines: `--target` flag description, `--component` flag description, and the install usage short target list line in the `runInstall` `--help` text. All other help paragraphs (the longer OpenCode help text, Antigravity help text, Codex help text, the Ownership contract paragraph, etc.) were already updated by the prior 3.3 slice and are unchanged.
  - `internal/cli/install_flags_test.go` — renamed `TestInstallCommandRejectsOpenCodeTarget` → `TestInstallCommandAcceptsOpenCodeTarget`; refreshed the docstring; renamed the focused test token from `secret-token=opencode-rejected` → `secret-token=opencode-supported`. Extended `TestInstallUsageIncludesTargetAndComponentFlags` with 3 new positive assertions (lock the new install usage line, the new `--target` flag description, and the new `--component` flag description) plus 1 defensive negative assertion that locks out regression to `[--target pi|codex|antigravity]`.
  - `openspec/changes/readd-opencode-support-from-gentle-ai/apply-{started,partial,progress,report}.md` — this slice's persistence artifacts.
- Dirty tree expected: yes — `git status` shows the prior slice changes (foundation, assets, regression, copy, ownership/repair) PLUS this cleanup slice's 2 source files + 4 OpenSpec artifact files. All changes are scoped to either (a) the bounded `mcp.lore` ownership/conflict path, the ownership contract copy in the user-facing surfaces, the gentle plugin-registration clarification in tests/docs/copy, or (b) this cleanup slice's copy/test-name polish. No new code paths, no new assets, no new test files.

## Validation
- Focused checks run (this cleanup slice):
  - `go build ./internal/cli ./internal/install ./internal/tui` → clean (no output).
  - `go vet ./internal/cli ./internal/install ./internal/tui` → clean (no output).
  - `go test ./internal/cli -run 'TestInstallCommand|TestInstallUsage' -count=1 -v` → passed. All focused tests green, including:
    - `TestInstallCommandAcceptsOpenCodeTarget` (renamed from `RejectsOpenCodeTarget`, with new `secret-token=opencode-supported` token name) → PASS.
    - `TestInstallUsageIncludesTargetAndComponentFlags` (with 3 new positive assertions + 1 defensive negative assertion locking the install usage short target list and the new flag descriptions) → PASS.
    - `TestInstallCommandRejectsUnsupportedInstallTarget`, `TestInstallCommandSupportsAntigravityDryRunAndApply`, `TestInstallCommandAcceptsLoreServerMCPWithPiTarget`, `TestInstallCommandDryRunAcceptsExplicitPiTargetAndComponents`, plus all other Pi install command tests → PASS.
  - `go test ./internal/cli ./internal/install ./internal/tui -count=1` → passed (this is the orchestrator-requested validation command). All three packages green:
    - `internal/cli` → ok (1.835s).
    - `internal/install` → ok (0.931s).
    - `internal/tui` → ok (2.300s).
- Broad checks intentionally deferred to `sdd-verify` (per the prior apply-report handoff; unchanged by this cleanup slice):
  - End-to-end install dry-run against a real temp home (Pi, OpenCode, Codex, Antigravity) with the OpenCode path explicitly exercising the fail-closed `mcp.lore` ownership check end-to-end (foreign `mcp.lore` → typed conflict error, backup on disk, no on-disk write; resolved `mcp.lore` → normal additive merge).
  - End-to-end OpenCode install with the `opencode-plugins` component selected to verify the plugin .ts files are actually written under `~/.config/opencode/plugins/` and the `tui.json` is actually written with only the community `opencode-subagent-statusline` in its `plugins` array.
  - End-to-end OpenCode install with `lore-server-mcp` selected to verify the `opencode-config` check surfaces the plaintext-token warning and never embeds the saved token, AND the rendered `mcp.lore` block carries the `managed_by: lore-cli` marker.
  - Negative-regression gates from the 1.3 + 2.x + 3.x + 3.3 slices (no Gentle wording leakage, no `sdd-engram` / `logo` plugin names, no token leakage, additive merge idempotency, backup-before-overwrite, fail-closed ownership on foreign `mcp.lore`) are already covered by the focused test set and were re-run as part of the orchestrator-requested `go test ./internal/cli ./internal/install ./internal/tui -count=1` validation in this cleanup slice.
  - **`install --help` smoke** (lightweight, can be performed by `sdd-verify`): after this cleanup slice, the help stderr should now include `Usage: lore install [--dry-run] [--yes] [--target pi|opencode|codex|antigravity] [--component <id>]` (the verify-report warning list called this out as stale copy; it is now updated and locked in the test surface by the new positive assertion in `TestInstallUsageIncludesTargetAndComponentFlags` plus the defensive negative assertion that locks out regression to `[--target pi|codex|antigravity]`).

## Recovery Handoff
- Resume from: the bounded slice is complete. The next agent (or the orchestrator) should re-delegate to `sdd-verify` to upgrade the verify report from `PASS WITH WARNINGS` to `PASS`. The verify worker should:
  1. Re-run `go test ./internal/cli ./internal/install ./internal/tui -count=1` (orchestrator-requested validation) to confirm the cleanup slice is green.
  2. Re-run the `install --help` smoke and confirm the install usage short target list now includes `opencode` (the prior verify warning) and that the `--target` / `--component` flag descriptions now include OpenCode in the supported target set.
  3. Confirm the renamed `TestInstallCommandAcceptsOpenCodeTarget` is present in the test surface and the legacy `TestInstallCommandRejectsOpenCodeTarget` name is gone.
  4. Re-run the end-to-end install dry-run smoke against a real temp home (Pi, OpenCode, Codex, Antigravity) with the OpenCode path explicitly exercising the fail-closed `mcp.lore` ownership check.
- Required next action: launch `sdd-verify` against the current branch. The 4.x verify slice is the final step for this change.
- Lore memory note: this session has no `lore_*` MCP tools and `lore status` reports auth failure. Per the verify-report protocol and the launch-prompt fallback policy, all apply artifacts for this slice are persisted to the OpenSpec fallback store at `openspec/changes/readd-opencode-support-from-gentle-ai/`. If Lore memory becomes available, the next slice (or the next `sdd-archive` run) should sync these artifacts back into Lore under `sdd/readd-opencode-support-from-gentle-ai/apply-{started,partial,progress,report}` topic keys.
- Out-of-scope dependencies for the next slice (unchanged from the prior apply-report):
  - The 4.x verify slice must run a real end-to-end dry-run against a temp home to verify the plugin .ts files and tui.json are actually written to disk and that the AGENTS.md managed-surface copy documents the bounded bundle, exclusions, plaintext-token warning, and the new `mcp.lore` ownership contract.
  - The 4.x verify slice must run the OpenCode install with `lore-server-mcp` selected to verify the `opencode-config` check surfaces the plaintext-token warning (path, server URL, auth header name) and never embeds the saved token, AND the rendered `mcp.lore` block carries the `managed_by: lore-cli` marker.
  - The 4.x verify slice must run the OpenCode install with an existing foreign `mcp.lore` block in the temp home to verify the fail-closed ownership error fires end-to-end (typed conflict error, backup on disk, no on-disk write), AND the OpenCode install with the foreign `mcp.lore` block removed/resolved must proceed with the normal additive merge.
