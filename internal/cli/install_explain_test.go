package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/install"
)

type explainWorkflowSpy struct {
	result        install.Result
	executeResult install.Result
	request       install.Request
	prepareCalls  int
	executeCalls  int
}

func (w *explainWorkflowSpy) Prepare(_ context.Context, request install.Request) (install.Prepared, install.Result) {
	w.prepareCalls++
	w.request = request.Clone()
	return install.Prepared{}, w.result.Clone()
}

func (w *explainWorkflowSpy) Execute(context.Context, install.Prepared, install.Observer) install.Result {
	w.executeCalls++
	return w.executeResult.Clone()
}

func TestInstallExplainHumanUsesSharedWorkflowAndStableChannels(t *testing.T) {
	app, stdout, stderr := newTestApp(&fakeStore{path: "/unused"}, nil)
	workflow := &explainWorkflowSpy{result: explainSuccessResult()}
	app.InstallWorkflow = workflow
	exit := app.Run([]string{"install", "--explain", "--target", "pi", "--component", "core-pack"})
	if exit != 0 || workflow.prepareCalls != 1 || workflow.executeCalls != 0 {
		t.Fatalf("exit/calls = %d/%d/%d, stdout=%q stderr=%q", exit, workflow.prepareCalls, workflow.executeCalls, stdout.String(), stderr.String())
	}
	if workflow.request.Mode != install.ModeExplain || workflow.request.Target != install.TargetPi || len(workflow.request.Components) != 1 || workflow.request.Components[0] != install.ComponentCorePack {
		t.Fatalf("request = %#v", workflow.request)
	}
	for _, want := range []string{"mode=explain", "route=canonical-sealed", "outcome=ready", "mutations=0", "operation action=create resource=skills/a.md"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	assertExplainSecretsAbsent(t, stdout.String(), stderr.String())
}

func TestInstallExplainJSONIsSingularVersionedAndDeterministic(t *testing.T) {
	app, stdout, stderr := newTestApp(&fakeStore{path: "/unused"}, nil)
	app.InstallWorkflow = &explainWorkflowSpy{result: explainSuccessResult()}

	if exit := app.Run([]string{"install", "--explain", "--format", "json"}); exit != 0 {
		t.Fatalf("exit = %d, stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	first := stdout.String()
	if strings.Count(first, "\n") != 1 || stderr.Len() != 0 || strings.Contains(first, "\x1b[") {
		t.Fatalf("JSON streams = stdout:%q stderr:%q", first, stderr.String())
	}
	var envelope struct {
		SchemaVersion string `json:"schema_version"`
		Result        struct {
			Mode, Route, Outcome string
			Admitted             bool `json:"admitted"`
			ChangedState         bool `json:"changed_state"`
			Report               struct {
				MutationCount int `json:"mutation_count"`
			} `json:"report"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(first), &envelope); err != nil {
		t.Fatalf("Unmarshal: %v, output=%q", err, first)
	}
	if envelope.SchemaVersion != install.ResultSchemaVersion || envelope.Result.Mode != "explain" || envelope.Result.Route != "canonical-sealed" || envelope.Result.Outcome != "ready" || !envelope.Result.Admitted || envelope.Result.ChangedState || envelope.Result.Report.MutationCount != 0 {
		t.Fatalf("envelope = %#v", envelope)
	}
	stdout.Reset()
	if exit := app.Run([]string{"install", "--explain", "--format=json"}); exit != 0 || stdout.String() != first {
		t.Fatalf("repeat exit/output = %d/%q, want %q", exit, stdout.String(), first)
	}
	assertExplainSecretsAbsent(t, first, stderr.String())
}

func TestInstallExplainFailuresStayOnAssignedStream(t *testing.T) {
	for _, format := range []string{"human", "json"} {
		t.Run(format, func(t *testing.T) {
			app, stdout, stderr := newTestApp(&fakeStore{path: "/unused"}, nil)
			app.InstallWorkflow = defaultInstallWorkflow()
			exit := app.Run([]string{"install", "--explain", "--format", format})
			if exit != 1 {
				t.Fatalf("exit = %d, stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
			}
			if format == "json" {
				if stderr.Len() != 0 || strings.Count(stdout.String(), "\n") != 1 || !strings.Contains(stdout.String(), `"code":"canonical_route_disabled"`) {
					t.Fatalf("JSON failure streams = stdout:%q stderr:%q", stdout.String(), stderr.String())
				}
			} else if stdout.Len() != 0 || !strings.Contains(stderr.String(), "error[canonical_route_disabled]") {
				t.Fatalf("human failure streams = stdout:%q stderr:%q", stdout.String(), stderr.String())
			}
		})
	}
	if got := installExitCode(install.Result{Status: install.StatusSucceeded, Admitted: true, ResidualRisk: true}); got != 3 {
		t.Fatalf("residual-risk precedence exit = %d", got)
	}
}

func TestInstallExplainRejectsUsageBeforeWorkflow(t *testing.T) {
	cases := [][]string{
		{"install", "--explain", "--dry-run"},
		{"install", "--explain", "--legacy"},
		{"install", "--explain", "--yes"},
		{"install", "--explain", "--format", "yaml"},
		{"install", "--explain", "--unknown"},
	}
	for _, args := range cases {
		app, stdout, stderr := newTestApp(&fakeStore{path: "/unused"}, nil)
		workflow := &explainWorkflowSpy{result: explainSuccessResult()}
		app.InstallWorkflow = workflow
		if exit := app.Run(args); exit != 2 || workflow.prepareCalls != 0 || stdout.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf("args=%v exit/calls/streams=%d/%d/%q/%q", args, exit, workflow.prepareCalls, stdout.String(), stderr.String())
		}
	}
}

func TestW47InstallDryRunHumanAndJSONUseOneSharedWorkflow(t *testing.T) {
	for _, format := range []string{"human", "json"} {
		t.Run(format, func(t *testing.T) {
			app, stdout, stderr := newTestApp(&fakeStore{path: "/unused"}, nil)
			ready := dryRunSuccessResult()
			ready.Status = install.StatusReady
			workflow := &explainWorkflowSpy{result: ready, executeResult: dryRunSuccessResult()}
			app.InstallWorkflow = workflow
			exit := app.Run([]string{"install", "--dry-run", "--format", format, "--target", "pi", "--component", "core-pack"})
			if exit != 0 || workflow.prepareCalls != 1 || workflow.executeCalls != 1 || workflow.request.Mode != install.ModeDryRun {
				t.Fatalf("exit/calls/request=%d/%d/%d/%#v stdout=%q stderr=%q", exit, workflow.prepareCalls, workflow.executeCalls, workflow.request, stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String(), "dry-run") || !strings.Contains(stdout.String(), "canonical-sealed") || stderr.Len() != 0 {
				t.Fatalf("streams stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
			first := stdout.String()
			stdout.Reset()
			if exit := app.Run([]string{"install", "--dry-run", "--format=" + format}); exit != 0 || stdout.String() != first {
				t.Fatalf("repeat exit/output=%d/%q want=%q", exit, stdout.String(), first)
			}
		})
	}
}

func TestW48InstallApplyConfirmsThenUsesOneSharedWorkflowWithExitPrecedence(t *testing.T) {
	app, stdout, stderr := newTestApp(&fakeStore{path: "/unused"}, nil)
	ready, done := explainSuccessResult(), dryRunSuccessResult()
	ready.Mode, done.Mode, ready.Status, done.ChangedState = install.ModeApply, install.ModeApply, install.StatusReady, true
	workflow := &explainWorkflowSpy{result: ready, executeResult: done}
	app.InstallWorkflow, app.InstallConfirm = workflow, func() (bool, error) { return true, nil }
	if exit := app.Run([]string{"install", "--format=json"}); exit != 0 || workflow.prepareCalls != 1 || workflow.executeCalls != 1 || workflow.request.Mode != install.ModeApply || stderr.Len() != 0 || !strings.Contains(stdout.String(), `"mode":"apply"`) {
		t.Fatalf("apply parity = exit:%d calls:%d/%d request:%#v streams:%q/%q", exit, workflow.prepareCalls, workflow.executeCalls, workflow.request, stdout.String(), stderr.String())
	}
	if installExitCode(install.Result{Interrupted: true}) != 130 || installExitCode(install.Result{Interrupted: true, ResidualRisk: true}) != 3 {
		t.Fatal("signal/residual-risk exit precedence drifted")
	}
	blocked, _, _ := newTestApp(&fakeStore{path: "/unused"}, nil)
	input := strings.NewReader("y\n")
	blocked.InstallWorkflow, blocked.Stdin = &explainWorkflowSpy{result: ready}, input
	if exit := blocked.Run([]string{"install"}); exit != 1 || input.Len() != 2 {
		t.Fatalf("non-TTY confirmation read stdin or returned %d", exit)
	}
}

func TestW47InstallDryRunDisabledFailsClosedWithoutExecute(t *testing.T) {
	app, stdout, stderr := newTestApp(&fakeStore{path: "/unused"}, nil)
	workflow := &explainWorkflowSpy{result: install.Result{SchemaVersion: install.ResultSchemaVersion, Mode: install.ModeDryRun, Route: install.RouteCanonical, Target: install.TargetPi, Status: install.StatusFailed, Error: installDisabledErrorForTest()}}
	app.InstallWorkflow = workflow
	if exit := app.Run([]string{"install", "--dry-run", "--format=json"}); exit != 1 || workflow.prepareCalls != 1 || workflow.executeCalls != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "canonical_route_disabled") || strings.Contains(stdout.String(), "explicit-legacy") {
		t.Fatalf("exit/calls/streams=%d/%d/%d/%q/%q", exit, workflow.prepareCalls, workflow.executeCalls, stdout.String(), stderr.String())
	}
}

func dryRunSuccessResult() install.Result {
	result := explainSuccessResult()
	result.Mode, result.Status = install.ModeDryRun, install.StatusSucceeded
	return result
}

func installDisabledErrorForTest() *install.InstallError {
	workflow := defaultInstallWorkflow()
	_, result := workflow.Prepare(context.Background(), install.Request{Mode: install.ModeDryRun, Target: install.TargetPi})
	return result.Error
}

func explainSuccessResult() install.Result {
	return install.Result{
		SchemaVersion: install.ResultSchemaVersion, Mode: install.ModeExplain, Route: install.RouteCanonical,
		Target: install.TargetPi, Status: install.StatusReady, Admitted: true,
		Report:     install.TransactionReport{IRID: "ir-safe", ProvenancePath: "/Users/private/project", ManifestHash: "manifest-safe", AllAdmitted: true},
		Operations: []install.Operation{{Resource: "skills/a.md", Action: "create"}},
		Guidance:   []install.Guidance{{Code: "canonical-gate", Message: "E"}},
	}
}

func assertExplainSecretsAbsent(t *testing.T, streams ...string) {
	t.Helper()
	observed := strings.Join(streams, "|")
	for _, forbidden := range []string{"/Users/private/project", "Bearer top-secret", "secret-token"} {
		if strings.Contains(observed, forbidden) {
			t.Fatalf("output leaked %q: %q", forbidden, observed)
		}
	}
}
