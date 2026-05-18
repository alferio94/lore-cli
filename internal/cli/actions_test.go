package cli

import (
	"context"
	"encoding/json"
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

func TestRunRememberAndRecallUseSavedAuthConfig(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token"}}
	client := &fakeClient{memory: httpclient.Memory{ID: "m1", ProjectID: "p1", Scope: "project", Type: "decision", Title: "t1", CreatedBy: "user-1"}, memories: []httpclient.Memory{{ID: "m1", ProjectID: "p1", Scope: "project", Type: "decision", Title: "t1", CreatedBy: "user-1"}}}
	app, stdout, _ := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })

	if err := app.runRemember(rememberOptions{ProjectID: "p1", Type: "decision", Title: "t1", Content: "c1", JSONOutput: false}); err != nil {
		t.Fatalf("runRemember() error = %v", err)
	}
	if client.createToken != "secret-token" || client.createRequest.Scope != "project" {
		t.Fatalf("create call = token=%q req=%+v", client.createToken, client.createRequest)
	}
	if err := app.runRecall(recallOptions{ProjectID: "p1", JSONOutput: false}); err != nil {
		t.Fatalf("runRecall() error = %v", err)
	}
	if client.listToken != "secret-token" || client.listFilter.Scope != "project" {
		t.Fatalf("list call = token=%q filter=%+v", client.listToken, client.listFilter)
	}
	assertNoTokenLeak(t, stdout.String(), "", "secret-token")
}

func TestParseMetadataJSONAndLoadAuthenticatedClientValidation(t *testing.T) {
	if _, err := parseMetadataJSON(`{"team":"cli"}`); err != nil {
		t.Fatalf("parseMetadataJSON() error = %v", err)
	}
	if _, err := parseMetadataJSON(`[]`); err == nil {
		t.Fatal("parseMetadataJSON() error = nil, want object validation error")
	}

	store := &fakeStore{path: "/tmp/lore/config.json", loadErr: config.ErrNotFound}
	app, _, _ := newTestApp(store, nil)
	if _, _, err := app.loadAuthenticatedClient(); err == nil || !strings.Contains(err.Error(), "run lore login") {
		t.Fatalf("loadAuthenticatedClient() err = %v, want login remediation", err)
	}

	store = &fakeStore{path: "/tmp/lore/config.json", loadErr: errors.New("decode config: invalid character 'b'")}
	app, _, _ = newTestApp(store, nil)
	if _, _, err := app.loadAuthenticatedClient(); err == nil || !strings.Contains(err.Error(), "inspect or remove") || strings.Contains(err.Error(), "decode config") {
		t.Fatalf("loadAuthenticatedClient() err = %v, want remediation without raw decode details", err)
	}

	store = &fakeStore{path: "/tmp/lore/config.json", loaded: config.Config{ServerURL: "https://example.test"}}
	app, _, _ = newTestApp(store, nil)
	if _, _, err := app.loadAuthenticatedClient(); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("loadAuthenticatedClient() err = %v, want incomplete config error", err)
	}
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

func TestRunAPIRequestUsesSavedAuthAndReturnsSuccessEnvelope(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token"}}
	client := &fakeClient{requestJSONResult: httpclient.RequestJSONResult{StatusCode: 200, RequestID: "req-context", Data: json.RawMessage(`{"project":"lore-cli"}`)}}
	app, stdout, _ := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })

	exitCode := app.runAPIRequest(apiRequestOptions{JSONOutput: true, Method: "get", Path: "/v1/context?project=lore-cli"})
	if exitCode != 0 {
		t.Fatalf("runAPIRequest() exitCode = %d, want 0", exitCode)
	}
	if client.requestJSONToken != "secret-token" || client.requestJSONMethod != "GET" {
		t.Fatalf("requestJSON call = token=%q method=%q", client.requestJSONToken, client.requestJSONMethod)
	}
	if client.requestJSONPath != "/v1/context?project=lore-cli" {
		t.Fatalf("requestJSON path = %q", client.requestJSONPath)
	}
	if got := stdout.String(); !strings.Contains(got, `"ok":true`) || !strings.Contains(got, `"request_id":"req-context"`) {
		t.Fatalf("stdout = %q, want success envelope", got)
	}
	assertNoTokenLeak(t, stdout.String(), "", "secret-token")
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
