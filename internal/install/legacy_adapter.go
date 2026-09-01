package install

import "context"

const legacyDeprecationMessage = "legacy install is deprecated; use the canonical route when its target gate is enabled"

// LegacyAdapter is the only compatibility seam for existing target Plan/Execute paths.
// RoutePolicy never invokes it; callers must first select an explicit legacy mode.
type LegacyAdapter interface {
	PrepareLegacy(context.Context, Request) (Prepared, Result)
	ExecuteLegacy(context.Context, Prepared, Observer) Result
}

// PrepareExplicitLegacy validates an already explicit legacy selection without
// consulting or bypassing canonical gates.
func PrepareExplicitLegacy(request Request) (Prepared, Result) {
	result := explicitLegacyResult(request, StatusFailed)
	if request.Mode != ModeLegacyDryRun && request.Mode != ModeLegacyApply || request.Mode == ModeLegacyDryRun && request.AssumeYes {
		result.Error = newInstallError(CodeInvalidWorkflowRequest)
		return Prepared{}, result
	}
	if _, err := ResolveInstallTarget(request.Target); err != nil {
		result.Error = newInstallError(CodeInvalidWorkflowRequest)
		return Prepared{}, result
	}
	result.Status, result.Admitted = StatusReady, true
	return newPrepared(request, RouteLegacy, TransactionPlan{}, result.Guidance), result
}

// ConsumeExplicitLegacy admits one execution of a prepared explicit legacy route.
func ConsumeExplicitLegacy(prepared Prepared) (Request, bool) {
	request := prepared.Request()
	validMode := request.Mode == ModeLegacyDryRun || request.Mode == ModeLegacyApply
	return request, prepared.Route() == RouteLegacy && validMode && prepared.consume()
}

// CompleteExplicitLegacy maps the legacy compatibility result into the shared,
// redacted presentation contract without exposing adapter diagnostics.
func CompleteExplicitLegacy(request Request, succeeded bool) Result {
	status := StatusFailed
	if succeeded {
		status = StatusSucceeded
	}
	result := explicitLegacyResult(request, status)
	result.Admitted = true
	result.ChangedState = succeeded && request.Mode == ModeLegacyApply
	if !succeeded {
		result.Error = newInstallError(CodeLegacyExecutionFailed)
	}
	return result
}

func explicitLegacyResult(request Request, status Status) Result {
	warning := Guidance{Code: "legacy-deprecated", Message: legacyDeprecationMessage}
	return Result{
		SchemaVersion: ResultSchemaVersion,
		Mode:          request.Mode,
		Route:         RouteLegacy,
		Target:        request.Target,
		Status:        status,
		Warnings:      []Guidance{warning},
	}
}
