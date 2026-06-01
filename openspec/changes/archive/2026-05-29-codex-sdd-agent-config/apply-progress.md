# Apply Progress: codex-sdd-agent-config (Pre-Archive Cleanup)

## Status
- Mode: Standard
- Current slice: completed
- Completed tasks: 10/10 (8 original + 2 repair + 2 pre-archive cleanup)

## Completed Tasks Cumulative

### Original (8 tasks)
- [x] 1.1 Create `internal/agentconfig/config.go` and `store.go`
- [x] 1.2 Export canonical SDD agent names and default model from agentpack
- [x] 2.1 Canonical JSON ordering in agentconfig
- [x] 2.2 Unit tests for agentconfig
- [x] 3.1 Wire agent-config into CLI actions and install service
- [x] 3.2 Update app.go and README.md for agent-config.json sibling contract
- [x] 4.1 Install tests proving auth leaves agent-config intact
- [x] 4.2 agentconfig store tests and CLI/install tests

### Repair Round 1
- [x] R1.1 Add AgentConfigStore to App struct + wire into install.Service
- [x] R2.1 Replaced private canonicalSDDPhases with agentpack.SDDPhaseAgentNames()
- [x] R2.2 Replaced private DefaultSDDModel with agentpack.DefaultSDDModel
- [x] R2.3 Exported agentpack.IsKnownSDDAgent()

### Pre-Archive Cleanup
- [x] C1.1 Refactor `internal/agentconfig/store.go` to reuse `config.ResolveDir()` instead of duplicating config-dir resolution logic

## Files Changed Cumulative
| File | Action | Task(s) | Notes |
|------|--------|---------|-------|
| internal/cli/app.go | Modified | R1.1 | AgentConfigStore interface + field |
| internal/cli/actions.go | Modified | R1.1 | Wired AgentConfigStore into install.Service |
| internal/agentconfig/config.go | Modified | R2.1, R2.2 | Delegates to agentpack |
| internal/agentpack/definition.go | Modified | R2.3 | Exported IsKnownSDDAgent() |
| internal/agentconfig/store.go | Modified | C1.1 | Reuses config.ResolveDir() |

## Validation Cumulative
| Command | Scope | Result | Notes |
|---------|-------|--------|-------|
| go test -count=1 ./internal/agentconfig/... | Focused | PASS | All 33 tests |
| go test -count=1 ./internal/config/... ./internal/agentpack/... | Focused | PASS | |
| go build ./... | Broad | PASS | |
| go test -count=1 ./... | Broad | PASS | All packages |

## Deviations and Risks
- None for this slice.
- Unrelated dirty worktree files (README.md, install/, cli/) are outside this change surface and were not touched.

## Next
- Archive: sync tasks to canonical spec and close change.