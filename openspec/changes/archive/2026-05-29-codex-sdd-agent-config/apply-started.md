# Apply Started: codex-sdd-agent-config (Pre-Archive Cleanup)

## Slice
- Tasks in scope: C1 (config.ResolveDir reuse), C2 (stale Lore tasks observation cleanup)
- Tasks explicitly out of scope: unrelated dirty worktree changes
- Expected files: internal/agentconfig/store.go, openspec/changes/codex-sdd-agent-config/tasks.md
- Validation planned: go test -count=1 ./internal/agentconfig/... && go build ./... && go test -count=1 ./...
- Risk budget: low — isolated file change, no behavioral change

## Preconditions
- Proposal/spec/design/tasks read: yes
- Previous apply-progress merged: yes (from prior repair + original implementation)
- Strict TDD mode: inactive