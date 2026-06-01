# Tasks: codex-sdd-agent-config

Out of scope: Codex runner execution, live subagent invocation, per-harness configurators, Claude adapter, and any auth/token persistence changes.

## Phase 5: Pre-Archive Cleanup
- [x] C1.1 Refactor `internal/agentconfig/store.go` to reuse `internal/config.ResolveDir()` instead of duplicating config-dir resolution.
- [x] C2.1 Deferred artifact cleanup: Lore write API unavailable; stale tasks observation (`76c81e9f-d20d-4f11-a071-d8ba4e5c8683`) cannot be updated. Canonical tasks are in OpenSpec `tasks.md` and Lore artifact `4435a338-3a10-4eb0-8499-6ebacbec6b40`. Archive phase should add a note identifying the stale duplicate to ignore.

## Phase 1: Core contract
- [x] 1.1 Create `internal/agentconfig/config.go` and `store.go` for `agent-config.json` path resolution, schema v1, load/save/ensure-default, and strict validation of known SDD agents plus nonblank models.
- [x] 1.2 Export canonical SDD agent names and `DefaultSDDModel = "gpt-5.4"` from `internal/agentpack/definition.go` and `defaults.go`; keep `config.json` auth-owned.

## Phase 2: Deterministic persistence
- [x] 2.1 Add canonical JSON ordering/rendering in `internal/agentconfig` so repeated saves are byte-stable, version-aware, and idempotent across equivalent logical input.
- [x] 2.2 Add unit tests in `internal/agentconfig/*_test.go` for default generation, malformed JSON, unknown agent keys, blank models, and rewrite stability.

## Phase 3: Minimal CLI touchpoints
- [x] 3.1 Wire read-only agent-config presence/validation into `internal/cli/actions.go` and `internal/install/service.go` so status/doctor/install summaries can report path + validity only.
- [x] 3.2 Update `internal/cli/app.go` and `README.md` to describe `agent-config.json` as a sibling to `config.json` and state the config-only boundary.

## Phase 4: Integration verification
- [x] 4.1 Add tests in `internal/install/service_test.go` proving auth rewrites leave `agent-config.json` intact and diagnostics stay secret-free.
- [x] 4.2 Add `internal/agentconfig/store_test.go` and CLI/install tests for path override, ensure-default idempotence, and validation reporting without implying Codex support.
