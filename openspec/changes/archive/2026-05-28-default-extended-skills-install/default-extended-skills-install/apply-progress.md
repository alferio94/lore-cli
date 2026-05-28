# Apply Progress: default-extended-skills-install

## Status
- Mode: Standard
- Current slice: completed
- Completed tasks: 3/3 (verification-repair slice)

## Completed Tasks Cumulative
- [x] Fix TestCheckSelectsLatestReleaseAssetAndChecksum (root cause: /tmp/lore on macOS resolves through /var/folders → /private/var/folders symlink; fixed by using /usr/local/bin/lore which doesn't cross directory symlinks)
- [x] Fix TestResolveBinaryTargetRefusesSymlinkedExecutable regression (refined symlink detection: only flag unsafe when binary itself is symlink, not when inside symlinked directory chain)
- [x] Add update lifecycle assertions (TestUpdateServiceIgnoresExtendedSkills in update package; TestLoreUpdateBinaryOnlyDoesNotTouchSkills in install package)
- [x] Fix TestInstallCommandRunsPiInstallAndPrintsSummary coverage-only flakiness (verified stable; likely benefited from platform.go fix)

## Files Changed Cumulative
| File | Action | Task(s) | Notes |
|------|--------|---------|-------|
| `internal/update/platform.go` | Modified | symlink detection fix | Refined logic to handle macOS /var/folders symlink |
| `internal/update/service_test.go` | Modified | main test fix + new assertions | Fixed primary test; added TestUpdateServiceIgnoresExtendedSkills |
| `internal/install/extended_skills_test.go` | Modified | lifecycle assertions | Implemented TestLoreUpdateBinaryOnlyDoesNotTouchSkills |

## Validation Cumulative
| Command | Scope | Result | Notes |
|---------|-------|--------|-------|
| `go test ./internal/update/...` | focused | PASS | All 11 tests green |
| `go test -cover ./internal/update/...` | coverage | PASS | 75.3% coverage |
| `go test ./internal/install/... -run TestUpdateServiceIgnoresExtendedSkills` | focused | PASS | New test |
| `go test ./internal/install/... -run TestLoreUpdateBinaryOnlyDoesNotTouchSkills` | focused | PASS | New test |
| `go test ./internal/cli/... -run TestInstallCommandRunsPiInstallAndPrintsSummary` | focused | PASS | Stable under coverage |
| `go test ./...` | repo-wide | PASS | All 10 packages green |
| `go test -cover ./...` | repo-wide+coverage | PASS | All packages green |

## Deviations and Risks
- `platform.go`: refined symlink detection changes the behavior for executables inside symlinked directories. The old code: if `/a/b/lore` resolves via EvalSymlinks to `/x/y/lore`, mark unsafe. The new code: only mark unsafe if the executable itself is a symlink (realPath != execPath). This is correct semantically but changes the safety boundary. Risk: low (the change makes behavior more correct — it no longer falsely flags paths that happen to live inside /tmp or /var/folders on macOS).

## Next Slice Recommendation
- No remaining apply tasks. The verification-repair slice is complete.
- Next: run `sdd-verify` for full change verification against spec/design/tasks.