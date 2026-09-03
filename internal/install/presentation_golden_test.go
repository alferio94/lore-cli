package install

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestW410RouteMatrixGoldenIsFourTargetDeterministicAndExplicitOnly(t *testing.T) {
	var got strings.Builder
	modes := []Mode{ModeExplain, ModeDryRun, ModeApply, ModeLegacyDryRun, ModeLegacyApply}
	for _, target := range SupportedTargets() {
		for _, gate := range []Gate{GateOff, GateExplain, GateDryRun, GateApply} {
			policy := routePolicyFixture(t, gate)
			fmt.Fprintf(&got, "%s %s", target, gate)
			for _, mode := range modes {
				decision, err := policy.Decide(Request{Mode: mode, Target: target})
				state := "blocked"
				if err == nil && decision.Admitted {
					state = "admitted"
				}
				fmt.Fprintf(&got, " %s=%s/%s", mode, state, decision.Route)
				if mode == ModeExplain && decision.Route != RouteCanonical || mode == ModeLegacyApply && decision.Route != RouteLegacy {
					t.Fatalf("route marker drift: %#v", decision)
				}
			}
			got.WriteByte('\n')
		}
	}
	assertInstallGolden(t, "testdata/w410_route_matrix.golden", got.String())
	assertInstallGolden(t, "testdata/w410_route_matrix.golden", got.String())
}

func assertInstallGolden(t *testing.T, path, got string) {
	t.Helper()
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("golden drift in %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}
