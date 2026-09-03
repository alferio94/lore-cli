package install

// CodeDryRunRejected identifies canonical facts that could not be sealed for a
// zero-effect dry-run.
const CodeDryRunRejected InstallErrorCode = "dry_run_rejected"

// PrepareDryRun admits and seals the exact canonical plan later consumed by
// ExecuteDryRun. It has no effect dependency and performs no confirmation,
// credential, authority, finalizer, persistence, or mutation work.
func PrepareDryRun(policy RoutePolicy, request Request, input TransactionInput) (Prepared, Result) {
	base := Result{
		SchemaVersion: ResultSchemaVersion,
		Mode:          ModeDryRun,
		Route:         RouteCanonical,
		Target:        request.Target,
		Status:        StatusFailed,
	}
	if request.Mode != ModeDryRun || request.AssumeYes {
		base.Error = newInstallError(CodeInvalidWorkflowRequest)
		return Prepared{}, base
	}
	decision, err := policy.Decide(request)
	base.Guidance = explainGateGuidance(decision.Gate)
	if err != nil {
		if routeError, ok := err.(*InstallError); ok {
			base.Error = routeError
		} else {
			base.Error = newInstallError(CodeInvalidWorkflowRequest)
		}
		return Prepared{}, base
	}
	plan, err := SealTransactionPlan(input)
	if err != nil || plan.IsZero() {
		base.Error = dryRunRejectedError()
		base.Guidance = append(base.Guidance, explainRejectionGuidance()...)
		return Prepared{}, base
	}
	report := plan.DryRun(nil)
	facts := input.Semantic.TargetFacts()
	if report.Target != request.Target || facts.Target != request.Target || facts.IRID != report.IRID {
		base.Error = dryRunRejectedError()
		base.Guidance = append(base.Guidance, explainRejectionGuidance()...)
		return Prepared{}, base
	}
	base.Status, base.Admitted, base.Report = StatusReady, true, report
	base.Operations = explainOperations(report)
	base.Guidance = append(base.Guidance, explainServerGuidance(facts.Guidance)...)
	return newPrepared(request, RouteCanonical, plan, base.Guidance), base
}

// ExecuteDryRun consumes one sealed Prepared and reports only deterministic
// prospective facts from that same plan.
func ExecuteDryRun(prepared Prepared, observer Observer) Result {
	request := prepared.Request()
	base := Result{SchemaVersion: ResultSchemaVersion, Mode: ModeDryRun, Route: RouteCanonical, Target: request.Target, Status: StatusFailed}
	if !prepared.consume() || request.Mode != ModeDryRun || request.AssumeYes || prepared.Route() != RouteCanonical || prepared.plan.IsZero() {
		base.Error = newInstallError(CodeInvalidWorkflowRequest)
		return base
	}
	report := prepared.plan.DryRun(nil)
	if report.Target != request.Target || report.MutationCount != 0 {
		base.Error = newInstallError(CodeInvalidWorkflowRequest)
		return base
	}
	events := []Event{
		{Seq: 1, Phase: PhasePrepare, Kind: EventStarted, Progress: Progress{Total: 2}},
		{Seq: 2, Phase: PhasePrepare, Kind: EventCompleted, Progress: Progress{Completed: 1, Total: 2}},
		{Seq: 3, Phase: PhaseSeal, Kind: EventStarted, Progress: Progress{Completed: 1, Total: 2}},
		{Seq: 4, Phase: PhaseSeal, Kind: EventCompleted, Progress: Progress{Completed: 2, Total: 2}},
	}
	for _, event := range events {
		observeDryRun(observer, event)
	}
	base.Status, base.Admitted, base.Report = StatusSucceeded, true, report
	base.Operations = explainOperations(report)
	base.Guidance = append([]Guidance(nil), prepared.guidance...)
	return base
}

func observeDryRun(observer Observer, event Event) {
	if observer == nil {
		return
	}
	defer func() { _ = recover() }()
	observer.Observe(event.Clone())
}

func dryRunRejectedError() *InstallError {
	return &InstallError{code: CodeDryRunRejected, path: "dry_run.plan", message: "canonical dry-run facts were rejected"}
}
