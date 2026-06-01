# Apply Progress: pi-default-hosted-mcp-install

## Status
- Mode: Standard
- Current slice: completed
- Completed tasks: 18/18 total (12 original + 6 repair tasks)
- Lore project key: lore-cli
- Artifact store: OpenSpec

## Completed Tasks Cumulative

### Original 12 tasks (from prior apply)
- [x] 1.1 Add `PiHostedMCPPackageRepo`/`PiHostedMCPPackageRef` constants with immutable git SHA
- [x] 1.2 Add `PiHostedMCPPackageSource()` returning `git:github.com/nicobailon/pi-mcp-adapter@<sha>`
- [x] 1.3 Change `ComponentPiExtensions` to `Optional: true`; change `ComponentLoreServerMCP` to default for Pi
- [x] 2.1 Update `defaultPiAdapter` capabilities
- [x] 2.2 Update `adapter_pi.go` Render to render `mcp.json` for hosted MCP
- [x] 2.3 Create `internal/install/assets/pi/mcp.json` stdio MCP template
- [x] 3.1 Update `service.go` `supportedTarget` to describe Pi hosted MCP default
- [x] 3.2 Update `PiLayout` managed files inventory
- [x] 3.3 Update validation and manifest behavior
- [x] 4.1 Update `app.go` usage text
- [x] 4.2 Update README.md
- [x] 4.3 Update `docs/releases/v0.4.0.md`

### Repair Slice (v3 — Contract bug: bearer token materialization)
- [x] 5.1 Fix `mcp.json` template: replace `${LORE_API_TOKEN}` shell placeholder with `{{LORE_API_TOKEN}}`
- [x] 5.2 Fix `PiInstallRequest.renderRequest()` in `pi.go` to pass `SavedToken` into `RenderRequest`
- [x] 5.3 Add `mcp_config_test.go` with adapter-level materialization and redaction tests
- [x] 5.4 Add CLI-level `TestInstallCommandPiMCPConfigMaterializesBearerTokenPlaintext`
- [x] 5.5 Update README.md wording to state Pi mcp.json materializes bearer token in plaintext
- [x] 5.6 Update `docs/releases/v0.4.0.md` wording to match

## Files Changed Cumulative

| File | Action | Task(s) | Notes |
|------|--------|---------|-------|
| `internal/install/assets/pi/mcp.json` | Modified | 5.1 | Replace `${LORE_API_TOKEN}` with `{{LORE_API_TOKEN}}` mustache template |
| `internal/install/pi.go` | Modified | 5.2 | Pass `SavedToken` into `RenderRequest` in `renderRequest()` |
| `internal/install/mcp_config_test.go` | Created | 5.3 | Adapter-level token materialization + redaction tests |
| `internal/cli/app_test.go` | Modified | 5.4 | CLI-level full-install materialization + output redaction test |
| `README.md` | Modified | 5.5 | Added "bearer token materialized in plaintext" wording |
| `docs/releases/v0.4.0.md` | Modified | 5.6 | Added "bearer token materialized in plaintext in ~/.pi/agent/mcp.json" |

## Validation Cumulative

| Command | Scope | Result | Notes |
|---------|-------|--------|-------|
| `go build ./...` | Type-check | ✅ PASS | All packages compile cleanly |
| `go test -count=1 ./internal/install/... -run 'TestPiAdapterRenderMaterializesBearerTokenPlaintext\|TestPiAdapterRenderRedactsTokenInOtherFiles' -v` | Adapter-level tests | ✅ PASS | Both token materialization and redaction tests pass |
| `go test -count=1 ./internal/cli/... -run 'TestInstallCommandPiMCPConfigMaterializesBearerTokenPlaintext' -v` | CLI-level test | ✅ PASS | Full install writes plaintext token in mcp.json; output is redacted |
| `go test -count=1 ./internal/install/... -run 'TestDefaultPiAdapterRenderUsesDefinitionAndPiAssets' -v` | Existing adapter test | ✅ PASS | Existing test still passes after fix |
| `go test -count=1 ./internal/cli/... -run 'TestInstallCommandDryRunSurfacesManagedFileActions\|TestInstallCommandDryRunReportsPlanWithoutMutation' -v` | Existing CLI tests | ✅ PASS | Dry-run tests pass after fix |
| `go test -count=1 ./...` | Full suite | ✅ PASS | All 11 packages pass |

## Deviations and Risks
- None. Fix was surgical: template placeholder in mcp.json + field propagation in pi.go. No behavioral regressions detected.

## Next Slice Recommendation
- Resume from: sdd-verify
- Required next action: run sdd-verify for `pi-default-hosted-mcp-install` to confirm all spec scenarios pass with the contract fix
