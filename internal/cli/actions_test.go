package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/config"
	"github.com/alferio94/lore-cli/internal/httpclient"
	"github.com/alferio94/lore-cli/internal/output"
)

func TestInteractiveActionsExposeAppHelpers(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token"}}
	client := &fakeClient{subject: httpclient.Subject{UserID: "user-1", Kind: "user", TokenSource: "api_token"}}
	app, _, _ := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })

	actions := app.InteractiveActions()
	if _, err := actions.Login(context.Background(), " https://example.test ", " secret-token "); err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if _, err := actions.Logout(context.Background()); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if got := actions.Status(context.Background()); got.Title != "Lore status" {
		t.Fatalf("Status().Title = %q, want %q", got.Title, "Lore status")
	}
	if got := actions.Doctor(context.Background()); got.Title != "Lore doctor" {
		t.Fatalf("Doctor().Title = %q, want %q", got.Title, "Lore doctor")
	}
}

func TestLoginActionMatchesCLIMessageAndTrimsInput(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loadErr: config.ErrNotFound}
	client := &fakeClient{subject: httpclient.Subject{ID: "subject-1", UserID: "user-1", Roles: []string{"admin"}, TokenID: "token-1", TokenSource: "api_token", Kind: "user"}}
	app, _, _ := newTestApp(store, func(baseURL string) (httpclient.Client, error) {
		if got, want := baseURL, "https://example.test"; got != want {
			t.Fatalf("baseURL = %q, want %q", got, want)
		}
		return client, nil
	})

	result, err := app.loginAction(context.Background(), " https://example.test ", " secret-token ")
	if err != nil {
		t.Fatalf("loginAction() error = %v", err)
	}
	if store.saved.ServerURL != "https://example.test" || store.saved.APIToken != "secret-token" {
		t.Fatalf("saved config = %+v, want trimmed values", store.saved)
	}
	if client.meToken != "secret-token" {
		t.Fatalf("Me token = %q, want trimmed token", client.meToken)
	}
	if !strings.Contains(result.Summary, output.FormatSubject(client.subject)) {
		t.Fatalf("summary = %q, want formatted subject", result.Summary)
	}
	assertNoTokenLeak(t, result.Summary, "", "secret-token")
}

func TestLogoutActionRemainsIdempotentAndLocalOnly(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token"}}
	app, _, _ := newTestApp(store, nil)

	first, err := app.logoutAction(context.Background())
	if err != nil {
		t.Fatalf("logoutAction() first error = %v", err)
	}
	second, err := app.logoutAction(context.Background())
	if err != nil {
		t.Fatalf("logoutAction() second error = %v", err)
	}
	if !strings.Contains(first.Summary, "removed local config") || !strings.Contains(second.Summary, "no local config remained") {
		t.Fatalf("logout summaries = %q / %q, want idempotent messaging", first.Summary, second.Summary)
	}
	assertNoTokenLeak(t, first.Summary+second.Summary, "", "secret-token")
}

func TestStatusAndDoctorActionsPreserveDiagnosticSemantics(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token"}}
	client := &fakeClient{
		readyErr: &httpclient.ReadinessError{APIError: httpclient.APIError{StatusCode: 503, Code: "service_unavailable", Message: "service not ready", RequestID: "req-ready"}},
		meErr:    &httpclient.UnauthorizedError{APIError: httpclient.APIError{StatusCode: 401, Code: "unauthorized", Message: "invalid token", RequestID: "req-auth"}},
	}
	app, _, _ := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.LookPath = func(name string) (string, error) { return "", errors.New("missing") }

	status := app.statusAction(context.Background())
	if status.Title != "Lore status" || status.ExitCode != 1 {
		t.Fatalf("statusAction() = %+v, want failing Lore status report", status)
	}
	assertCheckNames(t, status.Checks, "config", "healthz", "readyz", "auth")
	assertNoTokenLeak(t, output.RenderChecks(status.Title, status.Checks), "", "secret-token")

	doctor := app.doctorAction(context.Background())
	if doctor.Title != "Lore doctor" || doctor.ExitCode != 1 {
		t.Fatalf("doctorAction() = %+v, want failing Lore doctor report", doctor)
	}
	assertCheckNames(t, doctor.Checks, "config", "healthz", "readyz", "auth", "pi")
	assertNoTokenLeak(t, output.RenderChecks(doctor.Title, doctor.Checks), "", "secret-token")
}

func assertCheckNames(t *testing.T, checks []output.Check, want ...string) {
	t.Helper()
	if len(checks) != len(want) {
		t.Fatalf("len(checks) = %d, want %d", len(checks), len(want))
	}
	for i, name := range want {
		if got := checks[i].Name; got != name {
			t.Fatalf("checks[%d].Name = %q, want %q", i, got, name)
		}
	}
}
