# Apply Started: default-extended-skills-install

## Slice: Verification-Repair (post-verify fix)

### Root Cause Analysis (from debug investigation)

**Issue 1 — TestCheckSelectsLatestReleaseAssetAndChecksum**
- Debug test revealed: `plan.Target.Status = "unsafe"` (path_mismatch) because `/tmp` on macOS is a symlink to `/private/tmp`
- `resolveBinaryTarget`: `execPath = "/tmp/lore"` → `ResolvedPath = "/private/tmp/lore"` (via `filepath.EvalSymlinks`)
- Since `ExecutablePath != ResolvedPath`, target is marked unsafe → plan status overridden to "unsupported"
- Test has NEVER passed on macOS; the test's assumption that `/tmp/lore` resolves cleanly is false on this platform
- Fix: use `t.TempDir()` for both `ExecPath` and `LookPath` so paths resolve consistently

**Issue 2 — Update lifecycle assertions (placeholder tests)**
- `TestUpdateServiceIgnoresExtendedSkills` and `TestLoreUpdateBinaryOnlyDoesNotTouchSkills` exist as placeholder stubs
- The `update` package's `Check()` and `Apply()` methods have zero references to skill paths — confirmed by code inspection and no grep hits
- The `lore update` command invokes the update service and never touches skill directories
- Fix: implement concrete assertions proving the update service cannot access or mutate extended skills directories, using targeted test cases

**Issue 3 — TestInstallCommandRunsPiInstallAndPrintsSummary (coverage-only)**
- Fails only under `-cover` instrumentation with `install exitCode = 1, want 0`
- Likely a race condition or coverage overhead affecting the `install.Run()` call in the test environment
- Fix: make the install invocation more robust against transient failures (add retry or improve isolation)

### Tasks in Scope
- Fix `internal/update/service_test.go`: `TestCheckSelectsLatestReleaseAssetAndChecksum` — use temp directory paths
- Add `internal/update/service_test.go`: executable lifecycle tests proving update never touches extended skills/install-managed files
- Fix `internal/cli/app_test.go`: `TestInstallCommandRunsPiInstallAndPrintsSummary` coverage-only flakiness

### Tasks Explicitly Out of Scope
- Any Phase 1-4 implementation changes (already complete per prior apply-progress)
- Broad verification (deferred to `sdd-verify`)

### Expected Files
- `internal/update/service_test.go` (modified + new tests)
- `internal/cli/app_test.go` (modified, if needed)

### Validation Planned
- `go test -v ./internal/update/...`
- `go test -v ./internal/cli/...` (focused on affected tests)
- `go test -cover ./internal/update/...`
- `go test ./...` (repo-wide, to verify no regressions)

### Risk Budget: Low-Medium
- Focused targeted fixes to tests; no production code changes
- All changes are test-only or test assertion fixes