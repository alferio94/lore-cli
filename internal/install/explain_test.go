package install

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestW4ExplainSealsDeterministicOrderedLocalFacts(t *testing.T) {
	root := t.TempDir()
	firstInput := transactionFixture(t, TargetPi, root, false)
	secondInput := transactionFixture(t, TargetPi, root, true)
	request := Request{Mode: ModeExplain, Target: TargetPi, Components: []ComponentID{ComponentLoreServerMCP, ComponentCorePack}}
	policy := routePolicyFixture(t, GateExplain)

	firstPrepared, first := PrepareExplain(policy, request, firstInput)
	secondPrepared, second := PrepareExplain(policy, request, secondInput)
	if firstPrepared.IsZero() || secondPrepared.IsZero() || !first.Admitted || first.Status != StatusReady || first.ChangedState || first.Report.MutationCount != 0 {
		t.Fatalf("admitted explain = prepared:%t result:%#v", !firstPrepared.IsZero(), first)
	}
	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(firstPrepared.Report(), secondPrepared.Report()) {
		t.Fatalf("equivalent local facts produced different explain results")
	}
	resources := make([]string, len(first.Operations))
	for i, operation := range first.Operations {
		resources[i] = operation.Resource
	}
	if !sort.StringsAreSorted(resources) {
		t.Fatalf("operations are not ordered: %v", resources)
	}
	facts := firstInput.Semantic.TargetFacts()
	if len(first.Guidance) != len(facts.Guidance)+1 || first.Guidance[0] != (Guidance{Code: "canonical-gate", Message: string(GateExplain)}) {
		t.Fatalf("gate/guidance = %#v", first.Guidance)
	}
	for i, message := range facts.Guidance {
		if first.Guidance[i+1] != (Guidance{Code: "lore-server-guidance", Message: message}) {
			t.Fatalf("guidance[%d] = %#v", i, first.Guidance[i+1])
		}
	}

	requestCopy := firstPrepared.Request()
	reportCopy := firstPrepared.Report()
	resultCopy := first.Clone()
	requestCopy.Components[0] = ComponentContext7MCP
	reportCopy.Decisions[0].Resource = "changed"
	resultCopy.Operations[0].Resource = "changed"
	resultCopy.Guidance[0].Message = "changed"
	if firstPrepared.Request().Components[0] != ComponentLoreServerMCP || firstPrepared.Report().Decisions[0].Resource == "changed" || first.Operations[0].Resource == "changed" || first.Guidance[0].Message != string(GateExplain) {
		t.Fatal("explain result or Prepared aliases caller-visible slices")
	}
	preparedCopy := firstPrepared
	if !firstPrepared.consume() || preparedCopy.consume() || !secondPrepared.consume() {
		t.Fatal("Prepared copies must be one-shot while repeated Explain calls remain independent")
	}
}

func TestW4ExplainEnforcesGateEAndCanonicalOnly(t *testing.T) {
	for _, target := range SupportedTargets() {
		input := transactionFixture(t, target, t.TempDir(), false)
		request := Request{Mode: ModeExplain, Target: target}
		for _, gate := range []Gate{GateExplain, GateDryRun, GateApply} {
			prepared, result := PrepareExplain(routePolicyFixture(t, gate), request, input)
			if prepared.IsZero() || !result.Admitted || result.Route != RouteCanonical || result.Guidance[0].Message != string(gate) {
				t.Fatalf("%s/%s explain = prepared:%t result:%#v", target, gate, !prepared.IsZero(), result)
			}
		}
		prepared, result := PrepareExplain(routePolicyFixture(t, GateOff), request, input)
		if !prepared.IsZero() || result.Admitted || result.Route != RouteCanonical || result.Error == nil || result.Error.Code() != CodeCanonicalRouteDisabled {
			t.Fatalf("%s/off explain = prepared:%t result:%#v", target, !prepared.IsZero(), result)
		}
	}

	input := transactionFixture(t, TargetPi, t.TempDir(), false)
	for _, request := range []Request{
		{Mode: ModeDryRun, Target: TargetPi},
		{Mode: ModeApply, Target: TargetPi},
		{Mode: ModeLegacyDryRun, Target: TargetPi},
		{Mode: ModeLegacyApply, Target: TargetPi},
		{Mode: ModeExplain, Target: TargetPi, AssumeYes: true},
	} {
		prepared, result := PrepareExplain(routePolicyFixture(t, GateApply), request, input)
		if !prepared.IsZero() || result.Admitted || result.Error == nil || result.Error.Code() != CodeInvalidWorkflowRequest {
			t.Fatalf("non-Explain request %#v escaped domain gate: %#v", request, result)
		}
	}
}

func TestW4ExplainRejectsUnsealedFactsRedactedAndRepeatable(t *testing.T) {
	input := transactionFixture(t, TargetPi, t.TempDir(), false)
	forbidden := []string{"Bearer top-secret", "/Users/private/project", "repository-secret", `{"payload":"secret"}`}
	input.ServerIdentity = TransactionServerIdentity{
		ProjectID:    forbidden[0],
		ProjectKey:   forbidden[1],
		RepositoryID: forbidden[2],
		InferredFrom: forbidden[3],
	}
	request := Request{Mode: ModeExplain, Target: TargetPi}
	policy := routePolicyFixture(t, GateExplain)

	firstPrepared, first := PrepareExplain(policy, request, input)
	secondPrepared, second := PrepareExplain(policy, request, input)
	if !firstPrepared.IsZero() || !secondPrepared.IsZero() || !reflect.DeepEqual(first, second) {
		t.Fatalf("rejected Explain is not zero/repeatable: %#v %#v", firstPrepared, first)
	}
	if first.Admitted || first.ChangedState || !reflect.DeepEqual(first.Report, TransactionReport{}) || len(first.Operations) != 0 || first.Error == nil || first.Error.Code() != CodeExplainRejected || first.Error.Path() != "explain.plan" {
		t.Fatalf("rejected Explain exposed admitted/effect facts: %#v", first)
	}
	observed := fmt.Sprintf("%#v|%s", first, first.Error)
	for _, secret := range forbidden {
		if strings.Contains(observed, secret) {
			t.Fatalf("rejected Explain disclosed %q", secret)
		}
	}
}
