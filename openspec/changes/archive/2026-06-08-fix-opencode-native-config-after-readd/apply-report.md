# Apply Report: fix-opencode-native-config-after-readd

## Latest Slice Result
- Status: completed
- Tasks attempted: 1.1, 1.2, 1.3, 2.1, 2.2, 3.1, 3.2, 3.3, 4.1
- Tasks completed: 1.1, 1.2, 1.3, 2.1, 2.2, 3.1, 3.2, 3.3, 4.1
- Tasks remaining: none (the bounded slice is complete). Task 4.2 broad validation was completed as part of this slice because the user explicitly requested the focused test sets.

## Repository State Summary
- Files changed (bounded slice):
  - `internal/install/adapter_opencode.go`
  - `internal/install/opencode_install.go`
  - `internal/install/json_merge.go`
  - `internal/install/assets/opencode/tui.json`
  - `internal/install/opencode_install_test.go`
  - `internal/install/adapter_opencode_test.go`
  - `internal/install/adapter_opencode_plugins_test.go`
  - `internal/install/service.go`
- New artifacts: `openspec/changes/fix-opencode-native-config-after-readd/apply-started.md`, `apply-progress.md`, `apply-report.md`.
- Dirty tree expected: yes — the bounded slice mutated the install source and asset tree.

## Validation
- Focused checks run:
  - `go build ./...` → passed.
  - `go test ./internal/install -run 'TestOpenCode|Test.*MCP|Test.*Plugin' -count=1 -v` → passed (38 tests).
  - `go test ./internal/install ./internal/cli ./internal/tui -count=1` → passed (user-requested scope).
  - `go test ./... -count=1` → passed (full module, no regressions).
- Broad checks intentionally deferred to verify: `lore install --target opencode` dry-run/apply smoke (deferred to `sdd-verify`; the bounded slice does not require live harness execution and the existing end-to-end migration test covers the same on-disk path).

## Recovery Handoff
- Resume from: `sdd-verify` (verify the repair slice against the original change proposal, design, and tasks).
- Required next action: launch `sdd-verify` to confirm the post-repair opencode.json + tui.json shape, the mcp.lore ownership contract, the migration contract, and the token redaction contract are all preserved. If `sdd-verify` flags a defect, return to apply with the targeted defect; otherwise proceed to `sdd-archive`.
- Lore sync (optional, when Lore tooling is available): re-run this slice with Lore persistence enabled so the OpenSpec fallback tasks.md is migrated to `sdd/fix-opencode-native-config-after-readd/tasks` in project `lore-cli`. Until then, the OpenSpec files in this directory are the source of truth.
