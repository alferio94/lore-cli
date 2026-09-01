package install

import "context"

// CanonicalWorkflow is the single shared presentation boundary for canonical
// modes. It carries the same sealed plan from Prepare into Execute.
type CanonicalWorkflow struct {
	policy RoutePolicy
	input  TransactionInput
}

func NewCanonicalWorkflow(policy RoutePolicy, input TransactionInput) *CanonicalWorkflow {
	return &CanonicalWorkflow{policy: policy, input: input}
}

func NewExplainWorkflow(policy RoutePolicy, input TransactionInput) *CanonicalWorkflow {
	return NewCanonicalWorkflow(policy, input)
}

func (w *CanonicalWorkflow) Prepare(_ context.Context, request Request) (Prepared, Result) {
	if request.Mode == ModeDryRun {
		return PrepareDryRun(w.policy, request, w.input)
	}
	return PrepareExplain(w.policy, request, w.input)
}

func (w *CanonicalWorkflow) Execute(_ context.Context, prepared Prepared, observer Observer) Result {
	return ExecuteDryRun(prepared, observer)
}

// CodeExplainRejected identifies a canonical Explain rejection without
// disclosing the rejected local facts.
const CodeExplainRejected InstallErrorCode = "explain_rejected"

// PrepareExplain validates the canonical Explain route and seals only the
// already-local W3.3 facts carried by input. It has no effect dependency and
// cannot execute dry-run, apply, legacy, credential, network, or authority
// behavior.
func PrepareExplain(policy RoutePolicy, request Request, input TransactionInput) (Prepared, Result) {
	base := Result{
		SchemaVersion: ResultSchemaVersion,
		Mode:          ModeExplain,
		Route:         RouteCanonical,
		Target:        request.Target,
		Status:        StatusFailed,
	}
	if request.Mode != ModeExplain || request.AssumeYes {
		base.Error = newInstallError(CodeInvalidWorkflowRequest)
		base.Guidance = explainRejectionGuidance()
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
		base.Error = explainRejectedError()
		base.Guidance = append(base.Guidance, explainRejectionGuidance()...)
		return Prepared{}, base
	}
	report := cloneTransactionReport(plan.report)
	facts := input.Semantic.TargetFacts()
	if report.Target != request.Target || facts.Target != request.Target || facts.IRID != report.IRID {
		base.Error = explainRejectedError()
		base.Guidance = append(base.Guidance, explainRejectionGuidance()...)
		return Prepared{}, base
	}

	base.Status = StatusReady
	base.Admitted = true
	base.Report = report
	base.Operations = explainOperations(report)
	base.Guidance = append(base.Guidance, explainServerGuidance(facts.Guidance)...)
	return newPrepared(request, RouteCanonical, plan, base.Guidance), base
}

func explainGateGuidance(gate Gate) []Guidance {
	return []Guidance{{Code: "canonical-gate", Message: string(gate)}}
}

func explainServerGuidance(messages []string) []Guidance {
	guidance := make([]Guidance, len(messages))
	for i, message := range messages {
		guidance[i] = Guidance{Code: "lore-server-guidance", Message: message}
	}
	return guidance
}

func explainOperations(report TransactionReport) []Operation {
	operations := make([]Operation, len(report.Decisions))
	for i, decision := range report.Decisions {
		operations[i] = Operation{Resource: decision.Resource, Action: string(decision.Outcome)}
	}
	return operations
}

func explainRejectionGuidance() []Guidance {
	return []Guidance{{
		Code:    "review-canonical-facts",
		Message: "review profile, capability, ownership, migration, and route facts",
	}}
}

func explainRejectedError() *InstallError {
	return &InstallError{
		code:    CodeExplainRejected,
		path:    "explain.plan",
		message: "canonical explain facts were rejected",
	}
}
