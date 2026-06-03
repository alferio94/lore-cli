# Apply Partial: opencode-plugin-subagents (repair)

## Completed in This Slice
- [x] FIX-1: Fixed aggregate fail-closed semantics — unknown plugin/runtime/agents findings prevent overall `ready`
- [x] FIX-2: Enforced `AllowTempProbe` option before `TempWritableProbe` calls in probeConfigDir and probePluginsDir
- [x] FIX-3: Updated regression tests to match new fail-closed semantics

## Files Changed So Far
| File | Action | Notes |
|------|--------|-------|
| `internal/opencodeready/types.go` | Modified | aggregate function now checks hasUnknownPluginRuntimeAgents |
| `internal/opencodeready/detector.go` | Modified | probeConfigDir and probePluginsDir now check AllowTempProbe before TempWritableProbe |
| `internal/opencodeready/detector_test.go` | Modified | Updated tests to expect unknown (not blocking) when AllowTempProbe enabled but probe fails |
| `internal/cli/actions_opencode_test.go` | Modified | TestDoctorActionOpenCodeReadiness_DoesNotPanic documents architecture limitation |

## Validation So Far
- `go test ./internal/opencodeready -v -count=1` → PASS (all tests)
- `go test ./internal/cli -run 'TestDoctor|TestInstall|TestOpenCode' -v -count=1` → PASS (all tests)
- `go test ./... -count=1` → PASS (all packages)

## Repair Summary
1. **aggregate fail-closed**: Added `hasUnknownPluginRuntimeAgents` flag that prevents overall `ready` when plugin API, runtime, or native agents are unknown.
2. **AllowTempProbe enforcement**: probeConfigDir and probePluginsDir now only call TempWritableProbe when `opts.AllowTempProbe == true`. When AllowTempProbe is false or probe fails, the finding is `unknown` (not blocking).
3. **Regression tests updated**: All tests that expected blocking for missing dirs with AllowTempProbe now expect unknown (fail-closed).
4. **CLI action test limitation**: TestDoctorActionOpenCodeReadiness_DoesNotPanic documents that actions.go cannot be unit-tested for CLI absence because it passes nil for CommandRunner.

## Recovery Notes
- Safe resume point: verify phase
- All regression tests pass
- No further apply tasks needed for this repair