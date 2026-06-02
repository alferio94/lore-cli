# Tasks: add-opencode-install-target

> Fallback local checkpoint because Lore task artifact retrieval/update was unavailable in this slice.

## Phase 1: OpenCode groundwork

- [x] 1.1 Add `opencodeAdapter` registration and supported-target metadata in `internal/install/adapter.go`, `service.go`, `components.go`, and `harness.go`.
- [x] 1.2 Add `ResolveOpenCodeLayout` and OpenCode root/component constants in `internal/install/adapter_opencode.go` and `internal/install/opencode_install.go`.

## Phase 2: OpenCode rendering and owned JSON block

- [x] 2.1 Render `~/.config/opencode/AGENTS.md` and `skills/<name>/SKILL.md` from `agentpack` + `agentconfig` in `internal/install/adapter_opencode.go`.
- [x] 2.2 Add optional `commands/*.md` rendering omission/fail-closed behavior and an additive Lore-owned `opencode.json` block in `internal/install/opencode_install.go`.

## Phase 3: OpenCode plan/apply, manifest, backups, idempotency

- [x] 3.1 Implement plan/apply actions for create/update/unchanged files and backup-before-overwrite behavior for `AGENTS.md`, skills, commands if any, and `opencode.json`.
- [x] 3.2 Record manifest entries plus path/hash/merge validation in `internal/install/manifest.go` and `internal/install/opencode_install.go` so reruns stay stable.

## Phase 4: CLI/TUI exposure and docs

- [x] 4.1 Expose OpenCode in `internal/cli/actions.go`, `internal/cli/app.go`, and `internal/tui/root.go`; update dry-run/apply summaries and target copy to remove roadmap wording.
- [x] 4.2 Update `README.md` to describe bounded OpenCode support, approved files, backup behavior, and explicit non-goals.
Validation: `go test ./internal/cli -run 'TestInstall|TestOpenCode' -v` and `go test ./internal/tui -run 'TestInstall' -v`

## Phase 5: Tests and final validation

- [x] 5.1 Add/expand focused tests in `internal/install/*_test.go` for render output, `opencode.json` merge safety, manifest validation, backups, and idempotent reruns.
- [x] 5.2 Run final focused validation for install, CLI, and TUI, then `go test ./...` only if the focused checks pass.
Validation: `go test -count=1 ./internal/install -run 'TestOpenCode|TestManifest|TestInstall' -v` then `go test -count=1 ./...`
