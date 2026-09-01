package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/install"
)

func TestW410CLIGoldensGuardModesStreamsSchemaOrderingAndRedaction(t *testing.T) {
	for _, format := range []installFormat{installFormatHuman, installFormatJSON} {
		var fixture strings.Builder
		for _, mode := range []install.Mode{install.ModeExplain, install.ModeDryRun, install.ModeApply, install.ModeLegacyDryRun, install.ModeLegacyApply} {
			result := w410CLIResult(mode)
			var stdout, stderr bytes.Buffer
			exit := (&App{Stdout: &stdout, Stderr: &stderr}).presentInstallResult(format, result)
			if exit != 0 {
				t.Fatalf("%s/%s exit=%d", format, mode, exit)
			}
			if format == installFormatJSON {
				var envelope installResultEnvelope
				if stderr.Len() != 0 || strings.Count(stdout.String(), "\n") != 1 || json.Unmarshal(stdout.Bytes(), &envelope) != nil || envelope.SchemaVersion != install.ResultSchemaVersion {
					t.Fatalf("%s JSON stream/schema drift: %q/%q", mode, stdout.String(), stderr.String())
				}
			} else if mode == install.ModeLegacyApply || mode == install.ModeLegacyDryRun {
				if !strings.Contains(stderr.String(), "warning[legacy-deprecated]") {
					t.Fatalf("%s missing human warning: %q", mode, stderr.String())
				}
			} else if stderr.Len() != 0 {
				t.Fatalf("%s human stderr=%q", mode, stderr.String())
			}
			fmt.Fprintf(&fixture, "## %s exit=%d\n-- stdout --\n%s-- stderr --\n%s", mode, exit, stdout.String(), stderr.String())
			if !strings.HasSuffix(fixture.String(), "\n") {
				fixture.WriteByte('\n')
			}
		}
		observed := fixture.String()
		for _, forbidden := range []string{"/Users/private/credential", "Bearer fixture-secret", "\x1b["} {
			if strings.Contains(observed, forbidden) {
				t.Fatalf("%s leaked %q", format, forbidden)
			}
		}
		path := fmt.Sprintf("testdata/w410_install_%s.golden", format)
		assertCLIGolden(t, path, observed)
		assertCLIGolden(t, path, observed)
	}
}

func TestW410CLIExitAndCancellationGuards(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result install.Result
		want   int
	}{
		{name: "voluntary-cancel", result: install.Result{Status: install.StatusCancelled}, want: 0},
		{name: "interrupted", result: install.Result{Interrupted: true}, want: 130},
		{name: "residual-over-interrupt", result: install.Result{Interrupted: true, ResidualRisk: true}, want: 3},
		{name: "failure", result: install.Result{Status: install.StatusFailed}, want: 1},
	} {
		if got := installExitCode(tc.result); got != tc.want {
			t.Fatalf("%s exit=%d want=%d", tc.name, got, tc.want)
		}
	}
}

func w410CLIResult(mode install.Mode) install.Result {
	legacy := mode == install.ModeLegacyDryRun || mode == install.ModeLegacyApply
	apply := mode == install.ModeApply || mode == install.ModeLegacyApply
	status := install.StatusSucceeded
	if mode == install.ModeExplain {
		status = install.StatusReady
	}
	result := install.Result{
		SchemaVersion: install.ResultSchemaVersion, Mode: mode, Route: install.RouteCanonical, Target: install.TargetPi,
		Status: status, Admitted: true, ChangedState: apply,
		Report:     install.TransactionReport{IRID: "ir-safe", ManifestHash: "manifest-safe", AllAdmitted: true, FinalizationCount: 1, MutationCount: map[bool]int{true: 2}[apply], ProvenancePath: "/Users/private/credential", Profile: install.PersistenceFact{ProfileID: "Bearer fixture-secret"}},
		Operations: []install.Operation{{Resource: "AGENTS.md", Action: "replace"}, {Resource: "skills/a.md", Action: "create"}},
		Guidance:   []install.Guidance{{Code: "canonical-gate", Message: "stable guidance"}},
	}
	if legacy {
		result.Route = install.RouteLegacy
		result.Warnings = []install.Guidance{{Code: "legacy-deprecated", Message: "legacy install is deprecated; use canonical routing"}}
	}
	return result
}

func assertCLIGolden(t *testing.T, path, got string) {
	t.Helper()
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("golden drift in %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}
