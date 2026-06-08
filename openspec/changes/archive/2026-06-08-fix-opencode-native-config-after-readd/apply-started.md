# Apply Started (Repair): fix-opencode-native-config-after-readd

## Slice
- Tasks in scope (REPAIR — docs/mark reconciliation only, no code/behavior changes):
  - Update `README.md` to describe the native OpenCode contract:
    - `opencode.json` uses `$schema: https://opencode.ai/config.json`, `theme: system`, native `agent` overlay wiring every SDD phase to its `~/.config/opencode/skills/<name>/SKILL.md` prompt via `{file:...}` references, native `skills` block, and (when `lore-server-mcp` is selected) the documented top-level `mcp.lore` remote entry with `type: remote`, normalized `<lore_url>/v1/mcp` URL, `headers.Authorization = Bearer <token>`, and `managed_by: lore-cli` marker.
    - NO top-level Lore-only `lore` metadata block in `opencode.json`.
    - `tui.json` uses `$schema: https://opencode.ai/tui.json`, `theme: system`, and a singular `plugin` string array that contains only the community `opencode-subagent-statusline`.
    - Legacy broken shapes (top-level `lore` in `opencode.json`; top-level `lore` and plural `plugins` array of objects in `tui.json`) are silently repaired to the native shape on the next run; user-owned top-level keys (e.g. `theme`, custom `mcp.<other>`, custom `agent.<other>` overrides) are preserved.
  - Update `openspec/changes/fix-opencode-native-config-after-readd/tasks.md` to mark all 10 tasks `[x]` consistent with the prior slice's `apply-report.md` (1.1, 1.2, 1.3, 2.1, 2.2, 3.1, 3.2, 3.3, 4.1, 4.2).
- Tasks explicitly out of scope (deferred to follow-up slices):
  - Any code change in `internal/install/`.
  - Any change in install behavior, renderer, asset tree, or service description.
  - `service.go` / `renderOpenCodeAgentsMD` copy (already updated in the prior slice).
- Expected files:
  - `README.md`
  - `openspec/changes/fix-opencode-native-config-after-readd/tasks.md`
  - `openspec/changes/fix-opencode-native-config-after-readd/apply-started.md` (this file)
  - `openspec/changes/fix-opencode-native-config-after-readd/apply-partial.md`
  - `openspec/changes/fix-opencode-native-config-after-readd/apply-progress.md` (cumulative, merged with prior slice)
  - `openspec/changes/fix-opencode-native-config-after-readd/apply-report.md` (latest slice report)
- Validation planned:
  - `go test ./internal/install ./internal/cli ./internal/tui -count=1` (user-acceptable scope).
- Risk budget: low — docs/mark-only, no code or behavior mutation. The native-shape contract is unchanged; only the user-facing README and the OpenSpec `tasks.md` checkboxes move.

## Preconditions
- Proposal/spec/design/tasks read: yes (via tasks.md + adapter_opencode.go + json_merge.go + assets/opencode/tui.json + service.go + prior apply-progress.md + prior apply-report.md)
- Previous apply-progress merged: yes (prior slice left 1.1, 1.2, 1.3, 2.1, 2.2, 3.1, 3.2, 3.3, 4.1, 4.2 marked complete in apply-progress/apply-report, but the user-facing README and the OpenSpec `tasks.md` checkboxes were not reconciled — that is the bounded repair target).
- Strict TDD mode: inactive (no TDD module declared; this is a docs/mark-only repair slice).
- Lore persistence: unavailable in this runtime (no `lore_*` MCP tools exposed; using OpenSpec file fallback per skill-registry guidance).
