# Tasks: Re-add OpenCode Support from Gentle AI

Out of scope: `sdd-engram` and `logo` plugins, Pi overlay emulation, daemon/autostart, runtime subagents, bootstrap/package-manager behavior, and any Gentle-authored copy leaking into OpenCode assets.

## Phase 1: Target + render foundation
- [x] 1.1 Reintroduce `TargetOpenCode` in `internal/install/{adapter.go,components.go,service.go,harness.go}` and register it in the default install registry with supported component gating.
- [x] 1.2 Add `internal/install/adapter_opencode.go` + `opencode_install.go` to render `AGENTS.md`, `skills/*.md`, `opencode.json`, and manifest files from `internal/agentpack` markdown/content, with Lore MCP shaped like Pi/Antigravity.
- [x] 1.3 Add OpenCode plugin assets under `internal/install/assets/opencode/plugins/` for `background-agents.ts`, `model-variants.ts`, and community `opencode-subagent-statusline`; explicitly exclude `sdd-engram` and `logo`.
Validation: `go test ./internal/install -run 'TestOpenCode|Test.*Target|Test.*Render'`

## Phase 2: Test-first safety gates
- [x] 2.1 RED: add negative tests in `internal/install/adapter_opencode_test.go`, `internal/install/opencode_install_test.go`, and `internal/cli/install_flags_test.go` for no Gentle leakage, no excluded plugin names, and redacted token output.
- [x] 2.2 GREEN: make the OpenCode render/merge path pass those tests, including safe `opencode.json` merge/backup/idempotency and canonical `sdd-propose` naming.
Validation: `go test ./internal/install ./internal/cli -run 'TestOpenCode|Test.*Leak|Test.*Redact|Test.*Plugin'`

## Phase 3: CLI/TUI/docs wiring
- [x] 3.1 Restore OpenCode target copy in `internal/cli/{app.go,actions.go,actions_opencode_test.go}` and `internal/tui/{root.go,model_test.go}` so summaries describe config-only support, MCP, and plugin scope without Gentle wording.
- [x] 3.2 Update `README.md` and OpenCode install help to describe the re-added target, managed plugin assets, and explicit exclusions.
Validation: `go test ./internal/cli ./internal/tui -run 'Test.*OpenCode|Test.*Install|Test.*Copy'`

## Phase 3.3: mcp.lore ownership/conflict repair (verify follow-up)
- [x] 3.3 Add a `managed_by: lore-cli` ownership marker to the rendered `mcp.lore` block, implement a typed `*OpenCodeMCPConfigOwnershipError`, scope the ownership check to `opencode.json` (so `tui.json` is unaffected), surface the conflict as a `conflicted` plan action with backup-before-abort semantics, and update tests/docs/copy to document the fail-closed contract.
- [x] 3.4 Clarify gentle plugin-registration semantics: tests/docs/copy now assert the local plugin .ts files (background-agents.ts, model-variants.ts) are copied to `~/.config/opencode/plugins/` and are NOT registered in `tui.json`; only the community `opencode-subagent-statusline` is referenced from `tui.json`. The defensive negative assertions in `internal/cli/install_flags_test.go` lock this contract in.

## Phase 4: Focused verification slice
- [ ] 4.1 Verify `lore install --target opencode` dry-run/apply paths create/update the expected files and preserve unrelated user content, AND the new fail-closed `mcp.lore` ownership check fires end-to-end on a foreign `mcp.lore` block (typed conflict error, backup on disk, no on-disk write) and the resolved `mcp.lore` block proceeds with the normal additive merge.
- [ ] 4.2 Run broader install/CLI tests only after the OpenCode slice is green.
Validation: `go test ./...`

Apply slicing recommendation: keep 1.1-1.2 as the first bounded slice, 1.3 as a separate asset slice, then 2.x as the regression slice before any docs/UI cleanup.
