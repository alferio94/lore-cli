package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alferio94/lore-cli/internal/auth"
	"github.com/alferio94/lore-cli/internal/config"
	"github.com/alferio94/lore-cli/internal/httpclient"
	"github.com/alferio94/lore-cli/internal/install"
	"github.com/alferio94/lore-cli/internal/output"
)

// ActionMessage carries token-safe success output for interactive and CLI actions.
type ActionMessage struct {
	Summary string
}

// ActionReport carries token-safe diagnostic output for interactive and CLI actions.
type ActionReport struct {
	Title    string
	Checks   []output.Check
	ExitCode int
}

type rememberOptions struct {
	ProjectID    string
	Scope        string
	Type         string
	Title        string
	Content      string
	MetadataJSON string
	JSONOutput   bool
}

type recallOptions struct {
	ProjectID  string
	Scope      string
	Type       string
	Limit      int
	JSONOutput bool
}

type apiRequestOptions struct {
	JSONOutput bool
	Method     string
	Path       string
	BodyJSON   string
}

type apiMCPCallOptions struct {
	JSONOutput bool
	Tool       string
	ArgsJSON   string
}

type authConfigError struct {
	Code    string
	Message string
}

func (e *authConfigError) Error() string {
	if e == nil {
		return "saved login state is unavailable"
	}
	return e.Message
}

// InteractiveActions exposes reusable command behavior for the TUI.
type InteractiveActions struct {
	Login   func(ctx context.Context, serverURL, token string) (ActionMessage, error)
	Logout  func(ctx context.Context) (ActionMessage, error)
	Status  func(ctx context.Context) ActionReport
	Doctor  func(ctx context.Context) ActionReport
	Install func(ctx context.Context) ActionReport
}

// InteractiveActions returns the shared action set used by the CLI and future TUI.
func (a *App) InteractiveActions() InteractiveActions {
	return InteractiveActions{
		Login:   a.loginAction,
		Logout:  a.logoutAction,
		Status:  a.statusAction,
		Doctor:  a.doctorAction,
		Install: a.installAction,
	}
}

func (a *App) loginAction(ctx context.Context, serverURL, token string) (ActionMessage, error) {
	rawServer := strings.TrimSpace(serverURL)
	rawToken := strings.TrimSpace(token)

	client, err := a.ClientFactory(rawServer)
	if err != nil {
		return ActionMessage{}, err
	}
	subject, err := client.Me(ctx, rawToken)
	if err != nil {
		return ActionMessage{}, err
	}

	if err := a.authManager().Save(rawServer, rawToken); err != nil {
		return ActionMessage{}, explainAuthSaveError(err)
	}

	path, _ := a.Store.Path()
	return ActionMessage{Summary: output.FormatLoginSuccess(subject, path)}, nil
}

func (a *App) logoutAction(_ context.Context) (ActionMessage, error) {
	hadConfig := true
	if _, err := a.Store.Load(); err != nil {
		if errors.Is(err, config.ErrNotFound) {
			hadConfig = false
		} else {
			return ActionMessage{}, err
		}
	}
	if err := a.authManager().Logout(); err != nil {
		return ActionMessage{}, err
	}

	path, _ := a.Store.Path()
	return ActionMessage{Summary: output.FormatLogoutResult(path, hadConfig)}, nil
}

func (a *App) runRemember(args rememberOptions) error {
	client, session, err := a.loadAuthenticatedClient()
	if err != nil {
		return err
	}
	metadata, err := parseMetadataJSON(args.MetadataJSON)
	if err != nil {
		return err
	}
	memory, err := client.CreateMemory(context.Background(), session.Token, httpclient.CreateMemoryRequest{
		ProjectID: args.ProjectID,
		Scope:     defaultScope(args.Scope),
		Type:      args.Type,
		Title:     args.Title,
		Content:   args.Content,
		Metadata:  metadata,
	})
	if err != nil {
		return err
	}
	if args.JSONOutput {
		return writeJSON(a.Stdout, output.NewMemoryEnvelope(memory))
	}
	_, err = fmt.Fprintln(a.Stdout, output.FormatRememberSuccess(memory))
	return err
}

func (a *App) runRecall(args recallOptions) error {
	client, session, err := a.loadAuthenticatedClient()
	if err != nil {
		return err
	}
	memories, err := client.ListMemories(context.Background(), session.Token, httpclient.ListMemoriesFilter{
		ProjectID: args.ProjectID,
		Scope:     defaultScope(args.Scope),
		Type:      args.Type,
		Limit:     args.Limit,
	})
	if err != nil {
		return err
	}
	if args.JSONOutput {
		return writeJSON(a.Stdout, output.NewMemoriesEnvelope(memories))
	}
	_, err = fmt.Fprint(a.Stdout, output.FormatRecallResult(memories))
	return err
}

func (a *App) loadAuthenticatedClient() (httpclient.Client, auth.Session, error) {
	session, err := a.loadSavedAuthSession()
	if err != nil {
		return nil, auth.Session{}, err
	}
	client, err := a.ClientFactory(session.ServerURL)
	if err != nil {
		return nil, auth.Session{}, err
	}
	return client, session, nil
}

func (a *App) loadSavedAuthSession() (auth.Session, error) {
	session, err := a.authManager().Load()
	if err == nil {
		return session, nil
	}
	var authErr *auth.Error
	if errors.As(err, &authErr) {
		switch authErr.Code {
		case auth.ErrConfigNotFound:
			return auth.Session{}, &authConfigError{Code: "missing_config", Message: "no saved login state; run lore login --server <url> --token <token>"}
		case auth.ErrCredentialMissing:
			return auth.Session{}, &authConfigError{Code: "incomplete_config", Message: "saved login state is incomplete; run lore login --server <url> --token <token>"}
		case auth.ErrCredentialUnavailable:
			return auth.Session{}, &authConfigError{Code: "credential_unavailable", Message: unavailableCredentialMessage("saved login state could not access secure credential storage")}
		default:
			return auth.Session{}, &authConfigError{Code: "invalid_config", Message: "saved login state could not be read; inspect or remove the local config file and run lore login again"}
		}
	}
	return auth.Session{}, err
}

func (a *App) runAPIRequest(args apiRequestOptions) int {
	body := json.RawMessage(strings.TrimSpace(args.BodyJSON))
	if !args.JSONOutput {
		return a.writeBrokerError(2, 0, "invalid_request", "lore api request requires --json", "")
	}
	if _, err := httpclient.ValidateBrokerRequest(strings.ToUpper(strings.TrimSpace(args.Method)), strings.TrimSpace(args.Path), body); err != nil {
		return a.writeBrokerError(2, 400, "invalid_request", err.Error(), "")
	}

	client, session, err := a.loadAuthenticatedClient()
	if err != nil {
		var configErr *authConfigError
		if errors.As(err, &configErr) {
			return a.writeBrokerError(3, 0, configErr.Code, configErr.Message, "")
		}
		return a.writeBrokerError(6, 0, "internal_error", "failed to initialize authenticated client", "")
	}

	result, err := client.RequestJSON(context.Background(), strings.ToUpper(strings.TrimSpace(args.Method)), strings.TrimSpace(args.Path), session.Token, body)
	if err != nil {
		return a.writeBrokerRequestError(err)
	}
	if err := writeJSON(a.Stdout, map[string]any{"ok": true, "status": result.StatusCode, "request_id": result.RequestID, "data": json.RawMessage(result.Data)}); err != nil {
		return a.writeBrokerError(6, 0, "internal_error", "failed to encode broker response", "")
	}
	return 0
}

func (a *App) runAPIMCPCall(args apiMCPCallOptions) int {
	arguments := json.RawMessage(strings.TrimSpace(args.ArgsJSON))
	if _, _, err := httpclient.ValidateBrokerMCPCall(strings.TrimSpace(args.Tool), arguments); err != nil {
		return a.writeBrokerError(2, 400, "invalid_request", err.Error(), "")
	}

	client, session, err := a.loadAuthenticatedClient()
	if err != nil {
		var configErr *authConfigError
		if errors.As(err, &configErr) {
			return a.writeBrokerError(3, 0, configErr.Code, configErr.Message, "")
		}
		return a.writeBrokerError(6, 0, "internal_error", "failed to initialize authenticated client", "")
	}

	result, err := client.MCPCall(context.Background(), session.Token, strings.TrimSpace(args.Tool), arguments)
	if err != nil {
		return a.writeBrokerRequestError(err)
	}
	if args.JSONOutput {
		if err := writeJSON(a.Stdout, map[string]any{"ok": true, "status": result.StatusCode, "request_id": result.RequestID, "data": json.RawMessage(result.Data)}); err != nil {
			return a.writeBrokerError(6, 0, "internal_error", "failed to encode broker response", "")
		}
		return 0
	}
	fmt.Fprintln(a.Stdout, string(result.Data))
	return 0
}

func (a *App) writeBrokerRequestError(err error) int {
	var unauthorized *httpclient.UnauthorizedError
	if errors.As(err, &unauthorized) {
		return a.writeBrokerError(4, unauthorized.StatusCode, unauthorized.Code, unauthorized.Message, unauthorized.RequestID)
	}
	var apiErr *httpclient.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.Code
		if code == "" {
			code = "remote_error"
		}
		return a.writeBrokerError(5, apiErr.StatusCode, code, apiErr.Message, apiErr.RequestID)
	}
	var networkErr *httpclient.NetworkError
	if errors.As(err, &networkErr) {
		return a.writeBrokerError(5, 0, "network_error", "network request failed", "")
	}
	return a.writeBrokerError(2, 400, "invalid_request", err.Error(), "")
}

func (a *App) writeBrokerError(exitCode, status int, code, message, requestID string) int {
	_ = writeJSON(a.Stdout, map[string]any{"ok": false, "status": status, "code": code, "message": message, "request_id": requestID})
	return exitCode
}

func parseMetadataJSON(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("metadata-json must be valid JSON object: %w", err)
	}
	metadata, ok := decoded.(map[string]any)
	if !ok {
		return nil, errors.New("metadata-json must decode to a JSON object")
	}
	return metadata, nil
}

func defaultScope(scope string) string {
	if strings.TrimSpace(scope) == "" {
		return "project"
	}
	return strings.TrimSpace(scope)
}

func (a *App) statusAction(ctx context.Context) ActionReport {
	checks, exitCode := a.collectChecks(ctx, false)
	return ActionReport{Title: "Lore status", Checks: checks, ExitCode: exitCode}
}

func (a *App) doctorAction(ctx context.Context) ActionReport {
	checks, exitCode := a.collectChecks(ctx, true)
	return ActionReport{Title: "Lore doctor", Checks: checks, ExitCode: exitCode}
}

func (a *App) installAction(ctx context.Context) ActionReport {
	service := install.Service{Store: a.Store, Auth: a.authManager(), ClientFactory: install.ClientFactory(a.ClientFactory)}
	report := ActionReport{Title: "Lore install"}
	preflight := service.Preflight(ctx)
	report.Checks = append(report.Checks, preflight.Checks...)
	if !preflight.CanContinue {
		report.ExitCode = 1
		return report
	}

	homeDir, err := a.resolveUserHomeDir()
	if err != nil {
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusFail, Detail: err.Error(), Action: "Retry after HOME can be resolved for the current user."})
		report.ExitCode = 1
		return report
	}
	binaryPath, err := a.resolveExecutablePath()
	if err != nil {
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusFail, Detail: err.Error(), Action: "Retry from a normal Lore CLI binary context so the managed Pi manifest can record the CLI path."})
		report.ExitCode = 1
		return report
	}
	configPath, err := a.Store.Path()
	if err != nil {
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusFail, Detail: err.Error(), Action: "Fix the local config directory permissions or override LORE_CONFIG_DIR."})
		report.ExitCode = 1
		return report
	}

	result, err := service.InstallPi(install.PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      preflight.ServerURL,
		LoreBinaryPath: binaryPath,
		LoreConfigDir:  filepath.Dir(configPath),
		LoreCLIVersion: a.BuildInfo.Normalized().Version,
		SavedToken:     preflight.Token,
	})
	if err != nil {
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusFail, Detail: err.Error(), Action: "Inspect the Pi runtime directory and rerun lore install after fixing the reported issue."})
		report.ExitCode = 1
		return report
	}

	status := output.StatusOK
	if len(result.Summary.Failed) > 0 {
		status = output.StatusFail
		report.ExitCode = 1
	}
	report.Checks = append(report.Checks,
		output.Check{Name: "install", Status: status, Detail: formatInstallSummary(result)},
		output.Check{Name: "manifest", Status: output.StatusOK, Detail: fmt.Sprintf("verified %s auth_mode=%s managed_files=%d", result.Layout.ManifestPath, result.Manifest.AuthMode, len(result.Manifest.ManagedFiles))},
	)
	return report
}

func (a *App) collectChecks(ctx context.Context, includePi bool) ([]output.Check, int) {
	path, pathErr := a.Store.Path()
	if pathErr != nil {
		checks := []output.Check{{Name: "config-path", Status: output.StatusFail, Detail: pathErr.Error(), Action: "Fix the local config directory permissions or override LORE_CONFIG_DIR."}}
		if includePi {
			checks = append(checks, a.piCheck())
		}
		return checks, 1
	}

	cfg, err := a.Store.Load()
	if err != nil {
		if errors.Is(err, config.ErrNotFound) {
			checks := []output.Check{{Name: "config", Status: output.StatusWarn, Detail: fmt.Sprintf("no-config at %s", path), Action: "Run lore login --server <url> --token <token>."}}
			if includePi {
				checks = append(checks, a.piCheck())
			}
			return checks, 1
		}
		checks := []output.Check{{Name: "config", Status: output.StatusFail, Detail: err.Error(), Action: "Inspect or remove the local config file and log in again."}}
		if includePi {
			checks = append(checks, a.piCheck())
		}
		return checks, 1
	}

	checks := []output.Check{{Name: "config", Status: output.StatusOK, Detail: formatSavedLoginState(cfg, path)}}
	defaultAuthAction := "Run lore login again with a valid normal user API token."
	session, err := a.loadSavedAuthSession()
	if err != nil {
		action := defaultAuthAction
		var configErr *authConfigError
		if errors.As(err, &configErr) && configErr.Code == "credential_unavailable" {
			action = unavailableCredentialAction()
		}
		checks = append(checks, output.Check{Name: "auth", Status: output.StatusFail, Detail: err.Error(), Action: action})
		if includePi {
			checks = append(checks, a.piCheck())
		}
		return checks, 1
	}

	client, err := a.ClientFactory(session.ServerURL)
	if err != nil {
		checks = append(checks, output.Check{Name: "server-url", Status: output.StatusFail, Detail: err.Error(), Action: "Fix the server URL with lore login --server <http(s)://host> --token <token>."})
		if includePi {
			checks = append(checks, a.piCheck())
		}
		return checks, 1
	}

	exitCode := 0
	if err := client.Health(ctx); err != nil {
		checks = append(checks, output.Check{Name: "healthz", Status: output.StatusFail, Detail: explainEndpointError(err), Action: "Check server reachability and that the Lore API is running."})
		exitCode = 1
	} else {
		checks = append(checks, output.Check{Name: "healthz", Status: output.StatusOK, Detail: "server is live"})
	}

	if err := client.Ready(ctx); err != nil {
		checks = append(checks, output.Check{Name: "readyz", Status: output.StatusFail, Detail: explainEndpointError(err), Action: "Wait for the server to become ready or inspect server logs."})
		exitCode = 1
	} else {
		checks = append(checks, output.Check{Name: "readyz", Status: output.StatusOK, Detail: "server is ready"})
	}

	if subject, err := client.Me(ctx, session.Token); err != nil {
		checks = append(checks, output.Check{Name: "auth", Status: output.StatusFail, Detail: explainLoginError(err), Action: "Obtain a valid normal user API token and run lore login again."})
		exitCode = 1
	} else {
		checks = append(checks, output.Check{Name: "auth", Status: output.StatusOK, Detail: output.FormatSubject(subject)})
	}

	if includePi {
		pi := a.piCheck()
		checks = append(checks, pi)
		if pi.Status == output.StatusWarn && exitCode == 0 {
			exitCode = 1
		}
	}

	return checks, exitCode
}

func formatSavedLoginState(cfg config.Config, path string) string {
	return fmt.Sprintf("saved login state server=%s path=%s auth=OS keychain-backed metadata only", cfg.ServerURL, path)
}

func (a *App) authManager() AuthManager {
	if a.Auth != nil {
		return a.Auth
	}
	return auth.Manager{ConfigStore: a.Store}
}

func formatInstallSummary(result install.PiInstallResult) string {
	summary := fmt.Sprintf("target=%s created=%d updated=%d unchanged=%d backed_up=%d failed=%d", result.Manifest.Target, len(result.Summary.Created), len(result.Summary.Updated), len(result.Summary.Unchanged), len(result.Summary.BackedUp), len(result.Summary.Failed))
	if len(result.Summary.Failed) == 0 {
		return summary
	}
	return fmt.Sprintf("%s findings=%s", summary, strings.Join(result.Summary.Failed, "; "))
}

func (a *App) resolveUserHomeDir() (string, error) {
	if a.UserHomeDir != nil {
		return a.UserHomeDir()
	}
	return os.UserHomeDir()
}

func (a *App) resolveExecutablePath() (string, error) {
	if a.ExecutablePath != nil {
		return a.ExecutablePath()
	}
	return os.Executable()
}

func (a *App) piCheck() output.Check {
	if _, err := a.LookPath("pi"); err != nil {
		return output.Check{Name: "pi", Status: output.StatusWarn, Detail: "pi binary not found on PATH", Action: "Install Pi or add it to PATH if Pi automation is expected on this machine."}
	}
	return output.Check{Name: "pi", Status: output.StatusOK, Detail: "pi binary available on PATH"}
}

func explainLoginError(err error) string {
	var unauthorized *httpclient.UnauthorizedError
	if errors.As(err, &unauthorized) {
		return "normal user API token required; /v1/me rejected the provided token"
	}
	return explainEndpointError(err)
}

func explainAuthSaveError(err error) error {
	var authErr *auth.Error
	if errors.As(err, &authErr) && authErr.Code == auth.ErrCredentialUnavailable {
		return errors.New(unavailableCredentialMessage("could not store the API token in secure credential storage"))
	}
	return err
}

func unavailableCredentialMessage(prefix string) string {
	return fmt.Sprintf("%s; unlock or enable the OS keychain, and on headless Linux start a Secret Service session such as gnome-keyring, then run lore login again", prefix)
}

func unavailableCredentialAction() string {
	return "Unlock or enable the OS keychain, and on headless Linux start a Secret Service session such as gnome-keyring, then run lore login again."
}

func explainEndpointError(err error) string {
	var networkErr *httpclient.NetworkError
	if errors.As(err, &networkErr) {
		return "network request failed"
	}
	var readinessErr *httpclient.ReadinessError
	if errors.As(err, &readinessErr) {
		return readinessErr.Error()
	}
	var apiErr *httpclient.APIError
	if errors.As(err, &apiErr) {
		if apiErr.RequestID != "" {
			return fmt.Sprintf("%s (request_id=%s)", apiErr.Message, apiErr.RequestID)
		}
		return apiErr.Message
	}
	return err.Error()
}
