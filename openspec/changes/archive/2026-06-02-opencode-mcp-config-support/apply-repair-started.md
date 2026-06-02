# Apply Started: opencode-mcp-config-support (repair slice)

## Slice
- Tasks in scope:
  - Fix 1: Thread preflight.ServerURL/preflight.Token into PlanOpenCodeInstall
  - Fix 2: Fix render/merge pipeline so MCP-enabled opencode.json has BOTH lore + mcp.lore
  - Fix 3: Fix stale FormatTargetSelection copy in service.go
  - Fix 4: Write tasks.md checkpoint
  - Fix 5: Add focused regression tests for MCP-selected OpenCode install path
- Tasks explicitly out of scope: No new plugins, commands, profiles, bootstrap, runtime-subagent, or TUI behavior
- Expected files:
  - internal/cli/actions.go (Fix 1)
  - internal/install/opencode_install.go (Fix 2)
  - internal/install/service.go (Fix 3)
  - internal/install/adapter_opencode_test.go (Fix 5)
  - internal/install/opencode_install_test.go (Fix 5)
  - internal/install/adapter_test.go (Fix 5)
  - openspec/changes/opencode-mcp-config-support/tasks.md (Fix 4)
- Validation planned:
  - go test ./internal/install -run 'TestOpenCode|TestMCP' -v
  - go test ./internal/cli -run 'TestInstall' -v
  - go test ./internal/tui -run 'TestInstall' -v
- Risk budget: low — bounded fixes to known failures; no risky refactors

## Preconditions
- Proposal/spec/design/tasks read: yes
- Previous apply-progress merged: yes (openspec fallback)
- Strict TDD mode: inactive