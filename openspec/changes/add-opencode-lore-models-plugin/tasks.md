# Tasks: add-opencode-lore-models-plugin

## Phase 0: Preflight and worktree safety

- [x] 0.1 Re-run `git status --short` and note every unrelated modified file before touching OpenCode installer, plugin, CLI, or TUI code.
- [x] 0.2 Re-read the active change artifacts (`init`, `exploration`, `proposal`, `spec`, `design`, `state`) and confirm the contract: direct-from-OpenCode model/variant config, `lore-models` rename, safe hot-edit of `opencode.json`, and manifest-scoped stale cleanup.
- [x] 0.3 Identify any locally dirty OpenCode files that overlap this change and plan merges; do not overwrite user/agent work with a blanket replace.

## Phase 1: Plugin asset rename and cache continuity

- [x] 1.1 Rename the managed plugin asset from `internal/install/assets/opencode/plugins/model-variants.ts` to `lore-models.ts`, keeping the provider/model/variant discovery cache behavior intact.
- [x] 1.2 Preserve the cache file contract at `~/.lore/cache/opencode-model-variants.json`; ensure it remains metadata-only and never becomes the authority for user-chosen settings.
- [x] 1.3 Update OpenCode asset allowlists/registries/static guards so the managed bundle references `lore-models.ts`, still includes `background-agents.ts` and `opencode-subagent-statusline.ts`, and still excludes `sdd-engram` and `logo`.
- [x] 1.4 Update any install summaries, help copy, and tests that still name `model-variants`.

## Phase 2: In-OpenCode model selector UX and hot-edit persistence

- [x] 2.1 Wire the `lore-models` in-OpenCode entrypoint using a floating/dialog selector only when a verified safe OpenCode runtime API exists; otherwise provide the documented fallback command/tool flow entirely inside OpenCode.
- [x] 2.2 Implement the safe `opencode.json` hot-edit path for `agent.<name>.model` and `agent.<name>.variant`: validate the selected Lore-managed agent, deep-merge only the allowed fields, create a backup first, write atomically with restrictive permissions, reparse for verification, and redact secret-bearing values in logs/errors.
- [x] 2.3 Make the flow read the current agent/model/variant values, distinguish fresh runtime discovery from cached discovery, and support an explicit “no variant/default” removal path only when the user chooses it.
- [x] 2.4 Keep the fallback interaction entirely inside OpenCode; do not require shell access or manual editing of `opencode.json`.

## Phase 3: Installer rendering and reinstall preservation

- [x] 3.1 Update `opencode.json` rendering so `default_agent` stays `lore`, `agent.lore.mode` stays `primary`, `agent.lore.permission` is removed, and every non-lore managed agent (including `lore-worker`) renders with `mode: "subagent"`.
- [x] 3.2 Render `agent.<name>.model` and `agent.<name>.variant` from the effective existing OpenCode config when present so reinstall preserves user-chosen values instead of resetting them to managed defaults.
- [x] 3.3 Ensure `lore-worker` is included in the managed OpenCode overlay and that the copied skill/agent references still resolve correctly.
- [x] 3.4 Preserve unrelated user-owned config, foreign agents, commands, plugins, theme, and `mcp` content; keep the foreign `mcp.lore` fail-closed boundary and token redaction behavior unchanged.

## Phase 4: Manifest-scoped stale cleanup and migration

- [x] 4.1 Add a stale managed-file cleanup pass for OpenCode installs by comparing the previous managed manifest to the newly rendered managed paths.
- [x] 4.2 Delete `plugins/model-variants.ts` only when prior manifest ownership proves Lore managed it; backup first, then delete; never infer ownership from filename alone.
- [x] 4.3 Keep user-owned plugin files untouched when no prior manifest exists or ownership is ambiguous; update delete/summary reporting accordingly.
- [x] 4.4 Ensure fresh installs render only `lore-models.ts` and no stale `model-variants.ts` asset.

## Phase 5: Tests, safety assertions, and validation

- [x] 5.1 Update OpenCode adapter/install tests for the new plugin name, `lore-worker`, `mode: "subagent"`, removed permission, and preserved model/variant rendering.
- [x] 5.2 Add or extend tests for hot-edit persistence: atomic replacement, backup creation, parse failure handling, secret redaction, and “preserve only selected fields” semantics.
- [x] 5.3 Add or extend stale-cleanup tests that prove manifest-owned `model-variants.ts` is deleted and unowned similarly named files survive.
- [x] 5.4 Update CLI/TUI/docs tests and snapshots for changed install copy, bundle descriptions, and asset lists.
- [x] 5.5 Run focused validation commands in order: `go test ./internal/install -run 'TestOpenCode|Test.*Plugin|Test.*Manifest|Test.*Merge'`, then `go test ./internal/install ./internal/cli ./internal/tui`, then `go test ./internal/agentconfig ./internal/install` if any schema/fallback-state change lands.
- [ ] 5.6 If the repository has an OpenCode plugin/static-asset smoke test path available, run it after the Go suites to validate the renamed plugin bundle and selector flow. (deferred — no Go-side smoke test path for the TypeScript plugin exists in the repo; the plugin's hot-edit helper is exercised through the TypeScript source and the install-side rendering/test coverage; full OpenCode runtime testing is out-of-slice and remains a verify-phase follow-up)

## Phase 6: Docs and user-facing copy

- [x] 6.1 Update README/help/install prose to describe `lore-models`, direct OpenCode model/variant configuration, and the revised managed-agent contract.
- [x] 6.2 Remove outdated user-facing references to `model-variants.ts` and `agent.lore permission=allow`.
- [x] 6.3 Verify no docs, summaries, or errors expose raw token-bearing config values or plaintext MCP tokens.
