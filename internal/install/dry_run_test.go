package install

import (
	"reflect"
	"sort"
	"testing"
)

func TestW47DryRunConsumesSameSealedPlanWithOrderedEventsAndNoEffects(t *testing.T) {
	input := transactionFixture(t, TargetPi, t.TempDir(), true)
	request := Request{Mode: ModeDryRun, Target: TargetPi, Components: []ComponentID{ComponentCorePack}}
	workflow := NewCanonicalWorkflow(routePolicyFixture(t, GateDryRun), input)
	prepared, ready := workflow.Prepare(t.Context(), request)
	if prepared.IsZero() || !ready.Admitted || ready.Status != StatusReady || ready.ChangedState || ready.Report.MutationCount != 0 {
		t.Fatalf("Prepare = prepared:%t result:%#v", !prepared.IsZero(), ready)
	}
	var events []Event
	result := workflow.Execute(t.Context(), prepared, ObserverFunc(func(event Event) { events = append(events, event) }))
	if result.Status != StatusSucceeded || !result.Admitted || result.ChangedState || result.Route != RouteCanonical || result.Report.MutationCount != 0 || !reflect.DeepEqual(result.Report, prepared.Report()) {
		t.Fatalf("Execute = %#v", result)
	}
	wantEvents := []Event{
		{Seq: 1, Phase: PhasePrepare, Kind: EventStarted, Progress: Progress{Total: 2}},
		{Seq: 2, Phase: PhasePrepare, Kind: EventCompleted, Progress: Progress{Completed: 1, Total: 2}},
		{Seq: 3, Phase: PhaseSeal, Kind: EventStarted, Progress: Progress{Completed: 1, Total: 2}},
		{Seq: 4, Phase: PhaseSeal, Kind: EventCompleted, Progress: Progress{Completed: 2, Total: 2}},
	}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("events = %#v, want %#v", events, wantEvents)
	}
	resources := make([]string, len(result.Operations))
	for i, operation := range result.Operations {
		resources[i] = operation.Resource
	}
	if !sort.StringsAreSorted(resources) {
		t.Fatalf("operations are not deterministic: %v", resources)
	}
}

func TestW47DryRunRejectsMismatchReuseAndObserverInterference(t *testing.T) {
	workflow := NewCanonicalWorkflow(routePolicyFixture(t, GateApply), transactionFixture(t, TargetPi, t.TempDir(), false))
	prepared, _ := workflow.Prepare(t.Context(), Request{Mode: ModeDryRun, Target: TargetPi})
	prepared.request.Target = TargetOpenCode
	if result := workflow.Execute(t.Context(), prepared, nil); result.Error == nil || result.Error.Code() != CodeInvalidWorkflowRequest || result.ChangedState {
		t.Fatalf("mismatched Prepared = %#v", result)
	}
	prepared, _ = workflow.Prepare(t.Context(), Request{Mode: ModeDryRun, Target: TargetPi})
	first := workflow.Execute(t.Context(), prepared, ObserverFunc(func(Event) { panic("observer must not control dry-run") }))
	second := workflow.Execute(t.Context(), prepared, nil)
	if first.Status != StatusSucceeded || second.Error == nil || second.Error.Code() != CodeInvalidWorkflowRequest {
		t.Fatalf("one-shot/observer results = first:%#v second:%#v", first, second)
	}
}

func TestW47DryRunEnforcesIndependentGatesWithoutLegacyFallback(t *testing.T) {
	for _, target := range SupportedTargets() {
		input := transactionFixture(t, target, t.TempDir(), false)
		for _, gate := range []Gate{GateOff, GateExplain, GateDryRun, GateApply} {
			workflow := NewCanonicalWorkflow(routePolicyFixture(t, gate), input)
			prepared, result := workflow.Prepare(t.Context(), Request{Mode: ModeDryRun, Target: target})
			admitted := gate == GateDryRun || gate == GateApply
			if admitted != result.Admitted || admitted == prepared.IsZero() || result.Route != RouteCanonical {
				t.Fatalf("target=%s gate=%s prepared=%t result=%#v", target, gate, !prepared.IsZero(), result)
			}
			if !admitted && (result.Error == nil || result.Error.Code() != CodeCanonicalRouteDisabled) {
				t.Fatalf("target=%s gate=%s did not fail closed: %#v", target, gate, result)
			}
			if result.Route == RouteLegacy {
				t.Fatalf("target=%s gate=%s fell back to legacy", target, gate)
			}
		}
	}
}
