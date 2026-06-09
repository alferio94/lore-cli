# Apply Started: add-opencode-lore-models-plugin

## Slice
- Tasks in scope: 1.1-1.4 (plugin asset rename), 2.1-2.4 (in-OpenCode selector UX + hot-edit), 3.1-3.4 (config renderer + reinstall preservation), 4.1-4.4 (manifest-scoped stale cleanup).
- Tasks explicitly out of scope: 5.5/5.6 broad validation (delegate to verify), 6.x (small prose cleanups surfaced later).
- Expected files:
  - `internal/install/assets/opencode/plugins/model-variants.ts` (rename → `lore-models.ts`)
  - `internal/install/opencode_assets.go` (allowlist)
  - `internal/install/adapter_opencode.go` (renderer)
  - `internal/install/json_merge.go` (effective config preservation hooks)
  - `internal/install/opencode_install.go` (manifest-scoped stale cleanup)
  - `internal/install/components.go` (description copy)
  - `internal/install/service.go` (description copy)
  - `internal/install/adapter_opencode_test.go`
  - `internal/install/adapter_opencode_plugins_test.go`
  - `internal/install/opencode_install_test.go`
  - `openspec/changes/add-opencode-lore-models-plugin/tasks.md`
- Validation planned: `go test ./internal/install -run 'TestOpenCode|Test.*Plugin|Test.*Manifest|Test.*Merge'`; broader `./internal/install ./internal/cli ./internal/tui` is out-of-slice for this apply.
- Risk budget: medium — the change touches the opencode.json renderer that is already mutated by in-flight work in the dirty worktree (per `init.md`).

## Preconditions
- Proposal/spec/design/tasks read: yes
- Previous apply-progress merged: no previous progress (lore memory search returned no `apply-progress` artifact).
- Strict TDD mode: inactive (`openspec/changes/add-opencode-lore-models-plugin/init.md` does not declare strict_tdd; no strict-tdd.md is loaded).

## In-flight dirty worktree notes
The git status before this apply reported the following files as modified (carry-over from previous SDD changes, not this one):
- `internal/cli/actions.go`, `internal/cli/app.go`, `internal/cli/install_flags_test.go`
- `internal/install/adapter_opencode.go`, `adapter_opencode_plugins_test.go`, `adapter_opencode_test.go`
- `internal/install/adapter_test.go`
- `internal/install/assets/opencode/plugins/background-agents.ts`, `model-variants.ts`
- `internal/install/components.go`
- `internal/install/json_merge.go`
- `internal/install/opencode_install_test.go`
- `internal/install/service.go`, `service_test.go`
- `internal/tui/model_test.go`, `tui/root.go`

This apply MUST keep the in-flight work for unrelated surfaces intact. Changes for THIS change are limited to:
- the model-variants → lore-models plugin rename
- the opencode.json renderer changes scoped to agent overlay fields
- the install pipeline cleanup
- the renderer/render test assertions for the new contract
- description copy strings naming the new plugin and revised contract

## Decisions captured from design.md
- Plugin name: `LoreModelsPlugin` (constant); logs `[lore-models]`.
- Cache path unchanged: `~/.lore/cache/opencode-model-variants.json` (metadata only).
- Renderer: `agent.<name>` overlay; `lore` is `mode: primary` (no `permission`); all others `mode: subagent`; `variant` is rendered when non-empty.
- Hot-edit path: safe atomic write with backup + redaction; in-OpenCode selection flows via the documented fallback (plugin tool/command surface) — no dependency on undocumented floating selector APIs.
- Reinstall preservation: read `~/.config/opencode/opencode.json` `agent.<name>.{model,variant}` before render; non-empty values flow into the managed overlay before merge.
- Stale cleanup: `plugins/model-variants.ts` only when previously manifest-owned, backup-then-delete.
