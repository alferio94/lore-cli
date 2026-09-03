package install

import (
	"context"
	"errors"
	"reflect"
)

const (
	CodeApplyRejected        InstallErrorCode = "apply_rejected"
	CodeConfirmationRequired InstallErrorCode = "confirmation_required"
)

type CredentialResolverFunc func(provider, slot string) ([]byte, error)

func (f CredentialResolverFunc) Resolve(provider, slot string) ([]byte, error) {
	return f(provider, slot)
}

type CanonicalApplyOptions struct {
	Store             ProfileStore
	Project           PreparedProject
	ResolveCredential CredentialResolverFunc
}
type canonicalApplyRuntime struct {
	CanonicalApplyOptions
	fail transactionFSFailpoint
}

func NewCanonicalApplyWorkflow(policy RoutePolicy, input TransactionInput, options CanonicalApplyOptions) *CanonicalWorkflow {
	workflow := NewCanonicalWorkflow(policy, input)
	workflow.apply = &canonicalApplyRuntime{CanonicalApplyOptions: options}
	return workflow
}
func PrepareApply(policy RoutePolicy, request Request, input TransactionInput) (Prepared, Result) {
	base := Result{SchemaVersion: ResultSchemaVersion, Mode: ModeApply, Route: RouteCanonical, Target: request.Target, Status: StatusFailed}
	if request.Mode != ModeApply {
		base.Error = newInstallError(CodeInvalidWorkflowRequest)
		return Prepared{}, base
	}
	decision, err := policy.Decide(request)
	base.Guidance = explainGateGuidance(decision.Gate)
	if err != nil {
		base.Error, _ = err.(*InstallError)
		if base.Error == nil {
			base.Error = newInstallError(CodeInvalidWorkflowRequest)
		}
		return Prepared{}, base
	}
	plan, err := SealTransactionPlan(input)
	if err != nil || plan.IsZero() {
		base.Error = applyRejectedError()
		return Prepared{}, base
	}
	report := plan.DryRun(nil)
	facts := input.Semantic.TargetFacts()
	if report.Target != request.Target || facts.Target != request.Target || facts.IRID != report.IRID || !reflect.DeepEqual(report.Profile, input.ProfileCompletion) {
		base.Error = applyRejectedError()
		return Prepared{}, base
	}
	base.Status, base.Admitted, base.Report = StatusReady, true, report
	base.Operations = explainOperations(report)
	base.Guidance = append(base.Guidance, explainServerGuidance(facts.Guidance)...)
	return newPrepared(request, RouteCanonical, plan, base.Guidance), base
}
func (w *CanonicalWorkflow) executeApply(ctx context.Context, prepared Prepared, observer Observer) Result {
	request := prepared.Request()
	base := Result{SchemaVersion: ResultSchemaVersion, Mode: ModeApply, Route: RouteCanonical, Target: request.Target, Status: StatusFailed}
	if !prepared.consume() || request.Mode != ModeApply || prepared.Route() != RouteCanonical || prepared.plan.IsZero() || w.apply == nil || w.apply.ResolveCredential == nil {
		base.Error = newInstallError(CodeInvalidWorkflowRequest)
		return base
	}
	if _, err := w.policy.Decide(request); err != nil {
		base.Error, _ = err.(*InstallError)
		if base.Error == nil {
			base.Error = newInstallError(CodeInvalidWorkflowRequest)
		}
		return base
	}
	resealed, err := SealTransactionPlan(w.input)
	if err != nil || prepared.plan.DryRun(nil).Target != request.Target || !reflect.DeepEqual(prepared.plan.DryRun(nil), resealed.DryRun(nil)) || w.apply.Store.Path() == "" || w.apply.Project.path != w.apply.Store.Path() || w.apply.Project.id != w.input.ProfileCompletion.ProjectID {
		base.Error = applyRejectedError()
		return base
	}
	base.Admitted, base.Report = true, resealed.DryRun(nil)
	base.Operations, base.Guidance = explainOperations(base.Report), append([]Guidance(nil), prepared.guidance...)
	if ctx.Err() != nil {
		base.Status, base.Interrupted = StatusCancelled, true
		return base
	}
	emitApply(observer, Event{Seq: 1, Phase: PhasePrepare, Kind: EventCompleted, Progress: Progress{Completed: 1, Total: 7}})
	emitApply(observer, Event{Seq: 2, Phase: PhaseSeal, Kind: EventCompleted, Progress: Progress{Completed: 2, Total: 7}})
	emitApply(observer, Event{Seq: 3, Phase: PhaseBackup, Kind: EventStarted, Progress: Progress{Completed: 2, Total: 7}})
	writes := canonicalTransactionWrites(w.input)
	journal, err := applyTransactionFS(w.input.Layout.RootDir, writes, w.apply.fail)
	if err != nil {
		return applyFailure(base, err, transactionRollbackAttempted(err), observer, 4, PhaseWrite)
	}
	emitApply(observer, Event{Seq: 4, Phase: PhaseWrite, Kind: EventCompleted, Progress: Progress{Completed: 4, Total: 7}})
	if ctx.Err() != nil {
		return interruptedApply(base, journal.Rollback(), observer, 5)
	}
	emitApply(observer, Event{Seq: 5, Phase: PhaseFinalize, Kind: EventStarted, Progress: Progress{Completed: 4, Total: 7}})
	handoff, err := finalizeHostedMCP(prepared.plan, w.input, journal, w.apply.ResolveCredential, hostedMCPNativeRenderer{})
	if err != nil {
		return applyFailure(base, err, true, observer, 6, PhaseFinalize)
	}
	emitApply(observer, Event{Seq: 6, Phase: PhaseFinalize, Kind: EventCompleted, Progress: Progress{Completed: 5, Total: 7}})
	if ctx.Err() != nil {
		owner, claimErr := claimHostedMCPCompletionHandoff(handoff, prepared.plan, w.input)
		if claimErr != nil {
			return applyFailure(base, claimErr, false, observer, 7, PhaseRollback)
		}
		return interruptedApply(base, rollbackHostedMCPCompletion(owner, nil), observer, 7)
	}
	emitApply(observer, Event{Seq: 7, Phase: PhaseProfile, Kind: EventStarted, Progress: Progress{Completed: 5, Total: 7}})
	if err := completeHostedMCPCompletion(handoff, prepared.plan, w.input, w.apply.Store, w.apply.Project); err != nil {
		return applyFailure(base, err, true, observer, 8, PhasePublish)
	}
	emitApply(observer, Event{Seq: 8, Phase: PhasePublish, Kind: EventCompleted, Progress: Progress{Completed: 7, Total: 7}})
	base.Status, base.ChangedState = StatusSucceeded, len(base.Report.Decisions) > 0
	base.Report.MutationCount = len(base.Report.Decisions)
	return base
}
func canonicalTransactionWrites(input TransactionInput) []transactionFSWrite {
	facts := input.Semantic.TargetFacts().Resources
	writes := make([]transactionFSWrite, len(facts))
	for i, fact := range facts {
		writes[i] = transactionFSWrite{Path: fact.Resource, Data: append([]byte(nil), fact.Desired...)}
	}
	return writes
}
func interruptedApply(base Result, rollback error, observer Observer, seq uint64) Result {
	base.Interrupted, base.Rollback.Attempted = true, true
	if rollback != nil {
		return applyFailure(base, rollback, true, observer, seq, PhaseRollback)
	}
	base.Status, base.Rollback.Complete = StatusCancelled, true
	emitApply(observer, Event{Seq: seq, Phase: PhaseRollback, Kind: EventCompleted, Progress: Progress{Completed: 7, Total: 7}})
	return base
}
func applyFailure(base Result, err error, rollback bool, observer Observer, seq uint64, phase Phase) Result {
	base.Error = canonicalApplyError(err)
	base.Rollback = RollbackResult{Attempted: rollback, Complete: rollback && !base.Error.ResidualRisk()}
	if base.Error.ResidualRisk() {
		base.Status, base.ResidualRisk = StatusResidualRisk, true
	} else if errors.Is(err, errTransactionPostCommitCleanup) {
		base.Status, base.ChangedState, base.Rollback = StatusFailed, true, RollbackResult{}
	} else if rollback {
		base.Status = StatusRolledBack
	} else {
		base.Status = StatusFailed
	}
	emitApply(observer, Event{Seq: seq, Phase: phase, Kind: EventFailed, Error: base.Error})
	return base
}
func canonicalApplyError(err error) *InstallError {
	result := &InstallError{code: CodeApplyRejected, path: "apply", message: "canonical apply failed", retryable: true}
	var transactionOutcome *transactionFSOutcomeError
	var authorityOutcome *targetAuthorityError
	var finalizerOutcome *hostedMCPFinalizerError
	var profileOutcome *ProfileStoreError
	switch {
	case errors.As(err, &transactionOutcome):
		result.code, result.path = InstallErrorCode(transactionOutcome.Code()), transactionOutcome.Path()
	case errors.As(err, &authorityOutcome):
		result.code, result.path = InstallErrorCode(authorityOutcome.Code()), authorityOutcome.Path()
	case errors.As(err, &finalizerOutcome):
		result.code, result.path = InstallErrorCode(finalizerOutcome.Code()), finalizerOutcome.Path()
	case errors.As(err, &profileOutcome):
		result.code, result.path, result.residualRisk = InstallErrorCode(profileOutcome.Code()), profileOutcome.Path(), profileOutcome.ResidualRisk()
	}
	if errors.Is(err, CodeTransactionResidualRisk) {
		result.code, result.path, result.residualRisk, result.retryable = InstallErrorCode(CodeTransactionResidualRisk), "transaction.rollback", true, false
	}
	return result
}
func transactionRollbackAttempted(err error) bool {
	var applied *transactionFSApplyError
	return errors.As(err, &applied)
}
func emitApply(observer Observer, event Event) {
	if observer == nil {
		return
	}
	defer func() { _ = recover() }()
	observer.Observe(event.Clone())
}
func applyRejectedError() *InstallError {
	return &InstallError{code: CodeApplyRejected, path: "apply.plan", message: "canonical apply facts were rejected"}
}
func ConfirmationRequiredResult(request Request) Result {
	if request.Mode == ModeLegacyApply {
		result := explicitLegacyResult(request, StatusFailed)
		result.Error = &InstallError{code: CodeConfirmationRequired, path: "confirmation", message: "interactive confirmation is required"}
		return result
	}
	return Result{SchemaVersion: ResultSchemaVersion, Mode: ModeApply, Route: RouteCanonical, Target: request.Target, Status: StatusFailed, Error: &InstallError{code: CodeConfirmationRequired, path: "confirmation", message: "interactive confirmation is required"}}
}
