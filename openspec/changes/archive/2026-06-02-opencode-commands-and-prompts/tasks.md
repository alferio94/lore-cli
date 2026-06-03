# Tasks: OpenCode Commands and Prompt Materialization

Out of scope: plugins, profiles/model variants, TUI plugins, bootstrap/package-manager, runtime/native subagent claims.

## Phase 1: Component / model wiring
- [x] 1.1 Add optional `opencode-sdd-assets` to `internal/install/components.go`; keep it OpenCode-only and out of default selection.
- [x] 1.2 Wire `internal/install/adapter_opencode.go` to recognize commands/prompts only when the optional component is selected.
- [x] 1.3 Add/adjust `internal/install/adapter_opencode_test.go` coverage for omission-by-default and explicit-component gating.

Validation: `go test ./internal/install -run 'TestOpenCode|TestComponents'`

## Phase 2: Asset/content implementation
- [x] 2.1 Add Lore-authored/adapted command assets in `internal/install/assets/opencode/commands/sdd-*.md` OR render from bounded Go helpers. Commands use canonical `sdd-propose` name (not `sdd-proposal`).
- [x] 2.2 Add inert per-phase prompt assets in `internal/install/assets/opencode/prompts/sdd/sdd-*.md` with no `gentle-orchestrator` or runtime/subagent claims. 9 per-phase prompt files (sdd-init through sdd-archive) mirroring canonical phase names.
- [x] 2.3 Add/adjust helper tests to assert approved paths and banned phrases for command/prompt content.

Validation: `go test ./internal/install -run 'Test.*(Commands|Prompts|Asset)'`

## Phase 3: Render / plan / apply / manifest / backups / idempotency
- [x] 3.1 Extend `internal/install/opencode_install.go` render/plan paths so selected commands and prompts become managed files only for OpenCode. sdd-propose.md and sdd-proposal.md must NOT both exist.
- [x] 3.2 Extend apply/backups so `commands/*.md` and `prompts/sdd/*.md` back up before overwrite and restore cleanly on rerun.
- [x] 3.3 Extend manifest hashing/idempotency plus optional `lore.commands_dir` metadata handling without changing unrelated OpenCode keys. Lifecycle proof tests added.

Validation: `go test ./internal/install -run 'Test(OpenCode|Manifest|Install)'`

## Phase 4: CLI / TUI / docs copy and tests
- [x] 4.1 Update CLI/TUI install summaries and help copy to describe the optional opencode-sdd-assets component and staged prompt assets only.
- [x] 4.2 Update `README.md` and copy tests to describe optional SDD assets, opencode-sdd-assets component, and exclude plugin/profile/bootstrap/runtime-subagent claims. Keep MCP compatibility wording bounded.

Validation: `go test ./internal/cli ./internal/tui -run 'Test.*(Install|OpenCode|Copy)'`

## Phase 5: Final validation
- [x] 5.1 Run focused install, CLI, and TUI tests for the touched paths. — `go test ./internal/install ./internal/cli -run 'TestOpenCode|TestComponents|TestInstall'` PASS
- [x] 5.2 Run broader package tests only after focused slices pass. — `go test ./...` PASS with dg-2b0f4b09 warnings pending cleanup

Validation: `go test ./...`