# Proposal: Add OpenCode Lore Models Plugin

## Intent
Let users configure Lore-managed agent `model` and `variant` from inside OpenCode, with choices surviving `lore install --target opencode` and rendering into native OpenCode agent config.

## Scope
### In Scope
- Replace the managed OpenCode plugin asset `model-variants.ts` with `lore-models.ts` (or equivalent) and give it an in-OpenCode configuration entrypoint such as `lore-models`.
- Support direct OpenCode configuration of Lore agent `model` and `variant` for the primary `lore`, all `sdd-*` agents, and `lore-worker`.
- Prefer a floating/dialog selector UX when verified against documented/safe OpenCode plugin APIs; otherwise ship a documented in-OpenCode fallback UX.
- Persist overrides in a Lore-owned durable contract that the installer reads when rendering `agent.<name>.model` and `agent.<name>.variant`.
- Render OpenCode config with `default_agent: "lore"`, `agent.lore.mode = "primary"`, all non-lore agents `mode: "subagent"`, include `lore-worker`, and remove `agent.lore.permission = "allow"`.
- Add manifest-based cleanup for stale Lore-managed `model-variants.ts` during the rename without touching user-owned plugins.

### Out of Scope
- Depending on undocumented/internal OpenCode UI APIs as the only supported path.
- Provider auth changes, token handling changes, or broad installer cleanup outside this model-selection flow.
- Deleting plugin files unless prior manifest ownership proves Lore manages them.

## Capabilities
### New Capabilities
None.

### Modified Capabilities
- `opencode-install`: installer-managed OpenCode agents and plugins gain durable model/variant overrides, subagent-mode rendering, and safe managed-plugin migration.

## Approach
Use a Lore-owned override/state contract as source of truth for agent routing preferences, then have OpenCode install rendering project those values into native `agent.<name>.model` and `agent.<name>.variant` fields. The OpenCode plugin remains the user-facing control surface inside OpenCode: preferred UX is a floating selector, but the proposal explicitly allows a documented fallback command flow if the selector requires unsupported internals.

## Affected Areas
| Area | Impact | Description |
|---|---|---|
| `internal/install/assets/opencode/plugins/` | Modified | Replace plugin asset and expose in-OpenCode config UX. |
| `internal/agentconfig/` | Modified | Store durable per-agent model/variant overrides. |
| `internal/install/adapter_opencode.go` | Modified | Render `model`, `variant`, `mode`, and `lore-worker`. |
| `internal/install/opencode_install.go` | Modified | Manifest-scoped stale managed-file cleanup. |
| OpenCode install/TUI tests | Modified | Cover UX fallback, rendering, migration, and safety. |

## Risks
| Risk | Likelihood | Mitigation |
|---|---:|---|
| Floating selector needs unsupported APIs | Med | Treat selector as preferred, not required; keep documented fallback. |
| Override schema migration breaks reinstalls | Med | Version/migrate the Lore-owned state contract and test old installs. |
| Cleanup deletes user plugin files | Low | Delete only manifest-proven Lore-managed stale assets. |

## Rollback Plan
Revert plugin/state/rendering changes, restore prior managed asset name, and reinstall from backup/manifest-backed files.

## Dependencies
- Verified OpenCode config/schema support for `agent.<name>.model` and `agent.<name>.variant`.

## Success Criteria
- [ ] Users can change Lore agent `model` and `variant` from inside OpenCode.
- [ ] Choices survive `lore install --target opencode` and render into native OpenCode agent config.
- [ ] `lore-worker` and all non-lore agents render as subagents; `agent.lore.permission = "allow"` is absent.
- [ ] Plugin rename/migration removes only stale Lore-managed assets.
