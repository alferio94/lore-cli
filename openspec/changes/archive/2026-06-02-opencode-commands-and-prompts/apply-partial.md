# Apply Partial: opencode-commands-and-prompts

## Completed in This Slice
- [x] 1.1 Add `ComponentOpenCodeSDDAssets` to `components.go` — OpenCode-only, optional, no defaults
- [x] 1.2 Wire adapter to recognize SDD assets only when component is selected — fail-closed via renderOpenCodeCommandFiles(req, false) returning nil
- [x] 1.3 Add tests for omission-by-default and explicit-component gating

## Files Changed So Far
| File | Action | Notes |
|------|--------|-------|
| `internal/install/components.go` | Modified | Added ComponentOpenCodeSDDAssets constant and catalog entry |
| `internal/install/adapter.go` | Modified | Added CapabilityOpenCodeSDDAssets to CapabilityID enum |
| `internal/install/adapter_opencode.go` | Modified | Added capability to adapter, gated renderOpenCodeCommandFiles on component selection |
| `internal/install/adapter_opencode_test.go` | Modified | Added TestOpenCodeSDDAssetsComponentOmittedByDefault and TestOpenCodeSDDAssetsComponentExplicitlySelectable |

## Validation So Far
- `go test ./internal/install -run 'TestOpenCode|TestComponents' -v` → 28 tests passed, 0 failed

## Remaining in Current Slice
None — all three tasks completed.

## Recovery Notes
- Safe resume point: sdd-verify or next apply slice (tasks 1.4+)
- Known risks/blockers: None