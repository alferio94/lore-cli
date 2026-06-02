# Apply Started: add-opencode-install-target

## Slice
- Tasks in scope: 5.1 Add/expand focused tests in `internal/install/*_test.go` for OpenCode render output, `opencode.json` merge safety, manifest validation, backups, and idempotent reruns; 5.2 Run final focused validation for install, CLI, and TUI, and repair the known Antigravity CLI failure only if this change caused it or if a small manifest-validation compatibility fix is required.
- Tasks explicitly out of scope: any OpenCode scope expansion (plugins, profiles, TUI plugins, bootstrap/package-manager, MCP token persistence, runtime/native subagent claims, command generation beyond already-supported files), unrelated dirty worktree edits, or masking unrelated failures.
- Expected files: `internal/install/manifest.go`, `internal/install/manifest_test.go`, `internal/install/opencode_install_test.go`, `internal/install/adapter_opencode_test.go`, and only the smallest additional test files needed for focused validation/compatibility.
- Validation planned: `go test -count=1 ./internal/install -run 'TestOpenCode|TestManifest|TestInstall' -v`; `go test -count=1 ./internal/cli -run 'TestInstall|TestOpenCode' -v`; `go test -count=1 ./internal/tui -run 'TestInstall' -v`; run `go test -count=1 ./...` only if all focused checks pass and cost remains reasonable.
- Risk budget: medium — repository is already dirty, and the known Antigravity failure may require a surgical shared manifest compatibility repair without broadening change scope.

## Preconditions
- Proposal/spec/design/tasks read: yes.
- Previous apply-progress merged: yes — Phases 1-4 cumulative progress read from Lore and filesystem fallback artifacts.
- Strict TDD mode: inactive
