package cli

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alferio94/lore-cli/internal/config"
	"github.com/alferio94/lore-cli/internal/httpclient"
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

func TestHelpAndUnknownCommand(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loadErr: config.ErrNotFound}
	app, stdout, stderr := newTestApp(store, nil)

	if exitCode := app.Run([]string{"--help"}); exitCode != 0 {
		t.Fatalf("help exitCode = %d, want 0", exitCode)
	}
	if !strings.Contains(stdout.String(), "Commands:") {
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
	healthErr error
	readyErr  error
	meErr     error
	subject   httpclient.Subject
	meToken   string
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

func assertNoTokenLeak(t *testing.T, stdout, stderr, token string) {
	t.Helper()
	if strings.Contains(stdout, token) || strings.Contains(stderr, token) {
		t.Fatalf("raw token leaked in output: stdout=%q stderr=%q", stdout, stderr)
	}
}
