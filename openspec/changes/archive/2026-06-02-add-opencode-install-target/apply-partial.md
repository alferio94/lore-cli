# Apply Partial: add-opencode-install-target

## Completed in This Slice
- [x] 5.1 Focused validation/test expansion review — confirmed existing OpenCode render/merge/backup/idempotency coverage is already in place and added a shared manifest regression test so Antigravity `marker-merge` manifests remain valid alongside the new OpenCode manifest checks.

## Files Changed So Far
| File | Action | Notes |
|------|--------|-------|
| `internal/install/adapter.go` | Modified | Added canonical `MergeModeMarkerMerge` constant instead of ad hoc string use. |
| `internal/install/adapter_antigravity.go` | Modified | Switched Antigravity prompt rendering to the shared merge-mode constant. |
| `internal/install/manifest.go` | Modified | Restored manifest validation compatibility for `marker-merge` managed files. |
| `internal/install/manifest_test.go` | Modified | Added regression coverage proving Antigravity marker-merge manifests still validate. |
| `openspec/changes/add-opencode-install-target/apply-started.md` | Modified | Started Phase 5 slice checkpoint. |

## Validation So Far
- `go test -count=1 ./internal/install -run 'TestOpenCode|TestManifest|TestInstall' -v` → passed before repair; showed OpenCode/install suite already green.
- `gofmt -w internal/install/adapter.go internal/install/adapter_antigravity.go internal/install/manifest.go internal/install/manifest_test.go` → passed

## Remaining in Current Slice
- [ ] Run focused install/CLI/TUI validation after the compatibility repair.
- [ ] Decide whether `go test -count=1 ./...` is appropriate based on focused results and cost.

## Recovery Notes
- Safe resume point: rerun focused install, CLI, and TUI validation; if all pass, mark Phase 5 complete and hand off to verify.
- Known risks/blockers: repository is still dirty; broad validation should stay optional unless focused checks are all green.
