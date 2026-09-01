package tui

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/cli"
	"github.com/alferio94/lore-cli/internal/install"
	tea "github.com/charmbracelet/bubbletea"
)

type tuiWorkflowSpy struct {
	prepares      int
	executes      int
	request       install.Request
	result        install.Result
	executeResult install.Result
}

func (w *tuiWorkflowSpy) Prepare(_ context.Context, request install.Request) (install.Prepared, install.Result) {
	w.prepares++
	w.request = request.Clone()
	result := w.result.Clone()
	result.Mode, result.Target = request.Mode, request.Target
	return install.Prepared{}, result
}
func (w *tuiWorkflowSpy) Execute(ctx context.Context, _ install.Prepared, _ install.Observer) install.Result {
	w.executes++
	if w.executeResult.SchemaVersion != "" {
		return w.executeResult.Clone()
	}
	<-ctx.Done()
	return install.Result{SchemaVersion: install.ResultSchemaVersion, Mode: install.ModeApply, Route: install.RouteCanonical, Target: install.TargetPi, Status: install.StatusCancelled}
}

func explainTUIResult() install.Result {
	return install.Result{
		SchemaVersion: install.ResultSchemaVersion, Mode: install.ModeExplain, Route: install.RouteCanonical,
		Target: install.TargetPi, Status: install.StatusReady, Admitted: true,
		Report:     install.TransactionReport{Target: install.TargetPi, ProvenancePath: "/secret/token-path", AllAdmitted: true, MutationCount: 0},
		Operations: []install.Operation{{Resource: "AGENTS.md", Action: "replace"}},
		Guidance:   []install.Guidance{{Code: "canonical-gate", Message: "explain"}},
		Warnings:   []install.Guidance{{Code: "typed-warning", Message: "safe warning"}},
	}
}

func TestTUIExplainUsesSharedWorkflowWithTypedParityAndSafeNavigation(t *testing.T) {
	t.Setenv("LORE_NO_ANIMATION", "1")
	workflow := &tuiWorkflowSpy{result: explainTUIResult()}
	m := newModel(cli.InteractiveActions{InstallWorkflow: workflow})
	m = moveSelectionToInstall(t, m)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if cmd == nil || m.installTUI == nil || !m.installTUI.noAnimation {
		t.Fatal("canonical Explain did not start in reduced-motion mode")
	}
	updated, _ = m.Update(cmd())
	m = updated.(model)
	view := m.View()
	for _, want := range []string{"mode=explain", "route=canonical-sealed", "target=pi", "outcome=ready", "admitted=true", "changed_state=false", "operation action=replace", "warning[typed-warning]", "Esc back"} {
		if !strings.Contains(view, want) {
			t.Fatalf("TUI Explain missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "token-path") || workflow.prepares != 1 || workflow.executes != 0 {
		t.Fatalf("unsafe or duplicate workflow behavior: prepares=%d executes=%d view=%s", workflow.prepares, workflow.executes, view)
	}
	before := m.installTUI.result.Clone()
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 42, Height: 12})
	m = updated.(model)
	if m.installTUI.result.Route != before.Route || m.installTUI.result.Status != before.Status {
		t.Fatal("resize changed typed Explain semantics")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.installTUI != nil || !m.installSelectionPending || workflow.executes != 0 {
		t.Fatal("Esc did not discard Prepared and return to target selection without effects")
	}
}

func TestW47TUIDryRunRepreparesAndRendersCanonicalResultParity(t *testing.T) {
	ready := explainTUIResult()
	workflow := &tuiWorkflowSpy{result: ready, executeResult: install.Result{
		SchemaVersion: install.ResultSchemaVersion, Mode: install.ModeDryRun, Route: install.RouteCanonical,
		Target: install.TargetPi, Status: install.StatusSucceeded, Admitted: true,
		Operations: []install.Operation{{Resource: "AGENTS.md", Action: "replace"}},
	}}
	m := newInstallModel(workflow, install.Request{Mode: install.ModeExplain, Target: install.TargetPi}, true)
	m.update(m.prepareCmd()())
	cmd := m.update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || m.request.Mode != install.ModeDryRun || m.stage != installPreparing {
		t.Fatalf("Explain to dry-run transition = mode:%s stage:%s", m.request.Mode, m.stage)
	}
	m.update(cmd())
	cmd = m.update(tea.KeyMsg{Type: tea.KeyEnter})
	m.update(cmd())
	view := renderInstallView(m, 80)
	for _, want := range []string{"mode=dry-run", "route=canonical-sealed", "outcome=succeeded", "changed_state=false", "operation action=replace"} {
		if !strings.Contains(view, want) {
			t.Fatalf("TUI dry-run missing %q:\n%s", want, view)
		}
	}
	if workflow.prepares != 2 || workflow.executes != 1 || strings.Contains(view, "explicit-legacy") {
		t.Fatalf("workflow parity = prepares:%d executes:%d view:%s", workflow.prepares, workflow.executes, view)
	}
	cmd = m.update(tea.KeyMsg{Type: tea.KeyEnter})
	m.update(cmd())
	workflow.executeResult.Mode = install.ModeApply
	cmd = m.update(tea.KeyMsg{Type: tea.KeyEnter})
	m.update(cmd())
	if workflow.prepares != 3 || workflow.executes != 2 || m.result.Mode != install.ModeApply || m.result.Route != install.RouteCanonical {
		t.Fatalf("apply parity = prepares:%d executes:%d result:%#v", workflow.prepares, workflow.executes, m.result)
	}
}

func TestTUIExplainRendersTypedRouteErrorWithoutLegacyFallback(t *testing.T) {
	gates := map[install.TargetID]install.Gate{}
	for _, target := range install.SupportedTargets() {
		gates[target] = install.GateOff
	}
	policy, err := install.NewRoutePolicy(gates)
	if err != nil {
		t.Fatal(err)
	}
	workflow := install.NewExplainWorkflow(policy, install.TransactionInput{})
	m := newInstallModel(workflow, install.Request{Mode: install.ModeExplain, Target: install.TargetPi}, true)
	m.update(m.prepareCmd()())
	view := renderInstallView(m, 80)
	if !strings.Contains(view, "error[canonical_route_disabled]") || !strings.Contains(view, "route=canonical-sealed") || strings.Contains(view, "explicit-legacy") {
		t.Fatalf("typed route refusal/fallback parity failed:\n%s", view)
	}
}

func TestTUIExecuteCancellationBlocksNavigationUntilTypedResult(t *testing.T) {
	workflow := &tuiWorkflowSpy{}
	m := newInstallModel(workflow, install.Request{Mode: install.ModeApply, Target: install.TargetPi}, true)
	m.stage = installPrepared
	m.result = install.Result{Mode: install.ModeApply, Route: install.RouteCanonical, Target: install.TargetPi, Status: install.StatusReady, Admitted: true}
	cmd := m.update(tea.KeyMsg{Type: tea.KeyEnter})
	m.update(tea.KeyMsg{Type: tea.KeyLeft})
	if m.back || m.stage != installExecuting {
		t.Fatal("navigation escaped active execution")
	}
	m.update(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.cancelling || !strings.Contains(renderInstallView(m, 80), "waiting for final rollback result") {
		t.Fatal("cancel did not remain blocked with textual status")
	}
	m.update(cmd())
	if m.stage != installResult || m.result.Status != install.StatusCancelled || workflow.executes != 1 {
		t.Fatalf("typed cancel result mismatch: stage=%s status=%s executes=%d", m.stage, m.result.Status, workflow.executes)
	}
}

func TestTUIRetryRepreparesAndResidualRiskDisablesRetry(t *testing.T) {
	workflow := &tuiWorkflowSpy{result: explainTUIResult()}
	m := newInstallModel(workflow, install.Request{Mode: install.ModeExplain, Target: install.TargetPi}, true)
	m.stage, m.result = installResult, install.Result{Status: install.StatusFailed}
	cmd := m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m.update(cmd())
	if workflow.prepares != 1 || m.stage != installPrepared {
		t.Fatalf("retry did not re-Prepare: prepares=%d stage=%s", workflow.prepares, m.stage)
	}
	m.stage, m.result = installResult, install.Result{Status: install.StatusResidualRisk, ResidualRisk: true}
	if cmd := m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}}); cmd != nil || !strings.Contains(renderInstallView(m, 80), "Retry disabled") {
		t.Fatal("residual risk did not disable retry")
	}
}

type tuiLegacyAdapterSpy struct{ prepares, executes int }

func (s *tuiLegacyAdapterSpy) PrepareLegacy(_ context.Context, request install.Request) (install.Prepared, install.Result) {
	s.prepares++
	return install.PrepareExplicitLegacy(request)
}
func (s *tuiLegacyAdapterSpy) ExecuteLegacy(_ context.Context, prepared install.Prepared, _ install.Observer) install.Result {
	s.executes++
	request, ok := install.ConsumeExplicitLegacy(prepared)
	return install.CompleteExplicitLegacy(request, ok)
}

func TestW49TUIRequiresAdvancedLegacySelectionAndConfirmation(t *testing.T) {
	blockedLegacy := &tuiLegacyAdapterSpy{}
	blocked := newInstallModel(&tuiWorkflowSpy{result: install.Result{Mode: install.ModeExplain, Route: install.RouteCanonical, Target: install.TargetPi, Status: install.StatusFailed}}, install.Request{Mode: install.ModeExplain, Target: install.TargetPi}, true)
	blocked.legacy = blockedLegacy
	blocked.update(blocked.prepareCmd()())
	if cmd := blocked.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}); cmd == nil {
		t.Fatal("canonical gate failure hid explicit legacy selection")
	}

	newLegacyModel := func() (*installModel, *tuiLegacyAdapterSpy) {
		legacy := &tuiLegacyAdapterSpy{}
		m := newInstallModel(&tuiWorkflowSpy{result: explainTUIResult()}, install.Request{Mode: install.ModeExplain, Target: install.TargetPi}, true)
		m.legacy = legacy
		m.update(m.prepareCmd()())
		return m, legacy
	}
	m, legacy := newLegacyModel()
	cmd := m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if cmd == nil || legacy.prepares != 0 || legacy.executes != 0 {
		t.Fatal("advanced legacy selection did not require preparation")
	}
	m.update(cmd())
	view := renderInstallView(m, 80)
	for _, want := range []string{"mode=legacy-apply", "route=explicit-legacy", "legacy-deprecated", "confirm explicit legacy apply"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q in %q", want, view)
		}
	}
	m.update(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.back || legacy.executes != 0 {
		t.Fatal("legacy cancellation executed adapter")
	}

	m, legacy = newLegacyModel()
	m.update(m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})())
	cmd = m.update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || legacy.executes != 0 {
		t.Fatal("legacy confirmation did not defer execution")
	}
	m.update(cmd())
	if legacy.prepares != 1 || legacy.executes != 1 || m.result.Route != install.RouteLegacy || m.result.Status != install.StatusSucceeded {
		t.Fatalf("legacy parity prepares=%d executes=%d result=%#v", legacy.prepares, legacy.executes, m.result)
	}
}

func TestTUIRejectsNonTTYBeforeDomainWork(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "not-a-tty")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := requireTTY(file, file); err == nil {
		t.Fatal("non-TTY accepted")
	} else if usage, ok := err.(interface{ Usage() bool }); !ok || !usage.Usage() || !strings.Contains(err.Error(), "lore install --explain") {
		t.Fatalf("non-TTY error is not actionable usage guidance: %v", err)
	}
}
