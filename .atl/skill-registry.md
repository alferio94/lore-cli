# Skill Registry

- Source policy: project > pi-global
- Project root: `/Users/alfonsocarmona/personal/lore2/lore-cli`
- Refreshed: 2026-05-25
- Compatibility mode: disabled

## Skills

| Name | Source | Path | Triggers | Priority | Notes |
| --- | --- | --- | --- | --- | --- |
| lore-cli-mvp | project | `.pi/skills/lore-cli-mvp/SKILL.md` | lore-cli MVP scope, auth/config, install flow, runtime target UX | high | Product contract authority for CLI, TUI, and installer behavior |
| lore-cli-go | project | `.pi/skills/lore-cli-go/SKILL.md` | Go implementation, refactors, package boundaries, secret safety | high | Primary Go architecture guidance |
| lore-cli-testing | project | `.pi/skills/lore-cli-testing/SKILL.md` | lore-cli command/config/http/install/TUI tests | high | Overrides generic Go testing guidance |
| sdd-init | pi-global | `~/.pi/agent/skills/sdd-init/SKILL.md` | initialize SDD context | medium | Use with shared SDD protocol |
| sdd-explore | pi-global | `~/.pi/agent/skills/sdd-explore/SKILL.md` | repository investigation before proposal/spec | medium | Default next phase after init |
| sdd-propose | pi-global | `~/.pi/agent/skills/sdd-propose/SKILL.md` | write change proposal | medium | Use after exploration for scoped changes |
| sdd-spec | pi-global | `~/.pi/agent/skills/sdd-spec/SKILL.md` | write delta specs | medium | Required for contract-sensitive changes |
| sdd-design | pi-global | `~/.pi/agent/skills/sdd-design/SKILL.md` | technical design for risky flows | medium | Useful for cross-harness rendering and install decisions |
| sdd-tasks | pi-global | `~/.pi/agent/skills/sdd-tasks/SKILL.md` | break approved change into implementation steps | medium | Prepare bounded apply slices |
| sdd-apply | pi-global | `~/.pi/agent/skills/sdd-apply/SKILL.md` | implement approved tasks | medium | Follow bounded-slice apply protocol |
| sdd-verify | pi-global | `~/.pi/agent/skills/sdd-verify/SKILL.md` | validate implementation against specs and tasks | medium | Prefer focused verification evidence |
| skill-registry | pi-global | `~/.pi/agent/skills/skill-registry/SKILL.md` | create/refresh registry | low | Registry always lives at `.atl/skill-registry.md` |
| go-testing | pi-global | `~/.pi/agent/skills/go-testing/SKILL.md` | generic Go and Bubble Tea testing patterns | low | Fallback only when repo skill is insufficient |
| branch-pr | pi-global | `~/.pi/agent/skills/branch-pr/SKILL.md` | prepare PRs for review | low | Use only when opening a PR |

## Compact Rules

### SDD
- Treat `lore-cli` as the repository root and Lore memory project key as `lore-cli`.
- In Lore mode, persist full English artifacts and return only compact summaries.
- For architecture, contract, persistence, installer, or rollout-sensitive changes, stay in SDD rather than ad hoc edits.
- Do not create `openspec/` while Lore persistence is healthy.

### Product + Architecture
- Keep command parsing thin in `internal/cli`; push behavior into small testable packages.
- The app is a Go CLI plus Bubble Tea TUI with packages for auth, config, HTTP client, install, update, output, versioning, agentpack, and MCP proxy support.
- Never print or persist raw API tokens outside the OS keychain; config and generated agent files remain metadata-only.
- Normalize and validate server URLs before persistence and before client construction.
- Reuse the embedded runtime asset pattern already present under `internal/install/assets/pi/` when extending harness support.

### Install + Agent Runtime
- `install` is Pi-first and reuses healthy saved Lore login state plus `/healthz`, `/readyz`, and `/v1/me` preflight.
- Other runtimes may remain visible but unavailable unless an approved spec explicitly changes that contract.
- Generated harness assets must prefer brokered authenticated calls over embedding raw credentials.
- Portable agent-pack work should centralize persona/workflow/worker/model-routing intent once, then render target-specific config from that source of truth.

### Testing
- Prefer repo-local testing guidance over generic Go testing guidance.
- Use `go test ./...` as the baseline runner and `go test -cover ./...` for coverage.
- Use `httptest.Server`, temp dirs/files, and injected IO/config paths; avoid live Lore server dependencies.
- Keep Bubble Tea/TUI tests deterministic and assert that secrets never appear in output or diagnostics.

## Project Conventions
- Local skills live under `.pi/skills/`.
- Registry lives at `.atl/skill-registry.md`.
- CI release validation runs `go test ./...`, installer syntax validation, and installer smoke tests before building release archives.
- Existing managed runtime assets are embedded under `internal/install/assets/pi/` and installed through `internal/install` services.

## Last Refresh Note
Refreshed after validating the active Go CLI/TUI repo layout, current Pi-first runtime asset embedding, Go built-in test/coverage workflow, installer/release automation, and the portable agent-pack direction for multi-harness config rendering.
