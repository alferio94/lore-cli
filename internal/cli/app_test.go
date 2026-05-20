package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alferio94/lore-cli/internal/auth"
	"github.com/alferio94/lore-cli/internal/config"
	"github.com/alferio94/lore-cli/internal/httpclient"
	"github.com/alferio94/lore-cli/internal/install"
	"github.com/alferio94/lore-cli/internal/version"
)

func TestLoginSavesValidatedSession(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loadErr: config.ErrNotFound}
	client := &fakeClient{subject: httpclient.Subject{ID: "subject-1", UserID: "user-1", Roles: []string{"admin"}, TokenID: "token-1", TokenSource: "api_token", Kind: "user"}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) {
		if got, want := baseURL, "https://example.test/"; got != want {
			t.Fatalf("baseURL = %q, want %q", got, want)
		}
		return client, nil
	})

	exitCode := app.Run([]string{"login", "--server", " https://example.test/ ", "--token", " secret-token "})
	if exitCode != 0 {
		t.Fatalf("Run() exitCode = %d, want 0, stderr=%q", exitCode, stderr.String())
	}
	if store.saved.ServerURL != "https://example.test/" || store.saved.APIToken != "" || store.saved.CredentialAccount == "" {
		t.Fatalf("saved config = %+v, want metadata-only saved state", store.saved)
	}
	if got := app.Auth.(*fakeAuthManager).savedToken; got != "secret-token" {
		t.Fatalf("savedToken = %q, want secret-token", got)
	}
	if client.meToken != "secret-token" {
		t.Fatalf("Me token = %q, want trimmed token", client.meToken)
	}
	if !strings.Contains(stdout.String(), "login succeeded") || !strings.Contains(stdout.String(), "user_id=user-1") {
		t.Fatalf("stdout = %q, want login success summary", stdout.String())
	}
	assertNoTokenLeak(t, stdout.String(), stderr.String(), "secret-token")
}

func TestLoginRejectsUnauthorizedWithoutSaving(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loadErr: config.ErrNotFound}
	client := &fakeClient{meErr: &httpclient.UnauthorizedError{APIError: httpclient.APIError{StatusCode: 401, Code: "unauthorized", Message: "normal user API token required", RequestID: "req-401"}}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) {
		return client, nil
	})

	exitCode := app.Run([]string{"login", "--server", "https://example.test", "--token", "secret-token"})
	if exitCode != 1 {
		t.Fatalf("Run() exitCode = %d, want 1", exitCode)
	}
	if store.saveCalls != 0 {
		t.Fatalf("saveCalls = %d, want 0", store.saveCalls)
	}
	if !strings.Contains(stderr.String(), "normal user API token required") {
		t.Fatalf("stderr = %q, want actionable unauthorized guidance", stderr.String())
	}
	assertNoTokenLeak(t, stdout.String(), stderr.String(), "secret-token")
}

func TestLoginExplainsUnavailableKeychainWithoutLeakingToken(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loadErr: config.ErrNotFound}
	client := &fakeClient{subject: httpclient.Subject{UserID: "user-1", Kind: "user"}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) {
		return client, nil
	})
	app.Auth.(*fakeAuthManager).saveErr = &auth.Error{Code: auth.ErrCredentialUnavailable, Op: "store credential", Err: errors.New("keychain locked")}

	exitCode := app.Run([]string{"login", "--server", "https://example.test", "--token", "secret-token"})
	if exitCode != 1 {
		t.Fatalf("Run() exitCode = %d, want 1", exitCode)
	}
	for _, want := range []string{"OS keychain", "headless Linux", "gnome-keyring", "run lore login again"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want substring %q", stderr.String(), want)
		}
	}
	assertNoTokenLeak(t, stdout.String(), stderr.String(), "secret-token")
}

func TestLoginRejectsInvalidServerURLWithoutSaving(t *testing.T) {
	store := config.NewStore(t.TempDir())
	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	app := &App{
		Stdout: stdout,
		Stderr: stderr,
		Store:  store,
		ClientFactory: func(baseURL string) (httpclient.Client, error) {
			return httpclient.New(baseURL, 0)
		},
		LookPath: func(name string) (string, error) { return "/usr/bin/pi", nil },
	}

	exitCode := app.Run([]string{"login", "--server", "ftp://example.test", "--token", "secret-token"})
	if exitCode != 1 {
		t.Fatalf("Run() exitCode = %d, want 1", exitCode)
	}
	if _, err := store.Load(); !errors.Is(err, config.ErrNotFound) {
		t.Fatalf("Load() err = %v, want ErrNotFound", err)
	}
	if !strings.Contains(stderr.String(), "server URL must start with http:// or https://") {
		t.Fatalf("stderr = %q, want invalid URL guidance", stderr.String())
	}
	assertNoTokenLeak(t, stdout.String(), stderr.String(), "secret-token")
}

func TestLoginRejectsUnreachableServerWithoutSaving(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	serverURL := srv.URL
	srv.Close()

	store := config.NewStore(t.TempDir())
	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	app := &App{
		Stdout: stdout,
		Stderr: stderr,
		Store:  store,
		ClientFactory: func(baseURL string) (httpclient.Client, error) {
			return httpclient.New(baseURL, 50*time.Millisecond)
		},
		LookPath: func(name string) (string, error) { return "/usr/bin/pi", nil },
	}

	exitCode := app.Run([]string{"login", "--server", serverURL, "--token", "secret-token"})
	if exitCode != 1 {
		t.Fatalf("Run() exitCode = %d, want 1", exitCode)
	}
	if _, err := store.Load(); !errors.Is(err, config.ErrNotFound) {
		t.Fatalf("Load() err = %v, want ErrNotFound", err)
	}
	if !strings.Contains(stderr.String(), "network request failed") {
		t.Fatalf("stderr = %q, want network guidance", stderr.String())
	}
	assertNoTokenLeak(t, stdout.String(), stderr.String(), "secret-token")
}

func TestStatusWithoutConfigReportsNoConfig(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loadErr: config.ErrNotFound}
	app, stdout, _ := newTestApp(store, nil)

	exitCode := app.Run([]string{"status"})
	if exitCode != 1 {
		t.Fatalf("Run() exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stdout.String(), "no-config") {
		t.Fatalf("stdout = %q, want no-config", stdout.String())
	}
}

func TestStatusReportsHealthReadinessAndIdentity(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token"}}
	client := &fakeClient{
		subject: httpclient.Subject{ID: "subject-1", UserID: "user-1", Roles: []string{"developer"}, TokenID: "token-1", TokenSource: "api_token", Kind: "user"},
	}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })

	exitCode := app.Run([]string{"status"})
	if exitCode != 0 {
		t.Fatalf("Run() exitCode = %d, want 0, stderr=%q", exitCode, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"[OK] config", "[OK] healthz", "[OK] readyz", "[OK] auth", "user_id=user-1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout = %q, want substring %q", out, want)
		}
	}
	assertNoTokenLeak(t, out, stderr.String(), "secret-token")
}

func TestStatusWithMissingCredentialReportsSharedRemediation(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loaded: config.Config{ServerURL: "https://example.test", CredentialAccount: "acct-test"}}
	app, stdout, stderr := newTestApp(store, nil)

	exitCode := app.Run([]string{"status"})
	if exitCode != 1 {
		t.Fatalf("Run() exitCode = %d, want 1, stderr=%q", exitCode, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"[OK] config", "[FAIL] auth", "saved login state is incomplete", "Run lore login again with a valid normal user API token."} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout = %q, want substring %q", out, want)
		}
	}
}

func TestDoctorReportsFailuresAndPiAvailability(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token"}}
	client := &fakeClient{
		readyErr: &httpclient.ReadinessError{APIError: httpclient.APIError{StatusCode: 503, Code: "service_unavailable", Message: "service not ready", RequestID: "req-ready"}},
		meErr:    &httpclient.UnauthorizedError{APIError: httpclient.APIError{StatusCode: 401, Code: "unauthorized", Message: "invalid token", RequestID: "req-auth"}},
	}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.LookPath = func(name string) (string, error) { return "", errors.New("missing") }

	exitCode := app.Run([]string{"doctor"})
	if exitCode != 1 {
		t.Fatalf("Run() exitCode = %d, want 1", exitCode)
	}
	out := stdout.String()
	for _, want := range []string{"[OK] healthz", "[FAIL] readyz", "[FAIL] auth", "[WARN] pi", "request_id=req-ready", "normal user API token required"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout = %q, want substring %q", out, want)
		}
	}
	assertNoTokenLeak(t, out, stderr.String(), "secret-token")
}

func TestLogoutClearsExistingConfigAndRemainsIdempotent(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loaded: config.Config{ServerURL: "https://example.test", CredentialAccount: "acct-test"}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) {
		return &fakeClient{}, nil
	})
	app.Auth.(*fakeAuthManager).session = auth.Session{ServerURL: "https://example.test", Token: "secret-token", ConfigPath: store.path, CredentialAccount: "acct-test"}

	exitCode := app.Run([]string{"logout"})
	if exitCode != 0 {
		t.Fatalf("Run() exitCode = %d, want 0, stderr=%q", exitCode, stderr.String())
	}
	if _, err := store.Load(); !errors.Is(err, config.ErrNotFound) {
		t.Fatalf("Load() err = %v, want ErrNotFound after logout", err)
	}
	if !strings.Contains(stdout.String(), "removed local config") || !strings.Contains(stdout.String(), "no server-side token revocation was performed") {
		t.Fatalf("stdout = %q, want removal and local-only logout messaging", stdout.String())
	}
	assertNoTokenLeak(t, stdout.String(), stderr.String(), "secret-token")

	stdout.Reset()
	stderr.Reset()
	if exitCode := app.Run([]string{"logout"}); exitCode != 0 {
		t.Fatalf("second Run() exitCode = %d, want 0, stderr=%q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "no local config remained") || !strings.Contains(stdout.String(), "no server-side token revocation was performed") {
		t.Fatalf("stdout = %q, want idempotent no-config messaging", stdout.String())
	}
}

func TestLogoutIsIdempotentAndLocalOnly(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loadErr: config.ErrNotFound}
	app, stdout, stderr := newTestApp(store, nil)

	exitCode := app.Run([]string{"logout"})
	if exitCode != 0 {
		t.Fatalf("Run() exitCode = %d, want 0, stderr=%q", exitCode, stderr.String())
	}
	if store.deleteCalls != 1 {
		t.Fatalf("deleteCalls = %d, want 1", store.deleteCalls)
	}
	if !strings.Contains(stdout.String(), "no server-side token revocation was performed") {
		t.Fatalf("stdout = %q, want local-only logout messaging", stdout.String())
	}
}

func TestRememberRequiresFlagsAndSavedLogin(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loadErr: config.ErrNotFound}
	app, stdout, stderr := newTestApp(store, nil)

	if exitCode := app.Run([]string{"remember"}); exitCode != 1 {
		t.Fatalf("missing flags exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "Usage: lore remember") {
		t.Fatalf("stderr = %q, want remember usage", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := app.Run([]string{"remember", "--project-id", "p1", "--type", "decision", "--title", "t1", "--content", "c1"}); exitCode != 1 {
		t.Fatalf("no-login exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "run lore login") {
		t.Fatalf("stderr = %q, want login remediation", stderr.String())
	}
}

func TestRememberRejectsInvalidMetadataWithoutRequest(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token"}}
	client := &fakeClient{}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })

	exitCode := app.Run([]string{"remember", "--project-id", "p1", "--type", "decision", "--title", "t1", "--content", "c1", "--metadata-json", "[]"})
	if exitCode != 1 {
		t.Fatalf("Run() exitCode = %d, want 1", exitCode)
	}
	if client.createCalls != 0 {
		t.Fatalf("createCalls = %d, want 0", client.createCalls)
	}
	if !strings.Contains(stderr.String(), "metadata-json") {
		t.Fatalf("stderr = %q, want metadata validation error", stderr.String())
	}
	assertNoTokenLeak(t, stdout.String(), stderr.String(), "secret-token")
}

func TestRememberSupportsHumanAndJSONOutput(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token"}}
	memory := httpclient.Memory{ID: "m1", ProjectID: "p1", Scope: "project", Type: "decision", Title: "Ship it", Content: "long content", Metadata: map[string]any{"team": "cli"}, CreatedBy: "user-1"}
	client := &fakeClient{memory: memory}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })

	if exitCode := app.Run([]string{"remember", "--project-id", "p1", "--type", "decision", "--title", "Ship it", "--content", "long content"}); exitCode != 0 {
		t.Fatalf("human Run() exitCode = %d, want 0, stderr=%q", exitCode, stderr.String())
	}
	if client.createRequest.Scope != "project" || client.createRequest.ProjectID != "p1" {
		t.Fatalf("createRequest = %+v, want default scope and project", client.createRequest)
	}
	if strings.Contains(stdout.String(), "long content") {
		t.Fatalf("stdout leaked content: %q", stdout.String())
	}
	assertNoTokenLeak(t, stdout.String(), stderr.String(), "secret-token")

	stdout.Reset()
	stderr.Reset()
	if exitCode := app.Run([]string{"remember", "--project-id", "p1", "--type", "decision", "--title", "Ship it", "--content", "long content", "--json", "--metadata-json", `{"team":"cli"}`}); exitCode != 0 {
		t.Fatalf("json Run() exitCode = %d, want 0, stderr=%q", exitCode, stderr.String())
	}
	var got struct {
		Data httpclient.Memory `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, stdout=%q", err, stdout.String())
	}
	if got.Data.ID != "m1" || client.createRequest.Metadata["team"] != "cli" {
		t.Fatalf("json/output mismatch: got=%+v req=%+v", got.Data, client.createRequest)
	}
}

func TestRecallSupportsHumanAndJSONOutput(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token"}}
	client := &fakeClient{memories: []httpclient.Memory{{ID: "m1", ProjectID: "p1", Scope: "project", Type: "decision", Title: "t1", Content: "c1", CreatedBy: "user-1"}}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })

	if exitCode := app.Run([]string{"recall", "--project-id", "p1", "--type", "decision", "--limit", "10"}); exitCode != 0 {
		t.Fatalf("human Run() exitCode = %d, want 0, stderr=%q", exitCode, stderr.String())
	}
	if client.listFilter.ProjectID != "p1" || client.listFilter.Type != "decision" || client.listFilter.Scope != "project" || client.listFilter.Limit != 10 {
		t.Fatalf("listFilter = %+v", client.listFilter)
	}
	if strings.Contains(stdout.String(), "c1") {
		t.Fatalf("stdout leaked content: %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := app.Run([]string{"recall", "--project-id", "p1", "--json"}); exitCode != 0 {
		t.Fatalf("json Run() exitCode = %d, want 0, stderr=%q", exitCode, stderr.String())
	}
	var got struct {
		Data []httpclient.Memory `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, stdout=%q", err, stdout.String())
	}
	if len(got.Data) != 1 || got.Data[0].ID != "m1" {
		t.Fatalf("JSON output = %+v", got)
	}
	assertNoTokenLeak(t, stdout.String(), stderr.String(), "secret-token")
}

func TestRememberAndRecallRenderRequestIDsOnServerErrors(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token"}}
	client := &fakeClient{}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })

	client.createErr = &httpclient.APIError{StatusCode: 400, Code: "bad_request", Message: "invalid memory", RequestID: "req-remember"}
	if exitCode := app.Run([]string{"remember", "--project-id", "p1", "--type", "decision", "--title", "t1", "--content", "c1"}); exitCode != 1 {
		t.Fatalf("remember exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "remember failed: invalid memory (request_id=req-remember)") {
		t.Fatalf("remember stderr = %q, want request-id-safe server error", stderr.String())
	}
	assertNoTokenLeak(t, stdout.String(), stderr.String(), "secret-token")

	stdout.Reset()
	stderr.Reset()
	client.createErr = nil
	client.listErr = &httpclient.APIError{StatusCode: 500, Code: "internal_error", Message: "server exploded", RequestID: "req-recall"}
	if exitCode := app.Run([]string{"recall", "--project-id", "p1"}); exitCode != 1 {
		t.Fatalf("recall exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "recall failed: server exploded (request_id=req-recall)") {
		t.Fatalf("recall stderr = %q, want request-id-safe server error", stderr.String())
	}
	assertNoTokenLeak(t, stdout.String(), stderr.String(), "secret-token")
}

func TestVersionDefaultOutput(t *testing.T) {
	app, stdout, stderr := newVersionOnlyApp(version.Info{})

	exitCode := app.Run([]string{"version"})
	if exitCode != 0 {
		t.Fatalf("Run() exitCode = %d, want 0, stderr=%q", exitCode, stderr.String())
	}
	if got, want := stdout.String(), "lore version dev commit=none buildDate=unknown\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestVersionJSONOutput(t *testing.T) {
	app, stdout, stderr := newVersionOnlyApp(version.Info{Version: "v1.2.3", Commit: "abc1234", BuildDate: "2026-05-17T12:34:56Z"})

	exitCode := app.Run([]string{"version", "--json"})
	if exitCode != 0 {
		t.Fatalf("Run() exitCode = %d, want 0, stderr=%q", exitCode, stderr.String())
	}

	var got map[string]string
	if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, stdout=%q", err, stdout.String())
	}
	if got["version"] != "v1.2.3" || got["commit"] != "abc1234" || got["buildDate"] != "2026-05-17T12:34:56Z" {
		t.Fatalf("JSON output = %#v", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestVersionRejectsExtraArgs(t *testing.T) {
	app, _, stderr := newVersionOnlyApp(version.Info{})

	exitCode := app.Run([]string{"version", "extra"})
	if exitCode != 1 {
		t.Fatalf("Run() exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "Usage: lore version [--json]") {
		t.Fatalf("stderr = %q, want version usage", stderr.String())
	}
}

func TestVersionRunsWithoutStoreOrNetworkDependencies(t *testing.T) {
	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	app := &App{Stdout: stdout, Stderr: stderr, BuildInfo: version.Info{Version: "v9.9.9", Commit: "deadbeef", BuildDate: "2026-05-17T00:00:00Z"}}

	exitCode := app.Run([]string{"version"})
	if exitCode != 0 {
		t.Fatalf("Run() exitCode = %d, want 0, stderr=%q", exitCode, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "v9.9.9") || !strings.Contains(got, "deadbeef") {
		t.Fatalf("stdout = %q, want build metadata", got)
	}
}

func TestZeroArgAndExplicitTUIDispatch(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loadErr: config.ErrNotFound}
	app, stdout, stderr := newTestApp(store, nil)

	calls := 0
	app.TUIRunner = func(_ context.Context, actions InteractiveActions) error {
		calls++
		if actions.Login == nil || actions.Status == nil || actions.Logout == nil || actions.Doctor == nil {
			t.Fatalf("interactive actions were not wired")
		}
		return nil
	}

	if exitCode := app.Run(nil); exitCode != 0 {
		t.Fatalf("zero-arg exitCode = %d, want 0, stderr=%q", exitCode, stderr.String())
	}
	if exitCode := app.Run([]string{"tui"}); exitCode != 0 {
		t.Fatalf("explicit tui exitCode = %d, want 0, stderr=%q", exitCode, stderr.String())
	}
	if calls != 2 {
		t.Fatalf("TUIRunner calls = %d, want 2", calls)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("unexpected output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestTUIRunnerFailuresAndUsage(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loadErr: config.ErrNotFound}
	app, _, stderr := newTestApp(store, nil)
	app.TUIRunner = func(context.Context, InteractiveActions) error { return errors.New("tty unavailable") }

	if exitCode := app.Run(nil); exitCode != 1 {
		t.Fatalf("zero-arg exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "failed to start interactive UI") {
		t.Fatalf("stderr = %q, want interactive UI failure", stderr.String())
	}

	stderr.Reset()
	app.TUIRunner = func(context.Context, InteractiveActions) error { return nil }
	if exitCode := app.Run([]string{"tui", "extra"}); exitCode != 1 {
		t.Fatalf("tui extra exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "Usage: lore tui") {
		t.Fatalf("stderr = %q, want tui usage", stderr.String())
	}
}

func TestHelpAndUnknownCommand(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loadErr: config.ErrNotFound}
	app, stdout, stderr := newTestApp(store, nil)

	if exitCode := app.Run([]string{"--help"}); exitCode != 0 {
		t.Fatalf("help exitCode = %d, want 0", exitCode)
	}
	for _, want := range []string{"Commands:", "version", "api request", "install", "OS keychain-backed login metadata"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want substring %q", stdout.String(), want)
		}
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := app.Run([]string{"wat"}); exitCode != 1 {
		t.Fatalf("unknown exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "unknown command: wat") {
		t.Fatalf("stderr = %q, want unknown command message", stderr.String())
	}
}

func TestInstallCommandRunsPiInstallAndPrintsSummary(t *testing.T) {
	homeDir, piAgentDir := setIsolatedPiHome(t)
	store := &fakeStore{path: "/tmp/lore/config/config.json", loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token"}}
	client := &fakeClient{subject: httpclient.Subject{UserID: "user-1", Kind: "user"}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.ExecutablePath = func() (string, error) { return "/usr/local/bin/lore", nil }
	app.BuildInfo = version.Info{Version: "v1.2.3"}

	if exitCode := app.Run([]string{"install"}); exitCode != 0 {
		t.Fatalf("install exitCode = %d, want 0, stderr=%q", exitCode, stderr.String())
	}
	manifestPath := filepath.Join(piAgentDir, "lore-install.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("manifest stat err = %v, want manifest created in isolated PI_CODING_AGENT_DIR=%q (home=%q)", err, piAgentDir, homeDir)
	}
	out := stdout.String()
	for _, want := range []string{"Lore install", "[OK] healthz", "[OK] install", "created=4", "manifest", manifestPath} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout = %q, want substring %q", out, want)
		}
	}
	assertNoTokenLeak(t, out, stderr.String(), "secret-token")
}

func TestInstallCommandPassesSavedTokenToValidationWithoutLeakingIt(t *testing.T) {
	homeDir, piAgentDir := setIsolatedPiHome(t)
	store := &fakeStore{path: "/tmp/lore/config/config.json", loaded: config.Config{ServerURL: "https://example.test", APIToken: "export default function"}}
	client := &fakeClient{subject: httpclient.Subject{UserID: "user-1", Kind: "user"}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.ExecutablePath = func() (string, error) { return "/usr/local/bin/lore", nil }
	app.BuildInfo = version.Info{Version: "v1.2.3"}

	if exitCode := app.Run([]string{"install"}); exitCode != 1 {
		t.Fatalf("install exitCode = %d, want 1 for validation failure, stderr=%q stdout=%q", exitCode, stderr.String(), stdout.String())
	}
	manifestPath := filepath.Join(piAgentDir, "lore-install.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("manifest stat err = %v, want manifest retained for validation summary in isolated PI_CODING_AGENT_DIR=%q (home=%q)", err, piAgentDir, homeDir)
	}
	out := stdout.String()
	if !strings.Contains(out, "contains saved auth material") {
		t.Fatalf("stdout = %q, want secret-safe validation finding", out)
	}
	if !strings.Contains(out, "extensions/lore-") {
		t.Fatalf("stdout = %q, want managed file validation detail", out)
	}
	assertNoTokenLeak(t, out, stderr.String(), "export default function")
}

func TestInstallCommandBlocksWhenLoginIsRequired(t *testing.T) {
	homeDir, piAgentDir := setIsolatedPiHome(t)
	store := &fakeStore{path: "/tmp/lore/config/config.json", loadErr: config.ErrNotFound}
	app, stdout, stderr := newTestApp(store, nil)
	app.ExecutablePath = func() (string, error) { return "/usr/local/bin/lore", nil }

	if exitCode := app.Run([]string{"install"}); exitCode != 1 {
		t.Fatalf("install exitCode = %d, want 1", exitCode)
	}
	if got := stdout.String(); !strings.Contains(got, "Run lore login") && !strings.Contains(got, "run lore login") {
		t.Fatalf("stdout = %q, want login remediation", got)
	}
	if _, err := os.Stat(filepath.Join(piAgentDir, "lore-install.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest stat err = %v, want not exist on preflight validation failure (PI_CODING_AGENT_DIR=%q home=%q)", err, piAgentDir, homeDir)
	}
	if _, err := os.Stat(piAgentDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pi agent dir stat err = %v, want no partial install state on preflight validation failure", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestInstallUsageIncludesPiFirstGuidance(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loadErr: config.ErrNotFound}
	app, _, stderr := newTestApp(store, nil)

	if exitCode := app.Run([]string{"install", "unexpected"}); exitCode != 1 {
		t.Fatalf("install exitCode = %d, want 1", exitCode)
	}
	for _, want := range []string{
		"Usage: lore install",
		"Pi-first managed runtime",
		"saved Lore login state",
		"OS keychain-backed login metadata",
		"Claude Code, OpenCode, Codex, and Antigravity remain Coming soon",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want substring %q", stderr.String(), want)
		}
	}
}

func TestInstallCommandDryRunReportsPlanWithoutMutation(t *testing.T) {
	homeDir, piAgentDir := setIsolatedPiHome(t)
	piRoot := filepath.Join(homeDir, ".pi")
	if err := os.MkdirAll(piRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll piRoot: %v", err)
	}
	legacyPath := filepath.Join(piRoot, "legacy.txt")
	if err := os.WriteFile(legacyPath, []byte("keep-me"), 0o600); err != nil {
		t.Fatalf("WriteFile legacyPath: %v", err)
	}

	configDir := t.TempDir()
	store := &fakeStore{path: filepath.Join(configDir, "config.json"), loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token=plan"}}
	client := &fakeClient{subject: httpclient.Subject{UserID: "user-1", Kind: "user"}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.ExecutablePath = func() (string, error) { return "/usr/local/bin/lore", nil }
	app.BuildInfo = version.Info{Version: "v1.2.3"}

	if exitCode := app.Run([]string{"install", "--dry-run"}); exitCode != 0 {
		t.Fatalf("install --dry-run exitCode = %d, want 0, stderr=%q stdout=%q", exitCode, stderr.String(), stdout.String())
	}
	if got, err := os.ReadFile(legacyPath); err != nil || string(got) != "keep-me" {
		t.Fatalf("legacyPath after dry-run = %q err=%v, want unchanged", string(got), err)
	}
	if _, err := os.Stat(filepath.Join(piAgentDir, "lore-install.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest stat err = %v, want not exist after dry-run", err)
	}
	if _, err := os.Stat(filepath.Join(configDir, "backups")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup root stat err = %v, want not exist after dry-run", err)
	}
	out := stdout.String()
	lowerOut := strings.ToLower(out)
	for _, want := range []string{"dry-run", "backup", "manifest=", "managed_backup_root=", "full_backup_manifest=", "existing_pi_kind=directory"} {
		if !strings.Contains(lowerOut, strings.ToLower(want)) {
			t.Fatalf("stdout = %q, want dry-run plan detail containing %q", out, want)
		}
	}
	assertNoTokenLeak(t, out, stderr.String(), "secret-token=plan")
}

func TestInstallCommandDryRunSurfacesManagedFileActions(t *testing.T) {
	homeDir, piAgentDir := setIsolatedPiHome(t)
	layout := install.ResolvePiLayout(homeDir)
	if err := os.MkdirAll(layout.ExtensionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll extensions: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".pi", "legacy.txt"), []byte("keep-me"), 0o600); err != nil {
		t.Fatalf("WriteFile legacyPath: %v", err)
	}
	configDir := t.TempDir()
	loreMemory := renderInstallAssetForTest(t, filepath.Join("..", "install", "assets", "pi", "lore-memory.ts"), map[string]string{
		"{{LORE_SERVER_URL}}":    "https://example.test",
		"{{LORE_BINARY_PATH}}":   "/usr/local/bin/lore",
		"{{LORE_CONFIG_DIR}}":    configDir,
		"{{LORE_CLI_VERSION}}":   "v1.2.3",
		"{{LORE_SETTINGS_PATH}}": filepath.Join(piAgentDir, "settings.json"),
	})
	if err := os.WriteFile(filepath.Join(layout.ExtensionsDir, "lore-memory.ts"), []byte(loreMemory), 0o600); err != nil {
		t.Fatalf("WriteFile unchanged managed file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(layout.ExtensionsDir, "lore-delegation.ts"), []byte("legacy-delegation"), 0o600); err != nil {
		t.Fatalf("WriteFile updated managed file: %v", err)
	}

	store := &fakeStore{path: filepath.Join(configDir, "config.json"), loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token=plan-actions"}}
	client := &fakeClient{subject: httpclient.Subject{UserID: "user-1", Kind: "user"}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.ExecutablePath = func() (string, error) { return "/usr/local/bin/lore", nil }
	app.BuildInfo = version.Info{Version: "v1.2.3"}

	if exitCode := app.Run([]string{"install", "--dry-run"}); exitCode != 0 {
		t.Fatalf("install --dry-run exitCode = %d, want 0, stderr=%q stdout=%q", exitCode, stderr.String(), stdout.String())
	}
	if _, err := os.Stat(filepath.Join(layout.ExtensionsDir, "lore-footer.ts")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("footer stat err = %v, want no created file in dry-run", err)
	}
	if _, err := os.Stat(layout.SettingsPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("settings stat err = %v, want no created settings in dry-run", err)
	}
	if _, err := os.Stat(filepath.Join(piAgentDir, "lore-install.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest stat err = %v, want not exist after dry-run", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"managed_action=unchanged:extensions/lore-memory.ts",
		"managed_action=update:extensions/lore-delegation.ts",
		"managed_action=create:extensions/lore-footer.ts",
		"managed_action=create:settings.json",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout = %q, want action-level dry-run detail %q", out, want)
		}
	}
	assertNoTokenLeak(t, out, stderr.String(), "secret-token=plan-actions")
}

func TestInstallCommandYesModeBacksUpExistingPiWithoutPrompt(t *testing.T) {
	homeDir, _ := setIsolatedPiHome(t)
	piRoot := filepath.Join(homeDir, ".pi")
	if err := os.MkdirAll(filepath.Join(piRoot, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll nested piRoot: %v", err)
	}
	if err := os.WriteFile(filepath.Join(piRoot, "nested", "legacy.txt"), []byte("legacy-value"), 0o600); err != nil {
		t.Fatalf("WriteFile legacy backup source: %v", err)
	}

	configDir := t.TempDir()
	store := &fakeStore{path: filepath.Join(configDir, "config.json"), loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token=yes"}}
	client := &fakeClient{subject: httpclient.Subject{UserID: "user-1", Kind: "user"}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.ExecutablePath = func() (string, error) { return "/usr/local/bin/lore", nil }
	app.BuildInfo = version.Info{Version: "v1.2.3"}

	if exitCode := app.Run([]string{"install", "--yes"}); exitCode != 0 {
		t.Fatalf("install --yes exitCode = %d, want 0, stderr=%q stdout=%q", exitCode, stderr.String(), stdout.String())
	}
	backupDirs, err := filepath.Glob(filepath.Join(configDir, "backups", "pi", "*"))
	if err != nil {
		t.Fatalf("Glob backup dirs: %v", err)
	}
	if len(backupDirs) != 1 {
		t.Fatalf("backupDirs = %v, want exactly one full backup dir", backupDirs)
	}
	backupCopy := filepath.Join(backupDirs[0], "nested", "legacy.txt")
	if got, err := os.ReadFile(backupCopy); err != nil || string(got) != "legacy-value" {
		t.Fatalf("full backup file = %q err=%v, want copied legacy content", string(got), err)
	}
	out := stdout.String()
	lowerOut := strings.ToLower(out)
	if strings.Contains(lowerOut, "full backup?") {
		t.Fatalf("stdout = %q, want non-interactive --yes mode without prompt", out)
	}
	for _, want := range []string{"full-backup", "manifest=", "lore-pi-backup.json"} {
		if !strings.Contains(lowerOut, strings.ToLower(want)) {
			t.Fatalf("stdout = %q, want install reporting containing %q", out, want)
		}
	}
	assertNoTokenLeak(t, out, stderr.String(), "secret-token=yes")
}

func TestInstallCommandPromptsForFullBackupAndAllowsExplicitDecline(t *testing.T) {
	homeDir, piAgentDir := setIsolatedPiHome(t)
	piRoot := filepath.Join(homeDir, ".pi")
	if err := os.MkdirAll(piRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll piRoot: %v", err)
	}
	if err := os.WriteFile(filepath.Join(piRoot, "legacy.txt"), []byte("legacy-value"), 0o600); err != nil {
		t.Fatalf("WriteFile legacy source: %v", err)
	}

	configDir := t.TempDir()
	store := &fakeStore{path: filepath.Join(configDir, "config.json"), loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token=decline"}}
	client := &fakeClient{subject: httpclient.Subject{UserID: "user-1", Kind: "user"}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.ExecutablePath = func() (string, error) { return "/usr/local/bin/lore", nil }
	app.BuildInfo = version.Info{Version: "v1.2.3"}

	restoreStdin := installTestStdin(t, "n\n")
	defer restoreStdin()

	if exitCode := app.Run([]string{"install"}); exitCode != 0 {
		t.Fatalf("interactive install decline exitCode = %d, want 0, stderr=%q stdout=%q", exitCode, stderr.String(), stdout.String())
	}
	if _, err := os.Stat(filepath.Join(piAgentDir, "lore-install.json")); err != nil {
		t.Fatalf("manifest stat err = %v, want install to continue after explicit decline", err)
	}
	if _, err := os.Stat(filepath.Join(configDir, "backups")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup root stat err = %v, want no full backup after explicit decline", err)
	}
	combined := strings.ToLower(stdout.String() + "\n" + stderr.String())
	if !strings.Contains(combined, "full backup") {
		t.Fatalf("combined output = %q, want explicit full-backup prompt/summary", combined)
	}
	assertNoTokenLeak(t, stdout.String(), stderr.String(), "secret-token=decline")
}

func TestAPIRequestCommandReturnsMachineReadableExitCodes(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token"}}
	client := &fakeClient{}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })

	if exitCode := app.Run([]string{"api", "request", "--json", "--method", "GET", "--path", "https://example.test/v1/memories?project_id=lore-cli"}); exitCode != 2 {
		t.Fatalf("validation exitCode = %d, want 2", exitCode)
	}
	if got := stdout.String(); !strings.Contains(got, `"ok":false`) || !strings.Contains(got, `"code":"invalid_request"`) {
		t.Fatalf("stdout = %q, want machine validation envelope", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty for machine errors", stderr.String())
	}

	stdout.Reset()
	store.loadErr = config.ErrNotFound
	if exitCode := app.Run([]string{"api", "request", "--json", "--method", "GET", "--path", "/v1/memories?project_id=lore-cli"}); exitCode != 3 {
		t.Fatalf("missing-config exitCode = %d, want 3", exitCode)
	}
	if got := stdout.String(); !strings.Contains(got, `"code":"missing_config"`) {
		t.Fatalf("stdout = %q, want missing config envelope", got)
	}

	stdout.Reset()
	store.loadErr = nil
	store.loaded = config.Config{ServerURL: "https://example.test", APIToken: "secret-token"}
	client.requestJSONErr = &httpclient.UnauthorizedError{APIError: httpclient.APIError{StatusCode: 401, Code: "unauthorized", Message: "login required", RequestID: "req-auth"}}
	if exitCode := app.Run([]string{"api", "request", "--json", "--method", "GET", "--path", "/v1/memories?project_id=lore-cli"}); exitCode != 4 {
		t.Fatalf("auth exitCode = %d, want 4", exitCode)
	}
	if got := stdout.String(); !strings.Contains(got, `"request_id":"req-auth"`) || strings.Contains(got, "secret-token") {
		t.Fatalf("stdout = %q, want auth envelope without token leak", got)
	}
}

func TestAPIMCPCallCommandUsesSavedAuthAndJSONEnvelope(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token"}}
	client := &fakeClient{mcpResult: httpclient.RequestJSONResult{StatusCode: 200, RequestID: "req-mcp", Data: json.RawMessage(`{"ok":"context"}`)}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })

	args := `{"project_id":"00000000-0000-0000-0000-000000000001","memory_limit":3}`
	if exitCode := app.Run([]string{"api", "mcp-call", "--json", "--tool", "lore_project_context", "--args-json", args}); exitCode != 0 {
		t.Fatalf("mcp-call exitCode = %d, want 0 stderr=%q stdout=%q", exitCode, stderr.String(), stdout.String())
	}
	if client.mcpToken != "secret-token" || client.mcpTool != "lore_project_context" || string(client.mcpArgs) != args {
		t.Fatalf("MCPCall token/tool/args = %q/%q/%s", client.mcpToken, client.mcpTool, client.mcpArgs)
	}
	if got := stdout.String(); !strings.Contains(got, `"ok":true`) || !strings.Contains(got, `"request_id":"req-mcp"`) || strings.Contains(got, "secret-token") {
		t.Fatalf("stdout = %q, want JSON envelope without token leak", got)
	}
}

func TestAPIMCPCallRejectsNonAllowlistedToolBeforeAuth(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token"}}
	client := &fakeClient{}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })

	if exitCode := app.Run([]string{"api", "mcp-call", "--json", "--tool", "lore_delete", "--args-json", `{}`}); exitCode != 2 {
		t.Fatalf("mcp-call exitCode = %d, want 2 stderr=%q stdout=%q", exitCode, stderr.String(), stdout.String())
	}
	if client.mcpTool != "" {
		t.Fatalf("MCPCall was invoked for rejected tool %q", client.mcpTool)
	}
	if got := stdout.String(); !strings.Contains(got, `"code":"invalid_request"`) || !strings.Contains(got, "not allowlisted") {
		t.Fatalf("stdout = %q, want allowlist error envelope", got)
	}
}

func TestAPIMCPCallReportsJSONRPCError(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token"}}
	client := &fakeClient{mcpErr: &httpclient.APIError{StatusCode: 200, Code: "-32602", Message: "bad args", RequestID: "req-mcp"}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })

	if exitCode := app.Run([]string{"api", "mcp-call", "--json", "--tool", "lore_project_context", "--args-json", `{}`}); exitCode != 5 {
		t.Fatalf("mcp-call exitCode = %d, want 5 stderr=%q stdout=%q", exitCode, stderr.String(), stdout.String())
	}
	if got := stdout.String(); !strings.Contains(got, `"ok":false`) || !strings.Contains(got, `"code":"-32602"`) || !strings.Contains(got, `"request_id":"req-mcp"`) {
		t.Fatalf("stdout = %q, want JSON-RPC error envelope", got)
	}
}

func setIsolatedPiHome(t *testing.T) (homeDir string, piAgentDir string) {
	t.Helper()
	homeDir = t.TempDir()
	piAgentDir = filepath.Join(homeDir, ".pi", "agent")
	t.Setenv("HOME", homeDir)
	t.Setenv("PI_CODING_AGENT_DIR", piAgentDir)
	return homeDir, piAgentDir
}

func renderInstallAssetForTest(t *testing.T, relativePath string, replacements map[string]string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	assetPath := filepath.Clean(filepath.Join(filepath.Dir(thisFile), relativePath))
	content, err := os.ReadFile(assetPath)
	if err != nil {
		t.Fatalf("ReadFile asset %s: %v", assetPath, err)
	}
	rendered := string(content)
	for placeholder, value := range replacements {
		rendered = strings.ReplaceAll(rendered, placeholder, value)
	}
	return rendered
}

func newTestApp(store *fakeStore, factory ClientFactory) (*App, *strings.Builder, *strings.Builder) {
	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	if factory == nil {
		factory = func(baseURL string) (httpclient.Client, error) {
			return &fakeClient{}, nil
		}
	}
	return &App{Stdout: stdout, Stderr: stderr, Store: store, Auth: &fakeAuthManager{store: store}, ClientFactory: factory, LookPath: func(name string) (string, error) { return "/usr/bin/pi", nil }}, stdout, stderr
}

func newVersionOnlyApp(buildInfo version.Info) (*App, *strings.Builder, *strings.Builder) {
	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	return &App{Stdout: stdout, Stderr: stderr, BuildInfo: buildInfo}, stdout, stderr
}

type fakeStore struct {
	path        string
	loaded      config.Config
	loadErr     error
	saved       config.Config
	saveCalls   int
	deleteCalls int
}

type fakeAuthManager struct {
	store       *fakeStore
	session     auth.Session
	loadErr     error
	saveErr     error
	logoutErr   error
	savedServer string
	savedToken  string
	saveCalls   int
	loadCalls   int
	logoutCalls int
}

func (s *fakeStore) Load() (config.Config, error) {
	if s.loadErr != nil {
		return config.Config{}, s.loadErr
	}
	return s.loaded, nil
}

func (s *fakeStore) Save(cfg config.Config) error {
	s.saveCalls++
	s.saved = cfg
	s.loaded = cfg
	s.loadErr = nil
	return nil
}

func (s *fakeStore) Delete() error {
	s.deleteCalls++
	s.loaded = config.Config{}
	s.loadErr = config.ErrNotFound
	return nil
}

func (s *fakeStore) Path() (string, error) {
	return s.path, nil
}

func (m *fakeAuthManager) Save(serverURL, token string) error {
	m.saveCalls++
	if m.saveErr != nil {
		return m.saveErr
	}
	m.savedServer = strings.TrimSpace(serverURL)
	m.savedToken = strings.TrimSpace(token)
	account := "acct-test"
	configPath := ""
	if m.store != nil {
		configPath = m.store.path
		_ = m.store.Save(config.Config{ServerURL: m.savedServer, CredentialAccount: account})
	}
	m.session = auth.Session{ServerURL: m.savedServer, Token: m.savedToken, ConfigPath: configPath, CredentialAccount: account}
	return nil
}

func (m *fakeAuthManager) Load() (auth.Session, error) {
	m.loadCalls++
	if m.loadErr != nil {
		return auth.Session{}, m.loadErr
	}
	if m.session.ServerURL != "" || m.session.Token != "" || m.session.ConfigPath != "" {
		return m.session, nil
	}
	if m.store == nil {
		return auth.Session{}, &auth.Error{Code: auth.ErrConfigNotFound, Op: "load config", Err: config.ErrNotFound}
	}
	cfg, err := m.store.Load()
	if err != nil {
		if errors.Is(err, config.ErrNotFound) {
			return auth.Session{}, &auth.Error{Code: auth.ErrConfigNotFound, Op: "load config", Err: err}
		}
		return auth.Session{}, &auth.Error{Code: auth.ErrInvalidConfig, Op: "load config", Err: err}
	}
	if strings.TrimSpace(cfg.ServerURL) == "" {
		return auth.Session{}, &auth.Error{Code: auth.ErrInvalidConfig, Op: "normalize server url", Err: errors.New("server URL is required")}
	}
	token := strings.TrimSpace(cfg.APIToken)
	if token == "" {
		return auth.Session{}, &auth.Error{Code: auth.ErrCredentialMissing, Op: "load credential", Err: auth.ErrCredentialNotFound}
	}
	return auth.Session{ServerURL: strings.TrimSpace(cfg.ServerURL), Token: token, ConfigPath: m.store.path, CredentialAccount: cfg.CredentialAccount}, nil
}

func (m *fakeAuthManager) Logout() error {
	m.logoutCalls++
	if m.logoutErr != nil {
		return m.logoutErr
	}
	m.session = auth.Session{}
	if m.store != nil {
		return m.store.Delete()
	}
	return nil
}

type fakeCredentialStore struct {
	secrets map[string]string
	setErr  error
	getErr  error
	delErr  error
}

func (s *fakeCredentialStore) Set(service, account, secret string) error {
	if s.setErr != nil {
		return s.setErr
	}
	if s.secrets == nil {
		s.secrets = map[string]string{}
	}
	s.secrets[service+":"+account] = secret
	return nil
}

func (s *fakeCredentialStore) Get(service, account string) (string, error) {
	if s.getErr != nil {
		return "", s.getErr
	}
	if s.secrets == nil {
		return "", auth.ErrCredentialNotFound
	}
	secret, ok := s.secrets[service+":"+account]
	if !ok {
		return "", auth.ErrCredentialNotFound
	}
	return secret, nil
}

func (s *fakeCredentialStore) Delete(service, account string) error {
	if s.delErr != nil {
		return s.delErr
	}
	if s.secrets != nil {
		delete(s.secrets, service+":"+account)
	}
	return nil
}

type fakeClient struct {
	healthErr         error
	readyErr          error
	meErr             error
	createErr         error
	listErr           error
	subject           httpclient.Subject
	memory            httpclient.Memory
	memories          []httpclient.Memory
	meToken           string
	createToken       string
	listToken         string
	createRequest     httpclient.CreateMemoryRequest
	listFilter        httpclient.ListMemoriesFilter
	createCalls       int
	listCalls         int
	requestJSONToken  string
	requestJSONMethod string
	requestJSONPath   string
	requestJSONBody   json.RawMessage
	requestJSONResult httpclient.RequestJSONResult
	requestJSONErr    error
	mcpToken          string
	mcpTool           string
	mcpArgs           json.RawMessage
	mcpResult         httpclient.RequestJSONResult
	mcpErr            error
}

func (c *fakeClient) Health(_ context.Context) error { return c.healthErr }
func (c *fakeClient) Ready(_ context.Context) error  { return c.readyErr }
func (c *fakeClient) Me(_ context.Context, token string) (httpclient.Subject, error) {
	c.meToken = token
	if c.meErr != nil {
		return httpclient.Subject{}, c.meErr
	}
	return c.subject, nil
}
func (c *fakeClient) CreateMemory(_ context.Context, token string, req httpclient.CreateMemoryRequest) (httpclient.Memory, error) {
	c.createCalls++
	c.createToken = token
	c.createRequest = req
	if c.createErr != nil {
		return httpclient.Memory{}, c.createErr
	}
	return c.memory, nil
}
func (c *fakeClient) ListMemories(_ context.Context, token string, filter httpclient.ListMemoriesFilter) ([]httpclient.Memory, error) {
	c.listCalls++
	c.listToken = token
	c.listFilter = filter
	if c.listErr != nil {
		return nil, c.listErr
	}
	return c.memories, nil
}

func (c *fakeClient) RequestJSON(_ context.Context, method, path, token string, body json.RawMessage) (httpclient.RequestJSONResult, error) {
	c.requestJSONMethod = method
	c.requestJSONPath = path
	c.requestJSONToken = token
	c.requestJSONBody = body
	if c.requestJSONErr != nil {
		return httpclient.RequestJSONResult{}, c.requestJSONErr
	}
	return c.requestJSONResult, nil
}

func (c *fakeClient) MCPCall(_ context.Context, token, toolName string, arguments json.RawMessage) (httpclient.RequestJSONResult, error) {
	c.mcpToken = token
	c.mcpTool = toolName
	c.mcpArgs = arguments
	if c.mcpErr != nil {
		return httpclient.RequestJSONResult{}, c.mcpErr
	}
	return c.mcpResult, nil
}

func assertNoTokenLeak(t *testing.T, stdout, stderr, token string) {
	t.Helper()
	if strings.Contains(stdout, token) || strings.Contains(stderr, token) {
		t.Fatalf("raw token leaked in output: stdout=%q stderr=%q", stdout, stderr)
	}
}

func installTestStdin(t *testing.T, input string) func() {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	if _, err := writer.WriteString(input); err != nil {
		t.Fatalf("writer.WriteString() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v", err)
	}
	original := os.Stdin
	os.Stdin = reader
	return func() {
		os.Stdin = original
		_ = reader.Close()
	}
}
