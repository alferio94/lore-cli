# Apply Partial: pi-default-hosted-mcp-install (REPAIR v2)

## Completed in This Slice
- [x] Fix mcp.json to use HTTP endpoint pattern (url + Authorization) instead of stdio lore mcp serve
- [x] Add {{LORE_API_TOKEN}} replacement to renderRequestReplacements
- [x] Update mcp.json validation in pi.go to check for HTTP fields (url, headers) instead of command field
- [x] Fix release notes contradiction about lore mcp serve
- [x] Fix README.md HTTPS package ref wording to git pinning + HTTP MCP config

## Files Changed This Slice
| File | Action | Notes |
|------|--------|-------|
| `internal/install/assets/pi/mcp.json` | Modified | Replaced stdio `lore mcp serve` with HTTP `url` + `Authorization` Bearer pattern |
| `internal/install/adapter_pi.go` | Modified | Added `{{LORE_API_TOKEN}}` placeholder replacement |
| `internal/install/pi.go` | Modified | Updated mcp.json validation to check HTTP fields instead of command field |
| `docs/releases/v0.4.0.md` | Modified | Removed `lore mcp serve` preservation claim |
| `README.md` | Modified | Fixed HTTPS package ref wording; fixed mcp.json description |

## Validation This Slice
- `go build ./...` → PASS
- `go test ./internal/install/... -run "Pi|MCP|Hosted"` → PASS (all 23 targeted tests)
- `go test ./internal/cli/... -run Install` → PASS (all 14 CLI install tests)
- `go test ./...` → PASS (full repo suite)

## Recovery Notes
- Safe resume point: run sdd-verify to confirm all spec scenarios now pass
- All 3 verification failures from verify-report `1b112bad` are addressed