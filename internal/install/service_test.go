package install

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alferio94/lore-cli/internal/agentconfig"
	"github.com/alferio94/lore-cli/internal/agentpack"
	"github.com/alferio94/lore-cli/internal/auth"
	"github.com/alferio94/lore-cli/internal/config"
	"github.com/alferio94/lore-cli/internal/httpclient"
	"github.com/alferio94/lore-cli/internal/output"
)

func TestDefaultTargetsPreferPiAndMarkOthersComingSoon(t *testing.T) {
	targets := DefaultTargets()
	if len(targets) != 5 {
		t.Fatalf("len(targets) = %d, want 5", len(targets))
	}
	if got := targets[0]; got.ID != TargetPi || !got.Available || !got.Recommended {
		t.Fatalf("targets[0] = %+v, want available recommended Pi", got)
	}
	if got := targets[0].Description; !containsAll(got, "uses hosted Lore MCP via pi-mcp-adapter", "optional explicit pi-extensions (lore-memory)") {
		t.Fatalf("targets[0].Description = %q, want hosted MCP default with optional explicit pi-extensions", got)
	}
	if got := findTarget(targets, TargetAntigravity); !got.Available || got.Recommended || !containsAll(got.Description, "prompt", "skills", "agent profile", "optional direct MCP config") {
		t.Fatalf("antigravity target = %+v, want supported prompt/skills target with managed Gemini agent profile and optional direct MCP config", got)
	}
	for _, want := range []TargetID{TargetClaudeCode, TargetOpenCode} {
		target := findTarget(targets, want)
		if target.Available {
			t.Fatalf("target %s unexpectedly available", want)
		}
		if got := target.Availability; got != "Coming soon" {
			t.Fatalf("target %s availability = %q, want Coming soon", want, got)
		}
	}
	// Codex is now a supported target (config-only projection).
	if got := findTarget(targets, TargetCodex); !got.Available {
		t.Fatalf("target codex should be available, got %+v", got)
	}
}

func TestResolveInstallTargetKeepsPiDefaultAndRejectsRoadmapTargets(t *testing.T) {
	selected, err := ResolveInstallTarget("")
	if err != nil {
		t.Fatalf("ResolveInstallTarget(\"\") error = %v, want nil", err)
	}
	if selected.ID != TargetPi || !selected.Available || !selected.Recommended {
		t.Fatalf("ResolveInstallTarget(\"\") = %+v, want recommended Pi", selected)
	}

	selected, err = ResolveInstallTarget(TargetPi)
	if err != nil {
		t.Fatalf("ResolveInstallTarget(pi) error = %v, want nil", err)
	}
	if selected.ID != TargetPi {
		t.Fatalf("ResolveInstallTarget(pi) = %+v, want Pi", selected)
	}

	selected, err = ResolveInstallTarget(TargetAntigravity)
	if err != nil {
		t.Fatalf("ResolveInstallTarget(antigravity) error = %v, want nil", err)
	}
	if selected.ID != TargetAntigravity || !selected.Available {
		t.Fatalf("ResolveInstallTarget(antigravity) = %+v, want supported Antigravity target", selected)
	}

	if _, err := ResolveInstallTarget(TargetClaudeCode); err == nil || !containsAll(err.Error(), string(TargetClaudeCode), "Coming soon", "supported targets") {
		t.Fatalf("ResolveInstallTarget(claude-code) error = %v, want roadmap guardrail", err)
	}
	if _, err := ResolveInstallTarget(TargetID("unknown-target")); err == nil || !containsAll(err.Error(), "unknown target") {
		t.Fatalf("ResolveInstallTarget(unknown-target) error = %v, want unknown target rejection", err)
	}
}

func TestFormatTargetSelectionExplainsPiNativePathAndMCPDeferral(t *testing.T) {
	formatted := FormatTargetSelection(DefaultTargets())
	for _, want := range []string{"Choose an install target:", "Pi — Recommended", "uses hosted Lore MCP", "Antigravity:", "prompt + skills", "Coming soon", "Pi remains the default recommended path", "uses hosted Lore MCP by default", "~/.gemini/config/agents/lore.json", "optionally write direct MCP config"} {
		if !strings.Contains(formatted, want) {
			t.Fatalf("FormatTargetSelection() = %q, want substring %q", formatted, want)
		}
	}
}

func TestResolvePiLayoutModelsManagedPaths(t *testing.T) {
	layout := ResolvePiLayout("/tmp/home")
	if got, want := layout.AgentDir, "/tmp/home/.pi/agent"; got != want {
		t.Fatalf("AgentDir = %q, want %q", got, want)
	}
	if got, want := layout.ManifestPath, "/tmp/home/.pi/agent/lore-install.json"; got != want {
		t.Fatalf("ManifestPath = %q, want %q", got, want)
	}
	if len(layout.ManagedFiles) != 5 {
		t.Fatalf("ManagedFiles = %v, want 5 managed paths (mcp.json + settings.json + 3 extended skills) — lore-memory assets are optional for Pi default", layout.ManagedFiles)
	}
}

func TestPreflightAllowsContinuationWithHealthySavedAuth(t *testing.T) {
	store := stubStore{path: "/tmp/lore/config.json", cfg: config.Config{ServerURL: "https://example.test", APIToken: "secret-token"}}
	client := &stubClient{subject: httpclient.Subject{UserID: "user-1", Kind: "user"}}
	service := Service{Store: store, Auth: stubAuthLoader{session: auth.Session{ServerURL: "https://example.test", Token: "secret-token", ConfigPath: "/tmp/lore/config.json"}}, ClientFactory: func(baseURL string) (httpclient.Client, error) {
		if got, want := baseURL, "https://example.test"; got != want {
			t.Fatalf("baseURL = %q, want %q", got, want)
		}
		return client, nil
	}}

	result := service.Preflight(context.Background())
	if !result.CanContinue || result.LoginRequired {
		t.Fatalf("result = %+v, want continue without login", result)
	}
	if got := result.Targets[0].ID; got != TargetPi {
		t.Fatalf("recommended target = %q, want %q", got, TargetPi)
	}
	assertCheck(t, result.Checks[0], "config", output.StatusOK)
	assertCheck(t, result.Checks[1], "healthz", output.StatusOK)
	assertCheck(t, result.Checks[2], "readyz", output.StatusOK)
	assertCheck(t, result.Checks[3], "auth", output.StatusOK)
}

func TestPreflightBlocksMissingConfigAndAuthFailure(t *testing.T) {
	t.Run("missing config", func(t *testing.T) {
		service := Service{Store: stubStore{path: "/tmp/lore/config.json", err: config.ErrNotFound}}
		result := service.Preflight(context.Background())
		if result.CanContinue || !result.LoginRequired {
			t.Fatalf("result = %+v, want blocked login-required state", result)
		}
		assertCheck(t, result.Checks[0], "config", output.StatusWarn)
		if got := result.Checks[0].Action; got == "" || !containsAll(got, "lore login", "--email", "--token") {
			t.Fatalf("config action = %q, want password-first login guidance with token compatibility", got)
		}
	})

	t.Run("auth failure", func(t *testing.T) {
		store := stubStore{path: "/tmp/lore/config.json", cfg: config.Config{ServerURL: "https://example.test", APIToken: "secret-token"}}
		client := &stubClient{meErr: &httpclient.UnauthorizedError{APIError: httpclient.APIError{StatusCode: 401, Code: "unauthorized", Message: "login required", RequestID: "req-auth"}}}
		service := Service{Store: store, Auth: stubAuthLoader{session: auth.Session{ServerURL: "https://example.test", Token: "secret-token", ConfigPath: "/tmp/lore/config.json"}}, ClientFactory: func(string) (httpclient.Client, error) { return client, nil }}
		result := service.Preflight(context.Background())
		if result.CanContinue || !result.LoginRequired {
			t.Fatalf("result = %+v, want auth-blocked login-required state", result)
		}
		assertCheck(t, result.Checks[3], "auth", output.StatusFail)
		if got := result.Checks[3].Detail; got == "" || !containsAll(got, "normal user API token required", "/v1/me") {
			t.Fatalf("auth detail = %q, want token remediation detail", got)
		}
		if got := result.Checks[3].Action; got != "Obtain a valid password-login session or compatibility token and run lore login again." {
			t.Fatalf("auth action = %q, want password-first unauthorized remediation", got)
		}
	})

	t.Run("missing credential", func(t *testing.T) {
		store := stubStore{path: "/tmp/lore/config.json", cfg: config.Config{ServerURL: "https://example.test", CredentialAccount: "acct-test"}}
		service := Service{Store: store, Auth: stubAuthLoader{err: &auth.Error{Code: auth.ErrCredentialMissing, Op: "load credential", Err: auth.ErrCredentialNotFound}}}
		result := service.Preflight(context.Background())
		if result.CanContinue || !result.LoginRequired {
			t.Fatalf("result = %+v, want blocked login-required state", result)
		}
		assertCheck(t, result.Checks[1], "auth", output.StatusFail)
		if got := result.Checks[1].Detail; got != "saved login state is incomplete" {
			t.Fatalf("auth detail = %q, want incomplete saved-login guidance", got)
		}
		if got := result.Checks[1].Action; got != "Run lore login again with password login or a valid compatibility token." {
			t.Fatalf("auth action = %q, want shared password-first remediation", got)
		}
	})

	t.Run("headless keychain failure", func(t *testing.T) {
		store := stubStore{path: "/tmp/lore/config.json", cfg: config.Config{ServerURL: "https://example.test"}}
		service := Service{Store: store, Auth: stubAuthLoader{err: &auth.Error{Code: auth.ErrCredentialUnavailable, Op: "load credential", Err: errors.New("no keyring")}}}
		result := service.Preflight(context.Background())
		if result.CanContinue || !result.LoginRequired {
			t.Fatalf("result = %+v, want blocked login-required state", result)
		}
		assertCheck(t, result.Checks[1], "auth", output.StatusFail)
		if got := result.Checks[1].Action; got == "" || !containsAll(got, "OS keychain", "headless Linux", "gnome-keyring", "lore login") {
			t.Fatalf("auth action = %q, want headless keychain remediation", got)
		}
		if got := result.Checks[1].Detail + "\n" + result.Checks[1].Action; strings.Contains(got, "secret-token") || strings.Contains(got, "password") || strings.Contains(got, "LORE_PASSWORD") || strings.Contains(got, "--password") {
			t.Fatalf("headless keychain guidance leaked unsafe secret handling: %q", got)
		}
	})

	t.Run("invalid saved server URL remediation is password-first", func(t *testing.T) {
		store := stubStore{path: "/tmp/lore/config.json", cfg: config.Config{ServerURL: "ftp://bad.example", CredentialAccount: "acct-test"}}
		service := Service{Store: store, Auth: stubAuthLoader{session: auth.Session{ServerURL: "ftp://bad.example", Token: "secret-token", ConfigPath: "/tmp/lore/config.json", CredentialAccount: "acct-test"}}, ClientFactory: func(string) (httpclient.Client, error) {
			return nil, errors.New("server URL must start with http:// or https://")
		}}
		result := service.Preflight(context.Background())
		if result.CanContinue || result.LoginRequired {
			t.Fatalf("result = %+v, want server-url failure without login-required state", result)
		}
		assertCheck(t, result.Checks[1], "server-url", output.StatusFail)
		if got := result.Checks[1].Action; got == "" || !containsAll(got, "--email", "--token", "password login") {
			t.Fatalf("server-url action = %q, want password-first remediation with token compatibility", got)
		}
		if got := result.Checks[1].Detail + "\n" + result.Checks[1].Action; strings.Contains(got, "secret-token") {
			t.Fatalf("server-url guidance leaked token: %q", got)
		}
	})
}

func TestExplainEndpointErrorKeepsDiagnosticsActionableAndTokenSafe(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "network",
			err:  &httpclient.NetworkError{URL: "https://lore.example/readyz", Err: errors.New("secret-token transport failure")},
			want: "network request failed",
		},
		{
			name: "readiness",
			err:  &httpclient.ReadinessError{APIError: httpclient.APIError{StatusCode: 503, Code: "not_ready", Message: "migrations pending", RequestID: "req-ready"}},
			want: "service not ready: migrations pending (request_id=req-ready)",
		},
		{
			name: "api with request id",
			err:  &httpclient.APIError{StatusCode: 500, Code: "internal", Message: "temporary failure", RequestID: "req-api"},
			want: "temporary failure (request_id=req-api)",
		},
		{
			name: "api without request id",
			err:  &httpclient.APIError{StatusCode: 400, Code: "bad_request", Message: "invalid payload"},
			want: "invalid payload",
		},
		{
			name: "generic",
			err:  errors.New("plain failure"),
			want: "plain failure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := explainEndpointError(tt.err)
			if got != tt.want {
				t.Fatalf("explainEndpointError() = %q, want %q", got, tt.want)
			}
			if strings.Contains(got, "secret-token") {
				t.Fatalf("explainEndpointError() leaked token: %q", got)
			}
		})
	}
}

func assertCheck(t *testing.T, got output.Check, wantName, wantStatus string) {
	t.Helper()
	if got.Name != wantName || got.Status != wantStatus {
		t.Fatalf("check = %+v, want name=%q status=%q", got, wantName, wantStatus)
	}
}

func findTarget(targets []Target, id TargetID) Target {
	for _, target := range targets {
		if target.ID == id {
			return target
		}
	}
	return Target{}
}

func containsAll(value string, wants ...string) bool {
	for _, want := range wants {
		if !strings.Contains(value, want) {
			return false
		}
	}
	return true
}

type stubStore struct {
	path string
	cfg  config.Config
	err  error
}

type stubAuthLoader struct {
	session auth.Session
	err     error
}

func (s stubStore) Load() (config.Config, error) {
	if s.err != nil {
		return config.Config{}, s.err
	}
	return s.cfg, nil
}

func (s stubStore) Path() (string, error) {
	if s.path == "" {
		return "", errors.New("missing path")
	}
	return s.path, nil
}

func (s stubAuthLoader) Load() (auth.Session, error) {
	if s.err != nil {
		return auth.Session{}, s.err
	}
	return s.session, nil
}

type stubClient struct {
	healthErr error
	readyErr  error
	meErr     error
	subject   httpclient.Subject
}

func (c *stubClient) Health(context.Context) error { return c.healthErr }
func (c *stubClient) Ready(context.Context) error  { return c.readyErr }
func (*stubClient) Login(context.Context, string, string) (httpclient.PasswordLoginResult, error) {
	panic("unexpected Login call")
}
func (c *stubClient) Me(context.Context, string) (httpclient.Subject, error) {
	if c.meErr != nil {
		return httpclient.Subject{}, c.meErr
	}
	return c.subject, nil
}
func (*stubClient) CreateMemory(context.Context, string, httpclient.CreateMemoryRequest) (httpclient.Memory, error) {
	panic("unexpected CreateMemory call")
}
func (*stubClient) ListMemories(context.Context, string, httpclient.ListMemoriesFilter) ([]httpclient.Memory, error) {
	panic("unexpected ListMemories call")
}
func (*stubClient) RequestJSON(context.Context, string, string, string, json.RawMessage) (httpclient.RequestJSONResult, error) {
	panic("unexpected RequestJSON call")
}

func TestInstallPiWritesManagedFilesBackupsAndManifest(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolvePiLayout(homeDir)
	if err := os.MkdirAll(layout.ExtensionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll extensions: %v", err)
	}
	// Write existing lore-memory.ts for legacy migration test (dormant after default change).
	if err := os.WriteFile(filepath.Join(layout.ExtensionsDir, "lore-memory.ts"), []byte("legacy token=secret-token"), 0o644); err != nil {
		t.Fatalf("WriteFile lore-memory.ts: %v", err)
	}
	if err := os.WriteFile(layout.SettingsPath, []byte("{\n  \"theme\": \"night\"\n}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile settings.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(layout.ExtensionsDir, "lore-delegation.ts"), []byte("legacy delegation"), 0o644); err != nil {
		t.Fatalf("WriteFile lore-delegation.ts: %v", err)
	}

	now := time.Date(2026, 5, 18, 19, 0, 0, 0, time.UTC)
	result, err := Service{}.InstallPi(PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  "/tmp/lore-config",
		LoreCLIVersion: "v1.2.3",
		SavedToken:     "secret-token",
		Now:            now,
	})
	if err != nil {
		t.Fatalf("InstallPi error: %v", err)
	}
	if len(result.Summary.Failed) != 0 {
		t.Fatalf("Failed = %v, want none", result.Summary.Failed)
	}
	// Default install creates: mcp.json + settings.json + 7 agent overlays = 9, plus extended skills.
	if len(result.Summary.Created) < 9 {
		t.Fatalf("Created = %v, want at least 9 files (mcp.json + settings.json + 7 agent overlays)", result.Summary.Created)
	}
	// settings.json and mcp.json should be updated, lore-memory is not touched by default.
	if len(result.Summary.Updated) < 1 {
		t.Fatalf("Updated = %v, want at least settings.json update", result.Summary.Updated)
	}
	if len(result.Summary.Deleted) != 1 || result.Summary.Deleted[0] != filepath.Join("extensions", "lore-delegation.ts") {
		t.Fatalf("Deleted = %v, want legacy delegation cleanup", result.Summary.Deleted)
	}
	if len(result.Summary.BackedUp) < 2 {
		t.Fatalf("BackedUp = %v, want at least 2 backups (legacy delegation + settings.json)", result.Summary.BackedUp)
	}
	if result.Manifest.AuthMode != "cli-request" || result.Manifest.ServerURL != "https://lore.example" {
		t.Fatalf("Manifest = %+v, want cli-request manifest with server URL", result.Manifest)
	}
	if got, want := result.Manifest.SchemaVersion, PortableManifestSchemaVersion; got != want {
		t.Fatalf("Manifest.SchemaVersion = %q, want %q", got, want)
	}
	if result.Manifest.BackupRoot == "" {
		t.Fatalf("Manifest.BackupRoot is empty, want backup root set")
	}
	// Default managed files: mcp.json + settings.json + 3 extended skills = 5.
	if len(result.Manifest.ManagedFiles) != 5 {
		t.Fatalf("len(Manifest.ManagedFiles) = %d, want 5 (mcp.json + settings.json + 3 extended skills) — lore-memory is optional for Pi default", len(result.Manifest.ManagedFiles))
	}
	if len(result.Manifest.ManagedAgentOverlays) != 10 {
		t.Fatalf("len(Manifest.ManagedAgentOverlays) = %d, want 10", len(result.Manifest.ManagedAgentOverlays))
	}
	// Default components: core-pack + lore-server-mcp + extended-skills (not pi-extensions).
	if got := result.Manifest.Components; !equalComponentIDs(got, []ComponentID{ComponentCorePack, ComponentLoreServerMCP, ComponentExtendedSkills}) {
		t.Fatalf("Manifest.Components = %v, want core-pack + lore-server-mcp + extended-skills (hosted MCP default)", got)
	}
	for i, want := range layout.ManagedFiles {
		managed := result.Manifest.ManagedFiles[i]
		if got := managed.Path; got != want {
			t.Fatalf("Manifest.ManagedFiles[%d].Path = %q, want %q", i, got, want)
		}
		if managed.Component == "" || managed.ContentHash == "" {
			t.Fatalf("Manifest.ManagedFiles[%d] = %+v, want component and content hash", i, managed)
		}
	}

	// Default install does NOT re-render lore-memory.ts or lore-footer.ts (dormant for Pi default).
	// The legacy lore-memory.ts from the pre-existing setup should remain untouched.
	// Only settings.json and mcp.json are managed by default install.

	if _, err := os.Stat(filepath.Join(layout.ExtensionsDir, "lore-delegation.ts")); !os.IsNotExist(err) {
		t.Fatalf("lore-delegation.ts stat error = %v, want file removed after cleanup", err)
	}
	if _, err := os.ReadFile(filepath.Join(result.Manifest.BackupRoot, "extensions", "lore-delegation.ts")); err != nil {
		t.Fatalf("ReadFile backup lore-delegation.ts: %v", err)
	}

	manifestContent, err := os.ReadFile(layout.ManifestPath)
	if err != nil {
		t.Fatalf("ReadFile manifest: %v", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestContent, &manifest); err != nil {
		t.Fatalf("Unmarshal manifest: %v", err)
	}
	if manifest.InstalledAt != now.Format(time.RFC3339) {
		t.Fatalf("manifest installed_at = %q, want %q", manifest.InstalledAt, now.Format(time.RFC3339))
	}
	// lore-memory.ts is dormant for default Pi install — no backup should be created.
	// The legacy lore-memory.ts remains untouched in the extensions dir.
	if _, err := os.Stat(filepath.Join(layout.ExtensionsDir, "lore-memory.ts")); err != nil {
		t.Fatalf("lore-memory.ts stat error = %v, want dormant file preserved", err)
	}
}

func TestMergeJSONAdditivePackagesPreservesOrderAndIdempotence(t *testing.T) {
	hostedPackage := PiHostedMCPPackageSource()
	merged, err := mergeJSONAdditive(
		[]byte(`{"packages":["pkg-a","pkg-b"],"lore":{"existing":true},"theme":"night"}`),
		[]byte(`{"packages":["pkg-b","`+hostedPackage+`"],"lore":{"auth_mode":"cli-request"},"theme":"alferio"}`),
	)
	if err != nil {
		t.Fatalf("mergeJSONAdditive error = %v, want nil", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(merged, &settings); err != nil {
		t.Fatalf("Unmarshal merged settings: %v", err)
	}
	packages, ok := settings["packages"].([]any)
	if !ok {
		t.Fatalf("packages = %T, want []any", settings["packages"])
	}
	if got, want := packages, []any{"pkg-a", "pkg-b", hostedPackage}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("packages = %v, want %v", got, want)
	}
	loreSettings, ok := settings["lore"].(map[string]any)
	if !ok || loreSettings["existing"] != true || loreSettings["auth_mode"] != "cli-request" {
		t.Fatalf("lore settings = %v, want merged lore config", settings["lore"])
	}
	if settings["theme"] != "night" {
		t.Fatalf("theme = %v, want preserved existing theme", settings["theme"])
	}

	rerun, err := mergeJSONAdditive(merged, []byte(`{"packages":["`+hostedPackage+`"]}`))
	if err != nil {
		t.Fatalf("mergeJSONAdditive rerun error = %v, want nil", err)
	}
	if string(rerun) != string(merged) {
		t.Fatalf("rerun settings = %s, want idempotent merge matching first result %s", rerun, merged)
	}
}

func TestMergeJSONAdditivePackagesPreservesUserPackageObjects(t *testing.T) {
	hostedPackage := PiHostedMCPPackageSource()
	merged, err := mergeJSONAdditive(
		[]byte(`{"packages":[{"url":"git:github.com/example/custom","label":"custom"},"pkg-a"]}`),
		[]byte(`{"packages":["pkg-a","`+hostedPackage+`"]}`),
	)
	if err != nil {
		t.Fatalf("mergeJSONAdditive error = %v, want nil", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(merged, &settings); err != nil {
		t.Fatalf("Unmarshal merged settings: %v", err)
	}
	packages, ok := settings["packages"].([]any)
	if !ok {
		t.Fatalf("packages = %T, want []any", settings["packages"])
	}
	if len(packages) != 3 {
		t.Fatalf("packages = %v, want preserved object + pkg-a + lore package", packages)
	}
	first, ok := packages[0].(map[string]any)
	if !ok || first["url"] != "git:github.com/example/custom" || first["label"] != "custom" {
		t.Fatalf("first package = %#v, want preserved user package object", packages[0])
	}
	if packages[1] != "pkg-a" || packages[2] != hostedPackage {
		t.Fatalf("packages = %v, want preserved order with idempotent lore package append", packages)
	}

	rerun, err := mergeJSONAdditive(merged, []byte(`{"packages":["`+hostedPackage+`"]}`))
	if err != nil {
		t.Fatalf("mergeJSONAdditive rerun error = %v, want nil", err)
	}
	if string(rerun) != string(merged) {
		t.Fatalf("rerun settings = %s, want idempotent merge for preserved object packages %s", rerun, merged)
	}
}

func TestInstallPiBootstrapsAlferioThemeOnFreshInstallAndPreservesUserThemeOnRerun(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolvePiLayout(homeDir)
	hostedPackage := PiHostedMCPPackageSource()
	req := PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Now:            time.Date(2026, 5, 20, 1, 0, 0, 0, time.UTC),
	}

	if _, err := (Service{}).InstallPi(req); err != nil {
		t.Fatalf("fresh InstallPi error: %v", err)
	}

	settingsContent, err := os.ReadFile(layout.SettingsPath)
	if err != nil {
		t.Fatalf("ReadFile settings.json: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(settingsContent, &settings); err != nil {
		t.Fatalf("Unmarshal settings.json: %v", err)
	}
	if got := settings["theme"]; got != "alferio" {
		t.Fatalf("fresh settings theme = %v, want alferio bootstrap", got)
	}
	bootstrappedTheme, err := os.ReadFile(layout.AlferioThemePath)
	if err != nil {
		t.Fatalf("ReadFile alferio theme: %v", err)
	}
	if got := string(bootstrappedTheme); !containsAll(got, `"name": "alferio"`, `"accent"`) {
		t.Fatalf("bootstrapped theme = %q, want alferio theme asset", got)
	}

	customTheme := []byte(`{"name":"alferio","palette":{"accent":"#123456"}}
`)
	if err := os.WriteFile(layout.AlferioThemePath, customTheme, 0o600); err != nil {
		t.Fatalf("WriteFile custom alferio theme: %v", err)
	}
	if err := os.WriteFile(layout.SettingsPath, []byte(`{"theme":"night","packages":["`+hostedPackage+`"]}
`), 0o600); err != nil {
		t.Fatalf("WriteFile custom settings: %v", err)
	}

	if _, err := (Service{}).InstallPi(req); err != nil {
		t.Fatalf("rerun InstallPi error: %v", err)
	}

	rerunSettingsContent, err := os.ReadFile(layout.SettingsPath)
	if err != nil {
		t.Fatalf("ReadFile rerun settings.json: %v", err)
	}
	settings = map[string]any{}
	if err := json.Unmarshal(rerunSettingsContent, &settings); err != nil {
		t.Fatalf("Unmarshal rerun settings.json: %v", err)
	}
	if got := settings["theme"]; got != "night" {
		t.Fatalf("rerun settings theme = %v, want preserved existing user theme", got)
	}
	preservedTheme, err := os.ReadFile(layout.AlferioThemePath)
	if err != nil {
		t.Fatalf("ReadFile preserved alferio theme: %v", err)
	}
	if string(preservedTheme) != string(customTheme) {
		t.Fatalf("preserved theme = %q, want user-customized alferio theme preserved", preservedTheme)
	}
}

func TestValidateManagedContentsRejectsRawToken(t *testing.T) {
	hostedPackage := PiHostedMCPPackageSource()
	// lore-memory content contains the saved token "secret-token".
	// The footer content is valid and does not contain the token.
	findings := validateManagedContents(map[string][]byte{
		"extensions/lore-memory.ts": []byte(`
const loreServerURL = "https://lore.example";
// broker args include "api", "request"
// Project context now uses the REST broker route
import { Text } from "@earendil-works/pi-tui";
export default function (pi: ExtensionAPI) {
  pi.registerTool({ name: "lore_search", renderCall() {}, renderResult() {} });
  pi.registerTool({ name: "lore_save", renderCall() {}, renderResult() {} });
  pi.registerTool({ name: "lore_get_observation", renderCall() {}, renderResult() {} });
  pi.registerTool({ name: "lore_context", renderCall() {}, renderResult() {} });
  pi.registerTool({ name: "lore_project_list", renderCall() {}, renderResult() {} });
  pi.registerTool({ name: "lore_project_create", renderCall() {}, renderResult() {} });
  pi.registerTool({ name: "lore_project_get", renderCall() {}, renderResult() {} });
  pi.registerTool({ name: "lore_skill_save", renderCall() {}, renderResult() {} });
  pi.registerTool({ name: "lore_skill_list", renderCall() {}, renderResult() {} });
  pi.registerTool({ name: "lore_skill_get", renderCall() {}, renderResult() {} });
}
text: formatContent(payload.data)
/v1/memories /v1/projects /v1/skills
secret-token
`),
		"extensions/lore-footer.ts": []byte("export default function (pi: ExtensionAPI) { ctx.ui.setFooter(() => ({ render() { return []; } })); } getContextUsage getExtensionStatuses"),
		"settings.json":             []byte(`{"packages":["` + hostedPackage + `"],"lore":{"server_url":"https://lore.example"}}`),
	}, PiInstallRequest{ServerURL: "https://lore.example", SavedToken: "secret-token"})
	if len(findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1", len(findings))
	}
	if got := findings[0]; !containsAll(got, "saved auth material", "extensions/lore-memory.ts") || strings.Contains(got, "secret-token") {
		t.Fatalf("finding = %q, want secret-safe token validation detail", got)
	}
}

func TestValidateManagedContentsRejectsLegacyMemoryRoutesWithoutRequiringDelegationSessions(t *testing.T) {
	hostedPackage := PiHostedMCPPackageSource()
	validMemory := []byte(`
const loreServerURL = "https://lore.example";
// broker args include "api", "request"
// Project context now uses the REST broker route
import { Text } from "@earendil-works/pi-tui";
export default function (pi: ExtensionAPI) {
  pi.registerTool({ name: "lore_search", renderCall() {}, renderResult() {} });
  pi.registerTool({ name: "lore_save", renderCall() {}, renderResult() {} });
  pi.registerTool({ name: "lore_get_observation", renderCall() {}, renderResult() {} });
  pi.registerTool({ name: "lore_context", renderCall() {}, renderResult() {} });
  pi.registerTool({ name: "lore_project_list", renderCall() {}, renderResult() {} });
  pi.registerTool({ name: "lore_project_create", renderCall() {}, renderResult() {} });
  pi.registerTool({ name: "lore_project_get", renderCall() {}, renderResult() {} });
  pi.registerTool({ name: "lore_skill_save", renderCall() {}, renderResult() {} });
  pi.registerTool({ name: "lore_skill_list", renderCall() {}, renderResult() {} });
  pi.registerTool({ name: "lore_skill_get", renderCall() {}, renderResult() {} });
}
text: formatContent(payload.data)
/v1/memories /v1/projects /v1/skills /v1/search /v1/observations /v1/context /v1/timeline /v1/stats /v1/sessions
`)
	findings := validateManagedContents(map[string][]byte{
		"extensions/lore-memory.ts": validMemory,
		"extensions/lore-footer.ts": []byte("export default function (pi: ExtensionAPI) { ctx.ui.setFooter(() => ({ render() { return []; } })); } getContextUsage getExtensionStatuses"),
		"settings.json":             []byte(`{"packages":["` + hostedPackage + `"],"lore":{"server_url":"https://lore.example"}}`),
	}, PiInstallRequest{ServerURL: "https://lore.example", SavedToken: "secret-token"})

	if len(findings) != 6 {
		t.Fatalf("findings = %#v, want one finding for each legacy lore-memory.ts route", findings)
	}
	for _, legacy := range []string{"/v1/search", "/v1/observations", "/v1/context", "/v1/timeline", "/v1/stats", "/v1/sessions"} {
		if !containsAny(findings, "extensions/lore-memory.ts", legacy, "forbidden legacy memory contract snippet") {
			t.Fatalf("findings = %#v, want scoped rejection for %s", findings, legacy)
		}
	}
}

func TestValidateRenderedPiFilesRejectsMissingDefaultFactoryBeforeWrites(t *testing.T) {
	layout := ResolvePiLayout(t.TempDir())
	hostedPackage := PiHostedMCPPackageSource()
	files := []renderedPiFile{
		{relativePath: managedPiExtensionRelativePaths[0], absolutePath: filepath.Join(layout.ExtensionsDir, "lore-memory.ts"), content: []byte("lore api request without factory")},
		{relativePath: managedPiExtensionRelativePaths[1], absolutePath: filepath.Join(layout.ExtensionsDir, "lore-footer.ts"), content: []byte("export default function (pi: ExtensionAPI) { ctx.ui.setFooter(() => ({ render() { return []; } })); } getContextUsage getExtensionStatuses")},
		{relativePath: "settings.json", absolutePath: layout.SettingsPath, content: []byte(`{"packages":["` + hostedPackage + `"]}`), mergeMode: MergeModeAdditiveJSON},
	}

	err := validateRenderedPiFiles(files)
	if err == nil || !containsAll(err.Error(), "extensions/lore-memory.ts", "export default function") {
		t.Fatalf("validateRenderedPiFiles error = %v, want default factory rejection", err)
	}
	if _, statErr := os.Stat(layout.AgentDir); !os.IsNotExist(statErr) {
		t.Fatalf("agent dir stat error = %v, want no writes before validation failure", statErr)
	}
}

func TestInstallPiRejectsInvalidRenderedExtensionShapeBeforeAnyWrite(t *testing.T) {
	homeDir := t.TempDir()

	// The test's original intent was to check that an "unexpected-extra.ts" file causes
	// validation to fail before writes. With the hosted MCP default, lore-memory is optional,
	// so the validation check for extension factory presence only applies when pi-extensions
	// is selected. The manifest validation happens after file writes, so we test that
	// the layout's managed files count matches the manifest's managed files count.
	//
	// To test the manifest validation with an unexpected extra path, we mutate the
	// package-level variable AND select pi-extensions so lore-memory files ARE rendered.
	original := append([]string(nil), managedPiExtensionRelativePaths...)
	managedPiExtensionRelativePaths = append(managedPiExtensionRelativePaths, filepath.Join("extensions", "unexpected-extra.ts"))
	defer func() {
		managedPiExtensionRelativePaths = original
	}()

	// Explicitly select pi-extensions + lore-server-mcp. With the extra path in
	// ManagedFiles but no component rendering it, manifest validation fails.
	_, err := Service{}.InstallPi(PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  "/tmp/lore-config",
		LoreCLIVersion: "v1.2.3",
		Components:     []ComponentID{ComponentCorePack, ComponentPiExtensions, ComponentLoreServerMCP},
		Now:            time.Date(2026, 5, 18, 20, 30, 0, 0, time.UTC),
	})

	// Manifest validation fails because ManagedFiles has the extra path but adapter
	// doesn't render unexpected-extra.ts.
	if err == nil {
		t.Fatalf("InstallPi error = nil, want manifest validation failure due to extra path mismatch")
	}
	if !strings.Contains(err.Error(), "validate manifest") && !strings.Contains(err.Error(), "managed_files") {
		t.Fatalf("InstallPi error = %v, want manifest validation error about managed_files", err)
	}
}

func TestLoadManifestRoundTripsAndMatchesPiLayout(t *testing.T) {
	homeDir := t.TempDir()
	now := time.Date(2026, 5, 18, 19, 30, 0, 0, time.UTC)
	result, err := Service{}.InstallPi(PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  "/tmp/lore-config",
		LoreCLIVersion: "v1.2.3",
		Now:            now,
	})
	if err != nil {
		t.Fatalf("InstallPi error: %v", err)
	}

	manifest, err := LoadManifest(result.Layout.ManifestPath)
	if err != nil {
		t.Fatalf("LoadManifest error: %v", err)
	}
	if err := manifest.Validate(result.Layout); err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	if manifest.LoreBinary != "/usr/local/bin/lore" || manifest.LoreConfigDir != "/tmp/lore-config" {
		t.Fatalf("manifest = %+v, want persisted binary and config dir", manifest)
	}
	if got, want := manifest.SchemaVersion, PortableManifestSchemaVersion; got != want {
		t.Fatalf("manifest schema_version = %q, want %q", got, want)
	}
}

func TestManifestValidateRejectsManagedFileMismatch(t *testing.T) {
	layout := ResolvePiLayout("/tmp/home")
	manifest := Manifest{
		SchemaVersion: PortableManifestSchemaVersion,
		Target:        TargetPi,
		AuthMode:      "cli-request",
		ServerURL:     "https://lore.example",
		LoreBinary:    "/usr/local/bin/lore",
		LoreConfigDir: "/tmp/lore-config",
		Components:    []ComponentID{ComponentCorePack, ComponentPiExtensions},
		ManagedFiles:  []ManagedFileRecord{{Path: "/tmp/home/.pi/agent/extensions/lore-memory.ts", Component: ComponentCorePack, MergeMode: MergeModeReplace, ContentHash: "abc"}},
		BackupRoot:    "/tmp/home/.pi/agent/backups/20260518T193000Z",
		InstalledAt:   time.Date(2026, 5, 18, 19, 30, 0, 0, time.UTC).Format(time.RFC3339),
	}

	if err := manifest.Validate(layout); err == nil || !strings.Contains(err.Error(), "managed_files") {
		t.Fatalf("Validate error = %v, want managed_files mismatch", err)
	}
}

func TestLoadManifestUpgradesLegacyPiManifest(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolvePiLayout(homeDir)
	if err := os.MkdirAll(filepath.Dir(layout.ManifestPath), 0o755); err != nil {
		t.Fatalf("MkdirAll manifest dir: %v", err)
	}
	legacy := `{
  "schema_version": "1",
  "target": "pi",
  "auth_mode": "cli-request",
  "server_url": "https://lore.example",
  "lore_binary_path": "/usr/local/bin/lore",
  "lore_config_dir": "/tmp/lore-config",
  "managed_files": [
    "/tmp/home/.pi/agent/extensions/lore-memory.ts",
    "/tmp/home/.pi/agent/extensions/lore-delegation.ts",
    "/tmp/home/.pi/agent/extensions/lore-footer.ts",
    "/tmp/home/.pi/agent/settings.json"
  ],
  "backup_root": "/tmp/home/.pi/agent/backups/20260518T193000Z",
  "installed_at": "2026-05-18T19:30:00Z",
  "lore_cli_version": "v1.2.3"
}
`
	if err := os.WriteFile(layout.ManifestPath, []byte(legacy), 0o600); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	manifest, err := LoadManifest(layout.ManifestPath)
	if err != nil {
		t.Fatalf("LoadManifest error: %v", err)
	}
	if got, want := manifest.SchemaVersion, PortableManifestSchemaVersion; got != want {
		t.Fatalf("schema_version = %q, want upgraded %q", got, want)
	}
	if got := manifest.Components; !equalComponentIDs(got, []ComponentID{ComponentCorePack, ComponentPiExtensions}) {
		t.Fatalf("Components = %v, want default Pi components", got)
	}
	// Legacy manifests predate extended-skills; upgrade preserves base files only.
	if len(manifest.ManagedFiles) != 3 {
		t.Fatalf("len(ManagedFiles) = %d, want 3 (legacy base files, extended skills not part of old install)", len(manifest.ManagedFiles))
	}
	for _, managed := range manifest.ManagedFiles {
		if strings.Contains(managed.Path, "lore-delegation.ts") {
			t.Fatalf("legacy delegation path unexpectedly preserved in upgraded manifest: %+v", managed)
		}
	}
}

func TestPlanPiInstallAcceptsLoreServerMCPWithPiExtensions(t *testing.T) {
	homeDir := t.TempDir()
	// lore-server-mcp is now supported for Pi; it can be combined with pi-extensions.
	_, err := Service{}.PlanPiInstall(PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Target:         TargetPi,
		Components:     []ComponentID{ComponentCorePack, ComponentPiExtensions, ComponentLoreServerMCP},
		Now:            time.Date(2026, 5, 19, 0, 5, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("PlanPiInstall(pi, pi-extensions+lore-server-mcp) error = %v, want nil (both components now supported for Pi)", err)
	}
}

func TestPlanPiInstallRejectsRoadmapTargetEnablement(t *testing.T) {
	homeDir := t.TempDir()
	_, err := Service{}.PlanPiInstall(PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Target:         TargetClaudeCode,
		Now:            time.Date(2026, 5, 19, 0, 6, 0, 0, time.UTC),
	})
	if err == nil || !containsAll(err.Error(), string(TargetClaudeCode), "Coming soon", "supported targets") {
		t.Fatalf("PlanPiInstall error = %v, want roadmap target guardrail", err)
	}
}

func TestPlanAntigravityInstallReportsPromptSkillsActions(t *testing.T) {
	homeDir := t.TempDir()
	now := time.Date(2026, 5, 25, 13, 0, 0, 0, time.UTC)

	plan, err := Service{}.PlanAntigravityInstall(InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		SavedToken:     "secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Target:         TargetAntigravity,
		Now:            now,
	})
	if err != nil {
		t.Fatalf("PlanAntigravityInstall error: %v", err)
	}
	if plan.Layout.Target != TargetAntigravity {
		t.Fatalf("plan.Layout.Target = %q, want %q", plan.Layout.Target, TargetAntigravity)
	}
	if got := len(plan.Files); got == 0 {
		t.Fatal("plan.Files is empty, want prompt/skills/manifest actions")
	}
	assertPlanFileAction(t, plan.Files, filepath.ToSlash(filepath.Join("..", "GEMINI.md")), "create")
	assertPlanFileAction(t, plan.Files, filepath.ToSlash(filepath.Join("..", "config", "agents", "lore.json")), "create")
	assertPlanFileAction(t, plan.Files, filepath.ToSlash(filepath.Join("..", "config", "mcp_config.json")), "create")
	assertPlanFileAction(t, plan.Files, filepath.ToSlash(filepath.Join("skills", "sdd-apply", "SKILL.md")), "create")
	assertPlanFileAction(t, plan.Files, "lore-install.json", "create")
	for _, action := range plan.Files {
		if strings.HasPrefix(action.RelativePath, filepath.ToSlash(filepath.Join("agents", ""))) || strings.HasPrefix(action.RelativePath, filepath.ToSlash(filepath.Join("extensions", ""))) || action.RelativePath == "settings.json" {
			t.Fatalf("plan leaked Pi-only artifact: %+v", action)
		}
	}
}

func TestPlanAntigravityInstallDefaultCanonicalAssetsMatchProjectedDefinition(t *testing.T) {
	homeDir := t.TempDir()
	now := time.Date(2026, 5, 25, 13, 15, 0, 0, time.UTC)
	assets := agentpack.DefaultOperationalAssets()

	canonicalPlan, err := Service{}.PlanAntigravityInstall(InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		SavedToken:     "secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Target:         TargetAntigravity,
		Now:            now,
	})
	if err != nil {
		t.Fatalf("PlanAntigravityInstall(canonical) error: %v", err)
	}

	projectedPlan, err := Service{}.PlanAntigravityInstall(InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		SavedToken:     "secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Target:         TargetAntigravity,
		Definition:     assets.Definition(),
		Now:            now,
	})
	if err != nil {
		t.Fatalf("PlanAntigravityInstall(projected definition) error: %v", err)
	}

	if !reflect.DeepEqual(canonicalPlan.Files, projectedPlan.Files) {
		t.Fatalf("plan files drifted between canonical assets and projected definition\ncanonical=%+v\nprojected=%+v", canonicalPlan.Files, projectedPlan.Files)
	}
	if !reflect.DeepEqual(canonicalPlan.Layout, projectedPlan.Layout) {
		t.Fatalf("plan layout drifted between canonical assets and projected definition\ncanonical=%+v\nprojected=%+v", canonicalPlan.Layout, projectedPlan.Layout)
	}
}

func TestExecuteAntigravityInstallWritesPromptSkillsAndManifest(t *testing.T) {
	homeDir := t.TempDir()
	now := time.Date(2026, 5, 25, 13, 30, 0, 0, time.UTC)
	service := Service{}

	plan, err := service.PlanAntigravityInstall(InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		SavedToken:     "secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Target:         TargetAntigravity,
		Now:            now,
	})
	if err != nil {
		t.Fatalf("PlanAntigravityInstall error: %v", err)
	}

	result, err := service.ExecuteAntigravityInstall(plan, InstallCommandOptions{})
	if err != nil {
		t.Fatalf("ExecuteAntigravityInstall error: %v", err)
	}
	if result.Target != TargetAntigravity {
		t.Fatalf("result.Target = %q, want %q", result.Target, TargetAntigravity)
	}
	if len(result.Summary.Created) == 0 || len(result.Summary.Failed) != 0 {
		t.Fatalf("summary = %+v, want created files and no failures", result.Summary)
	}
	promptPath := filepath.Join(homeDir, ".gemini", "GEMINI.md")
	promptContent, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("ReadFile(prompt) error: %v", err)
	}
	promptText := string(promptContent)
	if !containsAll(promptText, antigravityPromptStartMarker, antigravityPromptEndMarker, "Lore Runtime") {
		t.Fatalf("prompt content = %q, want managed Antigravity prompt block", promptText)
	}
	if strings.Contains(promptText, "agents/lore-managed") || strings.Contains(promptText, ".pi/agent") {
		t.Fatalf("prompt content leaked Pi semantics: %q", promptText)
	}
	skillPath := filepath.Join(homeDir, ".gemini", "antigravity-cli", "skills", "sdd-apply", "SKILL.md")
	skillContent, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("ReadFile(skill) error: %v", err)
	}
	skillText := string(skillContent)
	if !containsAll(skillText, "~/.gemini/antigravity-cli/skills/sdd-apply/SKILL.md", "~/.gemini/antigravity-cli/skills/_shared/sdd-phase-common.md") {
		t.Fatalf("skill content = %q, want Antigravity skill paths", skillText)
	}
	for _, forbidden := range []string{"~/.pi/agent/skills/", "agents/lore-managed", "managedBy:", "phase:", "skillPolicyMode:"} {
		if strings.Contains(skillText, forbidden) {
			t.Fatalf("skill content = %q, want %q omitted from Antigravity skill output", skillText, forbidden)
		}
	}
	agentProfilePath := filepath.Join(homeDir, ".gemini", "config", "agents", "lore.json")
	agentProfileContent, err := os.ReadFile(agentProfilePath)
	if err != nil {
		t.Fatalf("ReadFile(agent profile) error: %v", err)
	}
	agentProfileText := string(agentProfileContent)
	var agentProfile antigravityAgentProfile
	if err := json.Unmarshal(agentProfileContent, &agentProfile); err != nil {
		t.Fatalf("json.Unmarshal(agent profile) error: %v", err)
	}
	instruction := renderAntigravityAgentSystemInstruction(agentpack.DefaultDefinition())
	if agentProfile.ID != "lore" || agentProfile.Name != "Lore" || !agentProfile.Default {
		t.Fatalf("agent profile = %+v, want lore/Lore/default", agentProfile)
	}
	if agentProfile.Description != "Global Lore orchestrator specialized in SDD workflows and persistent context through Lore MCP" {
		t.Fatalf("agent profile description = %q, want updated English description", agentProfile.Description)
	}
	if agentProfile.SystemInstruction != instruction {
		t.Fatalf("agent profile systemInstruction = %q, want composed agentpack content %q", agentProfile.SystemInstruction, instruction)
	}
	if strings.Contains(agentProfileText, `"tools"`) {
		t.Fatalf("agent profile content = %q, want no tools field", agentProfileText)
	}
	manifestPath := filepath.Join(homeDir, ".gemini", "antigravity-cli", "lore-install.json")
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest(antigravity) error: %v", err)
	}
	if manifest.Target != TargetAntigravity || len(manifest.ManagedAgentOverlays) != 0 {
		t.Fatalf("manifest = %+v, want Antigravity manifest without managed overlays", manifest)
	}
	if err := manifest.ValidateForLayout(result.Layout, managedManifestPaths(manifest), filepath.Join(result.Layout.RootDir, "backups")); err != nil {
		t.Fatalf("ValidateForLayout(antigravity) error: %v", err)
	}
}

func TestExecuteAntigravityInstallMergesMCPConfigAtGeminiConfigPath(t *testing.T) {
	homeDir := t.TempDir()
	now := time.Date(2026, 5, 25, 13, 45, 0, 0, time.UTC)
	mcpPath := filepath.Join(homeDir, ".gemini", "config", "mcp_config.json")
	agentsDir := filepath.Join(homeDir, ".gemini", "config", "agents")
	agentProfilePath := filepath.Join(agentsDir, "lore.json")
	unrelatedAgentPath := filepath.Join(agentsDir, "keep-me.json")
	if err := os.MkdirAll(filepath.Dir(mcpPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(mcp config dir) error: %v", err)
	}
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(agents dir) error: %v", err)
	}
	if err := os.WriteFile(mcpPath, []byte("   \n"), 0o600); err != nil {
		t.Fatalf("WriteFile(empty mcp config) error: %v", err)
	}
	if err := os.WriteFile(agentProfilePath, []byte(`{"id":"legacy"}`), 0o600); err != nil {
		t.Fatalf("WriteFile(existing lore.json) error: %v", err)
	}
	if err := os.WriteFile(unrelatedAgentPath, []byte(`{"id":"keep-me"}`), 0o600); err != nil {
		t.Fatalf("WriteFile(unrelated agent profile) error: %v", err)
	}

	service := Service{}
	plan, err := service.PlanAntigravityInstall(InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		SavedToken:     "secret-token",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Target:         TargetAntigravity,
		Components:     []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
		Now:            now,
	})
	if err != nil {
		t.Fatalf("PlanAntigravityInstall(with MCP) error: %v", err)
	}

	result, err := service.ExecuteAntigravityInstall(plan, InstallCommandOptions{})
	if err != nil {
		t.Fatalf("ExecuteAntigravityInstall(with MCP) error: %v", err)
	}
	if !containsSummaryEntry(result.Summary.Updated, filepath.ToSlash(filepath.Join("..", "config", "mcp_config.json")), "") {
		t.Fatalf("Updated = %v, want managed MCP config update entry", result.Summary.Updated)
	}
	if !containsSummaryEntry(result.Summary.Updated, filepath.ToSlash(filepath.Join("..", "config", "agents", "lore.json")), "") {
		t.Fatalf("Updated = %v, want managed Gemini agent profile update entry", result.Summary.Updated)
	}
	agentProfileContent, err := os.ReadFile(filepath.Join(homeDir, ".gemini", "config", "agents", "lore.json"))
	if err != nil {
		t.Fatalf("ReadFile(agent profile) error: %v", err)
	}
	agentProfileText := string(agentProfileContent)
	var agentProfile antigravityAgentProfile
	if err := json.Unmarshal(agentProfileContent, &agentProfile); err != nil {
		t.Fatalf("json.Unmarshal(agent profile) error: %v", err)
	}
	instruction := renderAntigravityAgentSystemInstruction(agentpack.DefaultDefinition())
	if agentProfile.ID != "lore" || agentProfile.Name != "Lore" || !agentProfile.Default {
		t.Fatalf("agent profile = %+v, want lore/Lore/default", agentProfile)
	}
	if agentProfile.Description != "Global Lore orchestrator specialized in SDD workflows and persistent context through Lore MCP" {
		t.Fatalf("agent profile description = %q, want updated English description", agentProfile.Description)
	}
	if agentProfile.SystemInstruction != instruction {
		t.Fatalf("agent profile systemInstruction = %q, want composed agentpack content %q", agentProfile.SystemInstruction, instruction)
	}
	if strings.Contains(agentProfileText, `"tools"`) {
		t.Fatalf("agent profile = %q, want no tools field", agentProfileText)
	}
	mcpContent, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("ReadFile(mcp config) error: %v", err)
	}
	mcpText := string(mcpContent)
	if !containsAll(mcpText, `"mcpServers"`, `"lore"`, `"serverUrl": "https://lore.example/v1/mcp"`, `"Authorization": "Bearer secret-token"`) {
		t.Fatalf("mcp config = %q, want Lore MCP server written to ~/.gemini/config", mcpText)
	}
	for _, forbidden := range []string{"\"command\"", "--auth-file", "lore_mcp_auth.json"} {
		if strings.Contains(mcpText, forbidden) {
			t.Fatalf("mcp config = %q, want %q omitted", mcpText, forbidden)
		}
	}
	unrelatedAgentContent, err := os.ReadFile(unrelatedAgentPath)
	if err != nil {
		t.Fatalf("ReadFile(unrelated agent profile) error: %v", err)
	}
	if string(unrelatedAgentContent) != `{"id":"keep-me"}` {
		t.Fatalf("unrelated agent profile = %q, want preserved sibling file", string(unrelatedAgentContent))
	}
	manifestPath := filepath.Join(homeDir, ".gemini", "antigravity-cli", "lore-install.json")
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest(antigravity with MCP) error: %v", err)
	}
	if !containsSummaryEntry(managedManifestPaths(manifest), filepath.ToSlash(filepath.Join(".gemini", "config", "mcp_config.json")), "") {
		t.Fatalf("manifest managed paths = %v, want ~/.gemini/config/mcp_config.json", managedManifestPaths(manifest))
	}
	if !containsSummaryEntry(managedManifestPaths(manifest), filepath.ToSlash(filepath.Join(".gemini", "config", "agents", "lore.json")), "") {
		t.Fatalf("manifest managed paths = %v, want ~/.gemini/config/agents/lore.json", managedManifestPaths(manifest))
	}
}

func TestChmodWithBestEffortRejectsUnixFailures(t *testing.T) {
	err := chmodWithBestEffort("linux", "chmod file", func() error {
		return errors.New("chmod unsupported")
	})
	if err == nil || !containsAll(err.Error(), "chmod file", "chmod unsupported") {
		t.Fatalf("chmodWithBestEffort(linux) error = %v, want wrapped chmod failure", err)
	}
}

func TestChmodWithBestEffortAllowsWindowsFallback(t *testing.T) {
	called := false
	err := chmodWithBestEffort("windows", "chmod file", func() error {
		called = true
		return errors.New("chmod unsupported")
	})
	if !called {
		t.Fatal("chmodWithBestEffort(windows) did not call apply func")
	}
	if err != nil {
		t.Fatalf("chmodWithBestEffort(windows) error = %v, want nil", err)
	}
}

func TestInstallPiReportsValidationFailuresAndSummary(t *testing.T) {
	homeDir := t.TempDir()
	// Use default components (no pi-extensions) so manifest has 5 ManagedFiles matching layout.ManagedFiles.
	result, err := Service{}.InstallPi(PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  "/tmp/lore-config",
		LoreCLIVersion: "v1.2.3",
		SavedToken:     "export default function",
		Now:            time.Date(2026, 5, 18, 20, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("InstallPi error: %v", err)
	}
	// Default install has no lore-memory validation (dormant), so no validation failures expected.
	// Settings.json and mcp.json are valid with no token-shaped fixtures.
	if len(result.Summary.Failed) != 0 {
		t.Fatalf("Failed = %v, want no validation failures for default hosted MCP install", result.Summary.Failed)
	}
	// 5 managed files (settings + mcp + 3 skills) + 10 overlays.
	if len(result.Summary.Created) != 15 {
		t.Fatalf("Created = %v, want 15 entries (5 managed files + 10 overlays)", result.Summary.Created)
	}
	if result.Manifest.AuthMode != "cli-request" || result.Manifest.CLIVersion != "v1.2.3" {
		t.Fatalf("manifest = %+v, want persisted cli-request metadata", result.Manifest)
	}
	if got, want := result.Manifest.SchemaVersion, PortableManifestSchemaVersion; got != want {
		t.Fatalf("manifest schema_version = %q, want %q", got, want)
	}
}

func TestPlanPiInstallReportsNoExistingPi(t *testing.T) {
	homeDir := t.TempDir()
	now := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)

	plan, err := Service{}.PlanPiInstall(PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Now:            now,
	})
	if err != nil {
		t.Fatalf("PlanPiInstall error: %v", err)
	}
	if plan.ExistingPi.Exists {
		t.Fatalf("ExistingPi.Exists = true, want false")
	}
	if plan.FullBackup != nil {
		t.Fatalf("FullBackup = %+v, want nil when ~/.pi does not exist", plan.FullBackup)
	}
	if plan.Snapshot == "" {
		t.Fatalf("Snapshot = %q, want non-empty drift token", plan.Snapshot)
	}
}

func TestPlanPiInstallReportsExistingPiAndBackupPathOutsidePiDir(t *testing.T) {
	homeDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(homeDir, ".pi", "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll ~/.pi: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".pi", "nested", "marker.txt"), []byte("existing"), 0o644); err != nil {
		t.Fatalf("WriteFile marker: %v", err)
	}

	plan, err := Service{}.PlanPiInstall(PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Now:            time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("PlanPiInstall error: %v", err)
	}
	if !plan.ExistingPi.Exists {
		t.Fatalf("ExistingPi.Exists = false, want true")
	}
	if plan.FullBackup == nil {
		t.Fatal("FullBackup = nil, want scheduled full backup for existing ~/.pi")
	}
	piRoot := filepath.Join(homeDir, ".pi")
	if plan.FullBackup.BackupPath == piRoot || strings.HasPrefix(plan.FullBackup.BackupPath, piRoot+string(os.PathSeparator)) {
		t.Fatalf("FullBackup.BackupPath = %q, want path outside %q", plan.FullBackup.BackupPath, piRoot)
	}
}

func TestPlanPiInstallDefaultCanonicalAssetsMatchProjectedDefinition(t *testing.T) {
	homeDir := t.TempDir()
	now := time.Date(2026, 5, 25, 14, 0, 0, 0, time.UTC)
	base := PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Now:            now,
	}

	defaultPlan, err := Service{}.PlanPiInstall(base)
	if err != nil {
		t.Fatalf("PlanPiInstall(default) error: %v", err)
	}
	definitionPlan, err := Service{}.PlanPiInstall(PiInstallRequest{
		HomeDir:        base.HomeDir,
		ServerURL:      base.ServerURL,
		LoreBinaryPath: base.LoreBinaryPath,
		LoreConfigDir:  base.LoreConfigDir,
		LoreCLIVersion: base.LoreCLIVersion,
		Definition:     agentpack.DefaultDefinition(),
		Now:            base.Now,
	})
	if err != nil {
		t.Fatalf("PlanPiInstall(projected definition) error: %v", err)
	}
	if !reflect.DeepEqual(defaultPlan.ManagedFileActions, definitionPlan.ManagedFileActions) {
		t.Fatalf("PlanPiInstall(default assets) drifted from projected definition\ndefault=%+v\ndefinition=%+v", defaultPlan.ManagedFileActions, definitionPlan.ManagedFileActions)
	}
	if !reflect.DeepEqual(defaultPlan.ManagedAgentConflicts, definitionPlan.ManagedAgentConflicts) {
		t.Fatalf("PlanPiInstall(default assets) conflicts drifted\ndefault=%+v\ndefinition=%+v", defaultPlan.ManagedAgentConflicts, definitionPlan.ManagedAgentConflicts)
	}
}

func TestPlanPiInstallReportsManagedFileActions(t *testing.T) {
	homeDir := t.TempDir()
	req := PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Now:            time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC),
	}
	layout := ResolvePiLayout(homeDir)
	if err := os.MkdirAll(layout.ExtensionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll extensions: %v", err)
	}
	// Write lore-delegation.ts for legacy cleanup test.
	if err := os.WriteFile(filepath.Join(layout.ExtensionsDir, "lore-delegation.ts"), []byte("legacy-delegation"), 0o600); err != nil {
		t.Fatalf("WriteFile legacy delegation fixture: %v", err)
	}

	plan, err := Service{}.PlanPiInstall(req)
	if err != nil {
		t.Fatalf("PlanPiInstall error: %v", err)
	}

	// Default install: settings.json + mcp.json + 3 extended skills = 5 managed files
	// + 10 managed overlays + 1 legacy delegation cleanup = 16 total actions.
	// lore-memory.ts and lore-footer.ts are NOT included (dormant for default Pi install).
	if got := len(plan.ManagedFileActions); got != 16 {
		t.Fatalf("len(ManagedFileActions) = %d, want 16 (5 managed files + 10 overlays + 1 legacy cleanup — lore-memory dormant for default)", got)
	}
	actions := map[string]ManagedFileAction{}
	for _, action := range plan.ManagedFileActions {
		actions[action.RelativePath] = action
	}
	// lore-memory.ts should NOT be in the plan (dormant for default install).
	if _, ok := actions[filepath.Join("extensions", "lore-memory.ts")]; ok {
		t.Fatal("lore-memory.ts unexpectedly in managed file actions for default install")
	}
	if got := actions[filepath.Join("extensions", "lore-delegation.ts")]; got.Action != "delete" || !strings.HasPrefix(got.BackupPath, plan.ManagedBackupRoot) {
		t.Fatalf("lore-delegation action = %+v, want delete under %s", got, plan.ManagedBackupRoot)
	}
	for _, relativePath := range []string{"settings.json", "mcp.json", filepath.Join("agents", "lore-managed-lore-worker.md"), filepath.Join("agents", "lore-managed-sdd-apply.md")} {
		if got := actions[relativePath].Action; got != "create" {
			t.Fatalf("%s action = %q, want create", relativePath, got)
		}
	}
}

func TestExecutePiInstallDryRunDoesNotMutateFilesystem(t *testing.T) {
	homeDir := t.TempDir()
	req := PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Now:            time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC),
	}

	plan, err := Service{}.PlanPiInstall(req)
	if err != nil {
		t.Fatalf("PlanPiInstall error: %v", err)
	}
	if _, err := (Service{}).ExecutePiInstall(plan, InstallCommandOptions{DryRun: true}); err != nil {
		t.Fatalf("ExecutePiInstall dry-run error: %v", err)
	}

	layout := ResolvePiLayout(homeDir)
	if _, statErr := os.Stat(layout.AgentDir); !os.IsNotExist(statErr) {
		t.Fatalf("AgentDir stat error = %v, want no install writes in dry-run", statErr)
	}
	if _, statErr := os.Stat(layout.ManifestPath); !os.IsNotExist(statErr) {
		t.Fatalf("ManifestPath stat error = %v, want no manifest writes in dry-run", statErr)
	}
}

func TestExecutePiInstallAbortsOnPlanApplyDrift(t *testing.T) {
	homeDir := t.TempDir()
	req := PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Now:            time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC),
	}

	plan, err := Service{}.PlanPiInstall(req)
	if err != nil {
		t.Fatalf("PlanPiInstall error: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(homeDir, ".pi"), 0o755); err != nil {
		t.Fatalf("MkdirAll drift marker: %v", err)
	}

	_, err = Service{}.ExecutePiInstall(plan, InstallCommandOptions{AssumeYes: true})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "drift") {
		t.Fatalf("ExecutePiInstall error = %v, want drift abort", err)
	}

	layout := ResolvePiLayout(homeDir)
	if _, statErr := os.Stat(layout.ManifestPath); !os.IsNotExist(statErr) {
		t.Fatalf("ManifestPath stat error = %v, want no writes on drift", statErr)
	}
}

func TestExecutePiInstallRerunDoesNotDriftWhenManagedOverlaysAreUnchanged(t *testing.T) {
	homeDir := t.TempDir()
	req := PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Now:            time.Date(2026, 5, 19, 0, 0, 1, 0, time.UTC),
	}

	if _, err := (Service{}).InstallPi(req); err != nil {
		t.Fatalf("seed InstallPi error: %v", err)
	}

	plan, err := (Service{}).PlanPiInstall(req)
	if err != nil {
		t.Fatalf("PlanPiInstall rerun error: %v", err)
	}
	if got := len(plan.ManagedFileActions); got != 15 {
		t.Fatalf("len(ManagedFileActions) = %d, want 15 (5 base files + 10 overlays — lore-memory dormant for default install)", got)
	}
	for _, action := range plan.ManagedFileActions {
		if action.Action != "unchanged" {
			t.Fatalf("planned action for %s = %q, want unchanged on converged rerun", action.RelativePath, action.Action)
		}
	}

	sharedPlan := plan.InstallPlan()
	if sharedPlan.Layout.Target != TargetPi || sharedPlan.Layout.ManifestPath != plan.Layout.ManifestPath {
		t.Fatalf("shared plan layout = %+v, want Pi layout bridge", sharedPlan.Layout)
	}
	if sharedPlan.Request.Target != TargetPi || sharedPlan.Request.HomeDir != req.HomeDir {
		t.Fatalf("shared plan request = %+v, want Pi request bridge", sharedPlan.Request)
	}
	if got := len(sharedPlan.Files); got != len(plan.ManagedFileActions) {
		t.Fatalf("len(shared plan files) = %d, want %d", got, len(plan.ManagedFileActions))
	}
	if got := sharedPlan.Files[0].Action; got != plan.ManagedFileActions[0].Action {
		t.Fatalf("shared plan first action = %q, want %q", got, plan.ManagedFileActions[0].Action)
	}

	result, err := (Service{}).ExecutePiInstall(plan, InstallCommandOptions{AssumeYes: true})
	if err != nil {
		t.Fatalf("ExecutePiInstall rerun error: %v", err)
	}
	if len(result.Summary.Created) != 0 || len(result.Summary.Updated) != 0 || len(result.Summary.Deleted) != 0 {
		t.Fatalf("summary = %+v, want no create/update/delete actions on converged rerun", result.Summary)
	}
	if got := len(result.Summary.Unchanged); got != 15 {
		t.Fatalf("len(Unchanged) = %d, want 15 (5 base files + 10 overlays — lore-memory dormant for default install)", got)
	}
	if containsSummaryEntry(result.Summary.Unchanged, filepath.Join("themes", "alferio.json")) {
		t.Fatalf("Unchanged = %v, want theme bootstrap excluded from managed plan/summary accounting", result.Summary.Unchanged)
	}
	sharedResult := result.InstallResult()
	if sharedResult.Target != TargetPi || sharedResult.Layout.Target != TargetPi {
		t.Fatalf("shared result = %+v, want Pi shared install result bridge", sharedResult)
	}
}

func TestInstallPiRerunConvergesWithoutDelegationRegeneration(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolvePiLayout(homeDir)
	if err := os.MkdirAll(layout.ExtensionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll extensions: %v", err)
	}
	if err := os.WriteFile(filepath.Join(layout.ExtensionsDir, "lore-delegation.ts"), []byte("legacy delegation"), 0o600); err != nil {
		t.Fatalf("WriteFile lore-delegation.ts: %v", err)
	}

	req := PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Now:            time.Date(2026, 5, 19, 3, 0, 0, 0, time.UTC),
	}
	first, err := Service{}.InstallPi(req)
	if err != nil {
		t.Fatalf("first InstallPi error: %v", err)
	}
	if !containsSummaryEntry(first.Summary.Deleted, legacyPiDelegationRelativePath) {
		t.Fatalf("first Deleted = %v, want legacy delegation cleanup", first.Summary.Deleted)
	}

	second, err := Service{}.InstallPi(req)
	if err != nil {
		t.Fatalf("second InstallPi error: %v", err)
	}
	if len(second.Summary.Deleted) != 0 {
		t.Fatalf("second Deleted = %v, want no repeated cleanup once converged", second.Summary.Deleted)
	}
	if _, err := os.Stat(filepath.Join(layout.ExtensionsDir, "lore-delegation.ts")); !os.IsNotExist(err) {
		t.Fatalf("lore-delegation.ts stat error = %v, want absent after rerun", err)
	}
	manifestBytes, err := os.ReadFile(layout.ManifestPath)
	if err != nil {
		t.Fatalf("ReadFile manifest: %v", err)
	}
	if strings.Contains(string(manifestBytes), "lore-delegation.ts") {
		t.Fatalf("manifest unexpectedly references legacy delegation: %s", manifestBytes)
	}
}

func TestInstallPiRerunIgnoresTamperedManifestForRuntimePackages(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolvePiLayout(homeDir)
	hostedPackage := PiHostedMCPPackageSource()
	req := PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Now:            time.Date(2026, 5, 19, 3, 30, 0, 0, time.UTC),
	}
	if _, err := (Service{}).InstallPi(req); err != nil {
		t.Fatalf("first InstallPi error: %v", err)
	}

	tamperedManifest := `{
	  "schema_version": "portable-v2",
	  "target": "pi",
	  "auth_mode": "cli-request",
	  "server_url": "https://lore.example",
	  "lore_binary_path": "/usr/local/bin/lore",
	  "lore_config_dir": "` + filepath.ToSlash(filepath.Join(homeDir, ".lore")) + `",
	  "components": ["core-pack", "pi-extensions"],
	  "managed_files": [
	    {"path": "` + filepath.ToSlash(layout.ManagedFiles[0]) + `", "component": "pi-extensions", "merge_mode": "replace", "content_hash": "memory"},
	    {"path": "` + filepath.ToSlash(layout.ManagedFiles[1]) + `", "component": "pi-extensions", "merge_mode": "replace", "content_hash": "footer"},
	    {"path": "` + filepath.ToSlash(layout.ManagedFiles[2]) + `", "component": "core-pack", "merge_mode": "additive-json", "content_hash": "settings"},
	    {"path": "` + filepath.ToSlash(filepath.Join(layout.ExtensionsDir, "evil-runtime.ts")) + `", "component": "pi-extensions", "merge_mode": "replace", "content_hash": "evil"}
	  ],
	  "packages": ["git:github.com/example/evil-runtime", "` + hostedPackage + `"],
	  "runtime_package": "git:github.com/example/evil-runtime",
	  "backup_root": "` + filepath.ToSlash(filepath.Join(layout.AgentDir, "backups", "tampered")) + `",
	  "installed_at": "2026-05-19T03:30:00Z",
	  "lore_cli_version": "v1.2.3"
	}
`
	if err := os.WriteFile(layout.ManifestPath, []byte(tamperedManifest), 0o600); err != nil {
		t.Fatalf("WriteFile tampered manifest: %v", err)
	}

	second, err := Service{}.InstallPi(req)
	if err != nil {
		t.Fatalf("second InstallPi error: %v", err)
	}
	if len(second.Summary.Created) != 0 || len(second.Summary.Updated) != 0 || len(second.Summary.Deleted) != 0 {
		t.Fatalf("second summary = %+v, want no runtime file churn from manifest tampering", second.Summary)
	}

	settingsBytes, err := os.ReadFile(layout.SettingsPath)
	if err != nil {
		t.Fatalf("ReadFile settings.json: %v", err)
	}
	var settings struct {
		Packages []string `json:"packages"`
	}
	if err := json.Unmarshal(settingsBytes, &settings); err != nil {
		t.Fatalf("Unmarshal settings.json: %v", err)
	}
	if got, want := settings.Packages, []string{hostedPackage}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("settings packages = %v, want %v after manifest tamper rerun", got, want)
	}

	manifestBytes, err := os.ReadFile(layout.ManifestPath)
	if err != nil {
		t.Fatalf("ReadFile manifest after rerun: %v", err)
	}
	if strings.Contains(string(manifestBytes), "git:github.com/example/evil-runtime") || strings.Contains(string(manifestBytes), "evil-runtime.ts") || strings.Contains(string(manifestBytes), `"packages"`) {
		t.Fatalf("manifest retained tampered runtime hints: %s", manifestBytes)
	}
}

func TestInstallPiReinstallDeletesRestoredLegacyDelegationAgain(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolvePiLayout(homeDir)
	if err := os.MkdirAll(layout.ExtensionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll extensions: %v", err)
	}
	if err := os.WriteFile(filepath.Join(layout.ExtensionsDir, "lore-delegation.ts"), []byte("legacy delegation"), 0o600); err != nil {
		t.Fatalf("WriteFile lore-delegation.ts: %v", err)
	}

	req := PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Now:            time.Date(2026, 5, 19, 4, 0, 0, 0, time.UTC),
	}
	first, err := Service{}.InstallPi(req)
	if err != nil {
		t.Fatalf("first InstallPi error: %v", err)
	}
	backupPath := filepath.Join(first.Manifest.BackupRoot, legacyPiDelegationRelativePath)
	backupContent, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("ReadFile legacy delegation backup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(layout.ExtensionsDir, "lore-delegation.ts"), backupContent, 0o600); err != nil {
		t.Fatalf("restore lore-delegation.ts: %v", err)
	}

	second, err := Service{}.InstallPi(req)
	if err != nil {
		t.Fatalf("second InstallPi error: %v", err)
	}
	if !containsSummaryEntry(second.Summary.Deleted, legacyPiDelegationRelativePath) {
		t.Fatalf("second Deleted = %v, want restored legacy cleanup", second.Summary.Deleted)
	}
	if _, err := os.Stat(filepath.Join(layout.ExtensionsDir, "lore-delegation.ts")); !os.IsNotExist(err) {
		t.Fatalf("lore-delegation.ts stat error = %v, want absent after reinstall cleanup", err)
	}
}

func TestInstallPiManagedOverlayConflictsAndContractCompatibility(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolvePiLayout(homeDir)
	if err := os.MkdirAll(layout.ManagedAgentsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll managed agents: %v", err)
	}
	collisionPath := filepath.Join(layout.ManagedAgentsDir, "lore-managed-lore-worker.md")
	if err := os.WriteFile(collisionPath, []byte("user owned override"), 0o600); err != nil {
		t.Fatalf("WriteFile collision: %v", err)
	}

	req := PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Now:            time.Date(2026, 5, 19, 5, 0, 0, 0, time.UTC),
	}
	result, err := Service{}.InstallPi(req)
	if err != nil {
		t.Fatalf("InstallPi error: %v", err)
	}
	if !containsSummaryEntry(result.Summary.Conflicted, "lore-managed-lore-worker.md") {
		t.Fatalf("Conflicted = %v, want preserved user-owned collision", result.Summary.Conflicted)
	}
	if len(result.Manifest.ManagedAgentOverlays) != len(agentpack.DefaultDefinition().ManagedAgents)-1 {
		t.Fatalf("ManagedAgentOverlays = %+v, want all non-conflicting managed overlays recorded", result.Manifest.ManagedAgentOverlays)
	}
	if containsSummaryEntry(result.Summary.Updated, "lore-managed-lore-worker.md") {
		t.Fatalf("Updated = %v, want user-owned collision skipped", result.Summary.Updated)
	}

	if _, err := (Service{}).InstallPi(req.WithRuntimeContract(RuntimeContract{Version: 99})); err == nil || !containsAll(err.Error(), "contract", "agentResolution") {
		t.Fatalf("InstallPi incompatible contract error = %v, want fail-fast compatibility rejection", err)
	}
}

func TestInstallPiManagedOverlayRerunDeletesStaleTrackedOverlay(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolvePiLayout(homeDir)
	req := PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Now:            time.Date(2026, 5, 19, 6, 0, 0, 0, time.UTC),
	}
	first, err := Service{}.InstallPi(req)
	if err != nil {
		t.Fatalf("first InstallPi error: %v", err)
	}

	stalePath := filepath.Join(layout.ManagedAgentsDir, "lore-managed-obsolete.md")
	if err := os.WriteFile(stalePath, []byte("stale managed overlay"), 0o600); err != nil {
		t.Fatalf("WriteFile stale overlay: %v", err)
	}
	manifest := first.Manifest
	manifest.ManagedAgentOverlays = append(manifest.ManagedAgentOverlays, ManagedAgentOverlayRecord{
		AgentName:   "obsolete",
		Path:        stalePath,
		ContentHash: contentHash([]byte("stale managed overlay")),
	})
	manifestBytes, err := marshalManifest(manifest)
	if err != nil {
		t.Fatalf("marshalManifest() error = %v", err)
	}
	if err := os.WriteFile(layout.ManifestPath, manifestBytes, 0o600); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	second, err := Service{}.InstallPi(req)
	if err != nil {
		t.Fatalf("second InstallPi error: %v", err)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("stale overlay stat error = %v, want removed stale managed overlay", err)
	}
	if !containsSummaryEntry(second.Summary.Deleted, "lore-managed-obsolete.md") {
		t.Fatalf("Deleted = %v, want stale managed overlay cleanup", second.Summary.Deleted)
	}
	for _, overlay := range second.Manifest.ManagedAgentOverlays {
		if overlay.AgentName == "obsolete" {
			t.Fatalf("ManagedAgentOverlays = %+v, want stale overlay removed from manifest", second.Manifest.ManagedAgentOverlays)
		}
	}
}

func assertPlanFileAction(t *testing.T, actions []PlanFileAction, relativePath, wantAction string) {
	t.Helper()
	for _, action := range actions {
		if filepath.ToSlash(action.RelativePath) != filepath.ToSlash(relativePath) {
			continue
		}
		if action.Action != wantAction {
			t.Fatalf("plan action for %s = %+v, want action %q", relativePath, action, wantAction)
		}
		return
	}
	t.Fatalf("plan action for %s missing from %+v", relativePath, actions)
}

func managedManifestPaths(manifest Manifest) []string {
	paths := make([]string, 0, len(manifest.ManagedFiles))
	for _, file := range manifest.ManagedFiles {
		paths = append(paths, file.Path)
	}
	return paths
}

func containsSummaryEntry(entries []string, wants ...string) bool {
	return containsAny(entries, wants...)
}

func containsAny(entries []string, wants ...string) bool {
	for _, entry := range entries {
		if containsAll(entry, wants...) {
			return true
		}
	}
	return false
}

// fakeAgentConfigStore implements AgentConfigStore for testing.
type fakeAgentConfigStore struct {
	path string
	cfg  agentconfig.Config
	err  error
}

func (f *fakeAgentConfigStore) Path() (string, error) {
	return f.path, nil
}

func (f *fakeAgentConfigStore) Load() (agentconfig.Config, error) {
	return f.cfg, f.err
}

func (f *fakeAgentConfigStore) EnsureDefault() (agentconfig.Config, bool, error) {
	return f.cfg, false, f.err
}

func TestCheckAgentConfigValid(t *testing.T) {
	cfg := agentconfig.DefaultConfig()
	fake := &fakeAgentConfigStore{path: "/fake/lore/agent-config.json", cfg: cfg}
	svc := Service{AgentConfigStore: fake}
	var result PreflightResult
	svc.checkAgentConfig(&result)

	if !result.AgentConfigValid {
		t.Error("AgentConfigValid should be true for valid config")
	}
	if result.AgentConfigPath != "/fake/lore/agent-config.json" {
		t.Errorf("AgentConfigPath = %q, want /fake/lore/agent-config.json", result.AgentConfigPath)
	}

	// Should have an OK check with path, schema_version, and agent count.
	found := false
	for _, check := range result.Checks {
		if check.Name == "agent-config" {
			found = true
			if check.Status != output.StatusOK {
				t.Errorf("agent-config check status = %v, want OK", check.Status)
			}
			if !strings.Contains(check.Detail, "agent-config.json") {
				t.Errorf("agent-config detail = %q, should contain path", check.Detail)
			}
			if !strings.Contains(check.Detail, "schema_version=1") {
				t.Errorf("agent-config detail = %q, should contain schema_version", check.Detail)
			}
			if !strings.Contains(check.Detail, "sdd_agents=9") {
				t.Errorf("agent-config detail = %q, should contain sdd_agents=9", check.Detail)
			}
			break
		}
	}
	if !found {
		t.Error("agent-config check should be present in Checks")
	}
}

func TestCheckAgentConfigNotFound(t *testing.T) {
	fake := &fakeAgentConfigStore{path: "/fake/lore/agent-config.json", cfg: agentconfig.Config{}, err: agentconfig.ErrNotFound}
	svc := Service{AgentConfigStore: fake}
	var result PreflightResult
	svc.checkAgentConfig(&result)

	if result.AgentConfigValid {
		t.Error("AgentConfigValid should be false when not found")
	}

	found := false
	for _, check := range result.Checks {
		if check.Name == "agent-config" {
			found = true
			if check.Status != output.StatusWarn {
				t.Errorf("agent-config not-found status = %v, want Warn", check.Status)
			}
			if !strings.Contains(check.Detail, "not found") {
				t.Errorf("agent-config detail = %q, should say not found", check.Detail)
			}
			break
		}
	}
	if !found {
		t.Error("agent-config check should be present when not found")
	}
}

func TestCheckAgentConfigInvalid(t *testing.T) {
	// Config exists but has wrong schema version.
	fake := &fakeAgentConfigStore{path: "/fake/lore/agent-config.json", cfg: agentconfig.Config{SchemaVersion: 99}}
	svc := Service{AgentConfigStore: fake}
	var result PreflightResult
	svc.checkAgentConfig(&result)

	if result.AgentConfigValid {
		t.Error("AgentConfigValid should be false for invalid config")
	}

	found := false
	for _, check := range result.Checks {
		if check.Name == "agent-config" {
			found = true
			if check.Status != output.StatusFail {
				t.Errorf("agent-config invalid status = %v, want Fail", check.Status)
			}
			if !strings.Contains(check.Detail, "validation failed") {
				t.Errorf("agent-config detail = %q, should say validation failed", check.Detail)
			}
			break
		}
	}
	if !found {
		t.Error("agent-config check should be present for invalid config")
	}
}

func TestCheckAgentConfigNilStoreSkipped(t *testing.T) {
	svc := Service{AgentConfigStore: nil}
	var result PreflightResult
	svc.checkAgentConfig(&result)

	// Should not add any agent-config check when store is nil.
	for _, check := range result.Checks {
		if check.Name == "agent-config" {
			t.Error("agent-config check should not be present when AgentConfigStore is nil")
		}
	}
}
