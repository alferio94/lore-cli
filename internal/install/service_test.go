package install

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if got := targets[0].Description; !containsAll(got, "Pi-native Lore extensions", "MCP") {
		t.Fatalf("targets[0].Description = %q, want Pi-native extension guardrails", got)
	}
	for _, want := range []TargetID{TargetClaudeCode, TargetOpenCode, TargetCodex, TargetAntigravity} {
		target := findTarget(targets, want)
		if target.Available {
			t.Fatalf("target %s unexpectedly available", want)
		}
		if got := target.Availability; got != "Coming soon" {
			t.Fatalf("target %s availability = %q, want Coming soon", want, got)
		}
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

	if _, err := ResolveInstallTarget(TargetClaudeCode); err == nil || !containsAll(err.Error(), string(TargetClaudeCode), "Coming soon", "Pi-native Lore extensions") {
		t.Fatalf("ResolveInstallTarget(claude-code) error = %v, want roadmap guardrail", err)
	}
	if _, err := ResolveInstallTarget(TargetID("unknown-target")); err == nil || !containsAll(err.Error(), "unknown target") {
		t.Fatalf("ResolveInstallTarget(unknown-target) error = %v, want unknown target rejection", err)
	}
}

func TestFormatTargetSelectionExplainsPiNativePathAndMCPDeferral(t *testing.T) {
	formatted := FormatTargetSelection(DefaultTargets())
	for _, want := range []string{"Choose an install target:", "Pi — Recommended", "Pi-native Lore extensions", "Coming soon", "Only Pi is selectable in this slice.", "Pi MCP remains explicitly disabled by default."} {
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
	if len(layout.ManagedFiles) != 3 {
		t.Fatalf("ManagedFiles = %v, want 3 managed paths", layout.ManagedFiles)
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
func (*stubClient) MCPJSONRPC(context.Context, string, string, json.RawMessage) (httpclient.RequestJSONResult, error) {
	panic("unexpected MCPJSONRPC call")
}
func (*stubClient) MCPCall(context.Context, string, string, json.RawMessage) (httpclient.RequestJSONResult, error) {
	panic("unexpected MCPCall call")
}

func TestInstallPiWritesManagedFilesBackupsAndManifest(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolvePiLayout(homeDir)
	if err := os.MkdirAll(layout.ExtensionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll extensions: %v", err)
	}
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
	if len(result.Summary.Created) != 11 {
		t.Fatalf("Created = %v, want 1 extension file plus 10 managed overlays", result.Summary.Created)
	}
	if len(result.Summary.Updated) != 2 {
		t.Fatalf("Updated = %v, want settings + lore-memory", result.Summary.Updated)
	}
	if len(result.Summary.Deleted) != 1 || result.Summary.Deleted[0] != filepath.Join("extensions", "lore-delegation.ts") {
		t.Fatalf("Deleted = %v, want legacy delegation cleanup", result.Summary.Deleted)
	}
	if len(result.Summary.BackedUp) != 3 {
		t.Fatalf("BackedUp = %v, want 3 backups including deleted legacy delegation", result.Summary.BackedUp)
	}
	if result.Manifest.AuthMode != "cli-request" || result.Manifest.ServerURL != "https://lore.example" {
		t.Fatalf("Manifest = %+v, want cli-request manifest with server URL", result.Manifest)
	}
	if got, want := result.Manifest.SchemaVersion, PortableManifestSchemaVersion; got != want {
		t.Fatalf("Manifest.SchemaVersion = %q, want %q", got, want)
	}
	if result.Manifest.BackupRoot == "" || len(result.Manifest.ManagedFiles) != 3 || len(result.Manifest.ManagedAgentOverlays) != 10 {
		t.Fatalf("Manifest = %+v, want backup root, managed files, and managed overlays", result.Manifest)
	}
	if got := result.Manifest.Components; !equalComponentIDs(got, []ComponentID{ComponentCorePack, ComponentPiExtensions}) {
		t.Fatalf("Manifest.Components = %v, want core-pack + pi-extensions", got)
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

	memoryPath := filepath.Join(layout.ExtensionsDir, "lore-memory.ts")
	memoryContent, err := os.ReadFile(memoryPath)
	if err != nil {
		t.Fatalf("ReadFile lore-memory.ts: %v", err)
	}
	if got := string(memoryContent); !containsAll(got,
		"\"api\", \"request\"",
		"\"api\", \"mcp-call\"",
		"lore_project_context",
		"https://lore.example",
		"export default function",
		"ExtensionAPI",
		"Text",
		"renderCall(",
		"renderResult(",
		"text: formatContent(payload.data)",
		"pi.registerTool",
		"name: \"lore_search\"",
		"name: \"lore_save\"",
		"name: \"lore_get_observation\"",
		"name: \"lore_context\"",
		"name: \"lore_project_list\"",
		"name: \"lore_project_create\"",
		"name: \"lore_project_get\"",
		"name: \"lore_skill_save\"",
		"name: \"lore_skill_list\"",
		"name: \"lore_skill_get\"",
		"runBroker(\"GET\", path",
		"/v1/memories",
		"/v1/projects",
		"runBroker(\"GET\", \"/v1/projects\", undefined",
		"runBroker(\"GET\", `/v1/projects/${encodeURIComponent(id)}`, undefined",
		"runBroker(\"GET\", `/v1/memories/${encodeURIComponent(id)}`, undefined",
		"/v1/skills",
		"runBroker(\"GET\", \"/v1/skills\", undefined",
		"runBroker(\"GET\", `/v1/skills/${encodeURIComponent(name)}`, undefined") {
		t.Fatalf("lore-memory.ts missing broker markers/tool names: %q", got)
	} else if strings.Contains(got, "secret-token") {
		t.Fatalf("lore-memory.ts leaked token: %q", got)
	} else {
		for _, legacy := range []string{"/v1/search", "/v1/observations", "/v1/context", "/v1/timeline", "/v1/stats", "/v1/sessions"} {
			if strings.Contains(got, legacy) {
				t.Fatalf("lore-memory.ts contains legacy memory route %q: %q", legacy, got)
			}
		}
	}

	if _, err := os.Stat(filepath.Join(layout.ExtensionsDir, "lore-delegation.ts")); !os.IsNotExist(err) {
		t.Fatalf("lore-delegation.ts stat error = %v, want file removed after cleanup", err)
	}
	delegationBackup, err := os.ReadFile(filepath.Join(result.Manifest.BackupRoot, "extensions", "lore-delegation.ts"))
	if err != nil {
		t.Fatalf("ReadFile backup lore-delegation.ts: %v", err)
	}
	if got := string(delegationBackup); got != "legacy delegation" {
		t.Fatalf("delegation backup content = %q, want original content", got)
	}

	footerContent, err := os.ReadFile(filepath.Join(layout.ExtensionsDir, "lore-footer.ts"))
	if err != nil {
		t.Fatalf("ReadFile lore-footer.ts: %v", err)
	}
	if got := string(footerContent); !containsAll(got,
		"export default function",
		"ExtensionAPI",
		"ctx.ui.setFooter",
		"getContextUsage",
		"getExtensionStatuses") {
		t.Fatalf("lore-footer.ts missing extension markers: %q", got)
	}

	settingsContent, err := os.ReadFile(layout.SettingsPath)
	if err != nil {
		t.Fatalf("ReadFile settings.json: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(settingsContent, &settings); err != nil {
		t.Fatalf("Unmarshal settings.json: %v", err)
	}
	if got := settings["theme"]; got != "night" {
		t.Fatalf("settings theme = %v, want preserved existing key", got)
	}
	alferioTheme, err := os.ReadFile(layout.AlferioThemePath)
	if err != nil {
		t.Fatalf("ReadFile alferio theme: %v", err)
	}
	if got := string(alferioTheme); !containsAll(got, `"name": "alferio"`, `"accent"`) {
		t.Fatalf("alferio theme = %q, want bootstrap theme content", got)
	}
	loreSettings, ok := settings["lore"].(map[string]any)
	if !ok {
		t.Fatalf("settings lore block missing: %v", settings)
	}
	if got := loreSettings["auth_mode"]; got != "cli-request" {
		t.Fatalf("settings lore.auth_mode = %v, want cli-request", got)
	}
	agentPack, ok := loreSettings["agent_pack"].(map[string]any)
	if !ok {
		t.Fatalf("settings lore.agent_pack missing: %v", loreSettings)
	}
	if got := agentPack["persona_name"]; got != "Lore" {
		t.Fatalf("settings lore.agent_pack.persona_name = %v, want Lore", got)
	}
	packages, ok := settings["packages"].([]any)
	if !ok {
		t.Fatalf("settings packages missing: %v", settings)
	}
	if len(packages) != 1 || packages[0] != piRemoteSubagentsPackage {
		t.Fatalf("settings packages = %v, want [%q]", packages, piRemoteSubagentsPackage)
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

	backupContent, err := os.ReadFile(filepath.Join(result.Manifest.BackupRoot, "extensions", "lore-memory.ts"))
	if err != nil {
		t.Fatalf("ReadFile backup lore-memory.ts: %v", err)
	}
	if got := string(backupContent); got != "legacy token=secret-token" {
		t.Fatalf("backup content = %q, want original content", got)
	}
}

func TestMergeJSONAdditivePackagesPreservesOrderAndIdempotence(t *testing.T) {
	merged, err := mergeJSONAdditive(
		[]byte(`{"packages":["pkg-a","pkg-b"],"lore":{"existing":true},"theme":"night"}`),
		[]byte(`{"packages":["pkg-b","`+piRemoteSubagentsPackage+`"],"lore":{"auth_mode":"cli-request"},"theme":"alferio"}`),
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
	if got, want := packages, []any{"pkg-a", "pkg-b", piRemoteSubagentsPackage}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("packages = %v, want %v", got, want)
	}
	loreSettings, ok := settings["lore"].(map[string]any)
	if !ok || loreSettings["existing"] != true || loreSettings["auth_mode"] != "cli-request" {
		t.Fatalf("lore settings = %v, want merged lore config", settings["lore"])
	}
	if settings["theme"] != "night" {
		t.Fatalf("theme = %v, want preserved existing theme", settings["theme"])
	}

	rerun, err := mergeJSONAdditive(merged, []byte(`{"packages":["`+piRemoteSubagentsPackage+`"]}`))
	if err != nil {
		t.Fatalf("mergeJSONAdditive rerun error = %v, want nil", err)
	}
	if string(rerun) != string(merged) {
		t.Fatalf("rerun settings = %s, want idempotent merge matching first result %s", rerun, merged)
	}
}

func TestMergeJSONAdditivePackagesPreservesUserPackageObjects(t *testing.T) {
	merged, err := mergeJSONAdditive(
		[]byte(`{"packages":[{"url":"git:github.com/example/custom","label":"custom"},"pkg-a"]}`),
		[]byte(`{"packages":["pkg-a","`+piRemoteSubagentsPackage+`"]}`),
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
	if packages[1] != "pkg-a" || packages[2] != piRemoteSubagentsPackage {
		t.Fatalf("packages = %v, want preserved order with idempotent lore package append", packages)
	}

	rerun, err := mergeJSONAdditive(merged, []byte(`{"packages":["`+piRemoteSubagentsPackage+`"]}`))
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
	if err := os.WriteFile(layout.SettingsPath, []byte(`{"theme":"night","packages":["`+piRemoteSubagentsPackage+`"]}
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
	findings := validateManagedContents(map[string][]byte{
		"extensions/lore-memory.ts": []byte(`
const loreServerURL = "https://lore.example";
// broker args include "api", "request"
// MCP broker args include "api", "mcp-call" and lore_project_context
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
		"settings.json":             []byte(`{"packages":["` + piRemoteSubagentsPackage + `"],"lore":{"server_url":"https://lore.example"}}`),
	}, PiInstallRequest{ServerURL: "https://lore.example", SavedToken: "secret-token"})
	if len(findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1", len(findings))
	}
	if got := findings[0]; !containsAll(got, "saved auth material", "extensions/lore-memory.ts") || strings.Contains(got, "secret-token") {
		t.Fatalf("finding = %q, want secret-safe token validation detail", got)
	}
}

func TestValidateManagedContentsRejectsLegacyMemoryRoutesWithoutRequiringDelegationSessions(t *testing.T) {
	validMemory := []byte(`
const loreServerURL = "https://lore.example";
// broker args include "api", "request"
// MCP broker args include "api", "mcp-call" and lore_project_context
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
		"settings.json":             []byte(`{"packages":["` + piRemoteSubagentsPackage + `"],"lore":{"server_url":"https://lore.example"}}`),
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
	files := []renderedPiFile{
		{relativePath: managedPiExtensionRelativePaths[0], absolutePath: filepath.Join(layout.ExtensionsDir, "lore-memory.ts"), content: []byte("lore api request without factory")},
		{relativePath: managedPiExtensionRelativePaths[1], absolutePath: filepath.Join(layout.ExtensionsDir, "lore-footer.ts"), content: []byte("export default function (pi: ExtensionAPI) { ctx.ui.setFooter(() => ({ render() { return []; } })); } getContextUsage getExtensionStatuses")},
		{relativePath: "settings.json", absolutePath: layout.SettingsPath, content: []byte(`{"packages":["` + piRemoteSubagentsPackage + `"]}`), mergeMode: MergeModeAdditiveJSON},
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
	layout := ResolvePiLayout(homeDir)

	original := append([]string(nil), managedPiExtensionRelativePaths...)
	managedPiExtensionRelativePaths = append(managedPiExtensionRelativePaths, filepath.Join("extensions", "unexpected-extra.ts"))
	defer func() {
		managedPiExtensionRelativePaths = original
	}()

	_, err := Service{}.InstallPi(PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  "/tmp/lore-config",
		LoreCLIVersion: "v1.2.3",
		Now:            time.Date(2026, 5, 18, 20, 30, 0, 0, time.UTC),
	})
	if err == nil || !containsAll(err.Error(), "validate rendered Pi assets", "extensions/unexpected-extra.ts", "missing") {
		t.Fatalf("InstallPi error = %v, want preflight rendered-asset rejection", err)
	}
	if _, statErr := os.Stat(layout.AgentDir); !os.IsNotExist(statErr) {
		t.Fatalf("agent dir stat error = %v, want no writes when preflight fails", statErr)
	}
	if _, statErr := os.Stat(layout.ManifestPath); !os.IsNotExist(statErr) {
		t.Fatalf("manifest stat error = %v, want no manifest on preflight failure", statErr)
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
	if len(manifest.ManagedFiles) != 3 {
		t.Fatalf("len(ManagedFiles) = %d, want 3 without legacy delegation", len(manifest.ManagedFiles))
	}
	for _, managed := range manifest.ManagedFiles {
		if strings.Contains(managed.Path, "lore-delegation.ts") {
			t.Fatalf("legacy delegation path unexpectedly preserved in upgraded manifest: %+v", managed)
		}
	}
}

func TestPlanPiInstallRejectsUnsupportedExplicitComponent(t *testing.T) {
	homeDir := t.TempDir()
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
	if err == nil || !containsAll(err.Error(), "component", string(ComponentLoreServerMCP), string(TargetPi)) {
		t.Fatalf("Validate error = %v, want managed_files mismatch", err)
	}
}

func TestPlanPiInstallRejectsNonPiTargetEnablement(t *testing.T) {
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
	if err == nil || !containsAll(err.Error(), string(TargetClaudeCode), "not available yet", "Coming soon") {
		t.Fatalf("PlanPiInstall error = %v, want non-Pi target guardrail", err)
	}
}

func TestInstallPiReportsValidationFailuresAndSummary(t *testing.T) {
	homeDir := t.TempDir()
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
	if len(result.Summary.Failed) != 2 {
		t.Fatalf("Failed = %v, want 2 validation failures for managed extensions containing the token-shaped fixture", result.Summary.Failed)
	}
	for _, want := range []string{"extensions/lore-memory.ts", "extensions/lore-footer.ts"} {
		if !containsSummaryEntry(result.Summary.Failed, want, "contains saved auth material") {
			t.Fatalf("Failed = %v, want secret-safe validation entry for %s", result.Summary.Failed, want)
		}
	}
	for _, entry := range result.Summary.Failed {
		if strings.Contains(entry, "export default function") {
			t.Fatalf("Failed = %v, want no raw token echo", result.Summary.Failed)
		}
	}
	if len(result.Summary.Created) != 13 || len(result.Summary.Updated) != 0 || len(result.Summary.Unchanged) != 0 {
		t.Fatalf("summary = %+v, want 3 managed files plus 10 overlays and no updates/unchanged entries", result.Summary)
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
	rendered, err := renderPiFiles(layout, req)
	if err != nil {
		t.Fatalf("renderPiFiles error: %v", err)
	}
	if err := os.MkdirAll(layout.ExtensionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll extensions: %v", err)
	}
	for _, file := range rendered {
		switch file.relativePath {
		case filepath.Join("extensions", "lore-memory.ts"):
			if err := os.WriteFile(file.absolutePath, file.content, 0o600); err != nil {
				t.Fatalf("WriteFile unchanged fixture: %v", err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(layout.ExtensionsDir, "lore-delegation.ts"), []byte("legacy-delegation"), 0o600); err != nil {
		t.Fatalf("WriteFile legacy delegation fixture: %v", err)
	}

	plan, err := Service{}.PlanPiInstall(req)
	if err != nil {
		t.Fatalf("PlanPiInstall error: %v", err)
	}

	if got := len(plan.ManagedFileActions); got != 14 {
		t.Fatalf("len(ManagedFileActions) = %d, want 3 managed files + 10 overlays + legacy cleanup", got)
	}
	actions := map[string]ManagedFileAction{}
	for _, action := range plan.ManagedFileActions {
		actions[action.RelativePath] = action
	}
	if got := actions[filepath.Join("extensions", "lore-memory.ts")].Action; got != "unchanged" {
		t.Fatalf("lore-memory action = %q, want unchanged", got)
	}
	if got := actions[filepath.Join("extensions", "lore-delegation.ts")]; got.Action != "delete" || !strings.HasPrefix(got.BackupPath, plan.ManagedBackupRoot) {
		t.Fatalf("lore-delegation action = %+v, want delete under %s", got, plan.ManagedBackupRoot)
	}
	for _, relativePath := range []string{filepath.Join("extensions", "lore-footer.ts"), "settings.json", filepath.Join("agents", "lore-managed-lore-worker.md"), filepath.Join("agents", "lore-managed-sdd-apply.md")} {
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
	if got := len(plan.ManagedFileActions); got != 13 {
		t.Fatalf("len(ManagedFileActions) = %d, want 13 managed files + overlays without theme bootstrap accounting", got)
	}
	for _, action := range plan.ManagedFileActions {
		if action.Action != "unchanged" {
			t.Fatalf("planned action for %s = %q, want unchanged on converged rerun", action.RelativePath, action.Action)
		}
	}

	result, err := (Service{}).ExecutePiInstall(plan, InstallCommandOptions{AssumeYes: true})
	if err != nil {
		t.Fatalf("ExecutePiInstall rerun error: %v", err)
	}
	if len(result.Summary.Created) != 0 || len(result.Summary.Updated) != 0 || len(result.Summary.Deleted) != 0 {
		t.Fatalf("summary = %+v, want no create/update/delete actions on converged rerun", result.Summary)
	}
	if got := len(result.Summary.Unchanged); got != 13 {
		t.Fatalf("len(Unchanged) = %d, want 13 managed files + overlays recorded for plan validation", got)
	}
	if containsSummaryEntry(result.Summary.Unchanged, filepath.Join("themes", "alferio.json")) {
		t.Fatalf("Unchanged = %v, want theme bootstrap excluded from managed plan/summary accounting", result.Summary.Unchanged)
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
	  "packages": ["git:github.com/example/evil-runtime", "` + piRemoteSubagentsPackage + `"],
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
	if got, want := settings.Packages, []string{piRemoteSubagentsPackage}; len(got) != len(want) || got[0] != want[0] {
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
