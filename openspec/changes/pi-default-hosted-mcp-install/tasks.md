# Tasks: pi-default-hosted-mcp-install

## Phase 1: Constants and Component Defaults

- [x] 1.1 Add `PiHostedMCPPackageRepo`/`PiHostedMCPPackageRef` constants with immutable git SHA
- [x] 1.2 Add `PiHostedMCPPackageSource()` returning `git:github.com/nicobailon/pi-mcp-adapter@<sha>`
- [x] 1.3 Change `ComponentPiExtensions` to `Optional: true`, `DefaultForTarget: {}`; change `ComponentLoreServerMCP` to default for Pi

## Phase 2: Adapter and MCP Config

- [x] 2.1 Update `defaultPiAdapter` capabilities: `lore-server-mcp` enabled, `pi-extensions` optional
- [x] 2.2 Update `adapter_pi.go` Render to render `mcp.json` for hosted MCP, keep `lore-memory` optional
- [x] 2.3 Create `internal/install/assets/pi/mcp.json` stdio MCP template

## Phase 3: Installer Service and Manifest

- [x] 3.1 Update `service.go` `supportedTarget` to describe Pi hosted MCP default
- [x] 3.2 Update `PiLayout` managed files inventory (settings.json + mcp.json + extended-skills = 5)
- [x] 3.3 Update validation and manifest behavior for new managed file set

## Phase 4: CLI, Docs, and Tests

- [x] 4.1 Update `app.go` usage text for hosted MCP default and remove extension-first description
- [x] 4.2 Update README.md install behavior description for hosted MCP
- [x] 4.3 Update `docs/releases/v0.4.0.md` to document Pi hosted MCP contract; update test expectations for new package reference

## Phase 5: Contract Bug Repair (Bearer Token Materialization)

- [x] 5.1 Fix `mcp.json` template: replace `${LORE_API_TOKEN}` shell placeholder with `{{LORE_API_TOKEN}}` mustache template
- [x] 5.2 Fix `PiInstallRequest.renderRequest()` in `pi.go` to pass `SavedToken` into `RenderRequest`
- [x] 5.3 Add `mcp_config_test.go` with adapter-level materialization and redaction tests
- [x] 5.4 Add CLI-level `TestInstallCommandPiMCPConfigMaterializesBearerTokenPlaintext`
- [x] 5.5 Update README.md wording to state Pi mcp.json materializes bearer token in plaintext
- [x] 5.6 Update `docs/releases/v0.4.0.md` wording to match

## Completion

All 18 tasks completed. Repair resolves the contract bug: installed `~/.pi/agent/mcp.json` now contains the actual token in plaintext via `SavedToken` render replacement, matching Antigravity behavior, while CLI output remains redacted.
