package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"testing"

	"github.com/alferio94/lore-cli/internal/install"
)

var routeProfile = flag.String("route-profile", "default-off", "expected process route profile")

func TestProcessReleaseProfileRoutes(t *testing.T) {
	wantOpenCodeE := *routeProfile == "opencode-e"
	app := newApp(io.Discard, io.Discard)
	for _, target := range install.SupportedTargets() {
		for _, mode := range []install.Mode{install.ModeExplain, install.ModeDryRun, install.ModeApply} {
			_, result := app.InstallWorkflow.Prepare(context.Background(), install.Request{Target: target, Mode: mode})
			wantGate := install.GateOff
			if wantOpenCodeE && target == install.TargetOpenCode {
				wantGate = install.GateExplain
			}
			if got := resultGate(result); got != wantGate {
				t.Fatalf("%s/%s gate = %q, want %q", target, mode, got, wantGate)
			}
			wantAdmitted := wantOpenCodeE && target == install.TargetOpenCode && mode == install.ModeExplain
			blocked := errors.Is(result.Error, install.CodeCanonicalRouteDisabled)
			if blocked == wantAdmitted || result.Route != install.RouteCanonical {
				t.Fatalf("%s/%s result = %#v, want route admission=%t", target, mode, result, wantAdmitted)
			}
		}
	}
}

func resultGate(result install.Result) install.Gate {
	for _, guidance := range result.Guidance {
		if guidance.Code == "canonical-gate" {
			return install.Gate(guidance.Message)
		}
	}
	return ""
}
