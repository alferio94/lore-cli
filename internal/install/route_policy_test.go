package install

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRoutePolicyEnforcesPerTargetCanonicalGates(t *testing.T) {
	for _, target := range SupportedTargets() {
		for gateIndex, gate := range []Gate{GateOff, GateExplain, GateDryRun, GateApply} {
			for modeIndex, mode := range []Mode{ModeExplain, ModeDryRun, ModeApply} {
				decision, err := routePolicyFixture(t, gate).Decide(Request{Mode: mode, Target: target})
				wantAdmitted := gateIndex >= modeIndex+1
				if decision.Route != RouteCanonical || decision.Admitted != wantAdmitted {
					t.Fatalf("%s/%s/%s decision = %#v, want canonical admitted=%t", target, gate, mode, decision, wantAdmitted)
				}
				if wantAdmitted && err != nil || !wantAdmitted && !errors.Is(err, CodeCanonicalRouteDisabled) {
					t.Fatalf("%s/%s/%s error = %v", target, gate, mode, err)
				}
			}
		}
	}
}
func TestRoutePolicyNeverFallsBackAndLegacyMustBeExplicit(t *testing.T) {
	canonical, err := routePolicyFixture(t, GateOff).Decide(Request{Mode: ModeApply, Target: TargetPi})
	if !errors.Is(err, CodeCanonicalRouteDisabled) || canonical.Route != RouteCanonical || canonical.Admitted {
		t.Fatalf("disabled canonical decision = %#v, error = %v", canonical, err)
	}
	for _, mode := range []Mode{ModeLegacyDryRun, ModeLegacyApply} {
		legacy, err := routePolicyFixture(t, GateOff).Decide(Request{Mode: mode, Target: TargetPi})
		if err != nil || legacy.Route != RouteLegacy || !legacy.Admitted {
			t.Fatalf("explicit legacy %s decision = %#v, error = %v", mode, legacy, err)
		}
	}
}
func TestRoutePolicyDemotionIsTargetLocalAndRecorded(t *testing.T) {
	original := routePolicyFixture(t, GateApply)
	demoted, err := original.Demote(TargetPi, GateExplain)
	if err != nil {
		t.Fatal(err)
	}
	if original.Gate(TargetPi) != GateApply || demoted.Gate(TargetPi) != GateExplain || demoted.Gate(TargetCodex) != GateApply || !reflect.DeepEqual(demoted.Demotions(), []RouteDemotion{{Target: TargetPi, From: GateApply, To: GateExplain, Reason: "emergency-policy"}}) {
		t.Fatalf("demotion isolation/evidence = original:%s demoted:%s codex:%s evidence:%#v", original.Gate(TargetPi), demoted.Gate(TargetPi), demoted.Gate(TargetCodex), demoted.Demotions())
	}
	if _, err := demoted.Demote(TargetPi, GateApply); !errors.Is(err, CodeInvalidRouteDemotion) {
		t.Fatalf("promotion error = %v, want %s", err, CodeInvalidRouteDemotion)
	}
}
func TestW49ExplicitLegacyPreparationIsGateIndependentOneShotAndRedacted(t *testing.T) {
	for _, mode := range []Mode{ModeLegacyDryRun, ModeLegacyApply} {
		request := Request{Mode: mode, Target: TargetPi}
		prepared, result := PrepareExplicitLegacy(request)
		if result.Error != nil || !result.Admitted || result.Route != RouteLegacy || len(result.Warnings) != 1 || result.Warnings[0].Code != "legacy-deprecated" {
			t.Fatalf("%s preparation = %#v", mode, result)
		}
		if got, ok := ConsumeExplicitLegacy(prepared); !ok || got.Mode != mode {
			t.Fatalf("%s was not consumable once", mode)
		}
		if _, ok := ConsumeExplicitLegacy(prepared); ok {
			t.Fatalf("%s was reusable", mode)
		}
	}
	_, invalid := PrepareExplicitLegacy(Request{Mode: ModeLegacyApply, Target: TargetID("unsupported")})
	if invalid.Error == nil || invalid.Admitted || invalid.Route != RouteLegacy || strings.Contains(invalid.Error.Error(), "unsupported") {
		t.Fatalf("unsupported target was admitted or disclosed: %#v", invalid)
	}
}

func routePolicyFixture(t *testing.T, gate Gate) RoutePolicy {
	t.Helper()
	gates := make(map[TargetID]Gate)
	for _, target := range SupportedTargets() {
		gates[target] = gate
	}
	policy, err := NewRoutePolicy(gates)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}
