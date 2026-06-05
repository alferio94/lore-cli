package tui

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alferio94/lore-cli/internal/cli"
	"github.com/alferio94/lore-cli/internal/install"
	"github.com/alferio94/lore-cli/internal/output"
	cliupdate "github.com/alferio94/lore-cli/internal/update"
	"github.com/alferio94/lore-cli/internal/version"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func TestInitialRenderShowsMenuHintsAndInstallEntry(t *testing.T) {
	m := newModel(cli.InteractiveActions{})
	view := m.View()
	for _, want := range []string{"Lore", "Status", "Login", "Install", "Pi", "Antigravity", "password", "compatibility", "Explicit subcommands remain available"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	// 3.x docs/UI slice: the install menu description MUST mention
	// the bounded opencode-plugins bundle and the explicit exclusion
	// list so the user sees the bounded surface before they enter the
	// install flow. The bubbletea view wraps descriptions on word
	// boundaries, so we assert against the underlying items[]
	// description field instead of the wrapped view.
	var installDescription string
	for _, item := range m.items {
		if item.key == "install" {
			installDescription = item.description
			break
		}
	}
	if installDescription == "" {
		t.Fatal("install menu item description not found")
	}
	for _, want := range []string{
		"opencode-plugins",
		"background-agents",
		"model-variants",
		"opencode-subagent-statusline",
		"sdd-engram",
		"logo",
		"config-only projection",
		"managed_by: lore-cli",
		"fail-closed",
	} {
		if !strings.Contains(installDescription, want) {
			t.Fatalf("install menu description missing %q:\n%s", want, installDescription)
		}
	}
}

func TestNavigationAndInstallTargetSelectionMessage(t *testing.T) {
	calls := 0
	m := newModel(cli.InteractiveActions{Install: func(context.Context) cli.ActionReport {
		calls++
		return cli.ActionReport{Title: "Lore install", ExitCode: 0}
	}})
	for i := 0; i < 4; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(model)
	}
	if got := m.items[m.selected].key; got != "install" {
		t.Fatalf("selected key = %q, want install", got)
	}
	if m.items[m.selected].disabled {
		t.Fatal("install item should be selectable")
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if cmd != nil {
		t.Fatal("first install enter unexpectedly started async install")
	}
	if calls != 0 {
		t.Fatalf("install calls = %d, want 0 before confirming Pi target", calls)
	}
	if got := m.statusTitle; got != "Install Lore" {
		t.Fatalf("statusTitle = %q, want Install Lore", got)
	}
	for _, want := range []string{"Pi", "Recommended", "Claude Code", "Codex", "Antigravity", "Coming soon"} {
		if !strings.Contains(m.statusBody, want) {
			t.Fatalf("statusBody missing %q:\n%s", want, m.statusBody)
		}
	}
}

func TestInstallActionRendersSuccessAndLoginRemediationStates(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		m := newModel(cli.InteractiveActions{Install: func(context.Context) cli.ActionReport {
			return cli.ActionReport{Title: "Lore install", ExitCode: 0, Checks: []output.Check{{Name: "install", Status: output.StatusOK, Detail: "created=4 updated=0 unchanged=0 backed_up=0 failed=0"}, {Name: "manifest", Status: output.StatusOK, Detail: "lore-install.json verified"}}}
		}})
		for i := 0; i < 4; i++ {
			updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
			m = updated.(model)
		}
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(model)
		if cmd != nil {
			t.Fatal("first install enter unexpectedly started async install")
		}
		updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(model)
		updated, _ = m.Update(cmd())
		m = updated.(model)
		if got := m.statusTitle; got != "Lore install" {
			t.Fatalf("statusTitle = %q, want Lore install", got)
		}
		if !strings.Contains(m.statusBody, "created=4") || !strings.Contains(m.statusBody, "manifest") {
			t.Fatalf("statusBody = %q, want install summary and manifest info", m.statusBody)
		}
		if got := m.statusTone; got != toneSuccess {
			t.Fatalf("statusTone = %q, want success", got)
		}
	})

	t.Run("login required", func(t *testing.T) {
		m := newModel(cli.InteractiveActions{Install: func(context.Context) cli.ActionReport {
			return cli.ActionReport{Title: "Lore install", ExitCode: 1, Checks: []output.Check{{Name: "config", Status: output.StatusWarn, Detail: "no-config", Action: "Run lore login --server <url> --token <token>."}}}
		}})
		for i := 0; i < 4; i++ {
			updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
			m = updated.(model)
		}
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(model)
		if cmd != nil {
			t.Fatal("first install enter unexpectedly started async install")
		}
		updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(model)
		updated, _ = m.Update(cmd())
		m = updated.(model)
		if got := m.statusTone; got != toneError {
			t.Fatalf("statusTone = %q, want error", got)
		}
		if !strings.Contains(m.statusBody, "Run lore login") {
			t.Fatalf("statusBody = %q, want login remediation", m.statusBody)
		}
	})
}

func TestInstallTargetSelectionSurfacesPiDefaultAndAntigravityMVPGuidance(t *testing.T) {
	m := newModel(cli.InteractiveActions{})
	for i := 0; i < 4; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(model)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	for _, want := range []string{"Pi remains the default recommended path.", "Antigravity", "prompt + skills", "Choose an install target:", "Selected target: Pi"} {
		if !strings.Contains(m.statusBody, want) {
			t.Fatalf("statusBody = %q, want updated install guidance containing %q", m.statusBody, want)
		}
	}
	// 3.x docs/UI slice: the target-selection body must surface the
	// bounded opencode-plugins bundle and the explicit exclusion list
	// so the user sees them when picking the OpenCode target.
	for _, want := range []string{
		"opencode-plugins",
		"background-agents.ts",
		"model-variants.ts",
		"opencode-subagent-statusline",
		"sdd-engram",
		"logo",
		"managed_by: lore-cli",
		"fail-closed",
		"NOT registered in tui.json",
	} {
		if !strings.Contains(m.statusBody, want) {
			t.Fatalf("statusBody missing %q:\n%s", want, m.statusBody)
		}
	}
	if strings.Contains(m.statusBody, "Only Pi is selectable in this slice.") {
		t.Fatalf("statusBody = %q, want updated multi-target messaging", m.statusBody)
	}
	if strings.Contains(m.statusBody, "Claude Code — Recommended") {
		t.Fatalf("statusBody = %q, did not expect non-Pi targets to be marked recommended", m.statusBody)
	}
}

func TestInstallTargetSelectionAllowsAntigravityExecutionWithoutPiBackupPrompt(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	if err := os.MkdirAll(filepath.Join(homeDir, ".pi"), 0o755); err != nil {
		t.Fatalf("MkdirAll ~/.pi: %v", err)
	}

	legacyCalls := 0
	targetCalls := 0
	var gotTarget install.TargetID
	m := newModel(cli.InteractiveActions{
		Install: func(context.Context) cli.ActionReport {
			legacyCalls++
			return cli.ActionReport{Title: "legacy install", ExitCode: 0}
		},
		InstallTarget: func(_ context.Context, target install.TargetID) cli.ActionReport {
			targetCalls++
			gotTarget = target
			return cli.ActionReport{Title: "Lore install", ExitCode: 0, Checks: []output.Check{{Name: "install", Status: output.StatusOK, Detail: "install_target=antigravity"}}}
		},
	})
	m = moveSelectionToInstall(t, m)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	// First down from Pi goes to OpenCode (the bounded config-only
	// projection is supported again in this change).
	if got := m.selectedInstallTarget().ID; got != install.TargetOpenCode {
		t.Fatalf("selected target = %q, want opencode (first down from Pi after OpenCode re-add)", got)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if got := m.selectedInstallTarget().ID; got != install.TargetCodex {
		t.Fatalf("selected target = %q, want codex (second down from Pi)", got)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if got := m.selectedInstallTarget().ID; got != install.TargetAntigravity {
		t.Fatalf("selected target = %q, want antigravity (third down from Pi)", got)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("enter on Antigravity should start install")
	}
	if m.installBackupDecisionPending {
		t.Fatal("Antigravity should not trigger Pi full-backup confirmation")
	}
	updated, _ = m.Update(cmd())
	m = updated.(model)
	if legacyCalls != 0 || targetCalls != 1 {
		t.Fatalf("legacyCalls=%d targetCalls=%d, want 0 and 1", legacyCalls, targetCalls)
	}
	if gotTarget != install.TargetAntigravity {
		t.Fatalf("InstallTarget got %q, want antigravity", gotTarget)
	}
	if !strings.Contains(m.statusBody, "install_target=antigravity") {
		t.Fatalf("statusBody = %q, want Antigravity execution summary", m.statusBody)
	}
}

func TestInstallTargetSelectionMovesBetweenSupportedTargetsOnly(t *testing.T) {
	m := moveSelectionToInstall(t, newModel(cli.InteractiveActions{}))
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if got := m.selectedInstallTarget().ID; got != install.TargetPi {
		t.Fatalf("selected target = %q, want pi", got)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	// First down from Pi goes to OpenCode (the bounded config-only
	// projection is supported again in this change).
	if got := m.selectedInstallTarget().ID; got != install.TargetOpenCode {
		t.Fatalf("selected target after down = %q, want opencode (first down from Pi after OpenCode re-add)", got)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if got := m.selectedInstallTarget().ID; got != install.TargetCodex {
		t.Fatalf("selected target after down = %q, want codex (second down from Pi)", got)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if got := m.selectedInstallTarget().ID; got != install.TargetAntigravity {
		t.Fatalf("selected target after down = %q, want antigravity (third down from Pi)", got)
	}
	if strings.Contains(m.statusBody, "Selected target: Claude Code") {
		t.Fatalf("statusBody = %q, want roadmap targets visible but not selected", m.statusBody)
	}
}

func TestInstallDetectsExistingPiAndPromptsForFullBackupBeforeMutation(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	if err := os.MkdirAll(filepath.Join(homeDir, ".pi"), 0o755); err != nil {
		t.Fatalf("MkdirAll ~/.pi: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".pi", "legacy.txt"), []byte("legacy"), 0o600); err != nil {
		t.Fatalf("WriteFile ~/.pi/legacy.txt: %v", err)
	}

	calls := 0
	m := newModel(cli.InteractiveActions{Install: func(context.Context) cli.ActionReport {
		calls++
		return cli.ActionReport{Title: "Lore install", ExitCode: 0}
	}})
	m = moveSelectionToInstall(t, m)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if cmd != nil {
		t.Fatal("first install enter unexpectedly started async install")
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if cmd != nil {
		t.Fatal("second install enter should prompt for full-backup decision before install mutation")
	}
	if calls != 0 {
		t.Fatalf("install calls = %d, want 0 before deciding how to handle existing ~/.pi", calls)
	}
	combined := strings.ToLower(m.statusTitle + "\n" + m.statusBody)
	for _, want := range []string{"full backup", ".pi", "existing"} {
		if !strings.Contains(combined, want) {
			t.Fatalf("backup decision UI missing %q in title/body:\n%s", want, combined)
		}
	}
}

func TestInstallBackupDecisionDeclineContinuesWithoutFullBackup(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	if err := os.MkdirAll(filepath.Join(homeDir, ".pi"), 0o755); err != nil {
		t.Fatalf("MkdirAll ~/.pi: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".pi", "legacy.txt"), []byte("legacy"), 0o600); err != nil {
		t.Fatalf("WriteFile ~/.pi/legacy.txt: %v", err)
	}

	calls := 0
	m := newModel(cli.InteractiveActions{Install: func(context.Context) cli.ActionReport {
		calls++
		return cli.ActionReport{Title: "Lore install", ExitCode: 0, Checks: []output.Check{{Name: "install", Status: output.StatusOK, Detail: "full-backup=skipped by user choice"}}}
	}})
	m = moveSelectionToInstall(t, m)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("declining full backup should continue install with an explicit skip summary")
	}
	updated, _ = m.Update(cmd())
	m = updated.(model)
	if calls != 1 {
		t.Fatalf("install calls = %d, want 1 after explicit full-backup decline", calls)
	}
	if got := strings.ToLower(m.statusBody); !strings.Contains(got, "full-backup=skipped") {
		t.Fatalf("statusBody = %q, want skipped-backup summary after decline", m.statusBody)
	}
}

func TestInstallBackupDecisionAcceptContinuesInstall(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	if err := os.MkdirAll(filepath.Join(homeDir, ".pi"), 0o755); err != nil {
		t.Fatalf("MkdirAll ~/.pi: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".pi", "legacy.txt"), []byte("legacy"), 0o600); err != nil {
		t.Fatalf("WriteFile ~/.pi/legacy.txt: %v", err)
	}

	calls := 0
	m := newModel(cli.InteractiveActions{Install: func(context.Context) cli.ActionReport {
		calls++
		return cli.ActionReport{Title: "Lore install", ExitCode: 0, Checks: []output.Check{{Name: "install", Status: output.StatusOK, Detail: "full-backup=scheduled"}}}
	}})
	m = moveSelectionToInstall(t, m)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("accepting full backup should continue install")
	}
	updated, _ = m.Update(cmd())
	m = updated.(model)
	if calls != 1 {
		t.Fatalf("install calls = %d, want 1 after accepting full backup", calls)
	}
	if got := strings.ToLower(m.statusBody); !strings.Contains(got, "full-backup=scheduled") {
		t.Fatalf("statusBody = %q, want scheduled-backup summary after acceptance", m.statusBody)
	}
}

func TestInstallBackupDecisionUsesSharedPlanExecutePath(t *testing.T) {
	plan := install.PiInstallPlan{
		Layout:     install.ResolvePiLayout(t.TempDir()),
		ExistingPi: install.ExistingPiState{Exists: true, Path: "/tmp/test-home/.pi", Kind: "directory"},
		FullBackup: &install.FullPiBackupPlan{BackupPath: "/tmp/test-backup", ManifestPath: "/tmp/test-backup/lore-pi-backup.json"},
	}
	planCalls := 0
	execCalls := 0
	legacyCalls := 0
	var executedPlan install.PiInstallPlan
	m := newModel(cli.InteractiveActions{
		Install: func(context.Context) cli.ActionReport {
			legacyCalls++
			return cli.ActionReport{Title: "legacy install", ExitCode: 0}
		},
		PlanPiInstall: func(context.Context) (install.PiInstallPlan, cli.ActionReport, bool) {
			planCalls++
			return plan, cli.ActionReport{Title: "Lore install"}, true
		},
		ExecutePiInstall: func(_ context.Context, got install.PiInstallPlan) cli.ActionReport {
			execCalls++
			executedPlan = got
			return cli.ActionReport{Title: "Lore install", ExitCode: 0, Checks: []output.Check{{Name: "install", Status: output.StatusOK, Detail: "shared-plan-execute-path-used"}}}
		},
	})
	m = moveSelectionToInstall(t, m)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if cmd != nil {
		t.Fatal("shared plan with existing ~/.pi should prompt before execution")
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("declining full backup should continue through shared execute path")
	}
	updated, _ = m.Update(cmd())
	m = updated.(model)
	if planCalls != 1 || execCalls != 1 {
		t.Fatalf("planCalls=%d execCalls=%d, want 1 each", planCalls, execCalls)
	}
	if legacyCalls != 0 {
		t.Fatalf("legacy install calls = %d, want 0", legacyCalls)
	}
	if executedPlan.FullBackup != nil {
		t.Fatalf("executed plan full backup = %+v, want nil after explicit decline", executedPlan.FullBackup)
	}
	if !strings.Contains(m.statusBody, "shared-plan-execute-path-used") {
		t.Fatalf("statusBody = %q, want shared plan/execute evidence", m.statusBody)
	}
}

func TestLoginFormCollectsEmailAndMaskedPasswordWithCompatibilityGuidance(t *testing.T) {
	m := newModel(cli.InteractiveActions{})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.focus != focusLogin {
		t.Fatalf("focus = %v, want focusLogin", m.focus)
	}
	if got := len(m.loginInputs); got != 3 {
		t.Fatalf("login input count = %d, want 3", got)
	}
	if got := m.loginInputs[2].EchoMode; got != textinput.EchoPassword {
		t.Fatalf("password EchoMode = %v, want password mode", got)
	}
	if got := m.statusBody; !strings.Contains(got, "compatibility") || !strings.Contains(got, "password") {
		t.Fatalf("statusBody = %q, want password-first compatibility guidance", got)
	}
	view := m.View()
	for _, want := range []string{"Server URL", "Email", "Password", "--password-stdin", "--token"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Enter on token submits") {
		t.Fatalf("view still contains stale token-submit hint:\n%s", view)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if !strings.Contains(m.loginError, "required") || !strings.Contains(m.loginError, "password") {
		t.Fatalf("loginError = %q, want required password error", m.loginError)
	}
}

func TestLogoutSelectionRendersIdempotentLocalOnlyResult(t *testing.T) {
	calls := 0
	actions := cli.InteractiveActions{
		Logout: func(context.Context) (cli.ActionMessage, error) {
			calls++
			if calls == 1 {
				return cli.ActionMessage{Summary: "logout succeeded: removed local config at /tmp/lore/config.json; no server-side token revocation was performed"}, nil
			}
			return cli.ActionMessage{Summary: "logout succeeded: no local config remained at /tmp/lore/config.json; no server-side token revocation was performed"}, nil
		},
	}
	m := newModel(actions)
	for i := 0; i < 2; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(model)
	}
	if got := m.items[m.selected].key; got != "logout" {
		t.Fatalf("selected key = %q, want logout", got)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	updated, _ = m.Update(cmd())
	m = updated.(model)
	if got := m.statusTitle; got != "Logout complete" {
		t.Fatalf("statusTitle = %q, want Logout complete", got)
	}
	if !strings.Contains(m.View(), "removed local config") || !strings.Contains(m.View(), "no server-side token revocation") {
		t.Fatalf("view = %q, want first logout result", m.View())
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	updated, _ = m.Update(cmd())
	m = updated.(model)
	if !strings.Contains(m.View(), "no local config remained") {
		t.Fatalf("view = %q, want idempotent repeat result", m.View())
	}
	if calls != 2 {
		t.Fatalf("logout calls = %d, want 2", calls)
	}
}

func TestStatusAndDoctorResultsRenderInDetailPane(t *testing.T) {
	actions := cli.InteractiveActions{
		Status: func(context.Context) cli.ActionReport {
			return cli.ActionReport{Title: "Lore status", ExitCode: 0, Checks: []output.Check{{Name: "healthz", Status: output.StatusOK, Detail: "server is live"}}}
		},
		Doctor: func(context.Context) cli.ActionReport {
			return cli.ActionReport{Title: "Lore doctor", ExitCode: 1, Checks: []output.Check{{Name: "readyz", Status: output.StatusFail, Detail: "service not ready", Action: "retry later"}}}
		},
	}
	m := newModel(actions)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if !m.loading || cmd == nil {
		t.Fatalf("status should enter loading state")
	}
	updated, _ = m.Update(cmd())
	m = updated.(model)
	if !strings.Contains(m.statusBody, "[OK] healthz") {
		t.Fatalf("statusBody = %q, want rendered status checks", m.statusBody)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	updated, _ = m.Update(cmd())
	m = updated.(model)
	if !strings.Contains(m.statusBody, "[FAIL] readyz") {
		t.Fatalf("doctor body = %q, want failure check", m.statusBody)
	}
	if got := m.statusTone; got != toneError {
		t.Fatalf("statusTone = %q, want error", got)
	}
}

func TestInitStartsNonBlockingUpdateCheckAndRendersAvailabilityBanner(t *testing.T) {
	calls := 0
	m := newModel(cli.InteractiveActions{CheckForUpdate: func(context.Context) cli.UpdateAvailability {
		calls++
		return cli.UpdateAvailability{Checked: true, Available: true, CurrentVersion: "v1.0.0", LatestVersion: "v1.1.0", Detail: "Binary-only update available: v1.0.0 → v1.1.0. Pi runtime and ~/.pi remain untouched."}
	}})
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init should start a background update check when the shared updater boundary is available")
	}
	if m.loading {
		t.Fatal("background update check must not flip the main loading state")
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if got := m.items[m.selected].key; got != "login" {
		t.Fatalf("selected key = %q, want login while update check is pending", got)
	}
	updated, _ = m.Update(cmd())
	m = updated.(model)
	if calls != 1 {
		t.Fatalf("update check calls = %d, want 1", calls)
	}
	view := m.View()
	for _, want := range []string{"Update available", "v1.0.0", "v1.1.0", "Pi runtime untouched"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestUpdateSelectionPromptsThenRunsBinaryOnlyApply(t *testing.T) {
	calls := 0
	m := newModel(cli.InteractiveActions{Update: func(context.Context) cli.ActionReport {
		calls++
		return cli.ActionReport{Title: "Lore update", ExitCode: 0, Checks: []output.Check{{Name: "update", Status: output.StatusOK, Detail: "current=v1.0.0 latest=v1.1.0 status=applied scope=binary-only pi_runtime=untouched pi_dir=~/.pi untouched"}}}
	}})
	updated, _ := m.Update(updateCheckMsg{availability: cli.UpdateAvailability{Checked: true, Available: true, CurrentVersion: "v1.0.0", LatestVersion: "v1.1.0", Detail: "Binary-only update available: v1.0.0 → v1.1.0. Pi runtime and ~/.pi remain untouched."}})
	m = updated.(model)
	m = moveSelectionToUpdate(t, m)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if cmd != nil {
		t.Fatal("selecting update should prompt for explicit confirmation before mutation")
	}
	if !m.updateConfirmationPending {
		t.Fatal("update confirmation should be pending after selecting an available update")
	}
	for _, want := range []string{"Update only the Lore CLI binary", "v1.0.0", "v1.1.0", "~/.pi remain untouched"} {
		if !strings.Contains(m.statusBody, want) {
			t.Fatalf("statusBody missing %q:\n%s", want, m.statusBody)
		}
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(model)
	if cmd == nil || !m.loading {
		t.Fatal("confirming update should enter async progress state")
	}
	updated, _ = m.Update(cmd())
	m = updated.(model)
	if calls != 1 {
		t.Fatalf("update calls = %d, want 1", calls)
	}
	if got := m.statusTitle; got != "Lore update" {
		t.Fatalf("statusTitle = %q, want Lore update", got)
	}
	for _, want := range []string{"scope=binary-only", "pi_runtime=untouched", "~/.pi untouched"} {
		if !strings.Contains(m.statusBody, want) {
			t.Fatalf("statusBody missing %q:\n%s", want, m.statusBody)
		}
	}
	if got := m.statusTone; got != toneSuccess {
		t.Fatalf("statusTone = %q, want success", got)
	}
}

func TestUpdateFlowUsesSharedUpdaterWithRealisticService(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix self-update test fixture requires shell execution")
	}

	const (
		currentVersion = "v1.0.0"
		latestVersion  = "v1.1.0"
	)

	targetDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(targetDir) error = %v", err)
	}
	targetPath := filepath.Join(targetDir, "lore")
	if err := os.WriteFile(targetPath, []byte(fakeVersionBinaryScript(currentVersion, "cur1234")), 0o755); err != nil {
		t.Fatalf("WriteFile(current lore) error = %v", err)
	}

	cacheRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(cacheRoot) error = %v", err)
	}
	cacheDir := filepath.Join(cacheRoot, "config")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(cacheDir) error = %v", err)
	}

	assetName := fmt.Sprintf("lore-cli_%s_%s_%s.tar.gz", latestVersion, runtime.GOOS, runtime.GOARCH)
	archiveBytes := mustUnixArchive(t, "lore", []byte(fakeVersionBinaryScript(latestVersion, "next5678")), 0o755)
	checksum := sha256Hex(archiveBytes)

	checkRequests := 0
	assetRequests := 0
	serverURL := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/alferio94/lore-cli/releases/latest":
			checkRequests++
			w.Header().Set("ETag", `"etag-v1.1.0"`)
			_, _ = fmt.Fprintf(w, `{"tag_name":%q,"assets":[{"name":%q,"browser_download_url":%q},{"name":"SHA256SUMS","browser_download_url":%q}]}`,
				latestVersion,
				assetName,
				serverURL+"/downloads/"+assetName,
				serverURL+"/downloads/SHA256SUMS",
			)
		case "/downloads/" + assetName:
			assetRequests++
			_, _ = w.Write(archiveBytes)
		case "/downloads/SHA256SUMS":
			_, _ = w.Write([]byte(checksum + "  " + assetName + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	serverURL = server.URL
	defer server.Close()

	probed := []string{}
	app := &cli.App{
		LookPath:       func(string) (string, error) { return targetPath, nil },
		ExecutablePath: func() (string, error) { return targetPath, nil },
		BuildInfo:      version.Info{Version: currentVersion, Commit: "cur1234", BuildDate: "2026-05-20T00:00:00Z"},
		UpdateServiceFactory: func() (cliupdate.Service, error) {
			return cliupdate.Service{
				HTTP:      server.Client(),
				Now:       func() time.Time { return time.Date(2026, 5, 20, 22, 0, 0, 0, time.UTC) },
				ExecPath:  func() (string, error) { return targetPath, nil },
				LookPath:  func(string) (string, error) { return targetPath, nil },
				ConfigDir: func() (string, error) { return cacheDir, nil },
				CandidateVersion: func(ctx context.Context, path string) (version.Info, error) {
					probed = append(probed, path)
					return probeTestBinaryVersion(ctx, path)
				},
				GitHubBaseURL: serverURL,
				GOOS:          runtime.GOOS,
				GOARCH:        runtime.GOARCH,
				BuildInfo:     version.Info{Version: currentVersion, Commit: "cur1234", BuildDate: "2026-05-20T00:00:00Z"},
			}, nil
		},
	}

	m := newModel(app.InteractiveActions())
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init should start a real update check")
	}
	updated, _ := m.Update(cmd())
	m = updated.(model)
	if !m.updateAvailable {
		t.Fatalf("updateAvailable = %v, want true", m.updateAvailable)
	}
	m = moveSelectionToUpdate(t, m)

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if cmd != nil || !m.updateConfirmationPending {
		t.Fatal("selecting update should open confirmation without starting apply")
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(model)
	if cmd == nil || !m.loading {
		t.Fatal("confirming update should enter async progress state")
	}
	updated, _ = m.Update(cmd())
	m = updated.(model)

	if got := m.statusTitle; got != "Lore update" {
		t.Fatalf("statusTitle = %q, want Lore update", got)
	}
	if got := m.statusTone; got != toneSuccess {
		t.Fatalf("statusTone = %q, want success", got)
	}
	for _, want := range []string{"status=applied", "installed=v1.1.0", "scope=binary-only", "~/.pi untouched"} {
		if !strings.Contains(m.statusBody, want) {
			t.Fatalf("statusBody missing %q:\n%s", want, m.statusBody)
		}
	}
	if checkRequests < 2 {
		t.Fatalf("release checks = %d, want at least 2 (background availability + apply preflight)", checkRequests)
	}
	if assetRequests != 1 {
		t.Fatalf("asset downloads = %d, want 1", assetRequests)
	}
	if len(probed) != 2 {
		t.Fatalf("CandidateVersion calls = %d, want 2", len(probed))
	}
	if probed[0] == targetPath {
		t.Fatalf("first probe path = %q, want extracted candidate path", probed[0])
	}
	if got := probed[1]; got != targetPath {
		t.Fatalf("second probe path = %q, want %q", got, targetPath)
	}
	installed, err := probeTestBinaryVersion(context.Background(), targetPath)
	if err != nil {
		t.Fatalf("probe installed lore error = %v", err)
	}
	if got := installed.Version; got != latestVersion {
		t.Fatalf("installed version = %q, want %q", got, latestVersion)
	}
}

func TestLoginSuccessAndFailureStates(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		actions := cli.InteractiveActions{
			LoginWithInput: func(_ context.Context, input cli.LoginInput) (cli.ActionMessage, error) {
				if input.ServerURL != "https://example.test" || input.Email != "admin@example.com" || input.Password != "super-secret-password" || input.Mode != "password" {
					t.Fatalf("unexpected login input: %+v", input)
				}
				return cli.ActionMessage{Summary: "login succeeded"}, nil
			},
		}
		m := newModel(actions)
		m.selected = 1
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(model)
		m.loginInputs[0].SetValue("https://example.test")
		m.loginInputs[1].SetValue("admin@example.com")
		m.loginInputs[2].SetValue("super-secret-password")
		m.loginInputs[0].Blur()
		m.loginInputs[2].Focus()
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(model)
		updated, _ = m.Update(cmd())
		m = updated.(model)
		if got := m.statusTitle; got != "Login complete" {
			t.Fatalf("statusTitle = %q, want Login complete", got)
		}
		if strings.Contains(m.View(), "super-secret-password") {
			t.Fatalf("raw password leaked in view: %s", m.View())
		}
	})

	t.Run("failure", func(t *testing.T) {
		actions := cli.InteractiveActions{
			LoginWithInput: func(context.Context, cli.LoginInput) (cli.ActionMessage, error) {
				return cli.ActionMessage{}, errors.New("password login is unsupported on this server; use lore login --server <url> --token <token>")
			},
		}
		m := newModel(actions)
		m.selected = 1
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(model)
		m.loginInputs[0].SetValue("https://example.test")
		m.loginInputs[1].SetValue("admin@example.com")
		m.loginInputs[2].SetValue("bad-password")
		m.loginInputs[0].Blur()
		m.loginInputs[2].Focus()
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(model)
		updated, _ = m.Update(cmd())
		m = updated.(model)
		if got := m.statusTitle; got != "Login failed" {
			t.Fatalf("statusTitle = %q, want Login failed", got)
		}
		if got := m.statusTone; got != toneError {
			t.Fatalf("statusTone = %q, want error", got)
		}
		if strings.Contains(m.View(), "bad-password") {
			t.Fatalf("raw password leaked in view: %s", m.View())
		}
		if !strings.Contains(m.statusBody, "--token") {
			t.Fatalf("statusBody = %q, want compatibility guidance", m.statusBody)
		}
	})
}

func moveSelectionToInstall(t *testing.T, m model) model {
	t.Helper()
	for i := 0; i < 4; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(model)
	}
	if got := m.items[m.selected].key; got != "install" {
		t.Fatalf("selected key = %q, want install", got)
	}
	return m
}

func moveSelectionToUpdate(t *testing.T, m model) model {
	t.Helper()
	for i := 0; i < 5; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(model)
	}
	if got := m.items[m.selected].key; got != "update" {
		t.Fatalf("selected key = %q, want update", got)
	}
	return m
}

func fakeVersionBinaryScript(versionValue, commit string) string {
	return fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"version\" ] && [ \"$2\" = \"--json\" ]; then\n  printf '{\"version\":\"%s\",\"commit\":\"%s\",\"buildDate\":\"2026-05-20T00:00:00Z\"}'\n  exit 0\nfi\nprintf 'unexpected args: %s %s\\n' \"$1\" \"$2\" >&2\nexit 1\n", versionValue, commit, "%s", "%s")
}

func probeTestBinaryVersion(ctx context.Context, path string) (version.Info, error) {
	out, err := exec.CommandContext(ctx, path, "version", "--json").Output()
	if err != nil {
		return version.Info{}, err
	}
	var info version.Info
	if err := json.Unmarshal(out, &info); err != nil {
		return version.Info{}, err
	}
	return info.Normalized(), nil
}

func mustUnixArchive(t *testing.T, name string, data []byte, mode int64) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data))}); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar.Close() error = %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip.Close() error = %v", err)
	}
	return buf.Bytes()
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
