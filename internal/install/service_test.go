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

func TestResolvePiLayoutModelsManagedPaths(t *testing.T) {
	layout := ResolvePiLayout("/tmp/home")
	if got, want := layout.AgentDir, "/tmp/home/.pi/agent"; got != want {
		t.Fatalf("AgentDir = %q, want %q", got, want)
	}
	if got, want := layout.ManifestPath, "/tmp/home/.pi/agent/lore-install.json"; got != want {
		t.Fatalf("ManifestPath = %q, want %q", got, want)
	}
	if len(layout.ManagedFiles) != 4 {
		t.Fatalf("ManagedFiles = %v, want 4 managed paths", layout.ManagedFiles)
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
	if len(result.Summary.Created) != 2 {
		t.Fatalf("Created = %v, want 2 created extension files", result.Summary.Created)
	}
	if len(result.Summary.Updated) != 2 {
		t.Fatalf("Updated = %v, want settings + lore-memory", result.Summary.Updated)
	}
	if len(result.Summary.BackedUp) != 2 {
		t.Fatalf("BackedUp = %v, want 2 backups", result.Summary.BackedUp)
	}
	if result.Manifest.AuthMode != "cli-request" || result.Manifest.ServerURL != "https://lore.example" {
		t.Fatalf("Manifest = %+v, want cli-request manifest with server URL", result.Manifest)
	}
	if result.Manifest.BackupRoot == "" || len(result.Manifest.ManagedFiles) != 4 {
		t.Fatalf("Manifest = %+v, want backup root and managed files", result.Manifest)
	}
	for i, want := range layout.ManagedFiles {
		if got := result.Manifest.ManagedFiles[i]; got != want {
			t.Fatalf("Manifest.ManagedFiles[%d] = %q, want %q", i, got, want)
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

	delegationContent, err := os.ReadFile(filepath.Join(layout.ExtensionsDir, "lore-delegation.ts"))
	if err != nil {
		t.Fatalf("ReadFile lore-delegation.ts: %v", err)
	}
	if got := string(delegationContent); !containsAll(got,
		"export default function",
		"ExtensionAPI",
		"pi.registerCommand(\"lore-models\"",
		"sdd-init",
		"No model configured for Pi delegations. Use /lore-models to select a default or phase model.",
		"No available models detected. Configure/login to a Pi provider and API key, then use /lore-models.",
		"getAvailableModelsSafe(",
		"Promise.resolve(registry.getAvailable.call(registry))",
		"findModelSafe(ctx, parsed.provider, parsed.id)",
		"pi.registerShortcut(\"ctrl+space\"",
		"name: \"delegate\"",
		"name: \"delegation_read\"",
		"name: \"delegation_list\"") {
		t.Fatalf("lore-delegation.ts missing local delegation/model-routing markers: %q", got)
	} else if strings.Contains(got, "lore_delegate") || strings.Contains(got, "/v1/sessions") {
		t.Fatalf("lore-delegation.ts contains unsupported remote delegation contract: %q", got)
	} else if strings.Contains(got, "pi.registerCommand(\"sdd-models\"") {
		t.Fatalf("lore-delegation.ts still contains removed /sdd-models alias: %q", got)
	} else if !strings.Contains(got, "anchor: \"center\",\n      width: \"96%\",\n      minWidth: 72,\n      maxHeight: \"80%\",") {
		t.Fatalf("lore-delegation.ts missing centered subagents overlay marker: %q", got)
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
	loreSettings, ok := settings["lore"].(map[string]any)
	if !ok {
		t.Fatalf("settings lore block missing: %v", settings)
	}
	if got := loreSettings["auth_mode"]; got != "cli-request" {
		t.Fatalf("settings lore.auth_mode = %v, want cli-request", got)
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
		"extensions/lore-delegation.ts": []byte("export default function (pi: ExtensionAPI) { ctx.ui.setStatus(\"lore-delegation\", \"\"); }"),
		"extensions/lore-footer.ts":     []byte("export default function (pi: ExtensionAPI) { ctx.ui.setFooter(() => ({ render() { return []; } })); } getContextUsage getExtensionStatuses"),
		"settings.json":                 []byte(`{"lore":{"server_url":"https://lore.example"}}`),
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
		"extensions/lore-memory.ts":     validMemory,
		"extensions/lore-delegation.ts": []byte("export default function (pi: ExtensionAPI) { ctx.ui.setStatus(\"lore-delegation\", \"\"); }"),
		"extensions/lore-footer.ts":     []byte("export default function (pi: ExtensionAPI) { ctx.ui.setFooter(() => ({ render() { return []; } })); } getContextUsage getExtensionStatuses"),
		"settings.json":                 []byte(`{"lore":{"server_url":"https://lore.example"}}`),
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
		{relativePath: managedPiExtensionRelativePaths[1], absolutePath: filepath.Join(layout.ExtensionsDir, "lore-delegation.ts"), content: []byte("export default function (pi: ExtensionAPI) {}")},
		{relativePath: managedPiExtensionRelativePaths[2], absolutePath: filepath.Join(layout.ExtensionsDir, "lore-footer.ts"), content: []byte("export default function (pi: ExtensionAPI) { ctx.ui.setFooter(() => ({ render() { return []; } })); } getContextUsage getExtensionStatuses")},
		{relativePath: "settings.json", absolutePath: layout.SettingsPath, content: []byte(`{}`), mergeJSON: true},
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
}

func TestManifestValidateRejectsManagedFileMismatch(t *testing.T) {
	layout := ResolvePiLayout("/tmp/home")
	manifest := Manifest{
		SchemaVersion: "1",
		Target:        string(TargetPi),
		AuthMode:      "cli-request",
		ServerURL:     "https://lore.example",
		LoreBinary:    "/usr/local/bin/lore",
		LoreConfigDir: "/tmp/lore-config",
		ManagedFiles:  []string{"/tmp/home/.pi/agent/extensions/lore-memory.ts"},
		BackupRoot:    "/tmp/home/.pi/agent/backups/20260518T193000Z",
		InstalledAt:   time.Date(2026, 5, 18, 19, 30, 0, 0, time.UTC).Format(time.RFC3339),
	}

	if err := manifest.Validate(layout); err == nil || !strings.Contains(err.Error(), "managed_files") {
		t.Fatalf("Validate error = %v, want managed_files mismatch", err)
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
	if len(result.Summary.Failed) != 3 {
		t.Fatalf("Failed = %v, want 3 validation failures for managed extensions", result.Summary.Failed)
	}
	for _, want := range []string{"extensions/lore-memory.ts", "extensions/lore-delegation.ts", "extensions/lore-footer.ts"} {
		if !containsSummaryEntry(result.Summary.Failed, want, "contains saved auth material") {
			t.Fatalf("Failed = %v, want secret-safe validation entry for %s", result.Summary.Failed, want)
		}
	}
	for _, entry := range result.Summary.Failed {
		if strings.Contains(entry, "export default function") {
			t.Fatalf("Failed = %v, want no raw token echo", result.Summary.Failed)
		}
	}
	if len(result.Summary.Created) != 4 || len(result.Summary.Updated) != 0 || len(result.Summary.Unchanged) != 0 {
		t.Fatalf("summary = %+v, want 4 created files and no updates/unchanged entries", result.Summary)
	}
	if result.Manifest.AuthMode != "cli-request" || result.Manifest.CLIVersion != "v1.2.3" {
		t.Fatalf("manifest = %+v, want persisted cli-request metadata", result.Manifest)
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
		case filepath.Join("extensions", "lore-delegation.ts"):
			if err := os.WriteFile(file.absolutePath, []byte("legacy-delegation"), 0o600); err != nil {
				t.Fatalf("WriteFile updated fixture: %v", err)
			}
		}
	}

	plan, err := Service{}.PlanPiInstall(req)
	if err != nil {
		t.Fatalf("PlanPiInstall error: %v", err)
	}
	if got := len(plan.ManagedFileActions); got != 4 {
		t.Fatalf("len(ManagedFileActions) = %d, want 4", got)
	}
	actions := map[string]ManagedFileAction{}
	for _, action := range plan.ManagedFileActions {
		actions[action.RelativePath] = action
	}
	if got := actions[filepath.Join("extensions", "lore-memory.ts")].Action; got != "unchanged" {
		t.Fatalf("lore-memory action = %q, want unchanged", got)
	}
	if got := actions[filepath.Join("extensions", "lore-delegation.ts")]; got.Action != "update" || !strings.HasPrefix(got.BackupPath, plan.ManagedBackupRoot) {
		t.Fatalf("lore-delegation action = %+v, want update under %s", got, plan.ManagedBackupRoot)
	}
	for _, relativePath := range []string{filepath.Join("extensions", "lore-footer.ts"), "settings.json"} {
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
