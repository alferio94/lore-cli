# Apply Partial: codex-install-config-adapter

## Completed in This Slice
- [x] 1.1 Added Codex target metadata and registry wiring in `internal/install/adapter.go`, `harness.go`, `service.go`, and `components.go`; defined `~/.codex` root, managed `agents.md`, `skills/*/SKILL.md`, and manifest/backup paths.
- [x] 1.2 Threaded `AgentConfigStore.EnsureDefault()` through install preflight in `internal/install/service.go` so Codex planning always starts from Lore-owned `agent-config.json`.

## Files Changed So Far
| File | Action | Notes |
|------|--------|-------|
| internal/install/adapter_codex.go | Created | codexAdapter, capabilities, ResolveCodexLayout, renderers for agents.md and skills |
| internal/install/codex_install.go | Created | Plan/Execute Codex install using shared managed-file semantics |
| internal/install/adapter.go | Modified | Registered Codex in defaultInstallRegistry; added AgentConfig field to RenderRequest |
| internal/install/components.go | Modified | Codex supports core-pack + extended-skills; no MCP |
| internal/install/service.go | Modified | Codex target available; target text says config-only/no MCP/no runner |
| internal/install/service_test.go | Modified | Updated test to reflect Codex as supported target |

## Validation So Far
- `go build ./internal/install/...` → PASS
- `go test -count=1 ./internal/install -run 'Test(DefaultTargets|ResolveInstallTarget|FormatTargetSelection)' -v` → PASS
- `go test -count=1 ./internal/install -run 'TestCheckAgentConfig' -v` → PASS (4 tests)

## Remaining in Current Slice
None — Slice 1 complete.

## Recovery Notes
- Safe resume point: Slice 2 (Phase 2: Codex rendering and plan/apply — tasks 2.1, 2.2)
- Known risks/blockers: None

## Next Slice
- Tasks 2.1, 2.2: Add adapter/codex render tests, plan/apply tests for create/update/unchanged, backup creation, and no writes outside `~/.codex`.