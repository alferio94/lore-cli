package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/cli"
	"github.com/alferio94/lore-cli/internal/output"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func TestInitialRenderShowsMenuHintsAndInstallEntry(t *testing.T) {
	m := newModel(cli.InteractiveActions{})
	view := m.View()
	for _, want := range []string{"Lore", "Status", "Login", "Install", "Pi", "Explicit subcommands remain available"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
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
	if got := m.statusTitle; got != "Install Lore for Pi" {
		t.Fatalf("statusTitle = %q, want Install Lore for Pi", got)
	}
	for _, want := range []string{"Pi", "Recommended", "Claude Code", "OpenCode", "Codex", "Antigravity", "Coming soon"} {
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

func TestInstallTargetSelectionListsOnlyPiAsSelectable(t *testing.T) {
	m := newModel(cli.InteractiveActions{})
	for i := 0; i < 4; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(model)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if !strings.Contains(m.statusBody, "Only Pi is selectable in this slice.") {
		t.Fatalf("statusBody = %q, want Pi-only guidance", m.statusBody)
	}
	if strings.Contains(m.statusBody, "Claude Code — Recommended") {
		t.Fatalf("statusBody = %q, did not expect non-Pi targets to be marked recommended", m.statusBody)
	}
}

func TestLoginFormMasksTokenAndValidatesRequiredFields(t *testing.T) {
	m := newModel(cli.InteractiveActions{})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.focus != focusLogin {
		t.Fatalf("focus = %v, want focusLogin", m.focus)
	}
	if got := m.loginInputs[1].EchoMode; got != textinput.EchoPassword {
		t.Fatalf("token EchoMode = %v, want password mode", got)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if !strings.Contains(m.loginError, "required") {
		t.Fatalf("loginError = %q, want required-fields error", m.loginError)
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

func TestLoginSuccessAndFailureStates(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		actions := cli.InteractiveActions{
			Login: func(_ context.Context, serverURL, token string) (cli.ActionMessage, error) {
				if serverURL != "https://example.test" || token != "secret-token" {
					t.Fatalf("unexpected credentials: %q %q", serverURL, token)
				}
				return cli.ActionMessage{Summary: "login succeeded"}, nil
			},
		}
		m := newModel(actions)
		m.selected = 1
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(model)
		m.loginInputs[0].SetValue("https://example.test")
		m.loginInputs[1].SetValue("secret-token")
		m.loginInputs[0].Blur()
		m.loginInputs[1].Focus()
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(model)
		updated, _ = m.Update(cmd())
		m = updated.(model)
		if got := m.statusTitle; got != "Login complete" {
			t.Fatalf("statusTitle = %q, want Login complete", got)
		}
		if strings.Contains(m.View(), "secret-token") {
			t.Fatalf("raw token leaked in view: %s", m.View())
		}
	})

	t.Run("failure", func(t *testing.T) {
		actions := cli.InteractiveActions{
			Login: func(context.Context, string, string) (cli.ActionMessage, error) {
				return cli.ActionMessage{}, errors.New("normal user API token required")
			},
		}
		m := newModel(actions)
		m.selected = 1
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(model)
		m.loginInputs[0].SetValue("https://example.test")
		m.loginInputs[1].SetValue("bad-token")
		m.loginInputs[0].Blur()
		m.loginInputs[1].Focus()
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
	})
}
