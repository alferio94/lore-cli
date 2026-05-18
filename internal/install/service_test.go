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
	service := Service{Store: store, ClientFactory: func(baseURL string) (httpclient.Client, error) {
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
		if got := result.Checks[0].Action; got == "" || !containsAll(got, "lore login", "--server", "--token") {
			t.Fatalf("config action = %q, want login guidance", got)
		}
	})

	t.Run("auth failure", func(t *testing.T) {
		store := stubStore{path: "/tmp/lore/config.json", cfg: config.Config{ServerURL: "https://example.test", APIToken: "secret-token"}}
		client := &stubClient{meErr: &httpclient.UnauthorizedError{APIError: httpclient.APIError{StatusCode: 401, Code: "unauthorized", Message: "login required", RequestID: "req-auth"}}}
		service := Service{Store: store, ClientFactory: func(string) (httpclient.Client, error) { return client, nil }}
		result := service.Preflight(context.Background())
		if result.CanContinue || !result.LoginRequired {
			t.Fatalf("result = %+v, want auth-blocked login-required state", result)
		}
		assertCheck(t, result.Checks[3], "auth", output.StatusFail)
		if got := result.Checks[3].Detail; got == "" || !containsAll(got, "normal user API token required", "/v1/me") {
			t.Fatalf("auth detail = %q, want token remediation detail", got)
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

type stubClient struct {
	healthErr error
	readyErr  error
	meErr     error
	subject   httpclient.Subject
}

func (c *stubClient) Health(context.Context) error { return c.healthErr }
func (c *stubClient) Ready(context.Context) error  { return c.readyErr }
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

	memoryPath := filepath.Join(layout.ExtensionsDir, "lore-memory.ts")
	memoryContent, err := os.ReadFile(memoryPath)
	if err != nil {
		t.Fatalf("ReadFile lore-memory.ts: %v", err)
	}
	if got := string(memoryContent); !containsAll(got,
		"lore api request",
		"https://lore.example",
		"export const lore_search",
		"export const lore_save",
		"export const lore_update",
		"export const lore_delete",
		"export const lore_get_observation",
		"export const lore_context",
		"export const lore_timeline",
		"export const lore_stats",
		"export const lore_session_summary",
		"brokerArgs(\"GET\", \"/v1/search\")",
		"brokerArgs(\"POST\", \"/v1/observations\", body)",
		"brokerArgs(\"PATCH\", `/v1/observations/${id}`, body)",
		"brokerArgs(\"DELETE\", `/v1/observations/${id}`)",
		"brokerArgs(\"GET\", `/v1/observations/${id}`)",
		"brokerArgs(\"GET\", \"/v1/context\")",
		"brokerArgs(\"GET\", \"/v1/timeline\")",
		"brokerArgs(\"GET\", \"/v1/stats\")",
		"brokerArgs(\"POST\", \"/v1/sessions\", body)") {
		t.Fatalf("lore-memory.ts missing broker markers/tool names: %q", got)
	} else if strings.Contains(got, "secret-token") {
		t.Fatalf("lore-memory.ts leaked token: %q", got)
	}

	delegationContent, err := os.ReadFile(filepath.Join(layout.ExtensionsDir, "lore-delegation.ts"))
	if err != nil {
		t.Fatalf("ReadFile lore-delegation.ts: %v", err)
	}
	if got := string(delegationContent); !containsAll(got, "lore api request", "--path", "/v1/sessions", "https://lore.example") {
		t.Fatalf("lore-delegation.ts missing broker contract: %q", got)
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
// lore api request
export const lore_search = { name: "lore_search", broker: () => brokerArgs("GET", "/v1/search") };
export const lore_save = { name: "lore_save", broker: (body?: unknown) => brokerArgs("POST", "/v1/observations", body) };
export const lore_update = { name: "lore_update", broker: (id: string, body?: unknown) => brokerArgs("PATCH", "/v1/observations", body) };
export const lore_delete = { name: "lore_delete", broker: (id: string) => brokerArgs("DELETE", "/v1/observations") };
export const lore_get_observation = { name: "lore_get_observation", broker: (id: string) => brokerArgs("GET", "/v1/observations") };
export const lore_context = { name: "lore_context", broker: () => brokerArgs("GET", "/v1/context") };
export const lore_timeline = { name: "lore_timeline", broker: () => brokerArgs("GET", "/v1/timeline") };
export const lore_stats = { name: "lore_stats", broker: () => brokerArgs("GET", "/v1/stats") };
export const lore_session_summary = { name: "lore_session_summary", broker: (body?: unknown) => brokerArgs("POST", "/v1/sessions", body) };
secret-token
`),
		"extensions/lore-delegation.ts": []byte("lore api request https://lore.example /v1/sessions"),
		"extensions/lore-footer.ts":     []byte("lore api request https://lore.example"),
		"settings.json":                 []byte(`{"lore":{"server_url":"https://lore.example"}}`),
	}, PiInstallRequest{ServerURL: "https://lore.example", SavedToken: "secret-token"})
	if len(findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1", len(findings))
	}
	if got := findings[0]; !containsAll(got, "saved auth material", "extensions/lore-memory.ts") || strings.Contains(got, "secret-token") {
		t.Fatalf("finding = %q, want secret-safe token validation detail", got)
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
		SavedToken:     "lore api request",
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
		if strings.Contains(entry, "lore api request") {
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

func containsSummaryEntry(entries []string, wants ...string) bool {
	for _, entry := range entries {
		if containsAll(entry, wants...) {
			return true
		}
	}
	return false
}
