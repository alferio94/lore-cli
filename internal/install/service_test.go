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
		t.Fatalf("len(targets) = %d, want 5 (Pi + OpenCode + Codex + Antigravity + ClaudeCode as Coming soon)", len(targets))
	}
	if got := targets[0]; got.ID != TargetPi || !got.Available || !got.Recommended {
		t.Fatalf("targets[0] = %+v, want available recommended Pi", got)
	}
	if got := targets[0].Description; !containsAll(got, "uses hosted Lore MCP via pi-mcp-adapter", "optional explicit pi-extensions (lore-footer.ts only)") {
		t.Fatalf("targets[0].Description = %q, want hosted MCP default with optional explicit pi-extensions", got)
	}
	if got := findTarget(targets, TargetAntigravity); !got.Available || got.Recommended || !containsAll(got.Description, "Full Antigravity projection", "skills", "agent profile", "global ~/.gemini/config/mcp_config.json") {
		t.Fatalf("antigravity target = %+v, want supported full Antigravity target with managed Gemini agent profile and global direct MCP config", got)
	}
	// OpenCode is supported again: it must be in the visible default target
	// list as a bounded config-only target.
	if got := findTarget(targets, TargetOpenCode); !got.Available {
		t.Fatalf("target opencode = %+v, want supported (bounded config-only projection)", got)
	}
	if got := findTarget(targets, TargetOpenCode).Description; !containsAll(got, "Bounded OpenCode projection", "config-only") {
		t.Fatalf("opencode description = %q, want bounded config-only projection", got)
	}
	// The OpenCode target description must surface the fail-closed
	// mcp.lore ownership contract and the native-safe plugin
	// semantics (only the community statusline is installed; legacy
	// Lore-owned runtime plugins are rejected/cleaned up).
	opencodeDescription := findTarget(targets, TargetOpenCode).Description
	for _, want := range []string{
		"default_agent=lore",
		"`mode: \"primary\"`",
		"`mode: \"subagent\"`",
		"lore-worker",
		"no `permission: \"allow\"` bypass",
		"without Lore-only marker fields",
		"fails closed",
		"opencode-subagent-statusline",
		"tui.json",
		"sdd-engram",
		"logo",
		"background-agents.ts",
		"lore-models.ts",
		"not copied",
		"model hot-edit plugin",
	} {
		if !strings.Contains(opencodeDescription, want) {
			t.Fatalf("opencode description missing %q; got %q", want, opencodeDescription)
		}
	}
	claudeCode := findTarget(targets, TargetClaudeCode)
	if claudeCode.Available {
		t.Fatalf("target %s unexpectedly available", TargetClaudeCode)
	}
	if got := claudeCode.Availability; got != "Coming soon" {
		t.Fatalf("target %s availability = %q, want Coming soon", TargetClaudeCode, got)
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

	// OpenCode is supported again: `ResolveInstallTarget(opencode)` resolves
	// to the bounded config-only projection target.
	selected, err = ResolveInstallTarget(TargetOpenCode)
	if err != nil {
		t.Fatalf("ResolveInstallTarget(opencode) error = %v, want nil", err)
	}
	if selected.ID != TargetOpenCode || !selected.Available {
		t.Fatalf("ResolveInstallTarget(opencode) = %+v, want supported OpenCode target", selected)
	}

	if _, err := ResolveInstallTarget(TargetClaudeCode); err == nil || !containsAll(err.Error(), string(TargetClaudeCode), "Coming soon", "supported targets") {
		t.Fatalf("ResolveInstallTarget(claude-code) error = %v, want roadmap guardrail", err)
	}
	if _, err := ResolveInstallTarget(TargetID("unknown-target")); err == nil || !containsAll(err.Error(), "unknown-target", "pi", "codex", "antigravity") {
		t.Fatalf("ResolveInstallTarget(unknown-target) error = %v, want unsupported target rejection listing supported targets", err)
	}
}

func TestFormatTargetSelectionExplainsPiNativePathAndMCPDeferral(t *testing.T) {
	formatted := FormatTargetSelection(DefaultTargets())
	for _, want := range []string{"Choose an install target:", "Pi — Recommended", "uses hosted Lore MCP", "Antigravity:", "Full Antigravity projection", "Coming soon", "Pi remains the default recommended path", "uses hosted Lore MCP by default", "~/.gemini/config/agents/lore.json", "global ~/.gemini/config/mcp_config.json", "Bounded OpenCode projection", "~/.config/opencode"} {
		if !strings.Contains(formatted, want) {
			t.Fatalf("FormatTargetSelection() = %q, want substring %q", formatted, want)
		}
	}
}

// TestSupportedTargetsIncludesOpenCode verifies the spec scenario for
// `readd-opencode-support-from-gentle-ai` foundation slice:
//   - GIVEN the install target catalog
//   - WHEN code calls SupportedTargets()
//   - THEN the returned set is exactly the four supported targets
//   - AND the OpenCode entry is present
//   - AND the supported set is ordered Pi, OpenCode, Codex, Antigravity
//     to keep the visible TUI ordering consistent.
func TestSupportedTargetsIncludesOpenCode(t *testing.T) {
	got := SupportedTargets()
	want := []TargetID{TargetPi, TargetOpenCode, TargetCodex, TargetAntigravity}
	if len(got) != len(want) {
		t.Fatalf("SupportedTargets() length = %d, want %d (got=%v, want=%v)", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SupportedTargets()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestInstallAcceptsOpenCodeTarget verifies the spec scenario for
// `readd-opencode-support-from-gentle-ai` foundation slice:
//   - GIVEN a user runs `lore install --target opencode`
//   - WHEN the command runs
//   - THEN the install target resolves successfully
//   - AND the resolved target is the bounded config-only OpenCode target
//   - AND the default install target remains Pi (not OpenCode).
func TestInstallAcceptsOpenCodeTarget(t *testing.T) {
	selected, err := ResolveInstallTarget(TargetOpenCode)
	if err != nil {
		t.Fatalf("ResolveInstallTarget(opencode) error = %v, want nil", err)
	}
	if selected.ID != TargetOpenCode || !selected.Available {
		t.Fatalf("ResolveInstallTarget(opencode) = %+v, want supported OpenCode target", selected)
	}
	// The default install target must remain Pi.
	defaultTarget, err := ResolveInstallTarget("")
	if err != nil {
		t.Fatalf("ResolveInstallTarget(\"\") error = %v, want nil", err)
	}
	if defaultTarget.ID == TargetOpenCode {
		t.Fatalf("default target = %q, want non-OpenCode default", defaultTarget.ID)
	}
}

// TestNoActiveSourceImportsOpencodereadyPackage is a focused static
// guard for the spec invariant: no active (non-test) Go file under
// `internal/` may import the removed `internal/opencodeready` package.
// The `internal/opencodeready` package itself has been hard-removed;
// any surviving import is a regression. Test files are excluded from
// the active-source check because they may legitimately mention
// "opencodeready" in negative regression assertions.
//
// Note: the `~/.config/opencode` literal IS legitimately referenced by
// the OpenCode install surface (adapter_opencode.go,
// opencode_install.go, and tui/root.go), so the prior user-config path
// guard is now scoped to the `opencodeready` import only.
func TestNoActiveSourceImportsOpencodereadyPackage(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	activeDirs := []string{
		"internal/cli", "internal/tui", "internal/install",
		"internal/httpclient", "internal/config", "internal/output",
		"internal/auth", "internal/agentconfig", "internal/agentpack",
	}
	for _, dir := range activeDirs {
		abs := filepath.Join(repoRoot, dir)
		_ = filepath.WalkDir(abs, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			name := filepath.Base(path)
			if !strings.HasSuffix(name, ".go") {
				return nil
			}
			rel, _ := filepath.Rel(repoRoot, path)
			if strings.HasSuffix(name, "_test.go") {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			text := string(content)
			if strings.Contains(text, "internal/opencodeready") {
				t.Fatalf("active non-test source %s imports internal/opencodeready", rel)
			}
			return nil
		})
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
	// Write existing lore-memory.ts for legacy migration test. The default install
	// must back it up and remove it because the asset is deprecated and removed
	// from the install bundle; remaining copies would autoload inside the Pi
	// runtime if left untouched.
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
	// settings.json and mcp.json should be updated.
	if len(result.Summary.Updated) < 1 {
		t.Fatalf("Updated = %v, want at least settings.json update", result.Summary.Updated)
	}
	// Both legacy cleanup paths are applied: lore-delegation.ts (pre-`lore-pi-runtime`
	// delegation) and lore-memory.ts (deprecated Pi-native memory extension). The
	// order is delegation first, then deprecated memory; the manifest refresh must
	// reflect both cleanups.
	wantDeleted := []string{filepath.Join("extensions", "lore-delegation.ts"), managedDeprecatedLoreMemoryRelativePath}
	if len(result.Summary.Deleted) != len(wantDeleted) {
		t.Fatalf("Deleted = %v, want %v (legacy delegation + deprecated lore-memory cleanup)", result.Summary.Deleted, wantDeleted)
	}
	for _, path := range wantDeleted {
		if !containsSummaryEntry(result.Summary.Deleted, path) {
			t.Fatalf("Deleted = %v, want entry %q present", result.Summary.Deleted, path)
		}
	}
	// BackedUp must include both cleanup paths plus the updated settings.json.
	if len(result.Summary.BackedUp) < 3 {
		t.Fatalf("BackedUp = %v, want at least 3 backups (legacy delegation + deprecated lore-memory + settings.json)", result.Summary.BackedUp)
	}
	for _, path := range wantDeleted {
		if !containsSummaryEntry(result.Summary.BackedUp, path) {
			t.Fatalf("BackedUp = %v, want entry %q present", result.Summary.BackedUp, path)
		}
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
	// Pre-existing lore-memory.ts and lore-delegation.ts are both removed by the
	// backup-first cleanup paths and a copy of each is preserved under the managed
	// backup root so the user can roll back if needed. Only settings.json and
	// mcp.json are managed by default install.

	if _, err := os.Stat(filepath.Join(layout.ExtensionsDir, "lore-delegation.ts")); !os.IsNotExist(err) {
		t.Fatalf("lore-delegation.ts stat error = %v, want file removed after cleanup", err)
	}
	if _, err := os.ReadFile(filepath.Join(result.Manifest.BackupRoot, "extensions", "lore-delegation.ts")); err != nil {
		t.Fatalf("ReadFile backup lore-delegation.ts: %v", err)
	}
	if _, err := os.Stat(filepath.Join(layout.ExtensionsDir, "lore-memory.ts")); !os.IsNotExist(err) {
		t.Fatalf("lore-memory.ts stat error = %v, want deprecated file removed after cleanup", err)
	}
	if _, err := os.ReadFile(filepath.Join(result.Manifest.BackupRoot, managedDeprecatedLoreMemoryRelativePath)); err != nil {
		t.Fatalf("ReadFile backup lore-memory.ts: %v", err)
	}
	// The preserved backup must still contain the original bytes the test wrote,
	// not an empty file or a placeholder, so rollback can restore the prior state.
	if got, err := os.ReadFile(filepath.Join(result.Manifest.BackupRoot, managedDeprecatedLoreMemoryRelativePath)); err != nil || string(got) != "legacy token=secret-token" {
		t.Fatalf("backup lore-memory.ts content = %q err=%v, want original bytes preserved", string(got), err)
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
	// The manifest refresh must not record the deprecated lore-memory.ts as a
	// managed file (the cleanup is a delete, not a create/update).
	for _, managed := range manifest.ManagedFiles {
		if strings.Contains(managed.Path, "lore-memory.ts") {
			t.Fatalf("manifest managed_files includes deprecated lore-memory.ts: %+v", managed)
		}
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

func TestValidateManagedContentsRejectsDeprecatedLoreMemoryAndTokenLeak(t *testing.T) {
	hostedPackage := PiHostedMCPPackageSource()
	// lore-memory.ts was deprecated and removed. Even if a caller hands it in, the
	// validator must (1) reject the file as deprecated, and (2) flag any saved-token
	// leak without echoing the raw token back into the finding message.
	findings := validateManagedContents(map[string][]byte{
		"extensions/lore-memory.ts": []byte(`
const loreServerURL = "https://lore.example";
export default function (pi: ExtensionAPI) {
  pi.registerTool({ name: "lore_search" });
}
secret-token
`),
		"extensions/lore-footer.ts": []byte("export default function (pi: ExtensionAPI) { ctx.ui.setFooter(() => ({ render() { return []; } })); } getContextUsage getExtensionStatuses"),
		"settings.json":             []byte(`{"packages":["` + hostedPackage + `"],"lore":{"server_url":"https://lore.example"}}`),
	}, PiInstallRequest{ServerURL: "https://lore.example", SavedToken: "secret-token"})
	if len(findings) != 2 {
		t.Fatalf("len(findings) = %d, want 2 (deprecated + saved auth material)", len(findings))
	}
	var sawDeprecated, sawAuthMaterial bool
	for _, got := range findings {
		if containsAll(got, "extensions/lore-memory.ts", "deprecated") {
			sawDeprecated = true
		}
		if containsAll(got, "saved auth material", "extensions/lore-memory.ts") && !strings.Contains(got, "secret-token") {
			sawAuthMaterial = true
		}
	}
	if !sawDeprecated {
		t.Fatalf("findings = %#v, want deprecated finding for extensions/lore-memory.ts", findings)
	}
	if !sawAuthMaterial {
		t.Fatalf("findings = %#v, want saved-auth-material finding for extensions/lore-memory.ts without leaking the raw token", findings)
	}
}

func TestValidateManagedContentsAllowsCleanDefaultInstall(t *testing.T) {
	hostedPackage := PiHostedMCPPackageSource()
	// The default hosted-MCP install renders only settings.json + mcp.json (and any
	// extended skills). The deprecated lore-memory.ts must not appear, and the
	// validator must report zero findings for a clean default install.
	findings := validateManagedContents(map[string][]byte{
		"settings.json": []byte(`{"packages":["` + hostedPackage + `"],"lore":{"server_url":"https://lore.example"}}`),
		"mcp.json":      []byte(`{"mcpServers":{"lore":{"url":"https://lore.example/v1/mcp","headers":{"Authorization":"Bearer secret-token"}}}}`),
	}, PiInstallRequest{ServerURL: "https://lore.example", SavedToken: "secret-token"})
	if len(findings) != 0 {
		t.Fatalf("len(findings) = %d, want 0; findings = %#v", len(findings), findings)
	}
}

func TestValidateRenderedPiFilesRejectsDeprecatedLoreMemoryBeforeWrites(t *testing.T) {
	layout := ResolvePiLayout(t.TempDir())
	hostedPackage := PiHostedMCPPackageSource()
	files := []renderedPiFile{
		// Even with valid content, rendering the deprecated lore-memory.ts must fail
		// validation before any filesystem write.
		{relativePath: managedDeprecatedLoreMemoryRelativePath, absolutePath: filepath.Join(layout.ExtensionsDir, "lore-memory.ts"), content: []byte("export default function (pi: ExtensionAPI) { /* deprecated */ }")},
		{relativePath: managedPiExtensionRelativePaths[0], absolutePath: filepath.Join(layout.ExtensionsDir, "lore-footer.ts"), content: []byte("export default function (pi: ExtensionAPI) { ctx.ui.setFooter(() => ({ render() { return []; } })); } getContextUsage getExtensionStatuses")},
		{relativePath: "settings.json", absolutePath: layout.SettingsPath, content: []byte(`{"packages":["` + hostedPackage + `"]}`), mergeMode: MergeModeAdditiveJSON},
	}

	err := validateRenderedPiFiles(files)
	if err == nil || !containsAll(err.Error(), "extensions/lore-memory.ts", "deprecated") {
		t.Fatalf("validateRenderedPiFiles error = %v, want deprecated lore-memory.ts rejection", err)
	}
	if _, statErr := os.Stat(layout.AgentDir); !os.IsNotExist(statErr) {
		t.Fatalf("agent dir stat error = %v, want no writes before validation failure", statErr)
	}
}

func TestValidateRenderedPiFilesRejectsMissingDefaultFactoryBeforeWrites(t *testing.T) {
	layout := ResolvePiLayout(t.TempDir())
	hostedPackage := PiHostedMCPPackageSource()
	files := []renderedPiFile{
		// lore-footer.ts is the only remaining optional Pi-native extension; if it
		// is rendered without the documented `export default function` factory,
		// validation must fail before any write.
		{relativePath: managedPiExtensionRelativePaths[0], absolutePath: filepath.Join(layout.ExtensionsDir, "lore-footer.ts"), content: []byte("lore footer without factory")},
		{relativePath: "settings.json", absolutePath: layout.SettingsPath, content: []byte(`{"packages":["` + hostedPackage + `"]}`), mergeMode: MergeModeAdditiveJSON},
	}

	err := validateRenderedPiFiles(files)
	if err == nil || !containsAll(err.Error(), "extensions/lore-footer.ts", "export default function") {
		t.Fatalf("validateRenderedPiFiles error = %v, want default factory rejection", err)
	}
	if _, statErr := os.Stat(layout.AgentDir); !os.IsNotExist(statErr) {
		t.Fatalf("agent dir stat error = %v, want no writes before validation failure", statErr)
	}
}

func TestInstallPiRejectsInvalidRenderedExtensionShapeBeforeAnyWrite(t *testing.T) {
	homeDir := t.TempDir()

	// The test's original intent was to check that an "unexpected-extra.ts" file causes
	// validation to fail before writes. With the hosted MCP default, the only optional
	// Pi-native extension is lore-footer.ts (lore-memory.ts was deprecated and removed).
	// To exercise the manifest validation path, mutate the package-level extension list
	// to add an unexpected extra path that the adapter never renders, then explicitly
	// select pi-extensions so the footer rendering path runs.
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
	// The deprecated lore-memory.ts is filtered out by the upgrade path so the
	// resulting manifest matches the current install contract.
	if len(manifest.ManagedFiles) != 2 {
		t.Fatalf("len(ManagedFiles) = %d, want 2 (lore-footer + settings; lore-delegation and deprecated lore-memory must be filtered out)", len(manifest.ManagedFiles))
	}
	for _, managed := range manifest.ManagedFiles {
		if strings.Contains(managed.Path, "lore-delegation.ts") {
			t.Fatalf("legacy delegation path unexpectedly preserved in upgraded manifest: %+v", managed)
		}
		if strings.Contains(managed.Path, "lore-memory.ts") {
			t.Fatalf("deprecated lore-memory path unexpectedly preserved in upgraded manifest: %+v", managed)
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
	if !containsAll(promptText, antigravityPromptStartMarker, antigravityPromptEndMarker, "Lore Runtime", "prefer `lore_project_activity` first", "targeted `lore_memory_search`", "`lore_memory_get` for full memory content") {
		t.Fatalf("prompt content = %q, want managed Antigravity prompt block with Lore MCP context guidance", promptText)
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

// TestPlanDeprecatedLoreMemoryCleanupReturnsNilWhenAbsent verifies the cleanup
// planner is idempotent on a fresh install: a missing deprecated `lore-memory.ts`
// must produce a nil action so apply and dry-run do nothing.
func TestPlanDeprecatedLoreMemoryCleanupReturnsNilWhenAbsent(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolvePiLayout(homeDir)
	backupRoot := filepath.Join(layout.AgentDir, "backups", "20260519T050000Z")
	action, err := planDeprecatedLoreMemoryCleanup(layout, backupRoot)
	if err != nil {
		t.Fatalf("planDeprecatedLoreMemoryCleanup error: %v, want nil for absent file", err)
	}
	if action != nil {
		t.Fatalf("planDeprecatedLoreMemoryCleanup action = %+v, want nil for absent file", action)
	}
}

// TestPlanDeprecatedLoreMemoryCleanupReturnsDeleteActionForExistingFile verifies
// the cleanup planner produces a `delete` action with the documented relative
// path and a backup path rooted at the supplied backup root.
func TestPlanDeprecatedLoreMemoryCleanupReturnsDeleteActionForExistingFile(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolvePiLayout(homeDir)
	if err := os.MkdirAll(layout.ExtensionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll extensions: %v", err)
	}
	absolutePath := filepath.Join(layout.AgentDir, filepath.FromSlash(managedDeprecatedLoreMemoryRelativePath))
	if err := os.WriteFile(absolutePath, []byte("legacy memory leftover"), 0o600); err != nil {
		t.Fatalf("WriteFile lore-memory.ts: %v", err)
	}
	backupRoot := filepath.Join(layout.AgentDir, "backups", "20260519T050000Z")
	action, err := planDeprecatedLoreMemoryCleanup(layout, backupRoot)
	if err != nil {
		t.Fatalf("planDeprecatedLoreMemoryCleanup error: %v, want nil", err)
	}
	if action == nil {
		t.Fatalf("planDeprecatedLoreMemoryCleanup action = nil, want delete action for existing file")
	}
	if action.RelativePath != managedDeprecatedLoreMemoryRelativePath {
		t.Fatalf("action.RelativePath = %q, want %q", action.RelativePath, managedDeprecatedLoreMemoryRelativePath)
	}
	if action.Action != "delete" {
		t.Fatalf("action.Action = %q, want delete", action.Action)
	}
	wantBackup := filepath.Join(backupRoot, managedDeprecatedLoreMemoryRelativePath)
	if action.BackupPath != wantBackup {
		t.Fatalf("action.BackupPath = %q, want %q", action.BackupPath, wantBackup)
	}
}

// TestPlanDeprecatedLoreMemoryCleanupRejectsNonRegularFile verifies the cleanup
// planner refuses to follow a symlink or operate on a directory placed at the
// deprecated path. The user must move such an entry aside before reinstalling.
func TestPlanDeprecatedLoreMemoryCleanupRejectsNonRegularFile(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolvePiLayout(homeDir)
	if err := os.MkdirAll(layout.ExtensionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll extensions: %v", err)
	}
	absolutePath := filepath.Join(layout.AgentDir, filepath.FromSlash(managedDeprecatedLoreMemoryRelativePath))
	if err := os.Symlink("/tmp/somewhere-else", absolutePath); err != nil {
		t.Fatalf("Symlink deprecated path: %v", err)
	}
	action, err := planDeprecatedLoreMemoryCleanup(layout, filepath.Join(layout.AgentDir, "backups"))
	if err == nil || action != nil {
		t.Fatalf("planDeprecatedLoreMemoryCleanup = (%+v, %v), want non-nil error and nil action for symlink", action, err)
	}
	if !containsAll(err.Error(), managedDeprecatedLoreMemoryRelativePath, "symlink") {
		t.Fatalf("error = %v, want mention of deprecated path and symlink kind", err)
	}
}

// TestInstallPiRemovesAndBacksUpPreExistingLoreMemoryExtension is a focused
// guard for the spec invariant: a pre-existing `~/.pi/agent/extensions/lore-memory.ts`
// must be backed up under the managed backup root, removed from disk, and
// reported in the install summary's Deleted and BackedUp lists. The manifest
// refresh must not record the deprecated path as a managed file.
func TestInstallPiRemovesAndBacksUpPreExistingLoreMemoryExtension(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolvePiLayout(homeDir)
	if err := os.MkdirAll(layout.ExtensionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll extensions: %v", err)
	}
	absolutePath := filepath.Join(layout.AgentDir, filepath.FromSlash(managedDeprecatedLoreMemoryRelativePath))
	original := []byte("// pre-existing lore-memory.ts leftover from older install\nexport default function (pi) {}\n")
	if err := os.WriteFile(absolutePath, original, 0o600); err != nil {
		t.Fatalf("WriteFile lore-memory.ts: %v", err)
	}

	result, err := Service{}.InstallPi(PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Now:            time.Date(2026, 5, 19, 5, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("InstallPi error: %v", err)
	}
	if len(result.Summary.Failed) != 0 {
		t.Fatalf("Failed = %v, want none", result.Summary.Failed)
	}
	if !containsSummaryEntry(result.Summary.Deleted, managedDeprecatedLoreMemoryRelativePath) {
		t.Fatalf("Deleted = %v, want entry %q", result.Summary.Deleted, managedDeprecatedLoreMemoryRelativePath)
	}
	if !containsSummaryEntry(result.Summary.BackedUp, managedDeprecatedLoreMemoryRelativePath) {
		t.Fatalf("BackedUp = %v, want entry %q", result.Summary.BackedUp, managedDeprecatedLoreMemoryRelativePath)
	}
	if _, err := os.Stat(absolutePath); !os.IsNotExist(err) {
		t.Fatalf("lore-memory.ts stat error = %v, want file removed from installed extensions", err)
	}
	backupPath := filepath.Join(result.Manifest.BackupRoot, managedDeprecatedLoreMemoryRelativePath)
	backupContent, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("ReadFile lore-memory.ts backup: %v", err)
	}
	if string(backupContent) != string(original) {
		t.Fatalf("backup content = %q, want original %q", string(backupContent), string(original))
	}
	for _, managed := range result.Manifest.ManagedFiles {
		if strings.Contains(managed.Path, "lore-memory.ts") {
			t.Fatalf("manifest managed_files includes deprecated lore-memory.ts: %+v", managed)
		}
	}
}

// TestInstallPiIdempotentRerunAfterLoreMemoryCleanup verifies the cleanup is
// idempotent across reruns: once the deprecated `lore-memory.ts` is removed
// and backed up, subsequent applies report no duplicate cleanup, do not error
// on a missing source file, and the file remains absent.
func TestInstallPiIdempotentRerunAfterLoreMemoryCleanup(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolvePiLayout(homeDir)
	if err := os.MkdirAll(layout.ExtensionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll extensions: %v", err)
	}
	absolutePath := filepath.Join(layout.AgentDir, filepath.FromSlash(managedDeprecatedLoreMemoryRelativePath))
	if err := os.WriteFile(absolutePath, []byte("leftover"), 0o600); err != nil {
		t.Fatalf("WriteFile lore-memory.ts: %v", err)
	}

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
	if !containsSummaryEntry(first.Summary.Deleted, managedDeprecatedLoreMemoryRelativePath) {
		t.Fatalf("first Deleted = %v, want entry %q", first.Summary.Deleted, managedDeprecatedLoreMemoryRelativePath)
	}

	second, err := Service{}.InstallPi(req)
	if err != nil {
		t.Fatalf("second InstallPi error: %v, want idempotent success", err)
	}
	if containsSummaryEntry(second.Summary.Deleted, managedDeprecatedLoreMemoryRelativePath) {
		t.Fatalf("second Deleted = %v, want no entry %q on idempotent rerun", second.Summary.Deleted, managedDeprecatedLoreMemoryRelativePath)
	}
	if _, err := os.Stat(absolutePath); !os.IsNotExist(err) {
		t.Fatalf("lore-memory.ts stat error = %v, want file absent after rerun", err)
	}
	if len(second.Summary.Failed) != 0 {
		t.Fatalf("second Failed = %v, want no findings on idempotent rerun", second.Summary.Failed)
	}
}

// TestPlanPiInstallSurfacesLoreMemoryCleanupActionInDryRun verifies the
// dry-run plan surfaces the backup-first deprecated `lore-memory.ts` cleanup
// as a `managed_action=delete` entry whenever a pre-existing file is present.
func TestPlanPiInstallSurfacesLoreMemoryCleanupActionInDryRun(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolvePiLayout(homeDir)
	if err := os.MkdirAll(layout.ExtensionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll extensions: %v", err)
	}
	absolutePath := filepath.Join(layout.AgentDir, filepath.FromSlash(managedDeprecatedLoreMemoryRelativePath))
	if err := os.WriteFile(absolutePath, []byte("leftover"), 0o600); err != nil {
		t.Fatalf("WriteFile lore-memory.ts: %v", err)
	}

	plan, err := Service{}.PlanPiInstall(PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Now:            time.Date(2026, 5, 19, 7, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("PlanPiInstall error: %v", err)
	}

	// The action list must include a `delete` entry for the deprecated path.
	var sawDelete bool
	for _, action := range plan.ManagedFileActions {
		if action.RelativePath == managedDeprecatedLoreMemoryRelativePath {
			if action.Action != "delete" {
				t.Fatalf("action.Action = %q, want delete", action.Action)
			}
			if action.BackupPath == "" || !strings.HasPrefix(action.BackupPath, plan.ManagedBackupRoot) {
				t.Fatalf("action.BackupPath = %q, want path under ManagedBackupRoot %q", action.BackupPath, plan.ManagedBackupRoot)
			}
			sawDelete = true
		}
	}
	if !sawDelete {
		t.Fatalf("plan.ManagedFileActions = %+v, want delete action for %q", plan.ManagedFileActions, managedDeprecatedLoreMemoryRelativePath)
	}
}

// TestPlanPiInstallOmitsLoreMemoryCleanupActionWhenAbsent verifies the cleanup
// is a no-op for fresh installs and after a prior cleanup: no `delete` action
// is appended to the dry-run plan when the deprecated file is not present.
func TestPlanPiInstallOmitsLoreMemoryCleanupActionWhenAbsent(t *testing.T) {
	homeDir := t.TempDir()
	plan, err := Service{}.PlanPiInstall(PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      "https://lore.example",
		LoreBinaryPath: "/usr/local/bin/lore",
		LoreConfigDir:  filepath.Join(homeDir, ".lore"),
		LoreCLIVersion: "v1.2.3",
		Now:            time.Date(2026, 5, 19, 8, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("PlanPiInstall error: %v", err)
	}
	for _, action := range plan.ManagedFileActions {
		if action.RelativePath == managedDeprecatedLoreMemoryRelativePath {
			t.Fatalf("plan.ManagedFileActions = %+v, want no entry for %q on fresh install", plan.ManagedFileActions, managedDeprecatedLoreMemoryRelativePath)
		}
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
