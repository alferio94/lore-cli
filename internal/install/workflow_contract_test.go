package install

import "testing"

func TestPreparedAndObservableContractsDefensivelyCopy(t *testing.T) {
	request := Request{Mode: ModeDryRun, Target: TargetPi, Components: []ComponentID{ComponentCorePack}}
	report := TransactionReport{Target: TargetPi, Decisions: []TransactionDecision{{Resource: "AGENTS.md"}}}
	prepared := newPrepared(request, RouteCanonical, TransactionPlan{report: report}, nil)
	request.Components[0] = ComponentContext7MCP
	report.Decisions[0].Resource = "changed"
	if prepared.Request().Components[0] != ComponentCorePack || prepared.Report().Decisions[0].Resource != "AGENTS.md" || prepared.Route() != RouteCanonical || prepared.IsZero() {
		t.Fatalf("prepared did not preserve sealed values")
	}
	copyOfPrepared := prepared
	if !prepared.consume() || copyOfPrepared.consume() {
		t.Fatalf("prepared copies must share one consumption state")
	}
	operation := Operation{Resource: "AGENTS.md", Action: "replace"}
	event := Event{Seq: 1, Phase: PhaseWrite, Kind: EventProgress, Operation: &operation}
	observed := event.Clone()
	observed.Operation.Resource = "changed"
	if event.Operation.Resource != "AGENTS.md" {
		t.Fatalf("event clone aliased operation")
	}
	result := Result{SchemaVersion: ResultSchemaVersion, Mode: ModeDryRun, Route: RouteCanonical, Status: StatusReady, Operations: []Operation{operation}, Error: newInstallError(CodeCanonicalRouteDisabled)}
	clone := result.Clone()
	clone.Operations[0].Resource = "changed"
	if result.Operations[0].Resource != "AGENTS.md" || result.Error.Error() != "canonical route is disabled" || result.Error.Path() != "route_policy" {
		t.Fatalf("result/error contract mutated or disclosed unstable data")
	}
}
