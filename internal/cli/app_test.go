package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alferio94/lore-cli/internal/config"
	"github.com/alferio94/lore-cli/internal/httpclient"
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
	if store.saved.ServerURL != "https://example.test/" || store.saved.APIToken != "secret-token" {
		t.Fatalf("saved config = %+v, want trimmed values passed to store", store.saved)
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
	store := config.NewStore(t.TempDir())
	if err := store.Save(config.Config{ServerURL: "https://example.test", APIToken: "secret-token"}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	app := &App{
		Stdout: stdout,
		Stderr: stderr,
		Store:  store,
		ClientFactory: func(baseURL string) (httpclient.Client, error) {
			return &fakeClient{}, nil
		},
		LookPath: func(name string) (string, error) { return "/usr/bin/pi", nil },
	}

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
	if !strings.Contains(stdout.String(), "Commands:") || !strings.Contains(stdout.String(), "version") {
		t.Fatalf("stdout = %q, want root help", stdout.String())
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

func newTestApp(store *fakeStore, factory ClientFactory) (*App, *strings.Builder, *strings.Builder) {
	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	if factory == nil {
		factory = func(baseURL string) (httpclient.Client, error) {
			return &fakeClient{}, nil
		}
	}
	return &App{Stdout: stdout, Stderr: stderr, Store: store, ClientFactory: factory, LookPath: func(name string) (string, error) { return "/usr/bin/pi", nil }}, stdout, stderr
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

type fakeClient struct {
	healthErr     error
	readyErr      error
	meErr         error
	createErr     error
	listErr       error
	subject       httpclient.Subject
	memory        httpclient.Memory
	memories      []httpclient.Memory
	meToken       string
	createToken   string
	listToken     string
	createRequest httpclient.CreateMemoryRequest
	listFilter    httpclient.ListMemoriesFilter
	createCalls   int
	listCalls     int
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

func assertNoTokenLeak(t *testing.T, stdout, stderr, token string) {
	t.Helper()
	if strings.Contains(stdout, token) || strings.Contains(stderr, token) {
		t.Fatalf("raw token leaked in output: stdout=%q stderr=%q", stdout, stderr)
	}
}
