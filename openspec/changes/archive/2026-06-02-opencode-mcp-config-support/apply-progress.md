# Apply Progress: opencode-mcp-config-support

## Status
- Mode: Standard
- Current slice: completed
- Completed tasks: 5/5 (Phase 1-4) + 5/5 repair tasks (R1-R5) + repair continuation (dg-8ca2dde4 partial fix)

## Repair Continuation (dg-8ca2dde4 partial state recovery)

### Issues Fixed
1. **Compile errors (adapter_opencode_test.go)**: 5 test calls to `renderOpenCodeMCPConfig` missing `agentconfig.DefaultConfig()` as first argument. Fixed by adding `agentconfig.DefaultConfig()` to all calls.
2. **Fail-closed tests for empty token/URL (adapter_opencode_test.go)**: `renderOpenCodeMCPConfig` lacked validation. Added empty-server-url and empty-token guards returning errors before JSON marshaling.
3. **Argument order in mergeOpenCodeJSON (opencode_install.go)**: `planOpenCodeManagedFileActions` called `mergeOpenCodeJSON(desired, existing)` but the function expects `(base, overlay)` = `(existing, desired)`. Fixed: `mergeOpenCodeJSON(existing, desired)`. Updated doc comment accordingly.
4. **MCP component guard (adapter_opencode.go)**: Replaced `return nil, fmt.Errorf(...)` with skip-comment so the adapter returns non-MCP files (AGENTS.md, skills) while letting `renderOpenCodeFiles` in `opencode_install.go` produce the complete opencode.json. Fixed 3 tests: `TestOpenCodePlanWithMCPSelectsLoreAndMCPLoreBlocks`, `TestOpenCodeMCPMergePreservesExistingLoreBlock`, `TestOpenCodeInstallDryRunPassesServerURLAndTokenToPlan`.

### Files Changed (repair continuation)
| File | Action | Notes |
|------|--------|-------|
| internal/install/adapter_opencode.go | Modified | MCP guard: skip-with-comment instead of error-return; added empty token/URL validation to renderOpenCodeMCPConfig |
| internal/install/opencode_install.go | Modified | Fixed mergeOpenCodeJSON argument order (base=existing, overlay=desired) and updated doc comment |
| internal/install/adapter_opencode_test.go | Modified | Added agentconfig.DefaultConfig() to 5 renderOpenCodeMCPConfig calls |

## Completed Tasks Cumulative
- [x] 2.1 OpenCode adapter with CapabilityLoreServerMCP + opencodeMCPBlockKey + renderOpenCodeMCPConfig
- [x] 2.2 ServerURL/Token threading through OpenCode install pipeline
- [x] 2.3 renderOpenCodeMCPConfig: type=remote, url from config, Authorization header from preflight token
- [x] 3.1 Narrow opencode.json merge for mcp.lore — mergeOpenCodeJSON extended to handle mcp block with mcp.lore ownership validation (type==remote, url non-empty, Bearer auth). Unrelated mcp.* entries preserved. Fail-closed for ambiguous mcp.lore ownership.
- [x] 3.2 Backup/manifest/idempotency — applyOpenCodePlannedContent already backs up before write. planOpenCodeManifestAction handles unchanged/update/create correctly. Idempotent rerun produces identical merged JSON.
- [x] 4.1 OpenCode summaries (actions.go, app.go, root.go): warn about plaintext bearer-token in opencode.json when lore-server-mcp is selected/defaulted; reflect mcp=remote vs mcp=none dynamically.
- [x] 4.2 README.md updated to describe MCP support and exclude plugins/profiles/bootstrap claims.
- [x] R1: Thread preflight.ServerURL/preflight.Token into PlanOpenCodeInstall
- [x] R2: Fix render/merge pipeline so MCP-enabled opencode.json has BOTH lore + mcp.lore blocks
- [x] R3: Fix stale FormatTargetSelection copy and AGENTS.md copy to reflect MCP token persistence
- [x] R4: Write tasks.md checkpoint with repair task completion
- [x] R5: Add focused regression tests for MCP-selected OpenCode install path
- [x] RC1: Fix adapter_opencode_test.go compile errors (missing agentconfig.DefaultConfig() in renderOpenCodeMCPConfig calls)
- [x] RC2: Add empty-server-url and empty-token validation to renderOpenCodeMCPConfig
- [x] RC3: Fix mergeOpenCodeJSON argument order (base=existing, overlay=desired)
- [x] RC4: Fix MCP component guard to skip-with-comment instead of error-return

## Validation Cumulative
| Command | Scope | Result | Notes |
|---------|-------|--------|-------|
| go build ./... | Full | PASS | No compilation errors |
| go test ./internal/install -run 'TestOpenCode\|TestManifest\|TestInstall' -v | Focused | PASS | All 63 tests pass |
| go test ./internal/cli -run 'TestInstall\|TestOpenCode' -v | Focused | PASS | All 23 tests pass |
| go test ./internal/tui -run 'TestInstall' -v | Focused | PASS | All 9 tests pass |
| go test ./... | Full | PASS | All packages pass |

## Deviations and Risks
- None: all tasks completed per spec.

## Next Step
- sdd-verify phase to fully validate the change.