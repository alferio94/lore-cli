# Apply Started: opencode-plugin-subagents (repair)

## Slice: Verify failures repair

### Tasks in scope
- FIX-1: Fix aggregate fail-closed semantics — unknown plugin/runtime/agents findings prevent overall `ready`
- FIX-2: Enforce `AllowTempProbe` option before `TempWritableProbe` calls in probeConfigDir and probePluginsDir

### Tasks explicitly out of scope
- No integration changes
- No new tests for existing working behavior
- No documentation changes

### Expected files
- `internal/opencodeready/types.go` — aggregate function fix
- `internal/opencodeready/detector.go` — AllowTempProbe enforcement in probeConfigDir and probePluginsDir
- `internal/opencodeready/detector_test.go` — new regression tests

### Validation planned
- `go test ./internal/opencodeready -v -count=1`
- `go test ./internal/cli -run 'TestDoctor|TestInstall|TestOpenCode' -v -count=1`

### Risk budget
- Low — targeted fixes to aggregation and option enforcement
- No architectural changes
- Regression tests will verify existing behavior preserved