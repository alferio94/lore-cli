# Apply Partial: codex-sdd-agent-config (Pre-Archive Cleanup)

## Completed in This Slice
- [x] C1.1 Refactor store.go to reuse config.ResolveDir() — removed duplicated config-dir resolution, tests pass

## Files Changed So Far
| File | Action | Notes |
|------|--------|-------|
| internal/agentconfig/store.go | Modified | Removed private resolveDir(); now calls config.ResolveDir() |

## Validation So Far
- `go test -count=1 ./internal/agentconfig/...` → PASS (33 tests)
- `go build ./... && go test -count=1 ./...` → PASS (all packages)

## Remaining in Current Slice
- C2.1 (deferred to archive note): stale Lore tasks observation cannot be updated via API; will add archive note

## Recovery Notes
- Safe resume point: archive phase
- Known risks/blockers: Lore API unavailable for write operations; artifact cleanup deferred to archive note