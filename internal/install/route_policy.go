package install

type Gate string

const (
	GateOff     Gate = "off"
	GateExplain Gate = "E"
	GateDryRun  Gate = "D"
	GateApply   Gate = "A"
)

type RouteDecision struct {
	Mode     Mode
	Route    Route
	Target   TargetID
	Gate     Gate
	Admitted bool
}
type RouteDemotion struct {
	Target TargetID
	From   Gate
	To     Gate
	Reason string
}
type RoutePolicy struct {
	gates     map[TargetID]Gate
	demotions []RouteDemotion
}

func NewRoutePolicy(gates map[TargetID]Gate) (RoutePolicy, error) {
	if len(gates) != len(SupportedTargets()) {
		return RoutePolicy{}, newInstallError(CodeInvalidRoutePolicy)
	}
	copyGates := make(map[TargetID]Gate, len(gates))
	for _, target := range SupportedTargets() {
		gate, ok := gates[target]
		if !ok || gateRank(gate) < 0 {
			return RoutePolicy{}, newInstallError(CodeInvalidRoutePolicy)
		}
		copyGates[target] = gate
	}
	return RoutePolicy{gates: copyGates}, nil
}
func (p RoutePolicy) Gate(target TargetID) Gate { return p.gates[target] }
func (p RoutePolicy) Demotions() []RouteDemotion {
	return append([]RouteDemotion(nil), p.demotions...)
}
func (p RoutePolicy) Demote(target TargetID, to Gate) (RoutePolicy, error) {
	from, ok := p.gates[target]
	if !ok || gateRank(to) < 0 || gateRank(to) >= gateRank(from) {
		return RoutePolicy{}, newInstallError(CodeInvalidRouteDemotion)
	}
	next := RoutePolicy{gates: make(map[TargetID]Gate, len(p.gates)), demotions: p.Demotions()}
	for id, gate := range p.gates {
		next.gates[id] = gate
	}
	next.gates[target] = to
	next.demotions = append(next.demotions, RouteDemotion{Target: target, From: from, To: to, Reason: "emergency-policy"})
	return next, nil
}
func (p RoutePolicy) Decide(request Request) (RouteDecision, error) {
	gate, ok := p.gates[request.Target]
	if !ok || gateRank(gate) < 0 {
		return RouteDecision{}, newInstallError(CodeInvalidWorkflowRequest)
	}
	decision := RouteDecision{Mode: request.Mode, Target: request.Target, Gate: gate}
	switch request.Mode {
	case ModeLegacyDryRun, ModeLegacyApply:
		decision.Route, decision.Admitted = RouteLegacy, true
		return decision, nil
	case ModeExplain, ModeDryRun, ModeApply:
		decision.Route = RouteCanonical
	default:
		return RouteDecision{}, newInstallError(CodeInvalidWorkflowRequest)
	}
	required := map[Mode]int{ModeExplain: 1, ModeDryRun: 2, ModeApply: 3}[request.Mode]
	decision.Admitted = gateRank(gate) >= required
	if !decision.Admitted {
		return decision, newInstallError(CodeCanonicalRouteDisabled)
	}
	return decision, nil
}
func gateRank(gate Gate) int {
	rank, ok := map[Gate]int{GateOff: 0, GateExplain: 1, GateDryRun: 2, GateApply: 3}[gate]
	if !ok {
		return -1
	}
	return rank
}
