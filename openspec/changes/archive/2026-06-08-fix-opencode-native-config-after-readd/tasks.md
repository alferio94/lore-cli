# Tasks: Fix OpenCode Native Config After Re-add

## Phase 1: Native config shape foundation
- [x] 1.1 Update `internal/install/opencode_install.go` and `internal/install/adapter_opencode.go` so `opencode.json` renders the native OpenCode overlay: no top-level `lore`, only `mcp.lore` when MCP is enabled, with `managed_by`, headers, and schema fields preserved.
- [x] 1.2 Rewrite `internal/install/assets/opencode/tui.json` to the real OpenCode schema shape: singular `plugin` string array, valid schema metadata, and only the community statusline entry; keep local `.ts` assets copy-only.
- [x] 1.3 Adjust JSON-shape helpers in `internal/install/json_merge.go` and `internal/install/opencode_assets.go` so the renderer can treat OpenCode-native payloads separately from Lore-owned overlays without reintroducing the legacy top-level `lore` object.

## Phase 2: Cleanup and migration
- [x] 2.1 Remove or replace any remaining code paths that write or merge the broken Lore-generated top-level `lore`/`plugins` object into `opencode.json` or `tui.json`, including install-plan rendering and manifest bookkeeping.
- [x] 2.2 Add repair/migration handling for existing installs so stale `opencode.json` and `tui.json` from the broken shape are backed up and rewritten before the native payload is applied.

## Phase 3: Regression tests
- [x] 3.1 Add focused render tests in `internal/install/adapter_opencode_test.go` asserting `opencode.json` has no top-level `lore`, `mcp.lore` carries the expected headers/schema/managed_by shape, and non-MCP installs remain MCP-free.
- [x] 3.2 Add `tui.json` tests in `internal/install/adapter_opencode_plugins_test.go` and `internal/install/opencode_install_test.go` for the real schema, singular `plugin` array, and exclusion of the old managed-object shape.
- [x] 3.3 Add migration/end-to-end install tests covering repair of existing broken config without losing unrelated user content.

## Phase 4: Docs and validation
- [x] 4.1 Update `README.md`, `internal/install/service.go`, and install-help copy to describe the native OpenCode config shape, the `mcp.lore` remote contract, and plugin registration rules.
- [x] 4.2 Validate with focused `go test ./internal/install -run 'TestOpenCode|Test.*MCP|Test.*Plugin'`, then `go test ./...`, plus build/install smoke checks for `go build ./...` and `lore install --target opencode` dry-run/apply.
