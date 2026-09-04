package tui

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/install"
	"github.com/alferio94/lore-cli/internal/version"
)

func TestW410TUIGoldenGuardsModesRoutesCancellationAndRedaction(t *testing.T) {
	var fixture strings.Builder
	profile := tuiDiagnosticProfileFixture()
	modes := []install.Mode{install.ModeExplain, install.ModeDryRun, install.ModeApply, install.ModeLegacyDryRun, install.ModeLegacyApply}
	for _, mode := range modes {
		m := &installModel{request: install.Request{Mode: mode, Target: install.TargetPi}, result: w410TUIResult(mode), profile: profile, stage: installResult, legacy: &tuiLegacyAdapterSpy{}}
		fmt.Fprintf(&fixture, "## %s\n%s\n", mode, renderInstallView(m, 80))
	}
	cancelled := w410TUIResult(install.ModeApply)
	cancelled.Status, cancelled.ChangedState, cancelled.Rollback = install.StatusCancelled, false, install.RollbackResult{Attempted: true, Complete: true}
	fmt.Fprintf(&fixture, "## cancelled\n%s\n", renderInstallView(&installModel{request: install.Request{Mode: install.ModeApply, Target: install.TargetPi}, result: cancelled, profile: profile, stage: installResult}, 80))
	observed := fixture.String()
	for _, want := range []string{"canonical-sealed", "explicit-legacy", "legacy-deprecated", "outcome=cancelled", "rollback_complete=true"} {
		if !strings.Contains(observed, want) {
			t.Fatalf("TUI fixture missing %q", want)
		}
	}
	for _, want := range strings.Fields(profile.Summary()) {
		if !strings.Contains(observed, want) {
			t.Fatalf("TUI profile parity missing %q", want)
		}
	}
	for _, forbidden := range []string{"/Users/private/credential", "Bearer fixture-secret", "eyJzY2hlbWEiOi", "\x1b["} {
		if strings.Contains(observed, forbidden) {
			t.Fatalf("TUI fixture leaked %q", forbidden)
		}
	}
	assertTUIGolden(t, "testdata/w410_install.golden", observed)
	assertTUIGolden(t, "testdata/w410_install.golden", observed)
}

func tuiDiagnosticProfileFixture() version.ReleaseProfile {
	return version.ReleaseProfile{Schema: "lore.release-profile/v1", ID: "prerelease-opencode-e", Version: 1, Release: "v0.3.0-rc.1", Channel: "prerelease", ArtifactSHA256: strings.Repeat("a", 64), ProvenanceStatus: "valid", Gates: version.ProfileGates{Pi: "off", OpenCode: "E", Codex: "off", Antigravity: "off"}}
}

func w410TUIResult(mode install.Mode) install.Result {
	legacy := mode == install.ModeLegacyDryRun || mode == install.ModeLegacyApply
	apply := mode == install.ModeApply || mode == install.ModeLegacyApply
	status := install.StatusSucceeded
	if mode == install.ModeExplain {
		status = install.StatusReady
	}
	result := install.Result{
		SchemaVersion: install.ResultSchemaVersion, Mode: mode, Route: install.RouteCanonical, Target: install.TargetPi,
		Status: status, Admitted: true, ChangedState: apply,
		Report:     install.TransactionReport{ProvenancePath: "/Users/private/credential", Profile: install.PersistenceFact{ProfileID: "Bearer fixture-secret"}},
		Operations: []install.Operation{{Resource: "AGENTS.md", Action: "replace"}, {Resource: "skills/a.md", Action: "create"}},
		Guidance:   []install.Guidance{{Code: "canonical-gate", Message: "stable guidance"}},
	}
	if legacy {
		result.Route = install.RouteLegacy
		result.Warnings = []install.Guidance{{Code: "legacy-deprecated", Message: "legacy install is deprecated; use canonical routing"}}
	}
	return result
}

func assertTUIGolden(t *testing.T, path, got string) {
	t.Helper()
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("golden drift in %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}
