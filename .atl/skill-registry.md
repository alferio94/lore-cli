# Skill Registry

- Source policy: project > pi-global
- Project root: `/Users/alfonsocarmona/personal/lore2/lore-cli`
- Refreshed: 2026-05-17
- Compatibility mode: disabled

## Skills

| Name | Source | Path | Triggers | Priority | Notes |
| --- | --- | --- | --- | --- | --- |
| lore-cli-mvp | project | `.pi/skills/lore-cli-mvp/SKILL.md` | lore-cli MVP scope, auth/config, SDD for `create-lore-cli-mvp` | high | Authoritative product/HTTP guidance |
| lore-cli-go | project | `.pi/skills/lore-cli-go/SKILL.md` | Go scaffolding, command/config/http client structure | high | Use when implementation starts |
| lore-cli-testing | project | `.pi/skills/lore-cli-testing/SKILL.md` | lore-cli tests for config, HTTP, CLI output | high | Overrides generic Go testing guidance |
| sdd-init | pi-global | `~/.pi/agent/skills/sdd-init/SKILL.md` | initialize SDD context for lore-cli repo | medium | Use with shared SDD common protocol |
| sdd-explore | pi-global | `~/.pi/agent/skills/sdd-explore/SKILL.md` | explore `create-lore-cli-mvp` scope and contracts | medium | Use with shared SDD common protocol |
| skill-registry | pi-global | `~/.pi/agent/skills/skill-registry/SKILL.md` | create/refresh registry | medium | Registry always written to `.atl/skill-registry.md` |
| go-testing | pi-global | `~/.pi/agent/skills/go-testing/SKILL.md` | generic Go test patterns | low | Fallback only when lore-cli-testing is insufficient |

## Compact Rules

### SDD
- Treat `lore-cli` as the active project root for this change, even though Lore memory project key remains `lore2`.
- In Lore mode, persist full English artifacts and return only compact handoff summaries.
- For `create-lore-cli-mvp`, refresh `sdd/create-lore-cli-mvp/init` and `sdd/create-lore-cli-mvp/explore` against the current repo state, not the pre-directory workspace state.
- Keep exploration read-only; do not create Go module/code during init/explore.

### Product Scope
- MVP is limited to `login`, `status`, `logout`, and `doctor`.
- Normal login uses an already-issued user API token validated via `GET /v1/me`.
- Do not use `POST /v1/bootstrap/init` for normal CLI login.
- `logout` removes local credentials only unless a later spec adds remote revocation.
- Never print stored tokens or full Authorization headers.

### Implementation Planning
- Plan for a thin Go CLI with small packages for command handling, config, and a typed HTTP client.
- Normalize saved server URLs: require `http`/`https`, trim whitespace, remove trailing slash.
- Keep HTTP scope focused on `GET /healthz`, `GET /readyz`, and authenticated `GET /v1/me` for the MVP.
- Document config storage security tradeoffs explicitly before implementation.

### Testing
- Prefer repo-specific lore-cli testing guidance over generic Go testing guidance.
- Use `httptest.Server`, temp dirs/files, and injected IO/config paths.
- Assert token redaction in stdout, stderr, logs, and errors.
- Do not depend on live Railway or local lore-server for normal tests.

## Project Conventions
- Local skills live under `.pi/skills/`.
- Registry lives at `.atl/skill-registry.md`.
- Current repo state is intentionally minimal: local skills only, no Go module or app code yet.

## Last Refresh Note
Created after the `lore-cli` directory and project-local skills existed, superseding the earlier pre-directory SDD exploration context for `create-lore-cli-mvp`.
