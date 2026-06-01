# Apply Started: pi-default-hosted-mcp-install (REPAIR v3 — Contract bug: bearer token materialization)

## Change
User discovered the generated `~/.pi/agent/mcp.json` contained `Authorization: Bearer ${LORE_API_TOKEN}` (shell env-var placeholder) instead of the actual token materialized in plaintext like Antigravity `mcp_config.json`. This repair fixes the contract.

## Scope (Repair Slice)
Tasks:
- Fix `internal/install/assets/pi/mcp.json` — replace `${LORE_API_TOKEN}` shell placeholder with `{{LORE_API_TOKEN}}` mustache template placeholder
- Fix `internal/install/pi.go` `renderRequest()` — pass `SavedToken` into `RenderRequest`
- Add `internal/install/mcp_config_test.go` — `TestPiAdapterRenderMaterializesBearerTokenPlaintext` + `TestPiAdapterRenderRedactsTokenInOtherFiles`
- Add `TestInstallCommandPiMCPConfigMaterializesBearerTokenPlaintext` in `internal/cli/app_test.go`
- Update `README.md` wording to say "bearer token materialized in plaintext"
- Update `docs/releases/v0.4.0.md` wording to say plaintext materialization

## Out of Scope
- Package source constants (already fixed in prior apply)
- lore-memory source/assets preservation
- broad verification (deferred to sdd-verify)

## Preconditions
- Proposal/spec/design/tasks read: yes
- Lore project key: `lore-cli`
- Artifact store: OpenSpec

## Validation Planned
- `go build ./...`
- `go test ./internal/install/... ./internal/cli/...` (focused tests)
- `go test ./...` (full suite)

## Known Root Cause
`PiInstallRequest.renderRequest()` was building a `RenderRequest` but not populating its `SavedToken` field, so the adapter's `renderRequestReplacements()` always had an empty token. The fix is to pass `strings.TrimSpace(r.SavedToken)` into the request before validation.
