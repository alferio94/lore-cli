# SDD Init: add-opencode-lore-models-plugin

## Status
- Phase: init
- Change: `add-opencode-lore-models-plugin`
- Date: 2026-06-08
- Persistence: OpenSpec filesystem plus Lore MCP memory
- Scope: initialization only; no implementation code changed

## User Requirements
- Replace/rename managed OpenCode plugin `model-variants` to `lore-models`.
- Provide a TUI floating selector command to configure both `model` and `variant` for each Lore subagent individually, similar to OpenCode's built-in `/variants` `DialogSelect` UI.
- Persist model and variant choices across `lore install --target opencode` reinstall.
- Update OpenCode config generation so `lore` is primary, all non-lore agents are `mode: "subagent"`, current `agent.lore.permission = "allow"` is removed, and `lore-worker` is added as a subagent.
- Handle stale old plugin cleanup for previously installed `model-variants` assets.

## Repository SDD Context
- Project key: `lore-cli`.
- Repository root: `/Users/alfonsocarmona/personal/lore2/lore-cli`.
- OpenSpec layout exists under `openspec/changes/`.
- Active non-archive OpenSpec change present: `pi-default-hosted-mcp-install`.
- New change workspace: `openspec/changes/add-opencode-lore-models-plugin/`.
- Project skill registry: `.atl/skill-registry.md` declares project-local Go/testing guidance and says Lore memory project key is `lore-cli`.
- SDD persistence note: registry says not to create `openspec/` when Lore persistence is healthy, but the user explicitly requested the existing OpenSpec layout and artifact/state files. This init therefore writes OpenSpec files and also persists to Lore MCP.

## Lore Memory Context
- Lore MCP project activity is available for project `lore-cli` (`86836476-c996-4c1a-b21e-361caab8b8d4`).
- Relevant recent SDD history:
  - `fix-opencode-native-config-after-readd`: native OpenCode config shape, no top-level `lore`, singular `tui.json` `plugin` array, default `agent.lore.permission = "allow"` currently documented/implemented, migration of stale top-level `lore` and plural `plugins` shapes.
  - `readd-opencode-support-from-gentle-ai`: OpenCode target reintroduced; plugin bundle currently includes `background-agents.ts`, `model-variants.ts`, and `opencode-subagent-statusline.ts`; `sdd-engram` and `logo` remain explicit exclusions.
  - `opencode-plugin-subagents`: earlier OpenCode plugin/subagent readiness work and warnings around native/runtime subagent assumptions.
- Runtime note from Lore memory: Pi SDD workers should treat MCP Lore memory tools as a valid backend; this run used MCP memory tools directly.

## Baseline Technical Facts
- Go module: `github.com/alferio94/lore-cli`, Go `1.24.3`.
- Focused baseline validation run during init: `go test ./internal/install -run 'TestOpenCode|Test.*Plugin'` passed.
- Worktree was already dirty before init with modified OpenCode-related files in `internal/cli`, `internal/install`, and `internal/tui`; init avoided modifying those implementation files.
- Current OpenCode plugin asset files include:
  - `internal/install/assets/opencode/plugins/background-agents.ts`
  - `internal/install/assets/opencode/plugins/model-variants.ts`
  - `internal/install/assets/opencode/plugins/opencode-subagent-statusline.ts`
- Current `model-variants.ts` behavior: best-effort provider variant cache written to `~/.lore/cache/opencode-model-variants.json`; no selector command is present in that plugin.
- Current `tui.json` asset shape: native OpenCode `$schema`, `theme: "system"`, singular `plugin` array containing only `opencode-subagent-statusline`.
- Current plugin asset registry in `internal/install/opencode_assets.go` explicitly lists `model-variants.ts`.
- Current OpenCode config renderer in `internal/install/adapter_opencode.go`:
  - sets `default_agent` to `lore`;
  - renders `agent.lore` with `mode: "primary"` and `permission: "allow"`;
  - renders SDD phase agents with `model` and `prompt`, but without `mode: "subagent"`;
  - does not include `lore-worker` in the OpenCode `agent` overlay;
  - reads per-phase model overrides from `agentconfig.Config.SDDAgents`, but no variant override path was confirmed during init.

## Initial Scope Boundaries
- In scope for later phases: OpenCode installer config rendering, managed plugin asset rename/replacement, plugin command UX, persisted model+variant config contract, reinstall idempotency/migration, stale `model-variants` cleanup, docs/copy/tests.
- Out of scope for init: implementation, task breakdown, code edits outside this artifact/state, committing, PR creation.
- Must preserve explicit exclusions: `sdd-engram` and `logo` are never bundled/rendered/registered.
- Must preserve token safety: OpenCode MCP token behavior must not expose raw tokens in summaries/logs/errors.

## Candidate Files For Explore
- `internal/install/assets/opencode/plugins/model-variants.ts`
- `internal/install/assets/opencode/plugins/background-agents.ts`
- `internal/install/assets/opencode/tui.json`
- `internal/install/opencode_assets.go`
- `internal/install/adapter_opencode.go`
- `internal/install/opencode_install.go`
- `internal/install/json_merge.go`
- `internal/install/*opencode*_test.go`
- `internal/agentconfig/*`
- `internal/cli/actions.go`
- `internal/cli/app.go`
- `internal/tui/root.go`
- `internal/tui/model_test.go`
- `README.md`

## Risks To Carry Forward
- The requested selector depends on OpenCode plugin APIs and internal UI primitives; explore must verify the actual command/DialogSelect API before specifying implementation.
- Persisting both model and variant may require an agent-config schema change or a separate Lore/OpenCode-owned state file; this is a contract/persistence decision for proposal/spec/design.
- Reinstall persistence and stale cleanup interact: removing old `model-variants.ts` must not delete user-owned plugin files or reset existing choices.
- Existing dirty worktree may contain user/agent changes in the same files later phases will need; apply phases must read and merge carefully.
- Removing `agent.lore.permission = "allow"` may affect established OpenCode behavior and tests/docs that currently assert it.

## Recommended Next Step
- Continue to `sdd-explore` for targeted investigation of OpenCode plugin command/UI APIs, current agent-config persistence, managed-file cleanup/manifest behavior, and exact config overlay changes needed for `lore-worker` plus subagent modes.
