# Apply Started: opencode-commands-and-prompts (Slice 2)

## Slice
- Tasks in scope: 2.1, 2.2, 2.3 (Phase 2: SDD asset content rendering)
- Tasks explicitly out of scope: phase 1 remaining (1.4-1.6), all other phases, opencode.json agent prompt wiring
- Expected files:
  - `internal/install/adapter_opencode.go` (add SDD command/prompt render helpers)
  - `internal/install/adapter_opencode_test.go` (add content validation tests)
- Validation planned: `go test ./internal/install -run 'Test.*(Commands|Prompts|Asset)' -v`
- Risk budget: low — additive deterministic content, no behavioral change to existing paths

## Preconditions
- Proposal/spec/design/tasks read: yes (via orchestrator injection and file artifacts)
- Previous apply-progress merged: yes (Phase 1 tasks 1.1-1.3 complete)
- Strict TDD mode: inactive (lore-cli uses standard mode)
- Lore artifacts unavailable; using openspec fallback

## Design Decisions
- Use code-owned deterministic render helpers (not asset files), following existing
  `renderOpenCodeAgentsMD`/`renderOpenCodeManagedSkills` pattern in adapter_opencode.go
- Commands: 9 markdown files (one per canonical SDD phase), triggered by phase-name command
- Prompts: system prompt instruction fragment + per-phase guidance bounded to OpenCode/Lore
- Commands adapted from Gentle SDD patterns: no `gentle-orchestrator`, no runtime/subagent claims
- Prompts inert (install-time asset only), no opencode.json wiring in this slice
- Banned phrases enforced in tests: `gentle-orchestrator`, runtime claims, TUI, plugins, profiles

## Implementation Plan

### Task 2.1 — SDD Command assets
- Add `renderOpenCodeSDDCommands(req RenderRequest) []RenderedFile` in adapter_opencode.go
- Produces 9 RenderedFile entries in `commands/sdd-<phase>.md`
- Each file: frontmatter (name, trigger, description, phase, license) + bounded content
- Content adapted for Lore/OpenCode: phase summary, bounded scope, no agent/runtime claims
- Wire in adapter Render() via `renderOpenCodeSDDCommands(req)` after ComponentOpenCodeSDDAssets check

### Task 2.2 — SDD Prompt assets
- Add `renderOpenCodeSDDPrompts(req RenderRequest) []RenderedFile` in adapter_opencode.go
- Produces inert prompt content rendered into adapter output (not wired to opencode.json)
- System prompt instruction fragment: Lore orchestrator behavior, bounded delegation model
- Per-phase guidance: concise scope and output expectations per canonical SDD phase
- No `gentle-orchestrator` or runtime/subagent claims; bounded to OpenCode/Lore context
- Wire in adapter Render() as part of ComponentOpenCodeSDDAssets rendering

### Task 2.3 — Content validation tests
- Test: SDD command files appear at `commands/sdd-*.md` when component selected
- Test: all 9 canonical phases present (sdd-init through sdd-archive)
- Test: no banned phrases in command/prompt content
- Test: command files contain phase trigger and bounded scope description
- Test: prompt content bounded to OpenCode/Lore, no runtime claims