package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/auth"
	"github.com/alferio94/lore-cli/internal/config"
	"github.com/alferio94/lore-cli/internal/httpclient"
	"github.com/alferio94/lore-cli/internal/install"
	"github.com/alferio94/lore-cli/internal/output"
	"github.com/alferio94/lore-cli/internal/version"
)

func TestInteractiveActionsExposeAppHelpers(t *testing.T) {
	homeDir := t.TempDir()
	store := &fakeStore{path: filepath.Join(t.TempDir(), "config.json"), loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token"}}
	client := &fakeClient{subject: httpclient.Subject{UserID: "user-1", Kind: "user", TokenSource: "api_token"}}
	app, _, _ := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.UserHomeDir = func() (string, error) { return homeDir, nil }
	app.ExecutablePath = func() (string, error) { return "/usr/local/bin/lore", nil }
	app.BuildInfo = version.Info{Version: "v1.2.3"}

	actions := app.InteractiveActions()
	if _, err := actions.Login(context.Background(), " https://example.test ", " secret-token "); err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	plan, planReport, ok := actions.PlanPiInstall(context.Background())
	if !ok || plan.Request.Target != install.TargetPi || planReport.Title != "Lore install" {
		t.Fatalf("PlanPiInstall() = plan:%+v report:%+v ok:%t, want Pi install plan", plan, planReport, ok)
	}
	sharedPlan := plan.InstallPlan()
	if sharedPlan.Request.Target != install.TargetPi || sharedPlan.Layout.Target != install.TargetPi {
		t.Fatalf("shared plan = %+v, want Pi-target shared bridge without changing defaults", sharedPlan)
	}
	if got := formatInstallPlanSummary(plan, true); !strings.Contains(got, "install_target=pi") || !strings.Contains(got, "runtime=pi-remote-package") {
		t.Fatalf("formatInstallPlanSummary() = %q, want unchanged Pi summary wording", got)
	}
	if _, err := install.ResolveInstallTarget(install.TargetClaudeCode); err == nil {
		t.Fatal("ResolveInstallTarget(claude-code) error = nil, want fail-closed unsupported target behavior")
	}
	if _, err := install.ResolveInstallTarget(install.TargetID("unknown-target")); err == nil {
		t.Fatal("ResolveInstallTarget(unknown-target) error = nil, want fail-closed unknown target behavior")
	}
	installReport := actions.Install(context.Background())
	if installReport.ExitCode != 0 || installReport.Title != "Lore install" {
		t.Fatalf("Install() = %+v, want successful Pi install report", installReport)
	}
	antigravityReport := actions.InstallTarget(context.Background(), install.TargetAntigravity)
	if antigravityReport.ExitCode != 0 || antigravityReport.Title != "Lore install" {
		t.Fatalf("InstallTarget(antigravity) = %+v, want successful Antigravity install report", antigravityReport)
	}
	foundInstallSummary := false
	for _, check := range installReport.Checks {
		if check.Name == "install" && strings.Contains(check.Detail, "install_target=pi") {
			foundInstallSummary = true
			break
		}
	}
	if !foundInstallSummary {
		t.Fatalf("Install() checks = %+v, want unchanged Pi install summary", installReport.Checks)
	}
	foundAntigravitySummary := false
	for _, check := range antigravityReport.Checks {
		if check.Name == "install" && strings.Contains(check.Detail, "install_target=antigravity") {
			foundAntigravitySummary = true
			break
		}
	}
	if !foundAntigravitySummary {
		t.Fatalf("InstallTarget(antigravity) checks = %+v, want Antigravity install summary", antigravityReport.Checks)
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

func TestLoginActionPasswordModeMintsTokenAndLaterRequestsReuseBearer(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loadErr: config.ErrNotFound}
	client := &fakeClient{
		loginResult: httpclient.PasswordLoginResult{Token: "minted-token"},
		subject:     httpclient.Subject{ID: "subject-1", UserID: "user-1", Roles: []string{"admin"}, TokenID: "token-1", TokenSource: "api_token", Kind: "user"},
		memory:      httpclient.Memory{ID: "m1", ProjectID: "p1", Scope: "project", Type: "decision", Title: "t1", CreatedBy: "user-1"},
		memories:    []httpclient.Memory{{ID: "m1", ProjectID: "p1", Scope: "project", Type: "decision", Title: "t1", CreatedBy: "user-1"}},
	}
	app, stdout, _ := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })

	result, err := app.loginActionWithInput(context.Background(), LoginInput{Mode: "password", ServerURL: " https://example.test ", Email: "admin@example.com", Password: "super-secret-password"})
	if err != nil {
		t.Fatalf("loginActionWithInput() error = %v", err)
	}
	if client.loginEmail != "admin@example.com" || client.loginPassword != "super-secret-password" {
		t.Fatalf("Login() credentials = %q / %q", client.loginEmail, client.loginPassword)
	}
	if client.meToken != "minted-token" {
		t.Fatalf("Me token after password login = %q, want minted-token", client.meToken)
	}
	if got := app.Auth.(*fakeAuthManager).savedToken; got != "minted-token" {
		t.Fatalf("savedToken = %q, want minted-token", got)
	}
	if !strings.Contains(result.Summary, output.FormatSubject(client.subject)) {
		t.Fatalf("summary = %q, want formatted subject", result.Summary)
	}
	if err := app.runRemember(rememberOptions{ProjectID: "p1", Type: "decision", Title: "t1", Content: "c1"}); err != nil {
		t.Fatalf("runRemember() error = %v", err)
	}
	if err := app.runRecall(recallOptions{ProjectID: "p1"}); err != nil {
		t.Fatalf("runRecall() error = %v", err)
	}
	if client.createToken != "minted-token" || client.listToken != "minted-token" {
		t.Fatalf("later bearer reuse = create:%q list:%q, want minted-token", client.createToken, client.listToken)
	}
	if strings.Contains(stdout.String(), "super-secret-password") {
		t.Fatalf("stdout leaked password: %q", stdout.String())
	}
	assertNoTokenLeak(t, stdout.String()+result.Summary, "", "minted-token")
}

func TestLoginActionWithInputTokenModePreservesCompatibility(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loadErr: config.ErrNotFound}
	client := &fakeClient{subject: httpclient.Subject{UserID: "user-1", Kind: "user", TokenSource: "api_token"}}
	app, _, _ := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })

	_, err := app.loginActionWithInput(context.Background(), LoginInput{Mode: "token", ServerURL: " https://example.test ", Token: " secret-token "})
	if err != nil {
		t.Fatalf("loginActionWithInput() error = %v", err)
	}
	if client.loginCalls != 0 {
		t.Fatalf("Login() calls = %d, want token compatibility path to skip password login", client.loginCalls)
	}
	if client.meToken != "secret-token" {
		t.Fatalf("Me token = %q, want secret-token", client.meToken)
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
	if store.saved.ServerURL != "https://example.test" || store.saved.APIToken != "" || store.saved.CredentialAccount == "" {
		t.Fatalf("saved config = %+v, want metadata-only saved state", store.saved)
	}
	if got := app.Auth.(*fakeAuthManager).savedToken; got != "secret-token" {
		t.Fatalf("savedToken = %q, want secret-token", got)
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

func TestStatusActionMigratesLegacyConfigViaAuthManager(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loaded: config.Config{ServerURL: "https://example.test", APIToken: "legacy-token"}}
	client := &fakeClient{subject: httpclient.Subject{UserID: "user-1", Kind: "user"}}
	creds := &fakeCredentialStore{}
	app, _, _ := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.Auth = auth.Manager{ConfigStore: store, Credentials: creds}

	status := app.statusAction(context.Background())
	if status.ExitCode != 0 {
		t.Fatalf("statusAction() = %+v, want successful migrated status", status)
	}
	if store.saved.APIToken != "" || store.saved.CredentialAccount == "" {
		t.Fatalf("saved config = %+v, want scrubbed metadata-only config", store.saved)
	}
	if client.meToken != "legacy-token" {
		t.Fatalf("Me token = %q, want migrated legacy token", client.meToken)
	}
	if len(creds.secrets) != 1 {
		t.Fatalf("credential writes = %v, want 1 migrated secret", creds.secrets)
	}
	assertNoTokenLeak(t, output.RenderChecks(status.Title, status.Checks), "", "legacy-token")
}

func TestStatusActionFailsClosedWhenLegacyMigrationCredentialBackendUnavailable(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loaded: config.Config{ServerURL: "https://example.test", APIToken: "legacy-token"}}
	client := &fakeClient{}
	creds := &fakeCredentialStore{setErr: errors.New("keychain locked")}
	app, _, _ := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.Auth = auth.Manager{ConfigStore: store, Credentials: creds}

	status := app.statusAction(context.Background())
	if status.ExitCode != 1 {
		t.Fatalf("statusAction() exit = %d, want 1", status.ExitCode)
	}
	assertCheckNames(t, status.Checks, "config", "auth")
	if got := status.Checks[1].Detail; !containsAll(got, "secure credential storage", "run lore login again") {
		t.Fatalf("auth detail = %q, want keychain remediation", got)
	}
	if got := status.Checks[1].Action; !containsAll(got, "OS keychain", "gnome-keyring", "lore login") {
		t.Fatalf("auth action = %q, want unavailable credential action", got)
	}
	if store.saved.APIToken != "" || store.saved.CredentialAccount == "" {
		t.Fatalf("saved config = %+v, want scrubbed metadata-only rewrite", store.saved)
	}
	if client.meToken != "" {
		t.Fatalf("Me token = %q, want no authenticated request on failed migration", client.meToken)
	}
	assertNoTokenLeak(t, output.RenderChecks(status.Title, status.Checks), "", "legacy-token")
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
	assertCheckNames(t, status.Checks, "config", "healthz", "readyz", "auth", "agent-config")
	assertNoTokenLeak(t, output.RenderChecks(status.Title, status.Checks), "", "secret-token")

	doctor := app.doctorAction(context.Background())
	if doctor.Title != "Lore doctor" || doctor.ExitCode != 1 {
		t.Fatalf("doctorAction() = %+v, want failing Lore doctor report", doctor)
	}
	assertCheckNames(t, doctor.Checks, "config", "healthz", "readyz", "auth", "pi", "agent-config")
	assertNoTokenLeak(t, output.RenderChecks(doctor.Title, doctor.Checks), "", "secret-token")
}

func TestRunAPIRequestUsesSavedAuthAndReturnsSuccessEnvelope(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token"}}
	client := &fakeClient{requestJSONResult: httpclient.RequestJSONResult{StatusCode: 200, RequestID: "req-context", Data: json.RawMessage(`{"project":"lore-cli"}`)}}
	app, stdout, _ := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })

	exitCode := app.runAPIRequest(apiRequestOptions{JSONOutput: true, Method: "get", Path: "/v1/memories?project_id=lore-cli"})
	if exitCode != 0 {
		t.Fatalf("runAPIRequest() exitCode = %d, want 0", exitCode)
	}
	if client.requestJSONToken != "secret-token" || client.requestJSONMethod != "GET" {
		t.Fatalf("requestJSON call = token=%q method=%q", client.requestJSONToken, client.requestJSONMethod)
	}
	if client.requestJSONPath != "/v1/memories?project_id=lore-cli" {
		t.Fatalf("requestJSON path = %q", client.requestJSONPath)
	}
	if got := stdout.String(); !strings.Contains(got, `"ok":true`) || !strings.Contains(got, `"request_id":"req-context"`) {
		t.Fatalf("stdout = %q, want success envelope", got)
	}
	assertNoTokenLeak(t, stdout.String(), "", "secret-token")
}

func TestUpdateServiceWiresProductionCandidateProbe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell probe fixture is unix-only")
	}

	store := &fakeStore{path: filepath.Join(t.TempDir(), "config.json")}
	app, _, _ := newTestApp(store, nil)

	svc, err := app.updateService()
	if err != nil {
		t.Fatalf("updateService() error = %v", err)
	}
	if svc.CandidateVersion == nil {
		t.Fatal("updateService().CandidateVersion = nil, want production post-install probe")
	}

	probePath := filepath.Join(t.TempDir(), "lore")
	if err := os.WriteFile(probePath, []byte("#!/bin/sh\nprintf '{\"version\":\"v1.2.3\",\"commit\":\"abc1234\",\"buildDate\":\"2026-05-20T00:00:00Z\"}'\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(probe fixture) error = %v", err)
	}

	got, err := svc.CandidateVersion(context.Background(), probePath)
	if err != nil {
		t.Fatalf("CandidateVersion() error = %v", err)
	}
	want := (version.Info{Version: "v1.2.3", Commit: "abc1234", BuildDate: "2026-05-20T00:00:00Z"}).Normalized()
	if got != want {
		t.Fatalf("CandidateVersion() = %+v, want %+v", got, want)
	}
}

func TestOpenCodeInstallPlanSummaryUsesBoundedTargetSpecificCopy(t *testing.T) {
	plan := install.InstallPlan{
		Layout: install.HarnessLayout{
			Target:       install.TargetOpenCode,
			RootDir:      "/home/user/.config/opencode",
			ManifestPath: "/home/user/.config/opencode/lore-install.json",
		},
		Components: []install.ComponentID{install.ComponentCorePack, install.ComponentExtendedSkills},
		Files: []install.PlanFileAction{
			{RelativePath: "AGENTS.md", Action: "create"},
			{RelativePath: "opencode.json", Action: "create"},
			{RelativePath: "skills/sdd-apply/SKILL.md", Action: "create"},
			{RelativePath: "lore-install.json", Action: "create"},
		},
	}

	summary := formatOpenCodeInstallPlanSummary(plan, true)
	for _, forbidden := range []string{"runtime=antigravity", "prompt=", "mcp_optional", "runtime=codex", "config.toml"} {
		if strings.Contains(summary, forbidden) {
			t.Fatalf("OpenCode plan summary should omit %q: %s", forbidden, summary)
		}
	}
	for _, want := range []string{"install_target=opencode", "scope=config-only", "settings_merge=lore-top-level-only", "commands=omitted", "managed_action=create:AGENTS.md", "managed_action=create:opencode.json", "mode=dry-run"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("OpenCode plan summary = %q, want substring %q", summary, want)
		}
	}
}

func TestOpenCodeInstallSummaryUsesBoundedTargetSpecificCopy(t *testing.T) {
	result := install.InstallResult{
		Target: install.TargetOpenCode,
		Layout: install.HarnessLayout{Target: install.TargetOpenCode, RootDir: "/home/user/.config/opencode", ManifestPath: "/home/user/.config/opencode/lore-install.json"},
		Manifest: install.Manifest{
			SchemaVersion: "1.0",
			Target:        install.TargetOpenCode,
			AuthMode:      "config-only",
			Components:    []install.ComponentID{install.ComponentCorePack},
			ManagedFiles: []install.ManagedFileRecord{
				{Path: "/home/user/.config/opencode/AGENTS.md", Component: install.ComponentCorePack, MergeMode: install.MergeModeReplace, ContentHash: "abc"},
				{Path: "/home/user/.config/opencode/opencode.json", Component: install.ComponentCorePack, MergeMode: install.MergeModeAdditiveJSON, ContentHash: "def"},
			},
		},
		Summary: install.InstallSummary{Created: []string{"/home/user/.config/opencode/AGENTS.md", "/home/user/.config/opencode/opencode.json"}},
	}

	summary := formatOpenCodeInstallSummary(result)
	for _, forbidden := range []string{"runtime=antigravity", "mcp_optional", "runtime=codex", "config.toml"} {
		if strings.Contains(summary, forbidden) {
			t.Fatalf("OpenCode apply summary should omit %q: %s", forbidden, summary)
		}
	}
	for _, want := range []string{"install_target=opencode", "scope=config-only", "settings_merge=lore-top-level-only", "commands=omitted", "plugins=none", "profiles=none", "mcp=none"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("OpenCode apply summary = %q, want substring %q", summary, want)
		}
	}
}

// TestCodexInstallPlanSummaryNoAntigravityRuntime verifies that Codex dry-run/apply
// summaries do NOT mention Antigravity runtime, prompt, or MCP false claims.
func TestCodexInstallPlanSummaryNoAntigravityRuntime(t *testing.T) {
	plan := install.InstallPlan{
		Layout: install.HarnessLayout{
			Target:       install.TargetCodex,
			RootDir:      "/home/user/.codex",
			ManifestPath: "/home/user/.codex/lore-install.json",
			Paths:        map[string]string{"agents_md": "/home/user/.codex/agents.md"},
		},
		Components: []install.ComponentID{install.ComponentCorePack, install.ComponentLoreServerMCP},
		Files: []install.PlanFileAction{
			{RelativePath: "agents.md", Action: "create"},
			{RelativePath: "config.toml", Action: "create"},
			{RelativePath: "lore-install.json", Action: "create"},
		},
	}

	// Dry-run summary
	dryRun := formatCodexInstallPlanSummary(plan, true)
	if strings.Contains(dryRun, "runtime=antigravity") {
		t.Errorf("dry-run summary should not contain Antigravity runtime: %s", dryRun)
	}
	if strings.Contains(dryRun, "prompt=") {
		t.Errorf("dry-run summary should not contain prompt field: %s", dryRun)
	}
	if strings.Contains(dryRun, "mcp_optional") {
		t.Errorf("dry-run summary should not contain mcp_optional: %s", dryRun)
	}
	if !strings.Contains(dryRun, "runtime=codex-remote-mcp") {
		t.Errorf("dry-run summary should contain runtime=codex-remote-mcp: %s", dryRun)
	}
	if !strings.Contains(dryRun, "install_target=codex") {
		t.Errorf("dry-run summary should contain install_target=codex: %s", dryRun)
	}
	if !strings.Contains(dryRun, "mode=dry-run") {
		t.Errorf("dry-run summary should contain mode=dry-run: %s", dryRun)
	}

	// Apply summary
	apply := formatCodexInstallPlanSummary(plan, false)
	if strings.Contains(apply, "runtime=antigravity") {
		t.Errorf("apply summary should not contain Antigravity runtime: %s", apply)
	}
	if strings.Contains(apply, "prompt=") {
		t.Errorf("apply summary should not contain prompt field: %s", apply)
	}
	if strings.Contains(apply, "mcp_optional") {
		t.Errorf("apply summary should not contain mcp_optional: %s", apply)
	}
	if strings.Contains(apply, "mode=dry-run") {
		t.Errorf("apply summary should not contain mode=dry-run: %s", apply)
	}
}

// TestCodexInstallSummaryNoAntigravityRuntime verifies that Codex apply results
// summaries do NOT mention Antigravity runtime, prompt, or MCP false claims.
func TestCodexInstallSummaryNoAntigravityRuntime(t *testing.T) {
	result := install.InstallResult{
		Target: install.TargetCodex,
		Layout: install.HarnessLayout{
			Target:       install.TargetCodex,
			RootDir:      "/home/user/.codex",
			ManifestPath: "/home/user/.codex/lore-install.json",
		},
		Manifest: install.Manifest{
			SchemaVersion: "1.0",
			Target:        install.TargetCodex,
			AuthMode:      "config-only",
			Components:    []install.ComponentID{install.ComponentCorePack, install.ComponentLoreServerMCP},
			ManagedFiles: []install.ManagedFileRecord{
				{Path: "/home/user/.codex/agents.md", Component: install.ComponentCorePack, MergeMode: install.MergeModeReplace, ContentHash: "abc"},
				{Path: "/home/user/.codex/config.toml", Component: install.ComponentLoreServerMCP, MergeMode: install.MergeModeReplace, ContentHash: "def"},
			},
		},
		Summary: install.InstallSummary{
			Created:    []string{"/home/user/.codex/agents.md", "/home/user/.codex/config.toml"},
			Updated:    nil,
			Deleted:    nil,
			Unchanged:  nil,
			BackedUp:   nil,
			Conflicted: nil,
			Failed:     nil,
		},
	}

	summary := formatCodexInstallSummary(result)
	if strings.Contains(summary, "runtime=antigravity") {
		t.Errorf("summary should not contain Antigravity runtime: %s", summary)
	}
	if strings.Contains(summary, "mcp_optional") {
		t.Errorf("summary should not contain mcp_optional: %s", summary)
	}
	if !strings.Contains(summary, "install_target=codex") {
		t.Errorf("summary should contain install_target=codex: %s", summary)
	}
	if !strings.Contains(summary, "runtime=codex-remote-mcp") {
		t.Errorf("summary should contain runtime=codex-remote-mcp: %s", summary)
	}
	if !strings.Contains(summary, "auth_mode=config-only") {
		t.Errorf("summary should contain auth_mode=config-only: %s", summary)
	}
	if !strings.Contains(summary, "mcp=remote") {
		t.Errorf("summary should contain mcp=remote: %s", summary)
	}
	if !strings.Contains(summary, "runner=none") {
		t.Errorf("summary should contain runner=none: %s", summary)
	}
	if !strings.Contains(summary, "bootstrap=none") {
		t.Errorf("summary should contain bootstrap=none: %s", summary)
	}
}

// TestCodexInstallPlanSummaryIncludesManagedActions verifies that Codex plan
// summaries honestly report managed file actions.
func TestCodexInstallPlanSummaryIncludesManagedActions(t *testing.T) {
	plan := install.InstallPlan{
		Layout: install.HarnessLayout{
			Target:       install.TargetCodex,
			RootDir:      "/home/user/.codex",
			ManifestPath: "/home/user/.codex/lore-install.json",
			Paths:        map[string]string{"agents_md": "/home/user/.codex/agents.md"},
		},
		Components: []install.ComponentID{install.ComponentCorePack, install.ComponentLoreServerMCP},
		Files: []install.PlanFileAction{
			{RelativePath: "agents.md", Action: "create"},
			{RelativePath: "config.toml", Action: "create"},
			{RelativePath: "skills/sdd-apply/SKILL.md", Action: "create"},
			{RelativePath: "lore-install.json", Action: "create"},
		},
	}

	summary := formatCodexInstallPlanSummary(plan, false)
	if !strings.Contains(summary, "managed_action=create:agents.md") {
		t.Errorf("summary should contain create:agents.md action: %s", summary)
	}
	if !strings.Contains(summary, "managed_action=create:config.toml") {
		t.Errorf("summary should contain create:config.toml action: %s", summary)
	}
	if !strings.Contains(summary, "managed_action=create:skills/sdd-apply/SKILL.md") {
		t.Errorf("summary should contain create action for skill file: %s", summary)
	}
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

func containsAll(value string, wants ...string) bool {
	for _, want := range wants {
		if !strings.Contains(value, want) {
			return false
		}
	}
	return true
}
