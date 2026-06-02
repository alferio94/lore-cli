# Apply Started: opencode-mcp-config-support

## Slice
- Tasks in scope: 3.1 (narrow opencode.json merge for mcp.lore), 3.2 (backup/manifest/idempotency)
- Tasks explicitly out of scope: 4.x, 5.x (CLI wiring, docs)
- Expected files: internal/install/opencode_install.go, internal/install/manifest.go (reading), internal/install/adapter_opencode.go, internal/install/adapter_opencode_test.go, internal/install/adapter_test.go
- Validation planned: go test ./internal/install -run 'TestOpenCode.*(Merge|Plan|Execute|Manifest|Backup|Idempotent)' -v
- Risk budget: low — Phase 3 builds on Phase 1+2; narrow JSON merge logic; no risky refactors

## Preconditions
- Proposal/spec/design/tasks read: yes
- Previous apply-progress merged: no previous progress found (Lore unavailable, filesystem not yet initialized)
- Strict TDD mode: inactive