package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestLoginPasswordPromptUsesMintedTokenAndKeepsSecretsOutOfOutput(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loadErr: config.ErrNotFound}
	client := &fakeClient{
		loginResult: httpclient.PasswordLoginResult{Token: "minted-token"},
		subject:     httpclient.Subject{ID: "subject-1", UserID: "user-1", Roles: []string{"admin"}, TokenID: "token-1", TokenSource: "api_token", Kind: "user"},
	}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.PasswordPrompt = func() (string, error) { return "super-secret-password", nil }

	exitCode := app.Run([]string{"login", "--server", "https://example.test", "--email", "admin@example.com"})
	if exitCode != 0 {
		t.Fatalf("Run() exitCode = %d, want 0, stderr=%q", exitCode, stderr.String())
	}
	if client.loginEmail != "admin@example.com" || client.loginPassword != "super-secret-password" {
		t.Fatalf("Login() credentials = %q / %q", client.loginEmail, client.loginPassword)
	}
	if client.meToken != "minted-token" {
		t.Fatalf("Me token = %q, want minted-token", client.meToken)
	}
	if got := app.Auth.(*fakeAuthManager).savedToken; got != "minted-token" {
		t.Fatalf("savedToken = %q, want minted-token", got)
	}
	if strings.Contains(stdout.String(), "super-secret-password") || strings.Contains(stderr.String(), "super-secret-password") {
		t.Fatalf("password leaked in output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	assertNoTokenLeak(t, stdout.String(), stderr.String(), "minted-token")
}

func TestLoginPasswordPromptUsesRealAuthManagerKeychainOnlyPersistence(t *testing.T) {
	store := &fakeStore{path: filepath.Join(t.TempDir(), "config.json"), loadErr: config.ErrNotFound}
	creds := &fakeCredentialStore{}
	client := &fakeClient{
		loginResult: httpclient.PasswordLoginResult{Token: "minted-token"},
		subject:     httpclient.Subject{ID: "subject-1", UserID: "user-1", Roles: []string{"admin"}, TokenID: "token-1", TokenSource: "api_token", Kind: "user"},
	}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.Auth = auth.Manager{ConfigStore: store, Credentials: creds}
	app.PasswordPrompt = func() (string, error) { return "super-secret-password", nil }

	exitCode := app.Run([]string{"login", "--server", "https://example.test", "--email", "admin@example.com"})
	if exitCode != 0 {
		t.Fatalf("Run() exitCode = %d, want 0, stderr=%q", exitCode, stderr.String())
	}
	if store.saved.ServerURL != "https://example.test" || store.saved.APIToken != "" || store.saved.CredentialAccount == "" {
		t.Fatalf("saved config = %+v, want metadata-only keychain-backed state", store.saved)
	}
	if got := creds.secrets[auth.ServiceName+":"+store.saved.CredentialAccount]; got != "minted-token" {
		t.Fatalf("stored credential = %q, want minted-token", got)
	}
	if strings.Contains(stdout.String(), "super-secret-password") || strings.Contains(stderr.String(), "super-secret-password") {
		t.Fatalf("password leaked in output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	assertNoTokenLeak(t, stdout.String(), stderr.String(), "minted-token")
}

func TestLoginPasswordPromptFailsClosedWhenRealAuthManagerKeychainUnavailable(t *testing.T) {
	store := &fakeStore{path: filepath.Join(t.TempDir(), "config.json"), loadErr: config.ErrNotFound}
	creds := &fakeCredentialStore{setErr: errors.New("keychain locked")}
	client := &fakeClient{
		loginResult: httpclient.PasswordLoginResult{Token: "minted-token"},
		subject:     httpclient.Subject{UserID: "user-1", TokenSource: "api_token", Kind: "user"},
	}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.Auth = auth.Manager{ConfigStore: store, Credentials: creds}
	app.PasswordPrompt = func() (string, error) { return "super-secret-password", nil }

	exitCode := app.Run([]string{"login", "--server", "https://example.test", "--email", "admin@example.com"})
	if exitCode != 1 {
		t.Fatalf("Run() exitCode = %d, want 1", exitCode)
	}
	if store.saveCalls != 0 {
		t.Fatalf("saveCalls = %d, want 0 metadata writes on keychain failure", store.saveCalls)
	}
	if len(creds.secrets) != 0 {
		t.Fatalf("stored credentials = %v, want none on failed keychain save", creds.secrets)
	}
	for _, want := range []string{"OS keychain", "headless Linux", "gnome-keyring", "run lore login again"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want substring %q", stderr.String(), want)
		}
	}
	if strings.Contains(stdout.String(), "super-secret-password") || strings.Contains(stderr.String(), "super-secret-password") {
		t.Fatalf("password leaked in output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	assertNoTokenLeak(t, stdout.String(), stderr.String(), "minted-token")
}

func TestLoginTokenCompatibilityUsesRealAuthManagerKeychainPath(t *testing.T) {
	store := &fakeStore{path: filepath.Join(t.TempDir(), "config.json"), loadErr: config.ErrNotFound}
	creds := &fakeCredentialStore{}
	client := &fakeClient{subject: httpclient.Subject{ID: "subject-1", UserID: "user-1", Roles: []string{"admin"}, TokenID: "token-1", TokenSource: "api_token", Kind: "user"}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.Auth = auth.Manager{ConfigStore: store, Credentials: creds}

	exitCode := app.Run([]string{"login", "--server", "https://example.test", "--token", "secret-token"})
	if exitCode != 0 {
		t.Fatalf("Run() exitCode = %d, want 0, stderr=%q", exitCode, stderr.String())
	}
	if store.saved.ServerURL != "https://example.test" || store.saved.APIToken != "" || store.saved.CredentialAccount == "" {
		t.Fatalf("saved config = %+v, want metadata-only keychain-backed state", store.saved)
	}
	if got := creds.secrets[auth.ServiceName+":"+store.saved.CredentialAccount]; got != "secret-token" {
		t.Fatalf("stored credential = %q, want secret-token", got)
	}
	assertNoTokenLeak(t, stdout.String(), stderr.String(), "secret-token")
}

func TestLoginPasswordStdinUsesMintedToken(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loadErr: config.ErrNotFound}
	client := &fakeClient{
		loginResult: httpclient.PasswordLoginResult{Token: "minted-token"},
		subject:     httpclient.Subject{UserID: "user-1", TokenSource: "api_token", Kind: "user"},
	}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.Stdin = strings.NewReader("stdin-password\n")

	exitCode := app.Run([]string{"login", "--server", "https://example.test", "--email", "admin@example.com", "--password-stdin"})
	if exitCode != 0 {
		t.Fatalf("Run() exitCode = %d, want 0, stderr=%q", exitCode, stderr.String())
	}
	if client.loginPassword != "stdin-password" {
		t.Fatalf("Login() password = %q, want stdin-password", client.loginPassword)
	}
	if got := app.Auth.(*fakeAuthManager).savedToken; got != "minted-token" {
		t.Fatalf("savedToken = %q, want minted-token", got)
	}
	if strings.Contains(stdout.String(), "stdin-password") || strings.Contains(stderr.String(), "stdin-password") {
		t.Fatalf("stdin password leaked in output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	assertNoTokenLeak(t, stdout.String(), stderr.String(), "minted-token")
}

func TestLoginRequiresSafePasswordInputOrTokenCompatibility(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loadErr: config.ErrNotFound}
	app, _, stderr := newTestApp(store, nil)
	app.Stdin = strings.NewReader("")

	exitCode := app.Run([]string{"login", "--server", "https://example.test", "--email", "admin@example.com"})
	if exitCode != 1 {
		t.Fatalf("Run() exitCode = %d, want 1", exitCode)
	}
	for _, want := range []string{"--password-stdin", "--token", "safe password input"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want substring %q", stderr.String(), want)
		}
	}
}

func TestLoginDoesNotAcceptPasswordArgvFlag(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loadErr: config.ErrNotFound}
	app, _, stderr := newTestApp(store, nil)

	exitCode := app.Run([]string{"login", "--server", "https://example.test", "--email", "admin@example.com", "--password", "secret"})
	if exitCode != 1 {
		t.Fatalf("Run() exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined: -password") {
		t.Fatalf("stderr = %q, want unknown --password flag error", stderr.String())
	}
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
	for _, want := range []string{"[OK] config", "[FAIL] auth", "saved login state is incomplete", "Run lore login again with password login or a valid compatibility token."} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout = %q, want substring %q", out, want)
		}
	}
}

func TestStatusWithInvalidSavedServerURLReportsPasswordFirstRemediationAndNoLeak(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loaded: config.Config{ServerURL: "ftp://bad.example", APIToken: "secret-token"}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) {
		return nil, errors.New("server URL must start with http:// or https://")
	})

	exitCode := app.Run([]string{"status"})
	if exitCode != 1 {
		t.Fatalf("Run() exitCode = %d, want 1, stderr=%q", exitCode, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"[OK] config", "[FAIL] server-url", "--email", "--token", "password login"} {
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
	for _, want := range []string{"[OK] healthz", "[FAIL] readyz", "[FAIL] auth", "[WARN] pi", "request_id=req-ready", "normal user API token required", "Obtain a valid password-login session or compatibility token and run lore login again."} {
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
	for _, want := range []string{"Commands:", "version", "api request", "install", "update", "email + hidden password", "--password-stdin", "--token", "OS keychain-backed login metadata"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want substring %q", stdout.String(), want)
		}
	}
	for _, unsafe := range []string{"LORE_PASSWORD", "--password "} {
		if strings.Contains(stdout.String(), unsafe) {
			t.Fatalf("stdout = %q, want no unsafe password documentation", stdout.String())
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

func TestLoginUsageDescribesPasswordFirstCompatibilityAndStdin(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loadErr: config.ErrNotFound}
	app, _, stderr := newTestApp(store, nil)

	if exitCode := app.Run([]string{"login", "--help"}); exitCode != 1 {
		t.Fatalf("login --help exitCode = %d, want 1 with usage output", exitCode)
	}
	for _, want := range []string{"--password-stdin", "--token", "hidden password", "mint a reusable API token"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want substring %q", stderr.String(), want)
		}
	}
	for _, unsafe := range []string{"LORE_PASSWORD", "--password "} {
		if strings.Contains(stderr.String(), unsafe) {
			t.Fatalf("stderr = %q, want no unsafe password documentation", stderr.String())
		}
	}
}

func TestInstallCommandRunsPiInstallAndPrintsSummary(t *testing.T) {
	homeDir, piAgentDir := setIsolatedPiHome(t)
	layout := install.ResolvePiLayout(homeDir)
	legacyDelegationPath := filepath.Join(piAgentDir, "extensions", "lore-delegation.ts")
	if err := os.MkdirAll(filepath.Dir(legacyDelegationPath), 0o755); err != nil {
		t.Fatalf("MkdirAll legacy delegation dir: %v", err)
	}
	if err := os.MkdirAll(layout.ManagedAgentsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll managed agents dir: %v", err)
	}
	if err := os.WriteFile(legacyDelegationPath, []byte("legacy delegation"), 0o600); err != nil {
		t.Fatalf("WriteFile legacy delegation: %v", err)
	}
	trackedManagedOverlay := filepath.Join(layout.ManagedAgentsDir, "lore-managed-lore-worker.md")
	if err := os.WriteFile(trackedManagedOverlay, []byte("old managed worker"), 0o600); err != nil {
		t.Fatalf("WriteFile tracked managed overlay: %v", err)
	}
	staleManagedOverlay := filepath.Join(layout.ManagedAgentsDir, "lore-managed-stale-agent.md")
	if err := os.WriteFile(staleManagedOverlay, []byte("stale managed overlay"), 0o600); err != nil {
		t.Fatalf("WriteFile stale managed overlay: %v", err)
	}
	conflictingUserOverlay := filepath.Join(layout.ManagedAgentsDir, "lore-managed-sdd-archive.md")
	if err := os.WriteFile(conflictingUserOverlay, []byte("user-owned conflicting overlay"), 0o600); err != nil {
		t.Fatalf("WriteFile conflicting user overlay: %v", err)
	}
	manifestBytes, err := json.MarshalIndent(install.Manifest{
		SchemaVersion: install.PortableManifestSchemaVersion,
		ManagedAgentOverlays: []install.ManagedAgentOverlayRecord{
			{AgentName: "lore-worker", Path: trackedManagedOverlay, ContentHash: "tracked"},
			{AgentName: "stale-agent", Path: staleManagedOverlay, ContentHash: "stale"},
		},
	}, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent manifest: %v", err)
	}
	if err := os.WriteFile(layout.ManifestPath, append(manifestBytes, '\n'), 0o600); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
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
	if _, err := os.Stat(legacyDelegationPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy delegation stat err = %v, want cleanup after install", err)
	}
	if _, err := os.Stat(staleManagedOverlay); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale managed overlay stat err = %v, want cleanup after install", err)
	}
	out := stdout.String()
	for _, want := range []string{"Lore install", "[OK] healthz", "[OK] install", "runtime=pi-remote-package", "remote_package=git:github.com/nicobailon/pi-mcp-adapter@1091b34da83d58bd2d9fcaff2dc31f449a94bf1f", "managed_local_files=5", "project_agents=disabled(default-lore-managed)", "created=13", "updated=1", "deleted=2", "conflicted=1", "managed_action=update:agents/lore-managed-lore-worker.md", "managed_action=delete:agents/lore-managed-stale-agent.md", "managed_action=delete:extensions/lore-delegation.ts", "managed_action=conflict:agents/lore-managed-sdd-archive.md", "manifest", manifestPath} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout = %q, want substring %q", out, want)
		}
	}
	if strings.Contains(out, "managed_action=create:extensions/lore-delegation.ts") {
		t.Fatalf("stdout = %q, want no delegation regeneration action", out)
	}
	assertNoTokenLeak(t, out, stderr.String(), "secret-token")
}

func TestInstallCommandPassesSavedTokenToValidationWithoutLeakingIt(t *testing.T) {
	homeDir, piAgentDir := setIsolatedPiHome(t)
	// Use a distinctive token that will be detected in rendered lore-memory content.
	store := &fakeStore{path: "/tmp/lore/config/config.json", loaded: config.Config{ServerURL: "https://example.test", APIToken: "TEST_TOKEN_LEAK"}}
	client := &fakeClient{subject: httpclient.Subject{UserID: "user-1", Kind: "user"}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.ExecutablePath = func() (string, error) { return "/usr/local/bin/lore", nil }
	app.BuildInfo = version.Info{Version: "v1.2.3"}

	// Explicitly select pi-extensions so lore-memory.ts is installed and can trigger validation.
	// The install may fail due to manifest validation or content validation; both are acceptable.
	// The key assertion is that the raw token does not appear in output.
	_ = app.Run([]string{"install", "--component", "pi-extensions"})
	out := stdout.String()
	// Token must not be echoed in any output.
	if strings.Contains(out, "TEST_TOKEN_LEAK") {
		t.Fatalf("stdout = %q, want no raw token echo of TEST_TOKEN_LEAK", out)
	}
	if strings.Contains(stderr.String(), "TEST_TOKEN_LEAK") {
		t.Fatalf("stderr = %q, want no raw token echo of TEST_TOKEN_LEAK", stderr.String())
	}
	// Manifest should be retained (install wrote files even if validation failed).
	manifestPath := filepath.Join(piAgentDir, "lore-install.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("manifest stat err = %v, want manifest retained", err)
	}
	_ = homeDir
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
		"extended-skills bundle",
		"skill-creator",
		"skill-registry",
		"judgment-day",
		"Rerun lore install to refresh the extended-skills bundle",
		"lore update does not touch skill files",
		"Antigravity is a Full projection target",
		"Claude Code remains Coming soon",
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
	for _, want := range []string{"dry-run", "backup", "runtime=pi-remote-package", "remote_package=git:github.com/nicobailon/pi-mcp-adapter@1091b34da83d58bd2d9fcaff2dc31f449a94bf1f", "managed_local_files=5", "manifest=", "managed_backup_root=", "full_backup_manifest=", "existing_pi_kind=directory"} {
		if !strings.Contains(lowerOut, strings.ToLower(want)) {
			t.Fatalf("stdout = %q, want dry-run plan detail containing %q", out, want)
		}
	}
	assertNoTokenLeak(t, out, stderr.String(), "secret-token=plan")
}

func TestInstallCommandDryRunSurfacesManagedFileActions(t *testing.T) {
	homeDir, _ := setIsolatedPiHome(t)
	layout := install.ResolvePiLayout(homeDir)
	if err := os.MkdirAll(layout.ExtensionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll extensions: %v", err)
	}
	if err := os.MkdirAll(layout.ManagedAgentsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll managed agents dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".pi", "legacy.txt"), []byte("keep-me"), 0o600); err != nil {
		t.Fatalf("WriteFile legacyPath: %v", err)
	}
	configDir := t.TempDir()
	// Pre-existing deprecated lore-memory.ts is a backup-first cleanup candidate:
	// the dry-run must surface the action but must not actually mutate the file.
	if err := os.WriteFile(filepath.Join(layout.ExtensionsDir, "lore-memory.ts"), []byte("legacy memory leftover"), 0o600); err != nil {
		t.Fatalf("WriteFile legacy lore-memory.ts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(layout.ExtensionsDir, "lore-delegation.ts"), []byte("legacy-delegation"), 0o600); err != nil {
		t.Fatalf("WriteFile updated managed file: %v", err)
	}
	trackedManagedOverlay := filepath.Join(layout.ManagedAgentsDir, "lore-managed-lore-worker.md")
	if err := os.WriteFile(trackedManagedOverlay, []byte("old managed worker"), 0o600); err != nil {
		t.Fatalf("WriteFile tracked managed overlay: %v", err)
	}
	staleManagedOverlay := filepath.Join(layout.ManagedAgentsDir, "lore-managed-stale-agent.md")
	if err := os.WriteFile(staleManagedOverlay, []byte("stale managed overlay"), 0o600); err != nil {
		t.Fatalf("WriteFile stale managed overlay: %v", err)
	}
	conflictingUserOverlay := filepath.Join(layout.ManagedAgentsDir, "lore-managed-sdd-archive.md")
	if err := os.WriteFile(conflictingUserOverlay, []byte("user-owned conflicting overlay"), 0o600); err != nil {
		t.Fatalf("WriteFile conflicting user overlay: %v", err)
	}
	manifestBytes, err := json.MarshalIndent(install.Manifest{
		SchemaVersion: install.PortableManifestSchemaVersion,
		ManagedAgentOverlays: []install.ManagedAgentOverlayRecord{
			{AgentName: "lore-worker", Path: trackedManagedOverlay, ContentHash: "tracked"},
			{AgentName: "stale-agent", Path: staleManagedOverlay, ContentHash: "stale"},
		},
	}, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent manifest: %v", err)
	}
	if err := os.WriteFile(layout.ManifestPath, append(manifestBytes, '\n'), 0o600); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
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
	// Pre-existing legacy lore-memory.ts must be preserved untouched by the dry-run.
	if _, err := os.Stat(filepath.Join(layout.ExtensionsDir, "lore-memory.ts")); err != nil {
		t.Fatalf("legacy lore-memory.ts stat err = %v, want pre-existing file preserved across dry-run", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"runtime=pi-remote-package",
		"remote_package=git:github.com/nicobailon/pi-mcp-adapter@1091b34da83d58bd2d9fcaff2dc31f449a94bf1f",
		"managed_local_files=5",
		"project_agents=disabled(default-lore-managed)",
		// Pre-existing legacy cleanups surface as managed_action=delete entries.
		"managed_action=delete:extensions/lore-delegation.ts",
		"managed_action=delete:extensions/lore-memory.ts",
		"managed_action=create:settings.json",
		"managed_action=create:mcp.json",
		"managed_action=update:agents/lore-managed-lore-worker.md",
		"managed_action=delete:agents/lore-managed-stale-agent.md",
		"managed_action=conflict:agents/lore-managed-sdd-archive.md",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout = %q, want action-level dry-run detail %q", out, want)
		}
	}
	if strings.Contains(out, "managed_action=create:extensions/lore-footer.ts") {
		t.Fatalf("stdout = %q, want no lore-footer managed action (dormant for default)", out)
	}
	assertNoTokenLeak(t, out, stderr.String(), "secret-token=plan-actions")
}

// TestInstallCommandReportsLoreMemoryCleanupActionInDryRun verifies the dry-run
// CLI surface reports the backup-first deprecated `lore-memory.ts` cleanup as a
// `managed_action=delete:extensions/lore-memory.ts` entry whenever a pre-existing
// file is present in the home directory. The dry-run must not mutate the file.
func TestInstallCommandReportsLoreMemoryCleanupActionInDryRun(t *testing.T) {
	homeDir, _ := setIsolatedPiHome(t)
	layout := install.ResolvePiLayout(homeDir)
	if err := os.MkdirAll(layout.ExtensionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll extensions: %v", err)
	}
	absolutePath := filepath.Join(layout.AgentDir, filepath.FromSlash(install.ManagedDeprecatedLoreMemoryRelativePathForTest()))
	original := []byte("legacy memory leftover")
	if err := os.WriteFile(absolutePath, original, 0o600); err != nil {
		t.Fatalf("WriteFile lore-memory.ts: %v", err)
	}

	configDir := t.TempDir()
	store := &fakeStore{path: filepath.Join(configDir, "config.json"), loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token=dryrun-memory"}}
	client := &fakeClient{subject: httpclient.Subject{UserID: "user-1", Kind: "user"}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.ExecutablePath = func() (string, error) { return "/usr/local/bin/lore", nil }
	app.BuildInfo = version.Info{Version: "v1.2.3"}

	if exitCode := app.Run([]string{"install", "--dry-run"}); exitCode != 0 {
		t.Fatalf("install --dry-run exitCode = %d, want 0, stderr=%q stdout=%q", exitCode, stderr.String(), stdout.String())
	}
	// Dry-run must preserve the pre-existing file untouched.
	if got, err := os.ReadFile(absolutePath); err != nil || string(got) != string(original) {
		t.Fatalf("lore-memory.ts after dry-run = %q err=%v, want pre-existing content preserved", string(got), err)
	}
	out := stdout.String()
	if !strings.Contains(out, "managed_action=delete:extensions/lore-memory.ts") {
		t.Fatalf("stdout = %q, want managed_action=delete:extensions/lore-memory.ts", out)
	}
	assertNoTokenLeak(t, out, stderr.String(), "secret-token=dryrun-memory")
}

// TestInstallCommandApplyRemovesAndBacksUpPreExistingLoreMemoryExtension verifies
// a real apply (in a temp home) backs up the deprecated `lore-memory.ts`, removes
// it from disk, and reports the cleanup in the install summary, refreshing the
// manifest. The backup must be present under the managed backup root.
func TestInstallCommandApplyRemovesAndBacksUpPreExistingLoreMemoryExtension(t *testing.T) {
	homeDir, _ := setIsolatedPiHome(t)
	layout := install.ResolvePiLayout(homeDir)
	if err := os.MkdirAll(layout.ExtensionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll extensions: %v", err)
	}
	absolutePath := filepath.Join(layout.AgentDir, filepath.FromSlash(install.ManagedDeprecatedLoreMemoryRelativePathForTest()))
	original := []byte("legacy memory leftover")
	if err := os.WriteFile(absolutePath, original, 0o600); err != nil {
		t.Fatalf("WriteFile lore-memory.ts: %v", err)
	}

	configDir := t.TempDir()
	store := &fakeStore{path: filepath.Join(configDir, "config.json"), loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token=apply-memory"}}
	client := &fakeClient{subject: httpclient.Subject{UserID: "user-1", Kind: "user"}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.ExecutablePath = func() (string, error) { return "/usr/local/bin/lore", nil }
	app.BuildInfo = version.Info{Version: "v1.2.3"}

	if exitCode := app.Run([]string{"install", "--yes"}); exitCode != 0 {
		t.Fatalf("install --yes exitCode = %d, want 0, stderr=%q stdout=%q", exitCode, stderr.String(), stdout.String())
	}
	// File must be removed from the installed extensions directory.
	if _, err := os.Stat(absolutePath); !os.IsNotExist(err) {
		t.Fatalf("lore-memory.ts stat error = %v, want file removed after apply", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "managed_action=delete:extensions/lore-memory.ts") {
		t.Fatalf("stdout = %q, want managed_action=delete:extensions/lore-memory.ts", out)
	}
	// Manifest must be refreshed and must NOT record the deprecated path as a
	// managed file.
	manifestBytes, err := os.ReadFile(layout.ManifestPath)
	if err != nil {
		t.Fatalf("ReadFile manifest: %v", err)
	}
	if strings.Contains(string(manifestBytes), "lore-memory.ts") {
		t.Fatalf("manifest = %q, want no lore-memory.ts reference in manifest", string(manifestBytes))
	}
	// Backup must live under the managed backup root, preserved with the original
	// bytes. The backup root is recorded in the manifest and printed in the
	// install summary as `managed_backup_root=...`.
	backupDirs, err := filepath.Glob(filepath.Join(layout.AgentDir, "backups", "*"))
	if err != nil {
		t.Fatalf("Glob backup dirs: %v", err)
	}
	if len(backupDirs) == 0 {
		t.Fatalf("backup dirs = %v, want at least one managed backup root", backupDirs)
	}
	var foundBackup []byte
	for _, dir := range backupDirs {
		candidate := filepath.Join(dir, "extensions", "lore-memory.ts")
		if got, err := os.ReadFile(candidate); err == nil {
			foundBackup = got
			break
		}
	}
	if foundBackup == nil {
		t.Fatalf("no backup found for %q under %v", "extensions/lore-memory.ts", backupDirs)
	}
	if string(foundBackup) != string(original) {
		t.Fatalf("backup content = %q, want original %q", string(foundBackup), string(original))
	}
	assertNoTokenLeak(t, out, stderr.String(), "secret-token=apply-memory")
}

// TestInstallCommandIdempotentRerunAfterLoreMemoryCleanup verifies a real
// `lore install --yes` run twice in a temp home does not duplicate the cleanup
// on the second run, does not error, and leaves the deprecated file absent.
func TestInstallCommandIdempotentRerunAfterLoreMemoryCleanup(t *testing.T) {
	homeDir, _ := setIsolatedPiHome(t)
	layout := install.ResolvePiLayout(homeDir)
	if err := os.MkdirAll(layout.ExtensionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll extensions: %v", err)
	}
	absolutePath := filepath.Join(layout.AgentDir, filepath.FromSlash(install.ManagedDeprecatedLoreMemoryRelativePathForTest()))
	if err := os.WriteFile(absolutePath, []byte("leftover"), 0o600); err != nil {
		t.Fatalf("WriteFile lore-memory.ts: %v", err)
	}

	configDir := t.TempDir()
	store := &fakeStore{path: filepath.Join(configDir, "config.json"), loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token=rerun-memory"}}
	client := &fakeClient{subject: httpclient.Subject{UserID: "user-1", Kind: "user"}}
	app, _, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.ExecutablePath = func() (string, error) { return "/usr/local/bin/lore", nil }
	app.BuildInfo = version.Info{Version: "v1.2.3"}

	if exitCode := app.Run([]string{"install", "--yes"}); exitCode != 0 {
		t.Fatalf("first install --yes exitCode = %d, want 0, stderr=%q", exitCode, stderr.String())
	}
	if _, err := os.Stat(absolutePath); !os.IsNotExist(err) {
		t.Fatalf("lore-memory.ts stat error = %v, want absent after first apply", err)
	}

	// Second run must be idempotent: no failure, no duplicate cleanup, file absent.
	app2, stdout2, stderr2 := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app2.ExecutablePath = func() (string, error) { return "/usr/local/bin/lore", nil }
	app2.BuildInfo = version.Info{Version: "v1.2.3"}
	if exitCode := app2.Run([]string{"install", "--yes"}); exitCode != 0 {
		t.Fatalf("second install --yes exitCode = %d, want 0, stderr=%q stdout=%q", exitCode, stderr2.String(), stdout2.String())
	}
	if _, err := os.Stat(absolutePath); !os.IsNotExist(err) {
		t.Fatalf("lore-memory.ts stat error = %v, want absent after idempotent rerun", err)
	}
	out := stdout2.String()
	if strings.Contains(out, "managed_action=delete:extensions/lore-memory.ts") {
		t.Fatalf("stdout = %q, want no managed_action=delete:extensions/lore-memory.ts on idempotent rerun", out)
	}
	assertNoTokenLeak(t, out, stderr2.String(), "secret-token=rerun-memory")
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

// TestInstallCommandPiMCPConfigMaterializesBearerTokenPlaintext verifies that a full install
// (with lore-server-mcp component) writes the actual saved token in plaintext into
// ~/.pi/agent/mcp.json,matching Antigravity mcp_config.json behavior. This is the contract
// fix for pi-default-hosted-mcp-install repair v2: the installed mcp.json must contain
// "Authorization": "Bearer <token>" directly, not ${LORE_API_TOKEN} or another env-var
// placeholder.
//
// The test also verifies that the token does NOT appear in stdout/stderr, ensuring dry-run
// and install output remain redacted while the installed file contains plaintext.
func TestInstallCommandPiMCPConfigMaterializesBearerTokenPlaintext(t *testing.T) {
	homeDir, piAgentDir := setIsolatedPiHome(t)
	layout := install.ResolvePiLayout(homeDir)
	if err := os.MkdirAll(layout.ManagedAgentsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll managed agents dir: %v", err)
	}

	const tokenForMCPConfig = "TEST_PERSISTED_TOKEN_12345"
	configDir := t.TempDir()
	store := &fakeStore{path: filepath.Join(configDir, "config.json"), loaded: config.Config{ServerURL: "https://lore.example.test", APIToken: tokenForMCPConfig}}
	client := &fakeClient{subject: httpclient.Subject{UserID: "user-1", Kind: "user"}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.ExecutablePath = func() (string, error) { return "/usr/local/bin/lore", nil }
	app.BuildInfo = version.Info{Version: "v1.2.3"}

	// Run full install (not dry-run) so files are actually written.
	// Include all default components so the full 5-file managed set is produced and manifest
	// validation passes (settings.json + mcp.json + extended-skills = 5 managed files).
	if exitCode := app.Run([]string{"install", "--yes", "--component", "core-pack", "--component", "lore-server-mcp", "--component", "extended-skills"}); exitCode != 0 {
		t.Fatalf("install --yes exitCode = %d, want 0, stderr=%q stdout=%q", exitCode, stderr.String(), stdout.String())
	}

	// Verify mcp.json was written.
	mcpPath := filepath.Join(piAgentDir, "mcp.json")
	if _, err := os.Stat(mcpPath); err != nil {
		t.Fatalf("mcp.json stat err = %v, want file written", err)
	}

	// Read and verify the installed mcp.json contains the plaintext Bearer token.
	mcpBytes, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("ReadFile mcp.json: %v", err)
	}
	mcpText := string(mcpBytes)
	expectedAuthLine := `"Authorization": "Bearer ` + tokenForMCPConfig + `"`
	if !strings.Contains(mcpText, expectedAuthLine) {
		t.Fatalf("installed mcp.json does not contain plaintext Bearer token.\nfile content:\n%s\n\nwant Authorization line: %q", mcpText, expectedAuthLine)
	}

	// Verify the installed file does NOT contain env-var shell placeholders.
	if strings.Contains(mcpText, "${LORE_API_TOKEN}") {
		t.Fatalf("installed mcp.json contains forbidden shell env-var placeholder ${LORE_API_TOKEN}.\nfile content:\n%s", mcpText)
	}
	if strings.Contains(mcpText, "${}") {
		t.Fatalf("installed mcp.json contains env-var shell syntax.\nfile content:\n%s", mcpText)
	}
	if strings.Contains(mcpText, "{{}}") {
		t.Fatalf("installed mcp.json contains unrendered template placeholder.\nfile content:\n%s", mcpText)
	}

	// Verify output does NOT contain the plaintext token (redaction).
	assertNoTokenLeak(t, stdout.String(), stderr.String(), tokenForMCPConfig)

	_ = homeDir
}

func TestUpdateCommandDryRunReportsBinaryOnlyPlanWithoutPiMutation(t *testing.T) {
	homeDir, _ := setIsolatedPiHome(t)
	piRoot := filepath.Join(homeDir, ".pi")
	if err := os.MkdirAll(piRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll piRoot: %v", err)
	}
	legacyPath := filepath.Join(piRoot, "legacy.txt")
	if err := os.WriteFile(legacyPath, []byte("keep-me"), 0o600); err != nil {
		t.Fatalf("WriteFile legacyPath: %v", err)
	}

	store := &fakeStore{path: filepath.Join(t.TempDir(), "config.json"), loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token=update-dry-run"}}
	app, stdout, stderr := newTestApp(store, nil)
	app.ExecutablePath = func() (string, error) { return "/tmp/lore", nil }
	app.LookPath = func(name string) (string, error) { return "/tmp/other-lore", nil }
	app.BuildInfo = version.Info{Version: "v0.2.5"}

	if exitCode := app.Run([]string{"update", "--dry-run"}); exitCode != 1 {
		t.Fatalf("update --dry-run exitCode = %d, want 1, stderr=%q stdout=%q", exitCode, stderr.String(), stdout.String())
	}
	if got, err := os.ReadFile(legacyPath); err != nil || string(got) != "keep-me" {
		t.Fatalf("legacyPath after update dry-run = %q err=%v, want unchanged", string(got), err)
	}
	combined := strings.ToLower(stdout.String() + "\n" + stderr.String())
	for _, want := range []string{"latest", "dry-run", "untouched", "~/.pi", "path mismatch"} {
		if !strings.Contains(combined, strings.ToLower(want)) {
			t.Fatalf("combined output = %q, want substring %q", combined, want)
		}
	}
	assertNoTokenLeak(t, stdout.String(), stderr.String(), "secret-token=update-dry-run")
}

func TestUpdateCommandYesModeFailsClosedOnUnsafeTargetWithoutPiMutation(t *testing.T) {
	homeDir, _ := setIsolatedPiHome(t)
	piRoot := filepath.Join(homeDir, ".pi")
	if err := os.MkdirAll(piRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll piRoot: %v", err)
	}
	legacyPath := filepath.Join(piRoot, "legacy.txt")
	if err := os.WriteFile(legacyPath, []byte("keep-me"), 0o600); err != nil {
		t.Fatalf("WriteFile legacyPath: %v", err)
	}

	store := &fakeStore{path: filepath.Join(t.TempDir(), "config.json"), loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token=update-yes"}}
	app, stdout, stderr := newTestApp(store, nil)
	app.ExecutablePath = func() (string, error) { return "/tmp/link/lore", nil }
	app.LookPath = func(name string) (string, error) { return "/tmp/link/lore", nil }
	app.BuildInfo = version.Info{Version: "dev"}

	if exitCode := app.Run([]string{"update", "--yes"}); exitCode != 1 {
		t.Fatalf("update --yes exitCode = %d, want 1, stderr=%q stdout=%q", exitCode, stderr.String(), stdout.String())
	}
	if got, err := os.ReadFile(legacyPath); err != nil || string(got) != "keep-me" {
		t.Fatalf("legacyPath after update --yes = %q err=%v, want unchanged", string(got), err)
	}
	combined := strings.ToLower(stdout.String() + "\n" + stderr.String())
	for _, want := range []string{"dev", "unsafe", "untouched", "~/.pi"} {
		if !strings.Contains(combined, strings.ToLower(want)) {
			t.Fatalf("combined output = %q, want substring %q", combined, want)
		}
	}
	assertNoTokenLeak(t, stdout.String(), stderr.String(), "secret-token=update-yes")
}

func TestUpdateUsageIncludesBinaryOnlyGuidance(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loadErr: config.ErrNotFound}
	app, _, stderr := newTestApp(store, nil)

	if exitCode := app.Run([]string{"update", "unexpected"}); exitCode != 1 {
		t.Fatalf("update exitCode = %d, want 1", exitCode)
	}
	for _, want := range []string{
		"Usage: lore update",
		"Pi runtime",
		"~/.pi",
		"--dry-run",
		"--yes",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want substring %q", stderr.String(), want)
		}
	}
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

func TestNewConfiguresProductionDefaults(t *testing.T) {
	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	app := New(t.TempDir(), stdout, stderr, version.Info{Version: "v1.2.3"})

	if app.Stdout != stdout || app.Stderr != stderr {
		t.Fatal("New did not preserve provided stdout/stderr writers")
	}
	if app.Store == nil || app.Auth == nil || app.ClientFactory == nil || app.Stdin == nil || app.LookPath == nil || app.UserHomeDir == nil || app.ExecutablePath == nil {
		t.Fatalf("New returned incomplete app defaults: %+v", app)
	}
	if got := app.BuildInfo.Version; got != "v1.2.3" {
		t.Fatalf("BuildInfo.Version = %q, want v1.2.3", got)
	}
}

func TestComponentFlagParsesRepeatedCommaSeparatedValues(t *testing.T) {
	var flag componentFlag
	if err := flag.Set("core-pack, pi-extensions"); err != nil {
		t.Fatalf("Set comma-separated error = %v", err)
	}
	if err := flag.Set("lore-server-mcp"); err != nil {
		t.Fatalf("Set repeated error = %v", err)
	}
	if got, want := flag.String(), "core-pack,pi-extensions,lore-server-mcp"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
	components := flag.ComponentIDs()
	if got, want := len(components), 3; got != want {
		t.Fatalf("len(ComponentIDs()) = %d, want %d", got, want)
	}
	if components[0] != install.ComponentCorePack || components[1] != install.ComponentPiExtensions || components[2] != install.ComponentLoreServerMCP {
		t.Fatalf("ComponentIDs() = %v, want parsed component ids", components)
	}
	components[0] = "mutated"
	if got := flag.ComponentIDs()[0]; got != install.ComponentCorePack {
		t.Fatalf("ComponentIDs() exposed mutable backing store, got %q", got)
	}
	if err := flag.Set(" , "); err == nil || !strings.Contains(err.Error(), "component value cannot be empty") {
		t.Fatalf("Set empty error = %v, want empty component rejection", err)
	}
}

func testMCPFrame(payload string) string {
	return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(payload), payload)
}

func readTestMCPFrame(t *testing.T, framed string) []byte {
	t.Helper()
	reader := bufio.NewReader(strings.NewReader(framed))
	contentLength := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("ReadString() error = %v", err)
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			break
		}
		if strings.HasPrefix(trimmed, "Content-Length: ") {
			if _, err := fmt.Sscanf(trimmed, "Content-Length: %d", &contentLength); err != nil {
				t.Fatalf("Sscanf(Content-Length): %v", err)
			}
		}
	}
	payload := make([]byte, contentLength)
	if _, err := reader.Read(payload); err != nil {
		t.Fatalf("Read(payload) error = %v", err)
	}
	return payload
}

func readTestMCPJSONLResponse(t *testing.T, output string) []byte {
	t.Helper()
	line := strings.TrimSpace(output)
	if line == "" {
		t.Fatal("JSONL response was empty")
	}
	return []byte(line)
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
	loginErr          error
	meErr             error
	createErr         error
	listErr           error
	loginResult       httpclient.PasswordLoginResult
	subject           httpclient.Subject
	memory            httpclient.Memory
	memories          []httpclient.Memory
	loginEmail        string
	loginPassword     string
	meToken           string
	createToken       string
	listToken         string
	createRequest     httpclient.CreateMemoryRequest
	listFilter        httpclient.ListMemoriesFilter
	loginCalls        int
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
	mcpJSONRPCToken   string
	mcpJSONRPCMethod  string
	mcpJSONRPCParams  json.RawMessage
	mcpJSONRPCResult  httpclient.RequestJSONResult
	mcpJSONRPCErr     error
	mcpForwardToken   string
	mcpForwardMethod  string
	mcpForwardParams  json.RawMessage
	mcpForwardResult  json.RawMessage
	mcpForwardErr     error
}

func (c *fakeClient) Health(_ context.Context) error { return c.healthErr }
func (c *fakeClient) Ready(_ context.Context) error  { return c.readyErr }
func (c *fakeClient) Login(_ context.Context, email, password string) (httpclient.PasswordLoginResult, error) {
	c.loginCalls++
	c.loginEmail = email
	c.loginPassword = password
	if c.loginErr != nil {
		return httpclient.PasswordLoginResult{}, c.loginErr
	}
	return c.loginResult, nil
}
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
