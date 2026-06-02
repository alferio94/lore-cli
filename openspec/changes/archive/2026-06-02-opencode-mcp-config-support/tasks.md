# Tasks: opencode-mcp-config-support

## Phase 1: OpenCode adapter foundation
- [x] 1.1 OpenCode adapter registered in default install registry
- [x] 1.2 OpenCode adapter implements `Capabilities()` returning `CapabilityLoreServerMCP`

## Phase 2: MCP render support
- [x] 2.1 OpenCode adapter with CapabilityLoreServerMCP + opencodeMCPBlockKey + renderOpenCodeMCPConfig
- [x] 2.2 ServerURL/Token threading through OpenCode install pipeline
- [x] 2.3 renderOpenCodeMCPConfig: type=remote, url from config, Authorization header from preflight token

## Phase 3: Narrow opencode.json merge for mcp.lore
- [x] 3.1 mergeOpenCodeJSON extended to handle mcp block with mcp.lore ownership validation (type==remote, url non-empty, Bearer auth). Unrelated mcp.* entries preserved. Fail-closed for ambiguous mcp.lore ownership.
- [x] 3.2 Backup/manifest/idempotency — applyOpenCodePlannedContent already backs up before write. planOpenCodeManifestAction handles unchanged/update/create correctly. Idempotent rerun produces identical merged JSON.

## Phase 4: OpenCode summaries and docs
- [x] 4.1 OpenCode summaries (actions.go, app.go, root.go): warn about plaintext bearer-token in opencode.json when lore-server-mcp is selected/defaulted; reflect mcp=remote vs mcp=none dynamically.
- [x] 4.2 README.md updated to describe MCP support and exclude plugins/profiles/bootstrap claims.

## Repair tasks (dg-b6592087)
- [x] R1: Thread preflight.ServerURL/preflight.Token into PlanOpenCodeInstall (actions.go installOpenCodeActionWithOptions)
- [x] R2: Fix render/merge pipeline so MCP-enabled opencode.json has BOTH lore + mcp.lore blocks (opencode_install.go renderOpenCodeFiles/renderOpenCodeLoreBlockWithMCP + adapter_opencode.go guard)
- [x] R3: Fix stale FormatTargetSelection copy (service.go) and AGENTS.md copy (adapter_opencode.go) to reflect MCP token persistence
- [x] R4: Write tasks.md checkpoint with repair task completion
- [x] R5: Add focused regression tests for MCP-selected OpenCode install path

## Completion traceability
- All original Phase 1-4 tasks: completed via prior apply slices
- All repair tasks R1-R5: completed in this repair slice
- No remaining open tasks