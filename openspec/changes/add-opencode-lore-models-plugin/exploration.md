# SDD Exploration: add-opencode-lore-models-plugin

## Status
- Phase: explore
- Change: `add-opencode-lore-models-plugin`
- Date: 2026-06-08
- Repository: `/Users/alfonsocarmona/personal/lore2/lore-cli`
- Persistence: OpenSpec filesystem plus Lore MCP memory
- Outcome: exploration completed with one product/implementation decision still required before proposal/design

## Scope Investigated
- OpenCode plugin command/UI capabilities for a Lore-managed model selector.
- Current persistence contracts for per-agent model configuration.
- Safe stale-plugin cleanup using the existing installer manifest surface.
- Exact OpenCode config changes for `lore`, SDD agents, and `lore-worker`.
- Migration, testing, and documentation impact.

## Repository Evidence Summary

### 1. Current OpenCode plugin surface in this repo
- Managed plugin assets are explicitly bounded in `internal/install/opencode_assets.go` to:
  - `background-agents.ts`
  - `model-variants.ts`
  - `opencode-subagent-statusline.ts`
- `internal/install/assets/opencode/plugins/model-variants.ts` currently does one thing only: call `client.provider.list()` and write a best-effort cache to `~/.lore/cache/opencode-model-variants.json`.
- No OpenCode-local Lore plugin in this repo currently registers a TUI command, slash command, or selector UI.
- The only bundled OpenCode plugin with meaningful runtime hooks today is `background-agents.ts`, which uses plugin tools plus `experimental.chat.system.transform`, `experimental.session.compacting`, and `event` hooks.

### 2. What OpenCode officially documents today
- OpenCode plugin docs (`https://opencode.ai/docs/plugins`) document plugin hooks, custom tools, and events.
- The documented TUI-related plugin events are limited to:
  - `tui.prompt.append`
  - `tui.command.execute`
  - `tui.toast.show`
- The documented plugin API does **not** describe:
  - registering a new slash command directly from a plugin,
  - opening a floating selector/dialog from a plugin,
  - importing or invoking a public `DialogSelect`-style UI primitive.
- OpenCode command docs (`https://opencode.ai/docs/commands`) show that slash commands are configured through `opencode.json`/markdown command files, and those commands send prompts to an agent; they are not documented as arbitrary interactive plugin callbacks.
- OpenCode TUI docs (`https://opencode.ai/docs/tui`) document built-in slash commands and keybind behavior, but do not expose a documented plugin API for built-in floating dialogs.

### 3. OpenCode schema capabilities relevant to this change
- The live OpenCode config schema (`https://opencode.ai/config.json`) includes:
  - `agent.<name>.model`
  - `agent.<name>.variant`
  - `command.<name>.model`
  - `command.<name>.variant`
- This is important: **per-agent variant persistence is already a native OpenCode config capability**.
- The current Lore OpenCode renderer does not use that `variant` field anywhere.

### 4. Current Lore persistence contract for agent choices
- `internal/agentconfig/config.go` defines schema version `1`.
- Current `agent-config.json` stores only:
  - `schema_version`
  - `sdd_agents.<phase>.model`
- It does **not** store:
  - variants,
  - `lore-worker`,
  - the primary `lore` agent,
  - any non-SDD OpenCode-specific agent records.
- `internal/install/adapter_opencode.go` reads only `agentconfig.Config.SDDAgents` and only for model overrides.

### 5. Current OpenCode config rendering behavior
- `renderOpenCodeNativeConfig()` writes a native `opencode.json` with:
  - `$schema`
  - `theme`
  - `default_agent: "lore"`
  - `agent` overlay
  - `skills.path`
- `opencodeAgentOverlay()` currently renders:
  - `agent.lore` with `description`, `model`, `mode: "primary"`, `permission: "allow"`, and `prompt`
  - the 9 `sdd-*` agents with `model` and `prompt` only
- It does **not** currently:
  - add `mode: "subagent"` to non-lore agents,
  - include `lore-worker`,
  - render `variant` for any agent.

### 6. `lore-worker` is already present in managed assets, but not in OpenCode agent overlay
- `RenderRequest.effectiveManagedAgents()` returns the full managed agent set from the definition.
- This means OpenCode skill rendering already has the data needed to emit `skills/lore-worker/SKILL.md`.
- The gap is specifically in OpenCode agent overlay/config rendering and the OpenCode-facing docs/tests, not in the portable agent-pack definition.
- `internal/agentpack` evidence confirms `lore-worker` is a first-class managed agent alongside the nine SDD phase agents.

### 7. Current stale-file cleanup behavior for OpenCode
- `internal/install/opencode_install.go` plans managed file actions only from the newly rendered file set.
- It builds a new manifest from the new rendered set, but it does **not** compare the previous OpenCode manifest against the new managed set to schedule deletions.
- Result: a rename from `plugins/model-variants.ts` to `plugins/lore-models.ts` would leave the old file behind today.
- The existing manifest format is already sufficient to support safe cleanup:
  - `Manifest.ManagedFiles[]` stores absolute path, component, merge mode, and content hash.
  - Manifest lives at `~/.config/opencode/lore-install.json`.

### 8. Existing safe pattern for stale managed cleanup elsewhere in the repo
- Pi managed overlay cleanup already compares existing manifest-managed paths against current rendered paths and schedules backup-first delete actions for stale managed files.
- The relevant planning pattern exists in `internal/install/plan.go` for managed overlay cleanup.
- OpenCode does not yet have an equivalent stale managed-file cleanup pass.

## Constraints and Conclusions

### A. Floating selector UX is not currently backed by a verified public OpenCode plugin API
The exact requested UX is: a TUI floating selector command similar to OpenCode’s built-in `/variants` dialog.

Exploration result:
- I could verify that OpenCode supports variants in config/schema.
- I could verify plugin hooks/tools/events.
- I could **not** verify a documented plugin API for:
  - opening built-in floating dialogs,
  - importing a public dialog component such as `DialogSelect`, or
  - defining a new slash command that directly invokes custom interactive TUI code.

Practical implication:
- A **supported** implementation path exists for persistence (`agent.variant`), but
- the **exact** floating selector UX likely requires either:
  1. unsupported/internal OpenCode UI integration, or
  2. a different supported UX than the one requested.

This is the main blocker that needs user direction before proposal/design should lock an approach.

### B. Per-agent model + variant persistence should use a Lore-owned durable contract, not a plugin-local cache
The old `model-variants.ts` cache file is provider metadata only; it is not a user preference store.

Safe persistence contract options:
1. **Extend `agent-config.json`** (recommended direction)
   - bump schema version,
   - add `variant`,
   - add coverage for `lore-worker` plus SDD agents,
   - keep the primary `lore` orchestrator installer-owned unless requirements explicitly expand to primary-agent tuning.
2. Introduce a new OpenCode-specific Lore-owned state file under `~/.config/opencode/`
   - lower impact on existing cross-target agent-config contract,
   - but duplicates model-routing state and makes cross-target behavior less coherent.

Why option 1 looks safer long term:
- Lore already owns `agent-config.json` as the secret-free durable preference store.
- Reinstall persistence requirement is naturally satisfied if install rendering sources values from that store.
- It avoids coupling persistence to the OpenCode plugin runtime.

### C. Safe stale plugin cleanup should be manifest-based, not directory-scan-based
To remove old `model-variants.ts` without touching user-owned plugins:
- load the previous OpenCode manifest if present,
- compare previous manifest-managed files to the newly rendered managed file set,
- schedule backup-first delete actions only for files that were previously Lore-managed and are no longer rendered,
- specifically this will catch `plugins/model-variants.ts` on upgraded installs.

Do **not**:
- delete arbitrary `*.ts` files from `~/.config/opencode/plugins/`,
- infer ownership from filename alone when no prior manifest proves Lore ownership.

### D. OpenCode config changes are mechanically clear once the persistence/UI decision is made
The requested config behavior maps cleanly to the native OpenCode contract:
- keep `default_agent: "lore"`
- keep `agent.lore.mode = "primary"`
- remove `agent.lore.permission = "allow"`
- add `mode: "subagent"` to every non-lore Lore-managed agent
- add `agent.lore-worker`
- render `variant` where persisted values exist

The OpenCode schema supports these fields today.

## Recommended Technical Direction (pending UX decision)
If the user accepts a supported persistence/rendering path while the selector UX is clarified:

1. Rename the managed plugin asset from `model-variants.ts` to `lore-models.ts`.
2. Keep provider/model variant discovery inside the renamed plugin if still needed.
3. Extend Lore-owned durable preference storage to carry per-subagent `model` + `variant`.
4. Render `agent.<name>.model` and `agent.<name>.variant` into `opencode.json` for all Lore subagents.
5. Add `lore-worker` to the OpenCode `agent` overlay with `mode: "subagent"`.
6. Remove the installer-managed `agent.lore.permission = "allow"` field.
7. Add manifest-based stale managed-file cleanup so old `plugins/model-variants.ts` is deleted only when previously Lore-managed.

## Impacted Files

### Definitely impacted
- `internal/install/assets/opencode/plugins/model-variants.ts` (rename/replacement to `lore-models.ts`)
- `internal/install/opencode_assets.go`
- `internal/install/adapter_opencode.go`
- `internal/install/opencode_install.go`
- `internal/install/json_merge.go` (possibly only if command/config merge behavior is expanded)
- `internal/agentconfig/config.go`
- `internal/agentconfig/store.go` and tests
- `internal/install/adapter_opencode_test.go`
- `internal/install/adapter_opencode_plugins_test.go`
- `internal/install/opencode_install_test.go`
- `internal/install/service.go`
- `internal/cli/actions.go`
- `internal/cli/app.go`
- `internal/cli/install_flags_test.go`
- `internal/tui/root.go`
- `internal/tui/model_test.go`

### Possibly impacted depending on chosen UX
- OpenCode-managed command rendering path if Lore decides to install a managed `/lore-models` command definition.
- Additional docs or README sections if the new selector/persistence behavior is user-facing outside install summaries.

## Migration Notes
- Existing installs with `plugins/model-variants.ts` need a one-time managed stale-file cleanup.
- Existing installs without a prior OpenCode manifest should **not** have similarly named plugin files deleted automatically.
- Existing `agent-config.json` files will need a schema migration story if `variant` and/or non-SDD subagent entries are added.
- Existing tests and user-facing summaries that mention `model-variants` or `agent.lore permission=allow` will need synchronized updates.

## Validation Performed During Exploration
- Read the current repo implementations for:
  - OpenCode plugin assets
  - OpenCode renderer/install pipeline
  - manifest format and cleanup patterns
  - agent-config schema/store
- Consulted current OpenCode public docs and live schema for:
  - plugins
  - commands
  - TUI
  - agents
  - models
  - config schema
- Confirmed from live schema that `agent.variant` and `command.variant` are supported.

## Risks
1. The requested floating selector UI may require unsupported/internal OpenCode APIs.
2. Extending `agent-config.json` changes a cross-target Lore contract and needs careful migration/testing.
3. Stale plugin cleanup must be manifest-scoped or it risks deleting user-owned plugin files.

## Decision Needed Before Proposal/Design
**Question:** should this change preserve the exact requested floating selector UX even if it requires unsupported/internal OpenCode UI integration, or should it stay within documented OpenCode surfaces and accept a less rich but supported configuration UX?

### Option 1 — exact floating selector goal
- Pursue an internal/unsupported OpenCode UI integration if necessary.
- Pros: matches requested `/variants`-style experience more closely.
- Cons: higher fragility, weaker public-contract guarantees, harder maintenance.

### Option 2 — supported-surface goal
- Use documented config/plugin/command surfaces only.
- Persist per-agent `model` + `variant`, but accept a non-dialog configuration workflow unless a documented selector API is found later.
- Pros: safer contract, easier testing, lower maintenance.
- Cons: does not exactly match the requested built-in floating selector experience.

## Recommended Next Step
- After the UX direction is chosen, continue to `sdd-propose`.
