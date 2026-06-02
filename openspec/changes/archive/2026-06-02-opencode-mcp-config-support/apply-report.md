# Apply Report: opencode-mcp-config-support (repair continuation)

## Latest Slice Result
- Status: completed
- Tasks attempted: RC1-RC4 (4 repair-continuation tasks)
- Tasks completed: RC1-RC4
- Tasks remaining: none

## Repository State Summary
- Dirty tree: yes — 13 files modified from prior apply slices + repair fixes
- Key repair files: internal/install/adapter_opencode.go, internal/install/opencode_install.go, internal/install/adapter_opencode_test.go

## Validation
- Focused checks run:
  - `go test ./internal/install -run 'TestOpenCode|TestManifest|TestInstall' -v` → 63/63 PASS
  - `go test ./internal/cli -run 'TestInstall|TestOpenCode' -v` → 23/23 PASS
  - `go test ./internal/tui -run 'TestInstall' -v` → 9/9 PASS
  - `go test ./...` → all packages PASS

## Recovery Handoff
- Resume from: sdd-verify phase
- Required next action: run verify to confirm all spec scenarios and design constraints are satisfied end-to-end.

## Summary of Fixes
1. **adapter_opencode_test.go**: Added `agentconfig.DefaultConfig()` as first arg to 5 `renderOpenCodeMCPConfig` calls (was missing in partial edit from dg-8ca2dde4).
2. **adapter_opencode.go**: Added empty-server-url and empty-token validation to `renderOpenCodeMCPConfig` (fixing 2 fail-closed tests). Changed MCP component guard from `return nil, error` to skip-with-comment so AGENTS.md/skills still render and `renderOpenCodeFiles` produces the full opencode.json.
3. **opencode_install.go**: Fixed `mergeOpenCodeJSON` call order in `planOpenCodeManagedFileActions`: was `mergeOpenCodeJSON(desired, existing)` (wrong), now `mergeOpenCodeJSON(existing, desired)` (correct base+overlay semantics). Updated doc comment.