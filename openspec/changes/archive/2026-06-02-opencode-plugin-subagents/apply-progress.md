# Apply Progress: opencode-plugin-subagents (repair R2)

## Status
- Mode: Standard
- Current slice: completed
- Completed tasks: R2-FIX-1 (aggregate warn prevents ready), R2-FIX-2 (CLI status mapping for unknown), R2-FIX-3 (regression tests)
- Prior repair slices: FIX-1 (aggregate plugin/runtime/agents unknown), FIX-2 (AllowTempProbe enforcement), FIX-3 (regression tests for new semantics)
- Prior foundation slices: 1.1-1.3, 2.1-2.3, 4.1-4.4, 3.1-3.2, 4.5

## Completed Tasks Cumulative

### Repair Slice R2 (this pass)
- [x] R2-FIX-1: aggregate() now treats any warn finding as preventing overall ready, preserving blocking > unknown > warn > ready fail-closed ordering
- [x] R2-FIX-2: openCodeReadinessCheck maps overall unknown to StatusWarn (not StatusOK), ensuring CLI cannot imply full readiness when probe cannot confirm readiness
- [x] R2-FIX-3: Updated regression tests to match new warn-prevents-ready semantics and added test for unknown-status mapping

### Repair Slice R1 (from prior repair)
- [x] FIX-1: Fixed aggregate fail-closed semantics — unknown plugin/runtime/agents findings prevent overall `ready`
- [x] FIX-2: Enforced `AllowTempProbe` option before `TempWritableProbe` calls in probeConfigDir and probePluginsDir
- [x] FIX-3: Updated regression tests to match new fail-closed semantics

### Foundation Slices
- [x] 1.1-1.3: Foundation types and detector package created
- [x] 2.1-2.3: Extended probe behavior (config/plugins dirs, plugin API, runtime, native agents)
- [x] 3.1-3.2: UX integration into doctor/install preflight
- [x] 4.1-4.5: Additional tests and OpenCode install command support

## Files Changed Cumulative
| File | Action | Task(s) | Notes |
|------|--------|---------|-------|
| `internal/opencodeready/types.go` | Modified | R2-FIX-1, FIX-1 | aggregate fail-closed semantics with warn handling |
| `internal/opencodeready/detector.go` | Modified | FIX-2 | AllowTempProbe enforcement in dir probes |
| `internal/opencodeready/detector_test.go` | Modified | R2-FIX-3, FIX-3 | Regression tests for new semantics |
| `internal/cli/actions.go` | Modified | 3.x, R2-FIX-2 | openCodeReadinessCheck integration with status mapping |
| `internal/cli/actions_opencode_test.go` | Created | 4.5, R2-FIX-3 | OpenCode CLI integration tests |
| `internal/opencodeready/helpers.go` | Created | 1.x | runCommand, tempWritableProbe |
| `internal/cli/app.go` | Modified | 3.x | doctor action integration |
| `internal/install/adapter_opencode.go` | Modified | 4.x | OpenCode install support |

## Validation Cumulative
| Command | Scope | Result | Notes |
|---------|-------|--------|-------|
| `go test ./internal/opencodeready -v -count=1` | Unit tests | PASS | All tests pass including new warn-prevents-ready tests |
| `go test ./internal/cli -run 'TestDoctor|TestInstall|TestOpenCode' -v -count=1` | Integration | PASS | All tests pass including new unknown-status-mapping test |
| `go test ./... -count=1` | Broad | PASS | All packages pass |

## Deviations and Risks
- **No deviations**: All repair targets addressed; aggregate now correctly prevents ready when any warn or unknown finding exists
- **Architecture limitation**: actions.go passes nil for CommandRunner, so CLI absence cannot be unit-tested via LookPath mock. This is documented in the test file.

## Repair Summary for R2
1. **R2-FIX-1**: aggregate() now treats warn findings as preventing overall ready. The ordering is now: blocking > unknown > warn > ready. This ensures that warn findings (like missing-but-creatable dirs) cannot overstate readiness.
2. **R2-FIX-2**: openCodeReadinessCheck maps `report.Overall == StatusUnknown` to `output.StatusWarn`, not `output.StatusOK`. This ensures CLI status never implies full readiness when the probe cannot confirm readiness.
3. **R2-FIX-3**: Updated test cases to match new semantics and added `TestOpenCodeReadinessCheck_StatusMapping_UnknownNotOK` to verify the CLI mapping.

## Next Slice Recommendation
- Proceed to verify phase to validate the full repair and confirm archive readiness
- No further apply tasks for this change