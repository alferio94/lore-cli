# Apply Report: pi-default-hosted-mcp-install (REPAIR v3)

## Latest Slice Result
- Status: completed
- Tasks attempted: 6 repair tasks (contract bug fix)
- Tasks completed: 6/6
- Tasks remaining: none (all 18 tasks complete)

## Root Cause Identified
`PiInstallRequest.renderRequest()` in `pi.go` was building a `RenderRequest` but omitting the `SavedToken` field. The adapter's `renderRequestReplacements()` always resolved `{{LORE_API_TOKEN}}` to an empty string, but the template in `mcp.json` used `${LORE_API_TOKEN}` (shell env-var syntax) making it impossible to replace at all.

## Contract Fix Summary
- **Template**: `mcp.json` now uses `{{LORE_API_TOKEN}}` (mustache-style), matching all other placeholders
- **Propagation**: `renderRequest()` now passes `strings.TrimSpace(r.SavedToken)` into `RenderRequest.SavedToken`
- **Rendering**: Adapter's `renderRequestReplacements()` already handled token replacement; the fix unlocks it

## Repository State Summary
- Files changed: 6
  - `internal/install/assets/pi/mcp.json` — `${LORE_API_TOKEN}` → `{{LORE_API_TOKEN}}`
  - `internal/install/pi.go` — `SavedToken` field added to `RenderRequest` in `renderRequest()`
  - `internal/install/mcp_config_test.go` — CREATED (adapter-level token materialization + redaction tests)
  - `internal/cli/app_test.go` — `TestInstallCommandPiMCPConfigMaterializesBearerTokenPlaintext` added
  - `README.md` — 3 edits to clarify plaintext materialization
  - `docs/releases/v0.4.0.md` — 1 edit to clarify plaintext materialization
- Dirty tree: yes (uncommitted changes)
- New test file: `mcp_config_test.go` (4KB adapter tests)
- Additional test: `TestInstallCommandPiMCPConfigMaterializesBearerTokenPlaintext` (CLI tests)

## Validation
- Focused checks run:
  - `go build ./...` → ✅ PASS
  - `go test ./internal/install/... -run 'TestPiAdapterRenderMaterializesBearerTokenPlaintext|TestPiAdapterRenderRedactsTokenInOtherFiles|TestDefaultPiAdapterRenderUsesDefinitionAndPiAssets' -v` → ✅ PASS (all 3)
  - `go test ./internal/cli/... -run 'TestInstallCommandPiMCPConfigMaterializesBearerTokenPlaintext|TestInstallCommandDryRunSurfacesManagedFileActions|TestInstallCommandDryRunReportsPlanWithoutMutation' -v` → ✅ PASS (all 3)
  - `go test ./...` → ✅ PASS (full suite, all 11 packages)
- Broad checks intentionally deferred to verify: no

## Recovery Handoff
- Resume from: sdd-verify for `pi-default-hosted-mcp-install`
- Required next action: run sdd-verify to confirm all spec scenarios pass with the contract fix in place

## Key Behavioral Guarantee
- `~/.pi/agent/mcp.json` now contains `"Authorization": "Bearer <saved-token>"` in ASCII/UTF-8 plaintext
- No token appears in stdout/stderr during install/dry-run (retained redaction)
- Antigravity and Pi now both materialize the bearer token in plaintext in their respective MCP configs, matching the documented tradeoff
