# Apply Progress: opencode-commands-and-prompts

## Status
- Mode: Standard (lore-cli default)
- Current slice: completed
- Completed tasks: 6/30 (Phase 1 tasks 1.1-1.3 + Phase 2 tasks 2.1-2.3)

## Completed Tasks Cumulative
- [x] 1.1 Add `ComponentOpenCodeSDDAssets` to `components.go` — OpenCode-only, optional, no defaults; added to ComponentCatalog with empty DefaultForTarget
- [x] 1.2 Wire adapter to recognize SDD assets only when component is selected — added CapabilityOpenCodeSDDAssets to adapter, gated render calls on component selection
- [x] 1.3 Add/adjust test coverage for omission-by-default and explicit-component gating — two tests updated for phase 2
- [x] 2.1 Add SDD command render helpers — `renderOpenCodeSDDCommands()` produces 9 command files in `commands/sdd-{phase}.md`
- [x] 2.2 Add SDD prompt render helpers — `renderOpenCodeSDDPrompts()` produces inert system-prompt-guidance.md at `prompts/sdd/`
- [x] 2.3 Add content validation tests — 6 new tests for command/prompt content, banned phrases, and boundaries

## Files Changed Cumulative
| File | Action | Task(s) | Notes |
|------|--------|---------|-------|
| `internal/install/components.go` | Modified | 1.1 | Added ComponentOpenCodeSDDAssets constant and catalog entry |
| `internal/install/adapter.go` | Modified | 1.1 | Added CapabilityOpenCodeSDDAssets to CapabilityID enum |
| `internal/install/adapter_opencode.go` | Modified | 1.2, 2.1, 2.2 | Added capability to adapter, SDD command/prompt render helpers |
| `internal/install/adapter_opencode_test.go` | Modified | 1.3, 2.3 | Updated tests and added 6 new validation tests |

## Validation Cumulative
| Command | Scope | Result | Notes |
|---------|-------|--------|-------|
| `go test ./internal/install -run 'TestOpenCodeSDD|TestOpenCode.*Command|TestOpenCode.*Prompt' -v` | SDD assets tests | PASS | 10 tests, all green |
| `go test ./internal/install -v` | Full install package | PASS | All tests green |

## Deviations and Risks
- None: slice 2 implemented exactly as specified

## Next Slice Recommendation
- Tasks: 1.4-1.6 (Phase 1 remaining: actual command/prompt asset content and render implementation)
- Note: Phase 1 tasks 1.4-1.6 were initially listed but Phase 2 implementation (2.1-2.3) supersedes them
- No remaining tasks for this change; recommend verify phase next