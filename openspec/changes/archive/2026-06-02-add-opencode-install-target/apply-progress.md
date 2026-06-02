# Apply Progress: add-opencode-install-target

## Status
- Mode: Standard
- Current slice: completed
- Completed tasks: 10/10

## Completed Tasks Cumulative
- [x] 1.1 Add `opencodeAdapter` registration and supported-target metadata — registered OpenCode in the default install registry and updated user-facing target metadata/tests.
- [x] 1.2 Add `ResolveOpenCodeLayout` and OpenCode root/component constants — added layout scaffolding, path constants, and explicit bounded groundwork paths.
- [x] 2.1 Render `~/.config/opencode/AGENTS.md` and `skills/<name>/SKILL.md` from `agentpack` + `agentconfig` — added deterministic AGENTS and managed/extended skill rendering with OpenCode-specific skill path projection.
- [x] 2.2 Add optional `commands/*.md` rendering and a Lore-owned `opencode.json` block — commands now omit by default and fail closed without an approved explicit boundary, while `opencode.json` merges preserve unrelated user JSON and reject ambiguous Lore ownership.
- [x] 3.1 Implement plan/apply actions for create/update/unchanged files and backup-before-overwrite behavior — added config-only OpenCode plan/execute flows with deterministic actions, additive `opencode.json` handling, and backup-before-overwrite behavior for managed files.
- [x] 3.2 Record manifest entries plus path/hash/merge validation — OpenCode manifests now store hashes for final desired content (including merged `opencode.json`) and shared manifest validation now rejects duplicate paths, preserves Antigravity marker-merge compatibility, and requires content hashes.
- [x] 4.1 Expose OpenCode in CLI/TUI and summaries — added OpenCode CLI install routing plus target-specific dry-run/apply summaries and selectable install copy without roadmap wording.
- [x] 4.2 Update README for bounded OpenCode support — documented approved OpenCode files, backup-before-overwrite behavior, and explicit non-goals without implying commands/plugins/profiles/MCP persistence.
- [x] 5.1 Add/expand focused tests — confirmed OpenCode-focused render/merge/backup/idempotency coverage and added a shared manifest regression test for Antigravity marker-merge compatibility.
- [x] 5.2 Run final focused validation and optional broad validation — focused install/CLI/TUI suites all passed, so repo-wide `go test -count=1 ./...` was run and passed.

## Files Changed Cumulative
| File | Action | Task(s) | Notes |
|------|--------|---------|-------|
| `internal/install/adapter.go` | Modified | 1.1, 5.1 | Registered OpenCode and added shared `MergeModeMarkerMerge` constant. |
| `internal/install/service.go` | Modified | 1.1, 4.1 | Added bounded OpenCode target descriptions and shared target-selection copy. |
| `internal/install/adapter_opencode.go` | Created/Modified | 1.1, 1.2, 2.1 | Added adapter metadata, layout resolution, AGENTS rendering, and managed skill projection. |
| `internal/install/opencode_install.go` | Created/Modified | 1.2, 2.2, 3.1, 3.2 | Added OpenCode `opencode.json` ownership/merge helpers plus plan/apply, backups, and manifest builders. |
| `internal/install/manifest.go` | Modified | 3.2, 5.1 | Added shared managed-file validation for path uniqueness/content hashes and restored Antigravity marker-merge compatibility. |
| `internal/install/adapter_antigravity.go` | Modified | 5.1 | Switched prompt rendering to the shared marker-merge constant. |
| `internal/cli/actions.go` | Modified | 4.1 | Added OpenCode install execution path plus OpenCode-specific dry-run/apply summary formatting. |
| `internal/cli/app.go` | Modified | 4.1 | Updated install help text and target/component guidance for bounded OpenCode support. |
| `internal/tui/root.go` | Modified | 4.1 | Removed roadmap wording from the install menu and target-selection copy for OpenCode. |
| `README.md` | Modified | 4.2 | Documented approved OpenCode files, backup-before-overwrite behavior, and explicit non-goals. |
| `internal/install/adapter_test.go` | Modified | 1.1 | Updated default selection and registry expectations for OpenCode. |
| `internal/install/service_test.go` | Modified | 1.1, 4.1 | Updated target availability and shared target-selection wording expectations for OpenCode. |
| `internal/install/adapter_opencode_test.go` | Created/Modified | 1.2, 2.1, 2.2 | Added OpenCode layout, render, commands, and merge safety tests. |
| `internal/install/opencode_install_test.go` | Created | 3.1, 3.2 | Added focused OpenCode plan/execute/manifest/backup/idempotency coverage. |
| `internal/install/manifest_test.go` | Modified | 5.1 | Added regression coverage proving Antigravity marker-merge manifests still validate. |
| `internal/cli/actions_test.go` | Modified | 4.1 | Added OpenCode summary regression coverage. |
| `internal/cli/install_flags_test.go` | Modified | 4.1 | Added focused OpenCode dry-run/apply coverage and help-text expectations. |
| `internal/cli/app_test.go` | Modified | 4.1 | Updated install help expectation for supported OpenCode wording. |
| `internal/tui/model_test.go` | Modified | 4.1 | Updated install-target navigation/message expectations now that OpenCode is selectable. |
| `openspec/changes/add-opencode-install-target/tasks.md` | Modified | 1.1-5.2 | Filesystem fallback checkpoint for cumulative task completion. |

## Validation Cumulative
| Command | Scope | Result | Notes |
|---------|-------|--------|-------|
| `gofmt -w internal/install/adapter.go internal/install/service.go internal/install/adapter_opencode.go internal/install/opencode_install.go internal/install/adapter_test.go internal/install/service_test.go internal/install/adapter_opencode_test.go` | formatting | passed | Phase 1 groundwork files. |
| `go test ./internal/install -run 'Test(DefaultTargets|ResolveInstallTarget|FormatTargetSelection|CheckAgentConfig|RegistryResolveReturnsTargetAdapterAndCapabilities|ResolveOpenCodeLayout)' -v` | focused install tests | passed | Covers target metadata and layout groundwork. |
| `go test ./internal/install -run 'Test(OpenCodeAdapterRenderFailsClosedUntilLaterSlices|ResolveOpenCodeLayout)' -v` | focused OpenCode groundwork | passed | Prior slice fail-closed evidence before rendering existed. |
| `gofmt -w internal/install/adapter_opencode.go internal/install/opencode_install.go internal/install/adapter_opencode_test.go` | formatting | passed | Phase 2 render/merge files. |
| `go test ./internal/install -run 'TestOpenCode.*(Render|AgentConfig|Commands|Merge)' -v` | focused OpenCode render/merge tests | passed | Covers AGENTS/skills rendering, custom agent models, command omission, and `opencode.json` merge safety. |
| `gofmt -w internal/install/opencode_install.go internal/install/manifest.go internal/install/opencode_install_test.go` | formatting | passed | Phase 3 plan/apply/manifest files. |
| `go test ./internal/install -run 'Test(OpenCode.*(Plan|Execute|Manifest|Backup|Idempotent|Merge))' -v` | focused OpenCode apply tests | passed | Covers plan/apply actions, backups, manifest validation, merged-hash recording, and idempotent reruns. |
| `gofmt -w internal/cli/actions.go internal/cli/app.go internal/tui/root.go internal/install/service.go internal/cli/actions_test.go internal/cli/install_flags_test.go internal/install/service_test.go internal/tui/model_test.go internal/cli/app_test.go` | formatting | passed | Phase 4 CLI/TUI/docs files. |
| `go test ./internal/cli -run 'TestInstall|TestOpenCode' -v` | focused CLI tests | failed then passed | Initial failure exposed Antigravity `marker-merge` manifest validation regression; after the compatibility repair, the suite passed cleanly. |
| `go test ./internal/tui -run 'TestInstall' -v` | focused TUI tests | passed | Install menu copy and supported-target navigation remain green with OpenCode selectable. |
| `gofmt -w internal/install/adapter.go internal/install/adapter_antigravity.go internal/install/manifest.go internal/install/manifest_test.go` | formatting | passed | Phase 5 compatibility repair files. |
| `go test -count=1 ./internal/install -run 'TestOpenCode|TestManifest|TestInstall' -v` | focused install tests | passed | OpenCode and shared manifest/install suites passed with marker-merge regression coverage. |
| `go test -count=1 ./internal/cli -run 'TestInstall|TestOpenCode' -v` | focused CLI tests | passed | Confirms the Antigravity failure was a shared manifest regression repaired in this slice. |
| `go test -count=1 ./internal/tui -run 'TestInstall' -v` | focused TUI tests | passed | No TUI regression after final validation. |
| `go test -count=1 ./...` | repository-wide tests | passed | Focused checks passed and cost remained reasonable, so broad validation was run before handoff to verify. |

## Deviations and Risks
- Shared manifest validation now explicitly accepts Antigravity `marker-merge`; future merge-mode additions still need explicit validation coverage.
- Repository remains dirty with in-flight files for this change and adjacent work, so verify should use the persisted artifacts plus focused diff context.
- OpenCode commands remain intentionally omitted until a dedicated approved command asset boundary exists.

## Next Slice Recommendation
- Tasks: none
- Why these next: apply scope is complete; the next phase should verify the finished change against spec/design/tasks.
