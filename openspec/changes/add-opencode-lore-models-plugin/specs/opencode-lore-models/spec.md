# Delta Spec: OpenCode Lore Models Plugin

## Status
- Phase: spec
- Change: `add-opencode-lore-models-plugin`
- Date: 2026-06-09
- Repository: `/Users/alfonsocarmona/personal/lore2/lore-cli`
- Persistence: OpenSpec filesystem plus Lore MCP memory

## Scope
This delta specifies the required behavior for replacing the managed OpenCode `model-variants` plugin with `lore-models`, enabling in-OpenCode model and variant selection for Lore-managed agents, persisting those selections primarily by safe live edits to `opencode.json`, preserving selections across reinstall, and migrating managed assets without touching user-owned files.

## Requirement 1: Managed plugin replacement shall rename `model-variants` to `lore-models` without losing managed cache behavior
The OpenCode managed plugin bundle SHALL replace the Lore-managed plugin asset identity `model-variants` with `lore-models` while preserving Lore-owned provider/model variant cache behavior needed by the in-OpenCode configuration experience.

### Scenario: Fresh install renders the new managed plugin identity
- **GIVEN** a repository install target that renders Lore-managed OpenCode plugin assets
- **WHEN** Lore renders or installs managed OpenCode assets for a fresh OpenCode configuration
- **THEN** the managed plugin bundle includes `lore-models` instead of `model-variants`
- **AND** the rendered plugin set still includes the existing managed background/statusline assets that remain in scope
- **AND** explicit exclusions `sdd-engram` and `logo` remain excluded from the managed bundle

### Scenario: Variant discovery cache behavior remains available after rename
- **GIVEN** the `lore-models` plugin can obtain provider/model metadata from the OpenCode runtime
- **WHEN** the plugin refreshes discovery data for model or variant selection
- **THEN** it records Lore-owned cache data using the managed cache behavior for later reuse
- **AND** the cache remains metadata-only and not the primary persistence mechanism for user-chosen agent settings

## Requirement 2: Lore-managed agents shall be configurable from inside OpenCode
The `lore-models` plugin SHALL provide an in-OpenCode user flow to choose `model` and `variant` for Lore-managed agents. A floating or dialog selector is preferred where safely available; if the runtime does not provide a supported safe selector API, the plugin SHALL provide a documented fallback interaction that remains entirely inside OpenCode.

### Scenario: Preferred selector is used when supported
- **GIVEN** the active OpenCode runtime exposes a supported safe selector or dialog API to the plugin
- **WHEN** the user opens the Lore model configuration flow inside OpenCode
- **THEN** the plugin presents an in-OpenCode selector flow for agent, model, and variant choices
- **AND** the interaction does not require editing files outside OpenCode by hand

### Scenario: Fallback interaction remains inside OpenCode
- **GIVEN** the active OpenCode runtime does not expose a supported safe selector or dialog API to the plugin
- **WHEN** the user opens the Lore model configuration flow inside OpenCode
- **THEN** the plugin uses a documented fallback interaction inside OpenCode
- **AND** the fallback still allows the user to choose a Lore-managed agent plus its `model` and `variant`
- **AND** the fallback does not require the user to leave OpenCode or directly edit `opencode.json` by hand

## Requirement 3: Model and variant discovery shall prefer OpenCode runtime/provider data
The `lore-models` plugin SHALL discover providers, models, and variants from OpenCode runtime/provider data when that data is available, and SHALL use Lore-owned cached discovery metadata only as a reuse/fallback aid rather than as an authority over the runtime.

### Scenario: Runtime provider data is available
- **GIVEN** the OpenCode runtime can return provider, model, or variant metadata to the plugin
- **WHEN** the plugin prepares selectable choices
- **THEN** it uses the runtime/provider data as the primary source of truth
- **AND** any Lore-owned cache derived from that data reflects the discovered runtime state without inventing unsupported variants

### Scenario: Cached metadata helps when runtime discovery is temporarily unavailable
- **GIVEN** the plugin previously cached provider/model metadata
- **AND** runtime discovery is temporarily unavailable during a later configuration attempt
- **WHEN** the plugin needs to explain or recover from the unavailable runtime data
- **THEN** it may use cached metadata to inform the in-OpenCode experience
- **BUT** it does not present cached data as freshly verified runtime availability

## Requirement 4: Primary persistence shall hot-edit `opencode.json` safely
When a user changes a Lore-managed agent `model` or `variant` from inside OpenCode, the primary persistence behavior SHALL be a safe live edit of `opencode.json` for `agent.<name>.model` and `agent.<name>.variant`. The edit flow SHALL preserve unrelated user-owned config, avoid file corruption, use backup and atomic write protections, and avoid leaking secrets in logs or errors. A Lore-owned override/state file may exist only as a fallback or recovery mechanism and SHALL NOT replace live `opencode.json` editing as the primary path.

### Scenario: Successful hot-edit updates only the selected Lore-managed agent fields
- **GIVEN** `opencode.json` contains user-owned configuration and Lore-managed agent entries
- **WHEN** the user changes the `model` or `variant` for a Lore-managed agent from inside OpenCode
- **THEN** the plugin updates only `agent.<selected>.model` and/or `agent.<selected>.variant` as needed
- **AND** unrelated top-level keys, foreign agents, commands, plugins, and MCP configuration remain preserved

### Scenario: Safe write prevents config corruption
- **GIVEN** the plugin is ready to persist a new model or variant selection into `opencode.json`
- **WHEN** it writes the updated config
- **THEN** it performs the edit through a backup plus atomic replacement flow or equivalent corruption-safe write strategy
- **AND** the result is either a complete valid updated file or the prior file preserved for recovery

### Scenario: Secrets are not exposed during persistence failures
- **GIVEN** `opencode.json` may contain secret-bearing fields such as bearer authorization headers
- **WHEN** a read, merge, validate, backup, or write step fails during hot-edit persistence
- **THEN** logs, errors, and user-facing messages omit raw secret values
- **AND** any diagnostic output names only the affected path or field class needed for safe troubleshooting

## Requirement 5: Reinstall shall preserve user-chosen model and variant values
`lore install --target opencode` SHALL preserve user-chosen Lore agent `model` and `variant` values already present in the effective OpenCode configuration and SHALL NOT reset them to managed defaults unless the user invokes explicit reset semantics.

### Scenario: Reinstall preserves hot-edited values
- **GIVEN** a user previously changed one or more Lore-managed agent `model` or `variant` values from inside OpenCode
- **AND** those values are present in the effective OpenCode configuration before reinstall
- **WHEN** the user runs `lore install --target opencode`
- **THEN** the rendered and merged OpenCode configuration preserves those user-chosen values
- **AND** Lore-managed defaults do not overwrite them during normal reinstall

### Scenario: Explicit reset is the only allowed default-restoring path
- **GIVEN** a user wants to discard prior model or variant choices
- **WHEN** the user invokes an explicit reset semantic defined by Lore for this flow
- **THEN** Lore may restore managed defaults for the affected agent selections
- **AND** normal reinstall behavior continues to preserve user-chosen values when no reset was invoked

## Requirement 6: Generated OpenCode config shall use the revised managed agent contract
Lore-managed OpenCode config generation SHALL keep `lore` as the primary agent, SHALL render all non-lore managed agents with `mode: "subagent"`, SHALL include `lore-worker` as a managed subagent, SHALL remove installer-managed `agent.lore.permission = "allow"`, and SHALL render `model` plus `variant` fields for Lore-managed agents when values are available.

### Scenario: Fresh managed config uses the revised agent overlay
- **GIVEN** Lore renders a fresh native OpenCode configuration
- **WHEN** the agent overlay is generated
- **THEN** `default_agent` is `lore`
- **AND** `agent.lore.mode` is `primary`
- **AND** `agent.lore.permission` is absent
- **AND** each non-lore Lore-managed agent, including `lore-worker`, is rendered with `mode: "subagent"`

### Scenario: Persisted values render into managed agents
- **GIVEN** Lore has effective `model` and `variant` values for one or more Lore-managed agents
- **WHEN** OpenCode config is rendered or merged
- **THEN** those values appear in the corresponding `agent.<name>.model` and `agent.<name>.variant` fields
- **AND** agents without an effective variant do not receive an invented variant value

## Requirement 7: Migration shall delete only stale Lore-managed plugin assets proven by ownership
The OpenCode install flow SHALL clean up stale managed `model-variants` assets only when Lore ownership is proven by the existing managed manifest or equivalent Lore-owned install evidence. It SHALL NOT delete user-owned plugin files based on filename similarity alone.

### Scenario: Manifest-proven stale managed plugin is removed during rename
- **GIVEN** an existing OpenCode install manifest records `plugins/model-variants.ts` as Lore-managed
- **AND** the newly rendered managed plugin set no longer includes that path because it now renders `plugins/lore-models.ts`
- **WHEN** Lore plans or applies the OpenCode install update
- **THEN** it removes the stale `model-variants.ts` file through the managed cleanup flow
- **AND** the cleanup remains scoped to the manifest-proven Lore-managed path

### Scenario: Unknown similarly named plugin file is preserved
- **GIVEN** a plugin file named `model-variants.ts` or similarly named plugin exists in the OpenCode plugins directory
- **AND** no Lore-managed manifest or equivalent install evidence proves Lore owns that file
- **WHEN** Lore performs install migration or cleanup
- **THEN** Lore leaves that file untouched
- **AND** Lore does not infer ownership from the filename alone

## Requirement 8: Error handling and conflict boundaries shall fail safely
The `lore-models` flow and the OpenCode installer SHALL fail safely when configuration data is malformed, Lore encounters a foreign `mcp.lore` block, provider or variant data is unavailable, UI capabilities are unsupported, or file writes fail. Failures SHALL preserve recoverability, keep unrelated user config intact, and communicate the boundary without exposing secrets.

### Scenario: Malformed `opencode.json` blocks live editing safely
- **GIVEN** `opencode.json` is unreadable, malformed, or cannot be merged safely
- **WHEN** the plugin attempts to persist a model or variant change
- **THEN** it aborts the live edit without partially rewriting the file
- **AND** it reports a recoverable error inside OpenCode without exposing raw secret values

### Scenario: Foreign `mcp.lore` ownership boundary is preserved
- **GIVEN** the effective OpenCode config contains a non-Lore-owned `mcp.lore` configuration block
- **WHEN** Lore install or related config generation evaluates managed changes around OpenCode config
- **THEN** Lore preserves the existing foreign ownership boundary behavior
- **AND** the model-selection change does not broaden Lore's authority to overwrite the foreign `mcp.lore` subtree

### Scenario: Provider or variant availability is missing
- **GIVEN** the selected provider, model, or variant list cannot be verified at configuration time
- **WHEN** the user attempts to continue the in-OpenCode model-selection flow
- **THEN** the plugin reports that availability problem inside OpenCode
- **AND** it does not silently persist a fabricated unsupported variant value

### Scenario: Unsupported selector APIs fall back safely
- **GIVEN** the preferred floating or dialog selector path is unsupported or unavailable in the active runtime
- **WHEN** the user invokes the `lore-models` configuration flow
- **THEN** the plugin falls back to a documented in-OpenCode interaction when possible
- **AND** it does not attempt unsafe undocumented UI calls as the only path to configuration

### Scenario: Write failure preserves prior config
- **GIVEN** the plugin has prepared a config update but the backup, temp write, validation, or atomic replace step fails
- **WHEN** the persistence operation aborts
- **THEN** the prior `opencode.json` remains recoverable
- **AND** the partially written update does not become the authoritative active config

## Acceptance Notes
- This change is specification-only in this phase; implementation details remain for design/apply.
- Downstream validation should cover fresh install, upgrade install, reinstall preservation, UI fallback behavior, malformed config handling, secret-safe errors, and manifest-scoped cleanup.
