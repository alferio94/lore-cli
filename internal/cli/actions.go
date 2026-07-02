package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/term"

	"github.com/alferio94/lore-cli/internal/agentconfig"
	"github.com/alferio94/lore-cli/internal/auth"
	"github.com/alferio94/lore-cli/internal/config"
	"github.com/alferio94/lore-cli/internal/httpclient"
	"github.com/alferio94/lore-cli/internal/install"
	"github.com/alferio94/lore-cli/internal/output"
	cliupdate "github.com/alferio94/lore-cli/internal/update"
	"github.com/alferio94/lore-cli/internal/version"
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

type installCommandOptions struct {
	DryRun     bool
	Yes        bool
	Target     install.TargetID
	Components []install.ComponentID
}

type updateCommandOptions struct {
	DryRun bool
	Yes    bool
}

type UpdateAvailability struct {
	Checked        bool
	Available      bool
	CurrentVersion string
	LatestVersion  string
	Detail         string
}

type LoginInput struct {
	Mode      string
	ServerURL string
	Email     string
	Password  string
	Token     string
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
	Login            func(ctx context.Context, serverURL, token string) (ActionMessage, error)
	LoginWithInput   func(ctx context.Context, input LoginInput) (ActionMessage, error)
	Logout           func(ctx context.Context) (ActionMessage, error)
	Status           func(ctx context.Context) ActionReport
	Doctor           func(ctx context.Context) ActionReport
	Install          func(ctx context.Context) ActionReport
	InstallTarget    func(ctx context.Context, target install.TargetID) ActionReport
	PlanPiInstall    func(ctx context.Context) (install.PiInstallPlan, ActionReport, bool)
	ExecutePiInstall func(ctx context.Context, plan install.PiInstallPlan) ActionReport
	CheckForUpdate   func(ctx context.Context) UpdateAvailability
	Update           func(ctx context.Context) ActionReport
}

// InteractiveActions returns the shared action set used by the CLI and future TUI.
func (a *App) InteractiveActions() InteractiveActions {
	return InteractiveActions{
		Login:          a.loginAction,
		LoginWithInput: a.loginActionWithInput,
		Logout:         a.logoutAction,
		Status:         a.statusAction,
		Doctor:         a.doctorAction,
		Install:        a.installAction,
		InstallTarget: func(ctx context.Context, target install.TargetID) ActionReport {
			return a.installActionWithOptions(ctx, installCommandOptions{Target: target})
		},
		PlanPiInstall: func(ctx context.Context) (install.PiInstallPlan, ActionReport, bool) {
			return a.planPiInstallAction(ctx, installCommandOptions{})
		},
		ExecutePiInstall: a.executePiInstallAction,
		CheckForUpdate:   a.checkForUpdateAction,
		Update:           a.updateApplyAction,
	}
}

func (a *App) loginAction(ctx context.Context, serverURL, token string) (ActionMessage, error) {
	return a.loginActionWithInput(ctx, LoginInput{Mode: "token", ServerURL: serverURL, Token: token})
}

func (a *App) loginActionWithInput(ctx context.Context, input LoginInput) (ActionMessage, error) {
	rawServer := strings.TrimSpace(input.ServerURL)
	client, err := a.ClientFactory(rawServer)
	if err != nil {
		return ActionMessage{}, err
	}

	var token string
	switch strings.TrimSpace(input.Mode) {
	case "password":
		result, err := client.Login(ctx, strings.TrimSpace(input.Email), input.Password)
		if err != nil {
			return ActionMessage{}, err
		}
		token = result.Token
	case "", "token":
		token = strings.TrimSpace(input.Token)
	default:
		return ActionMessage{}, fmt.Errorf("unsupported login mode %q", input.Mode)
	}

	subject, err := client.Me(ctx, token)
	if err != nil {
		return ActionMessage{}, err
	}
	if err := a.authManager().Save(rawServer, token); err != nil {
		return ActionMessage{}, explainAuthSaveError(err)
	}

	path, _ := a.Store.Path()
	return ActionMessage{Summary: output.FormatLoginSuccess(subject, path)}, nil
}

func (a *App) readLoginPassword(fromStdin bool) (string, error) {
	if fromStdin {
		reader := a.Stdin
		if reader == nil {
			reader = os.Stdin
		}
		raw, err := io.ReadAll(io.LimitReader(reader, 1<<20))
		if err != nil {
			return "", fmt.Errorf("read password from stdin: %w", err)
		}
		password := strings.TrimRight(string(raw), "\r\n")
		if password == "" {
			return "", errors.New("password is required")
		}
		return password, nil
	}

	if a.PasswordPrompt != nil {
		password, err := a.PasswordPrompt()
		if err != nil {
			return "", err
		}
		password = strings.TrimRight(password, "\r\n")
		if password == "" {
			return "", errors.New("password is required")
		}
		return password, nil
	}

	stdinFile := os.Stdin
	if stdinFile == nil || !term.IsTerminal(stdinFile.Fd()) {
		return "", errors.New("safe password input unavailable; use --password-stdin or lore login --server <url> --token <token>")
	}
	if _, err := fmt.Fprint(a.Stderr, "Password: "); err != nil {
		return "", err
	}
	secret, err := term.ReadPassword(stdinFile.Fd())
	if _, newlineErr := fmt.Fprintln(a.Stderr); newlineErr != nil && err == nil {
		err = newlineErr
	}
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	password := strings.TrimRight(string(secret), "\r\n")
	if password == "" {
		return "", errors.New("password is required")
	}
	return password, nil
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
			return auth.Session{}, &authConfigError{Code: "missing_config", Message: "no saved login state; run lore login --server <url> --email <email> for password login or use lore login --server <url> --token <token> for compatibility mode"}
		case auth.ErrCredentialMissing:
			return auth.Session{}, &authConfigError{Code: "incomplete_config", Message: "saved login state is incomplete; run lore login again with password login or a valid compatibility token"}
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
	return a.installActionWithOptions(ctx, installCommandOptions{})
}

func (a *App) checkForUpdateAction(ctx context.Context) UpdateAvailability {
	current := a.BuildInfo.Normalized()
	execPath, err := a.resolveExecutablePath()
	if err != nil {
		return UpdateAvailability{Checked: true, CurrentVersion: current.Version, Detail: fmt.Sprintf("Update check unavailable: %v", err)}
	}

	pathPath := execPath
	if a.LookPath != nil {
		if lookedUp, lookErr := a.LookPath("lore"); lookErr == nil && strings.TrimSpace(lookedUp) != "" {
			pathPath = lookedUp
		}
	}
	if reason, unsafe := localUpdateSafetyReason(current.Version, execPath, pathPath); unsafe {
		return UpdateAvailability{Checked: true, CurrentVersion: current.Version, Detail: fmt.Sprintf("Update check unavailable: %s. Pi runtime and ~/.pi remain untouched.", reason)}
	}

	svc, err := a.updateService()
	if err != nil {
		return UpdateAvailability{Checked: true, CurrentVersion: current.Version, Detail: fmt.Sprintf("Update check unavailable: %v", err)}
	}
	plan, err := svc.Check(ctx, cliupdate.CheckOptions{})
	if err != nil {
		return UpdateAvailability{Checked: true, CurrentVersion: current.Version, Detail: fmt.Sprintf("Update check unavailable: %s", explainEndpointError(err))}
	}
	info := UpdateAvailability{Checked: true, CurrentVersion: plan.Current.Version, LatestVersion: fallbackUpdateValue(plan.LatestTag, plan.Current.Version)}
	if plan.Target.Status != cliupdate.TargetStatusOK || plan.Status == cliupdate.StatusDevBuild || plan.Status == cliupdate.StatusUnsupported {
		info.Detail = updatePlanAction(plan)
		return info
	}
	if plan.Status == cliupdate.StatusAvailable {
		info.Available = true
		info.Detail = fmt.Sprintf("Binary-only update available: %s → %s. Pi runtime and ~/.pi remain untouched.", plan.Current.Version, plan.LatestTag)
		return info
	}
	info.Detail = fmt.Sprintf("Lore CLI is current at %s. Pi runtime and ~/.pi remain untouched.", plan.Current.Version)
	return info
}

func (a *App) updateApplyAction(ctx context.Context) ActionReport {
	return a.updateActionWithOptions(ctx, updateCommandOptions{Yes: true})
}

func (a *App) updateActionWithOptions(ctx context.Context, opts updateCommandOptions) ActionReport {
	report := ActionReport{Title: "Lore update"}
	report.Checks = append(report.Checks, output.Check{Name: "scope", Status: output.StatusOK, Detail: "updates only the Lore CLI binary; Pi runtime and ~/.pi remain untouched"})

	current := a.BuildInfo.Normalized()
	execPath, err := a.resolveExecutablePath()
	if err != nil {
		report.Checks = append(report.Checks, output.Check{Name: "update", Status: output.StatusFail, Detail: fmt.Sprintf("current=%s latest=unresolved target=unknown: %v", current.Version, err), Action: "Retry from a normal Lore CLI binary context so the update target can be resolved safely."})
		report.ExitCode = 1
		return report
	}

	pathPath := execPath
	if a.LookPath != nil {
		if lookedUp, lookErr := a.LookPath("lore"); lookErr == nil && strings.TrimSpace(lookedUp) != "" {
			pathPath = lookedUp
		}
	}

	if reason, unsafe := localUpdateSafetyReason(current.Version, execPath, pathPath); unsafe {
		mode := "check"
		if opts.DryRun {
			mode = "dry-run"
		} else if opts.Yes {
			mode = "yes"
		}
		report.Checks = append(report.Checks, output.Check{Name: "update", Status: output.StatusFail, Detail: fmt.Sprintf("mode=%s current=%s latest=unresolved target=%s unsafe=%s", mode, current.Version, execPath, reason), Action: "Install a released Lore CLI binary on PATH and rerun lore update after the unsafe target is resolved."})
		report.ExitCode = 1
		return report
	}

	svc, err := a.updateService()
	if err != nil {
		report.Checks = append(report.Checks, output.Check{Name: "update", Status: output.StatusFail, Detail: fmt.Sprintf("current=%s latest=unresolved target=%s: %v", current.Version, execPath, err), Action: "Fix the local Lore config directory so update cache metadata can be stored safely."})
		report.ExitCode = 1
		return report
	}

	plan, err := svc.Check(ctx, cliupdate.CheckOptions{})
	if err != nil {
		report.Checks = append(report.Checks, output.Check{Name: "update", Status: output.StatusFail, Detail: fmt.Sprintf("current=%s latest=unresolved target=%s: %s", current.Version, execPath, explainEndpointError(err)), Action: "Retry later or verify GitHub release availability and local binary permissions."})
		report.ExitCode = 1
		return report
	}

	planDetail := formatUpdatePlanSummary(plan, opts.DryRun)
	if plan.Target.Status != cliupdate.TargetStatusOK || plan.Status == cliupdate.StatusDevBuild || plan.Status == cliupdate.StatusUnsupported {
		report.Checks = append(report.Checks, output.Check{Name: "update", Status: output.StatusFail, Detail: planDetail, Action: updatePlanAction(plan)})
		report.ExitCode = 1
		return report
	}
	if opts.DryRun || plan.Status == cliupdate.StatusUpToDate {
		report.Checks = append(report.Checks, output.Check{Name: "update", Status: output.StatusOK, Detail: planDetail})
		return report
	}

	if !opts.Yes {
		confirmed, confirmErr := a.confirmBinaryUpdate(plan)
		if confirmErr != nil {
			report.Checks = append(report.Checks, output.Check{Name: "update", Status: output.StatusFail, Detail: fmt.Sprintf("current=%s latest=%s target=%s: %v", plan.Current.Version, plan.LatestTag, plan.Target.ExecutablePath, confirmErr), Action: "Re-run lore update --yes to skip the interactive prompt once you trust the target binary path."})
			report.ExitCode = 1
			return report
		}
		if !confirmed {
			report.Checks = append(report.Checks, output.Check{Name: "update", Status: output.StatusWarn, Detail: fmt.Sprintf("current=%s latest=%s target=%s update cancelled before mutation", plan.Current.Version, plan.LatestTag, plan.Target.ExecutablePath)})
			return report
		}
	}

	result, err := svc.Apply(ctx, plan)
	if err != nil {
		report.Checks = append(report.Checks, output.Check{Name: "update", Status: output.StatusFail, Detail: fmt.Sprintf("current=%s latest=%s target=%s: %s", plan.Current.Version, plan.LatestTag, plan.Target.ExecutablePath, explainEndpointError(err)), Action: "Inspect the reported target path, keep the current binary, and retry only after the failure cause is understood."})
		report.ExitCode = 1
		return report
	}

	status := output.StatusOK
	if result.Status == cliupdate.ResultStatusUnsupported {
		status = output.StatusFail
		report.ExitCode = 1
	}
	detail := formatUpdateResultSummary(plan, result)
	action := ""
	if result.ManualRecovery != "" {
		action = result.ManualRecovery
	}
	report.Checks = append(report.Checks, output.Check{Name: "update", Status: status, Detail: detail, Action: action})
	return report
}

func (a *App) installActionWithOptions(ctx context.Context, opts installCommandOptions) ActionReport {
	selectedTarget, err := install.ResolveInstallTarget(opts.Target)
	if err == nil && selectedTarget.ID == install.TargetCodex {
		return a.installCodexActionWithOptions(ctx, opts)
	}
	if err == nil && selectedTarget.ID == install.TargetOpenCode {
		return a.installOpenCodeActionWithOptions(ctx, opts)
	}
	if err == nil && selectedTarget.ID == install.TargetAntigravity {
		return a.installAntigravityActionWithOptions(ctx, opts)
	}

	plan, report, ok := a.planPiInstallAction(ctx, opts)
	if !ok {
		return report
	}
	if opts.DryRun {
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusOK, Detail: formatInstallPlanSummary(plan, true)})
		return report
	}
	if plan.ExistingPi.Exists && plan.FullBackup != nil && !opts.Yes {
		includeBackup, promptErr := a.confirmFullBackup(plan)
		if promptErr != nil {
			report.Checks = append(report.Checks, output.Check{Name: "full-backup", Status: output.StatusFail, Detail: promptErr.Error(), Action: "Re-run lore install with --yes to accept the safe backup default non-interactively."})
			report.ExitCode = 1
			return report
		}
		if !includeBackup {
			plan.FullBackup = nil
		}
	}
	execReport := a.executePiInstallAction(ctx, plan)
	execReport.Checks = append(report.Checks, execReport.Checks...)
	if execReport.ExitCode == 0 && report.ExitCode != 0 {
		execReport.ExitCode = report.ExitCode
	}
	return execReport
}

func (a *App) installAntigravityActionWithOptions(ctx context.Context, opts installCommandOptions) ActionReport {
	service := install.Service{Store: a.Store, Auth: a.authManager(), ClientFactory: install.ClientFactory(a.ClientFactory), AgentConfigStore: a.AgentConfigStore}
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
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusFail, Detail: err.Error(), Action: "Retry from a normal Lore CLI binary context so the managed manifest can record the CLI path."})
		report.ExitCode = 1
		return report
	}
	configPath, err := a.Store.Path()
	if err != nil {
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusFail, Detail: err.Error(), Action: "Fix the local config directory permissions or override LORE_CONFIG_DIR."})
		report.ExitCode = 1
		return report
	}

	plan, err := service.PlanAntigravityInstall(install.InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      preflight.ServerURL,
		SavedToken:     preflight.Token,
		LoreBinaryPath: binaryPath,
		LoreConfigDir:  filepath.Dir(configPath),
		LoreCLIVersion: a.BuildInfo.Normalized().Version,
		Target:         install.TargetAntigravity,
		Components:     append([]install.ComponentID(nil), opts.Components...),
	})
	if err != nil {
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusFail, Detail: err.Error(), Action: "Inspect the requested Antigravity components and rerun lore install after fixing the reported issue."})
		report.ExitCode = 1
		return report
	}
	if opts.DryRun {
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusOK, Detail: formatSharedInstallPlanSummary(plan, true)})
		return report
	}
	result, err := service.ExecuteAntigravityInstall(plan, install.InstallCommandOptions{})
	if err != nil {
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusFail, Detail: err.Error(), Action: "Inspect the Antigravity runtime directory and rerun lore install after fixing the reported issue."})
		report.ExitCode = 1
		return report
	}
	status := output.StatusOK
	if len(result.Summary.Failed) > 0 {
		status = output.StatusFail
		report.ExitCode = 1
	}
	report.Checks = append(report.Checks,
		output.Check{Name: "install", Status: status, Detail: formatSharedInstallSummary(result)},
	)
	if antigravityMCPInstalled(result.Manifest.Components) {
		detail := fmt.Sprintf("managed MCP config path=%s server_url=%s auth_header=plaintext-bearer-token", result.Layout.Paths["mcp_config"], preflight.ServerURL)
		report.Checks = append(report.Checks, output.Check{Name: "mcp-config", Status: output.StatusWarn, Detail: detail, Action: "Rerun lore install after lore login if the saved session changes or if the Lore server URL changes."})
	}
	report.Checks = append(report.Checks,
		output.Check{Name: "manifest", Status: output.StatusOK, Detail: fmt.Sprintf("verified %s auth_mode=%s managed_files=%d managed_overlays=%d", result.Layout.ManifestPath, result.Manifest.AuthMode, len(result.Manifest.ManagedFiles), len(result.Manifest.ManagedAgentOverlays))},
	)
	return report
}

func (a *App) installCodexActionWithOptions(ctx context.Context, opts installCommandOptions) ActionReport {
	service := install.Service{Store: a.Store, Auth: a.authManager(), ClientFactory: install.ClientFactory(a.ClientFactory), AgentConfigStore: a.AgentConfigStore}
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
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusFail, Detail: err.Error(), Action: "Retry from a normal Lore CLI binary context so the managed manifest can record the CLI path."})
		report.ExitCode = 1
		return report
	}
	configPath, err := a.Store.Path()
	if err != nil {
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusFail, Detail: err.Error(), Action: "Fix the local config directory permissions or override LORE_CONFIG_DIR."})
		report.ExitCode = 1
		return report
	}

	plan, err := service.PlanCodexInstall(install.InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      preflight.ServerURL,
		SavedToken:     preflight.Token,
		LoreBinaryPath: binaryPath,
		LoreConfigDir:  filepath.Dir(configPath),
		LoreCLIVersion: a.BuildInfo.Normalized().Version,
		Target:         install.TargetCodex,
		Components:     append([]install.ComponentID(nil), opts.Components...),
	})
	if err != nil {
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusFail, Detail: err.Error(), Action: "Inspect the requested Codex components and rerun lore install after fixing the reported issue."})
		report.ExitCode = 1
		return report
	}
	if opts.DryRun {
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusOK, Detail: formatSharedInstallPlanSummary(plan, true)})
		return report
	}
	result, err := service.ExecuteCodexInstall(plan, install.InstallCommandOptions{})
	if err != nil {
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusFail, Detail: err.Error(), Action: "Inspect the Codex runtime directory and rerun lore install after fixing the reported issue."})
		report.ExitCode = 1
		return report
	}
	status := output.StatusOK
	if len(result.Summary.Failed) > 0 {
		status = output.StatusFail
		report.ExitCode = 1
	}
	report.Checks = append(report.Checks,
		output.Check{Name: "install", Status: status, Detail: formatSharedInstallSummary(result)},
	)
	report.Checks = append(report.Checks,
		output.Check{Name: "manifest", Status: output.StatusOK, Detail: fmt.Sprintf("verified %s auth_mode=%s managed_files=%d", result.Layout.ManifestPath, result.Manifest.AuthMode, len(result.Manifest.ManagedFiles))},
	)
	report.Checks = append(report.Checks,
		output.Check{Name: "codex-config", Status: output.StatusWarn, Detail: fmt.Sprintf("managed MCP config path=%s server_url=%s auth_header=plaintext-bearer-token; Lore also manages ~/.codex/AGENTS.md and ~/.codex/skills/.", result.Layout.Paths["config_toml"], preflight.ServerURL), Action: "Rerun lore install after lore login if the saved session changes or if the Lore server URL changes."},
	)
	return report
}

// installOpenCodeActionWithOptions is the foundation-slice entrypoint
// for `lore install --target opencode`. It runs the same preflight as
// the other shared targets, then delegates to PlanOpenCodeInstall /
// ExecuteOpenCodeInstall and surfaces the bounded foundation summary.
// Negative regression gates and the additive merge path are layered on
// top in a later slice.
func (a *App) installOpenCodeActionWithOptions(ctx context.Context, opts installCommandOptions) ActionReport {
	service := install.Service{Store: a.Store, Auth: a.authManager(), ClientFactory: install.ClientFactory(a.ClientFactory), AgentConfigStore: a.AgentConfigStore}
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
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusFail, Detail: err.Error(), Action: "Retry from a normal Lore CLI binary context so the managed manifest can record the CLI path."})
		report.ExitCode = 1
		return report
	}
	configPath, err := a.Store.Path()
	if err != nil {
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusFail, Detail: err.Error(), Action: "Fix the local config directory permissions or override LORE_CONFIG_DIR."})
		report.ExitCode = 1
		return report
	}

	plan, err := service.PlanOpenCodeInstall(install.InstallRequest{
		HomeDir:        homeDir,
		ServerURL:      preflight.ServerURL,
		SavedToken:     preflight.Token,
		LoreBinaryPath: binaryPath,
		LoreConfigDir:  filepath.Dir(configPath),
		LoreCLIVersion: a.BuildInfo.Normalized().Version,
		Target:         install.TargetOpenCode,
		Components:     append([]install.ComponentID(nil), opts.Components...),
	})
	if err != nil {
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusFail, Detail: err.Error(), Action: "Inspect the requested OpenCode components and rerun lore install after fixing the reported issue."})
		report.ExitCode = 1
		return report
	}
	if opts.DryRun {
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusOK, Detail: formatSharedInstallPlanSummary(plan, true)})
		return report
	}
	result, err := service.ExecuteOpenCodeInstall(plan, install.InstallCommandOptions{})
	if err != nil {
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusFail, Detail: err.Error(), Action: "Inspect the OpenCode runtime directory and rerun lore install after fixing the reported issue."})
		report.ExitCode = 1
		return report
	}
	status := output.StatusOK
	if len(result.Summary.Failed) > 0 {
		status = output.StatusFail
		report.ExitCode = 1
	}
	report.Checks = append(report.Checks,
		output.Check{Name: "install", Status: status, Detail: formatSharedInstallSummary(result)},
	)
	report.Checks = append(report.Checks,
		output.Check{Name: "manifest", Status: output.StatusOK, Detail: fmt.Sprintf("verified %s auth_mode=%s managed_files=%d", result.Layout.ManifestPath, result.Manifest.AuthMode, len(result.Manifest.ManagedFiles))},
	)
	if openCodeMCPInstalled(result.Manifest.Components) {
		detail := fmt.Sprintf("managed opencode.json path=%s server_url=%s auth_header=plaintext-bearer-token (saved token written under mcp.lore.headers.Authorization; the install summary never embeds the saved token)", result.Layout.Paths["opencode_json"], preflight.ServerURL)
		report.Checks = append(report.Checks, output.Check{Name: "opencode-config", Status: output.StatusWarn, Detail: detail, Action: "Rerun lore install after lore login if the saved session changes or if the Lore server URL changes."})
	}
	return report
}

func openCodeMCPInstalled(components []install.ComponentID) bool {
	for _, component := range components {
		if component == install.ComponentLoreServerMCP {
			return true
		}
	}
	return false
}

func (a *App) planPiInstallAction(ctx context.Context, opts installCommandOptions) (install.PiInstallPlan, ActionReport, bool) {
	service := install.Service{Store: a.Store, Auth: a.authManager(), ClientFactory: install.ClientFactory(a.ClientFactory), AgentConfigStore: a.AgentConfigStore}
	report := ActionReport{Title: "Lore install"}
	preflight := service.Preflight(ctx)
	report.Checks = append(report.Checks, preflight.Checks...)
	if !preflight.CanContinue {
		report.ExitCode = 1
		return install.PiInstallPlan{}, report, false
	}

	selectedTarget, err := install.ResolveInstallTarget(opts.Target)
	if err != nil {
		report.Checks = append(report.Checks, output.Check{Name: "install-target", Status: output.StatusFail, Detail: err.Error(), Action: install.FormatTargetSelection(install.DefaultTargets())})
		report.ExitCode = 1
		return install.PiInstallPlan{}, report, false
	}

	homeDir, err := a.resolveUserHomeDir()
	if err != nil {
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusFail, Detail: err.Error(), Action: "Retry after HOME can be resolved for the current user."})
		report.ExitCode = 1
		return install.PiInstallPlan{}, report, false
	}
	binaryPath, err := a.resolveExecutablePath()
	if err != nil {
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusFail, Detail: err.Error(), Action: "Retry from a normal Lore CLI binary context so the managed Pi manifest can record the CLI path."})
		report.ExitCode = 1
		return install.PiInstallPlan{}, report, false
	}
	configPath, err := a.Store.Path()
	if err != nil {
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusFail, Detail: err.Error(), Action: "Fix the local config directory permissions or override LORE_CONFIG_DIR."})
		report.ExitCode = 1
		return install.PiInstallPlan{}, report, false
	}

	req := install.PiInstallRequest{
		HomeDir:        homeDir,
		ServerURL:      preflight.ServerURL,
		LoreBinaryPath: binaryPath,
		LoreConfigDir:  filepath.Dir(configPath),
		LoreCLIVersion: a.BuildInfo.Normalized().Version,
		SavedToken:     preflight.Token,
		Target:         selectedTarget.ID,
		Components:     append([]install.ComponentID(nil), opts.Components...),
	}
	plan, err := service.PlanPiInstall(req)
	if err != nil {
		action := "Keep Pi on the native Lore extensions path for now; Pi MCP remains disabled by default. Inspect the requested target/components and rerun lore install after fixing the reported issue."
		if selectedTarget.ID == install.TargetAntigravity {
			action = "Pi remains the default recommended path today. Antigravity stays prompt + skills first, keeps MCP optional, does not emulate Pi overlays, and will need the generic target-driven install flow before CLI apply can proceed."
		}
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusFail, Detail: err.Error(), Action: action})
		report.ExitCode = 1
		return install.PiInstallPlan{}, report, false
	}
	return plan, report, true
}

func (a *App) executePiInstallAction(ctx context.Context, plan install.PiInstallPlan) ActionReport {
	service := install.Service{Store: a.Store, Auth: a.authManager(), ClientFactory: install.ClientFactory(a.ClientFactory), AgentConfigStore: a.AgentConfigStore}
	report := ActionReport{Title: "Lore install"}
	result, err := service.ExecutePiInstall(plan, install.InstallCommandOptions{})
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
	)
	if result.FullBackup != nil {
		report.Checks = append(report.Checks, output.Check{Name: "full-backup", Status: output.StatusOK, Detail: fmt.Sprintf("created path=%s manifest=%s entries=%d files=%d dirs=%d symlinks=%d", result.FullBackup.BackupPath, result.FullBackup.ManifestPath, result.FullBackup.EntriesCopied, result.FullBackup.FilesCopied, result.FullBackup.DirsCopied, result.FullBackup.SymlinksCopied), Action: fmt.Sprintf("To restore, move %s aside, copy %s back to %s, then inspect %s for the captured snapshot and metadata.", result.Layout.PiDir, result.FullBackup.BackupPath, result.Layout.PiDir, result.FullBackup.ManifestPath)})
	}
	report.Checks = append(report.Checks, output.Check{Name: "manifest", Status: output.StatusOK, Detail: fmt.Sprintf("verified %s auth_mode=%s managed_files=%d full_pi_backup=%t", result.Layout.ManifestPath, result.Manifest.AuthMode, len(result.Manifest.ManagedFiles), result.Manifest.FullPiBackup != nil)})
	return report
}

func (a *App) collectChecks(ctx context.Context, includePi bool) ([]output.Check, int) {
	path, pathErr := a.Store.Path()
	if pathErr != nil {
		checks := []output.Check{{Name: "config-path", Status: output.StatusFail, Detail: pathErr.Error(), Action: "Fix the local config directory permissions or override LORE_CONFIG_DIR."}}
		if includePi {
			checks = append(checks, a.piCheck())
			checks = append(checks, a.openCodeDoctorChecks()...)
		}
		return checks, 1
	}

	cfg, err := a.Store.Load()
	if err != nil {
		if errors.Is(err, config.ErrNotFound) {
			checks := []output.Check{{Name: "config", Status: output.StatusWarn, Detail: fmt.Sprintf("no-config at %s", path), Action: "Run lore login --server <url> --email <email> for password login, or use --token for compatibility mode."}}
			if includePi {
				checks = append(checks, a.piCheck())
				checks = append(checks, a.openCodeDoctorChecks()...)
			}
			return checks, 1
		}
		checks := []output.Check{{Name: "config", Status: output.StatusFail, Detail: err.Error(), Action: "Inspect or remove the local config file and log in again."}}
		if includePi {
			checks = append(checks, a.piCheck())
			checks = append(checks, a.openCodeDoctorChecks()...)
		}
		return checks, 1
	}

	checks := []output.Check{{Name: "config", Status: output.StatusOK, Detail: formatSavedLoginState(cfg, path)}}
	defaultAuthAction := "Run lore login again with password login or a valid compatibility token."
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
			checks = append(checks, a.openCodeDoctorChecks()...)
		}
		return checks, 1
	}

	client, err := a.ClientFactory(session.ServerURL)
	if err != nil {
		checks = append(checks, output.Check{Name: "server-url", Status: output.StatusFail, Detail: err.Error(), Action: "Fix the server URL with lore login --server <http(s)://host> --email <email> for password login, or use --token for compatibility mode."})
		if includePi {
			checks = append(checks, a.piCheck())
			checks = append(checks, a.openCodeDoctorChecks()...)
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
		checks = append(checks, output.Check{Name: "auth", Status: output.StatusFail, Detail: explainLoginError(err), Action: "Obtain a valid password-login session or compatibility token and run lore login again."})
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
		opencodeChecks := a.openCodeDoctorChecks()
		checks = append(checks, opencodeChecks...)
		if checksContainActionableOpenCodeFinding(opencodeChecks) {
			exitCode = 1
		}
	}

	// Read-only agent-config check in status and doctor; does not imply Codex execution support.
	checks = append(checks, a.agentConfigCheck())

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
	summary := fmt.Sprintf("install_target=%s runtime=pi-remote-package remote_package=%s components=%s managed_local_files=%d project_agents=%s created=%d updated=%d deleted=%d unchanged=%d backed_up=%d conflicted=%d failed=%d", result.Manifest.Target, installPiRemotePackage(), formatComponentIDs(result.Manifest.Components), len(result.Manifest.ManagedFiles), formatProjectAgentsPolicy(), len(result.Summary.Created), len(result.Summary.Updated), len(result.Summary.Deleted), len(result.Summary.Unchanged), len(result.Summary.BackedUp), len(result.Summary.Conflicted), len(result.Summary.Failed))
	parts := append([]string{summary}, formatManagedFileSummaryParts(result.Summary.Created, "create")...)
	parts = append(parts, formatManagedFileSummaryParts(result.Summary.Updated, "update")...)
	parts = append(parts, formatManagedFileSummaryParts(result.Summary.Deleted, "delete")...)
	parts = append(parts, formatManagedFileSummaryParts(result.Summary.Unchanged, "unchanged")...)
	parts = append(parts, formatManagedFileSummaryParts(result.Summary.Conflicted, "conflict")...)
	if len(result.Summary.Failed) > 0 {
		parts = append(parts, fmt.Sprintf("findings=%s", strings.Join(result.Summary.Failed, "; ")))
	}
	return strings.Join(parts, " ")
}

func formatSharedInstallSummary(result install.InstallResult) string {
	if result.Target == install.TargetPi {
		return fmt.Sprintf("install_target=%s runtime=pi-remote-package", result.Target)
	}
	if result.Target == install.TargetCodex {
		return formatCodexInstallSummary(result)
	}
	if result.Target == install.TargetOpenCode {
		return formatOpenCodeInstallSummary(result)
	}
	return formatAntigravityInstallSummary(result)
}

func formatInstallPlanSummary(plan install.PiInstallPlan, dryRun bool) string {
	parts := []string{
		fmt.Sprintf("install_target=%s", plan.Request.Target),
		"runtime=pi-remote-package",
		fmt.Sprintf("remote_package=%s", installPiRemotePackage()),
		fmt.Sprintf("components=%s", formatComponentIDs(plan.Request.Components)),
		fmt.Sprintf("target=%s", plan.Layout.AgentDir),
		fmt.Sprintf("manifest=%s", plan.Layout.ManifestPath),
		fmt.Sprintf("managed_backup_root=%s", plan.ManagedBackupRoot),
		fmt.Sprintf("managed_local_files=%d", len(plan.Layout.ManagedFiles)),
		fmt.Sprintf("project_agents=%s", formatProjectAgentsPolicy()),
	}
	if dryRun {
		parts = append(parts, "mode=dry-run")
	}
	if plan.ExistingPi.Exists {
		parts = append(parts, fmt.Sprintf("existing_pi=%s", plan.ExistingPi.Path), fmt.Sprintf("existing_pi_kind=%s", plan.ExistingPi.Kind))
		if plan.FullBackup != nil {
			parts = append(parts, fmt.Sprintf("full_backup=%s", plan.FullBackup.BackupPath), fmt.Sprintf("full_backup_manifest=%s", plan.FullBackup.ManifestPath))
		} else {
			parts = append(parts, "full_backup=declined")
		}
	} else {
		parts = append(parts, "existing_pi=missing", "full_backup=not-needed")
	}
	for _, action := range plan.ManagedFileActions {
		parts = append(parts, fmt.Sprintf("managed_action=%s:%s", action.Action, action.RelativePath))
	}
	parts = append(parts, formatManagedFileSummaryParts(plan.ManagedAgentConflicts, "conflict")...)
	return strings.Join(parts, " ")
}

func formatSharedInstallPlanSummary(plan install.InstallPlan, dryRun bool) string {
	if plan.Layout.Target == install.TargetPi {
		return fmt.Sprintf("install_target=%s runtime=pi-remote-package", plan.Layout.Target)
	}
	if plan.Layout.Target == install.TargetCodex {
		return formatCodexInstallPlanSummary(plan, dryRun)
	}
	if plan.Layout.Target == install.TargetOpenCode {
		return formatOpenCodeInstallPlanSummary(plan, dryRun)
	}
	return formatAntigravityInstallPlanSummary(plan, dryRun)
}

func antigravityMCPInstalled(components []install.ComponentID) bool {
	for _, component := range components {
		if component == install.ComponentLoreServerMCP {
			return true
		}
	}
	return false
}

// formatCodexInstallSummary produces the Codex-specific dry-run/apply summary.
func formatCodexInstallSummary(result install.InstallResult) string {
	summary := fmt.Sprintf("install_target=%s runtime=codex-remote-mcp auth_mode=%s components=%s managed_files=%d created=%d updated=%d unchanged=%d backed_up=%d failed=%d",
		result.Target, result.Manifest.AuthMode, formatComponentIDs(result.Manifest.Components),
		len(result.Manifest.ManagedFiles), len(result.Summary.Created), len(result.Summary.Updated),
		len(result.Summary.Unchanged), len(result.Summary.BackedUp), len(result.Summary.Failed))
	parts := []string{summary}
	parts = append(parts, formatManagedFileSummaryParts(result.Summary.Created, "create")...)
	parts = append(parts, formatManagedFileSummaryParts(result.Summary.Updated, "update")...)
	parts = append(parts, formatManagedFileSummaryParts(result.Summary.Unchanged, "unchanged")...)
	parts = append(parts, "mcp=remote", "runner=none", "bootstrap=none")
	if len(result.Summary.Failed) > 0 {
		parts = append(parts, fmt.Sprintf("findings=%s", strings.Join(result.Summary.Failed, "; ")))
	}
	return strings.Join(parts, " ")
}

// formatAntigravityInstallSummary produces the Antigravity-specific install summary.
func formatAntigravityInstallSummary(result install.InstallResult) string {
	summary := fmt.Sprintf("install_target=%s runtime=antigravity-prompt-skills components=%s managed_local_files=%d created=%d updated=%d unchanged=%d backed_up=%d failed=%d",
		result.Target, formatComponentIDs(result.Manifest.Components), len(result.Manifest.ManagedFiles),
		len(result.Summary.Created), len(result.Summary.Updated), len(result.Summary.Unchanged),
		len(result.Summary.BackedUp), len(result.Summary.Failed))
	parts := []string{summary}
	parts = append(parts, formatManagedFileSummaryParts(result.Summary.Created, "create")...)
	parts = append(parts, formatManagedFileSummaryParts(result.Summary.Updated, "update")...)
	parts = append(parts, formatManagedFileSummaryParts(result.Summary.Unchanged, "unchanged")...)
	if len(result.Summary.Failed) > 0 {
		parts = append(parts, fmt.Sprintf("findings=%s", strings.Join(result.Summary.Failed, "; ")))
	}
	return strings.Join(parts, " ")
}

// formatOpenCodeInstallSummary produces the OpenCode-specific install
// summary. It is shaped like the Codex summary but uses the
// `runtime=opencode-config-only` token, the `runner=none` /
// `bootstrap=none` markers that match the bounded foundation slice,
// and the documented bounded plugin bundle plus the explicit
// `sdd-engram` / `logo` exclusion list. The plaintext-token warning
// is always surfaced when `lore-server-mcp` is selected and is
// rendered as a separate `opencode-config` check (not embedded in
// the summary line) so the saved token can never leak through the
// summary. The `mcp_lore_ownership=fail-closed-on-foreign` token is
// always surfaced so the bounded ownership contract is part of the
// apply summary surface.
func formatOpenCodeInstallSummary(result install.InstallResult) string {
	summary := fmt.Sprintf("install_target=%s runtime=opencode-config-only auth_mode=%s components=%s managed_local_files=%d created=%d updated=%d unchanged=%d backed_up=%d conflicted=%d failed=%d",
		result.Target, result.Manifest.AuthMode, formatComponentIDs(result.Manifest.Components),
		len(result.Manifest.ManagedFiles), len(result.Summary.Created), len(result.Summary.Updated),
		len(result.Summary.Unchanged), len(result.Summary.BackedUp), len(result.Summary.Conflicted), len(result.Summary.Failed))
	parts := []string{summary}
	parts = append(parts, formatManagedFileSummaryParts(result.Summary.Created, "create")...)
	parts = append(parts, formatManagedFileSummaryParts(result.Summary.Updated, "update")...)
	parts = append(parts, formatManagedFileSummaryParts(result.Summary.Unchanged, "unchanged")...)
	parts = append(parts, formatManagedFileSummaryParts(result.Summary.Conflicted, "conflicted")...)
	parts = append(parts,
		"mcp=remote",
		"mcp_lore_ownership=fail-closed-on-foreign",
		"runner=none",
		"bootstrap=none",
		"opencode_background_subagents_env=OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS=true",
		"prompts=~/.config/opencode/prompts",
		"plugins=bundled:none",
		"legacy_plugins=absent:background-agents,lore-models,model-variants,opencode-subagent-statusline",
		"plugins_location=~/.config/opencode/plugins/ (no Lore-managed plugin files); tui.json registers no Lore-managed plugins",
		"migration=legacy-managed-plugins-to-native-agents",
		"rollback=restore-from-managed-backup-root-then-rerun-install",
		"excluded_plugins=sdd-engram,logo",
	)
	if len(result.Summary.Failed) > 0 {
		parts = append(parts, fmt.Sprintf("findings=%s", strings.Join(result.Summary.Failed, "; ")))
	}
	return strings.Join(parts, " ")
}

// formatOpenCodeInstallPlanSummary produces the OpenCode-specific plan
// summary for the bounded foundation slice. The summary mirrors the
// apply summary's plugin and exclusion tokens so dry-run output
// matches the apply surface. The plaintext-token warning is only
// surfaced at apply time (when `lore-server-mcp` is selected and the
// `opencode-config` check is emitted); dry-run does not embed the
// saved token. The `mcp_lore_ownership=fail-closed-on-foreign` token
// is always surfaced so the ownership contract is part of the
// dry-run surface too.
func formatOpenCodeInstallPlanSummary(plan install.InstallPlan, dryRun bool) string {
	parts := []string{
		fmt.Sprintf("install_target=%s", plan.Layout.Target),
		"runtime=opencode-config-only",
		"auth_mode=config-only",
		fmt.Sprintf("components=%s", formatComponentIDs(plan.Components)),
		fmt.Sprintf("target=%s", plan.Layout.RootDir),
		fmt.Sprintf("manifest=%s", plan.Layout.ManifestPath),
		"mcp=remote",
		"mcp_lore_ownership=fail-closed-on-foreign",
		"runner=none",
		"bootstrap=none",
		"opencode_background_subagents_env=OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS=true",
		"prompts=~/.config/opencode/prompts",
		"plugins=bundled:none",
		"legacy_plugins=absent:background-agents,lore-models,model-variants,opencode-subagent-statusline",
		"plugins_location=~/.config/opencode/plugins/ (no Lore-managed plugin files); tui.json registers no Lore-managed plugins",
		"migration=legacy-managed-plugins-to-native-agents",
		"rollback=restore-from-managed-backup-root-then-rerun-install",
		"excluded_plugins=sdd-engram,logo",
	}
	if dryRun {
		parts = append(parts, "mode=dry-run")
	}
	for _, action := range plan.Files {
		parts = append(parts, fmt.Sprintf("managed_action=%s:%s", action.Action, action.RelativePath))
	}
	return strings.Join(parts, " ")
}

// formatCodexInstallPlanSummary produces the Codex-specific plan summary.
func formatCodexInstallPlanSummary(plan install.InstallPlan, dryRun bool) string {
	parts := []string{
		fmt.Sprintf("install_target=%s", plan.Layout.Target),
		"runtime=codex-remote-mcp",
		"auth_mode=config-only",
		fmt.Sprintf("components=%s", formatComponentIDs(plan.Components)),
		fmt.Sprintf("target=%s", plan.Layout.RootDir),
		fmt.Sprintf("manifest=%s", plan.Layout.ManifestPath),
		"mcp=remote",
		"runner=none",
		"bootstrap=none",
	}
	if dryRun {
		parts = append(parts, "mode=dry-run")
	}
	for _, action := range plan.Files {
		parts = append(parts, fmt.Sprintf("managed_action=%s:%s", action.Action, action.RelativePath))
	}
	return strings.Join(parts, " ")
}

// formatAntigravityInstallPlanSummary produces the Antigravity-specific plan summary.
func formatAntigravityInstallPlanSummary(plan install.InstallPlan, dryRun bool) string {
	parts := []string{
		fmt.Sprintf("install_target=%s", plan.Layout.Target),
		"runtime=antigravity-prompt-skills",
		fmt.Sprintf("components=%s", formatComponentIDs(plan.Components)),
		fmt.Sprintf("target=%s", plan.Layout.RootDir),
		fmt.Sprintf("manifest=%s", plan.Layout.ManifestPath),
	}
	// Antigravity may have a shared prompt path.
	if promptPath, ok := plan.Layout.Paths["shared_prompt"]; ok && promptPath != "" {
		parts = append(parts, fmt.Sprintf("prompt=%s", promptPath))
	}
	parts = append(parts, "mcp_optional=true")
	if dryRun {
		parts = append(parts, "mode=dry-run")
	}
	for _, action := range plan.Files {
		parts = append(parts, fmt.Sprintf("managed_action=%s:%s", action.Action, action.RelativePath))
	}
	return strings.Join(parts, " ")
}

func formatManagedFileSummaryParts(paths []string, action string) []string {
	parts := make([]string, 0, len(paths))
	for _, path := range paths {
		parts = append(parts, fmt.Sprintf("managed_action=%s:%s", action, path))
	}
	return parts
}

func installPiRemotePackage() string {
	return install.PiHostedMCPPackageSource()
}

func formatProjectAgentsPolicy() string {
	return "disabled(default-lore-managed)"
}

func formatComponentIDs(components []install.ComponentID) string {
	if len(components) == 0 {
		return "default"
	}
	values := make([]string, 0, len(components))
	for _, component := range components {
		values = append(values, string(component))
	}
	return strings.Join(values, ",")
}

func (a *App) confirmFullBackup(plan install.PiInstallPlan) (bool, error) {
	if _, err := fmt.Fprintf(a.Stdout, "Existing ~/.pi detected at %s. Create a full backup before install? [Y/n]: ", plan.ExistingPi.Path); err != nil {
		return false, err
	}
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return true, nil
	}
	return !strings.EqualFold(answer, "n") && !strings.EqualFold(answer, "no"), nil
}

func (a *App) confirmBinaryUpdate(plan cliupdate.Plan) (bool, error) {
	if _, err := fmt.Fprintf(a.Stdout, "Update lore binary at %s from %s to %s? [y/N]: ", plan.Target.ExecutablePath, plan.Current.Version, plan.LatestTag); err != nil {
		return false, err
	}
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer = strings.TrimSpace(answer)
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}

func (a *App) updateService() (cliupdate.Service, error) {
	if a.UpdateServiceFactory != nil {
		return a.UpdateServiceFactory()
	}
	configPath, err := a.Store.Path()
	if err != nil {
		return cliupdate.Service{}, err
	}
	return cliupdate.Service{
		ExecPath:         a.resolveExecutablePath,
		LookPath:         a.LookPath,
		ConfigDir:        func() (string, error) { return filepath.Dir(configPath), nil },
		CandidateVersion: probeBinaryVersion,
		BuildInfo:        a.BuildInfo.Normalized(),
	}, nil
}

func probeBinaryVersion(ctx context.Context, path string) (version.Info, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return version.Info{}, fmt.Errorf("candidate binary path is empty")
	}
	out, err := exec.CommandContext(ctx, path, "version", "--json").Output()
	if err != nil {
		return version.Info{}, fmt.Errorf("probe %s version --json: %w", path, err)
	}
	var info version.Info
	if err := json.Unmarshal(out, &info); err != nil {
		return version.Info{}, fmt.Errorf("probe %s version --json: decode output: %w", path, err)
	}
	info = info.Normalized()
	if info.Version == "dev" {
		return version.Info{}, fmt.Errorf("probe %s version --json: reported dev build metadata", path)
	}
	if info.Commit == "none" {
		return version.Info{}, fmt.Errorf("probe %s version --json: reported empty commit metadata", path)
	}
	return info, nil
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

// agentConfigCheck performs a read-only diagnostic check of agent-config.json.
// It is included in status and doctor output. The check does not imply Codex execution support.
func (a *App) agentConfigCheck() output.Check {
	store := agentconfig.NewStore("")
	path, pathErr := store.Path()
	if pathErr != nil {
		return output.Check{Name: "agent-config", Status: output.StatusWarn, Detail: fmt.Sprintf("agent-config path could not be resolved: %v", pathErr)}
	}

	cfg, err := store.Load()
	if err != nil {
		if errors.Is(err, agentconfig.ErrNotFound) {
			return output.Check{Name: "agent-config", Status: output.StatusWarn, Detail: fmt.Sprintf("agent-config not found at %s (optional)", path)}
		}
		return output.Check{Name: "agent-config", Status: output.StatusFail, Detail: fmt.Sprintf("agent-config error: %v", err)}
	}

	if err := cfg.Validate(); err != nil {
		return output.Check{Name: "agent-config", Status: output.StatusFail, Detail: fmt.Sprintf("agent-config invalid: %v", err)}
	}

	return output.Check{Name: "agent-config", Status: output.StatusOK, Detail: fmt.Sprintf("agent-config path=%s schema=%d agents=%d", path, cfg.SchemaVersion, len(cfg.SDDAgents))}
}

func (a *App) piCheck() output.Check {
	if _, err := a.LookPath("pi"); err != nil {
		return output.Check{Name: "pi", Status: output.StatusWarn, Detail: "pi binary not found on PATH", Action: "Install Pi or add it to PATH if Pi automation is expected on this machine."}
	}
	return output.Check{Name: "pi", Status: output.StatusOK, Detail: "pi binary available on PATH"}
}

func (a *App) openCodeDoctorChecks() []output.Check {
	checks := []output.Check{openCodeBackgroundSubagentsEnvCheck()}

	homeDir, err := a.resolveUserHomeDir()
	if err != nil {
		return append(checks, output.Check{Name: "opencode-config", Status: output.StatusWarn, Detail: fmt.Sprintf("OpenCode config path could not be resolved: %v", err), Action: "Fix HOME/user directory resolution, then rerun lore doctor."})
	}
	rootDir := filepath.Join(homeDir, ".config", "opencode")
	configPath := filepath.Join(rootDir, "opencode.json")
	tuiPath := filepath.Join(rootDir, "tui.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			checks = append(checks, output.Check{Name: "opencode-config", Status: output.StatusOK, Detail: fmt.Sprintf("OpenCode config not found at %s (optional); run lore install --target opencode to install native Lore agents", configPath)})
			checks = append(checks, inspectOpenCodeTUIPluginRefs(tuiPath))
			return checks
		}
		checks = append(checks, output.Check{Name: "opencode-config", Status: output.StatusFail, Detail: fmt.Sprintf("read %s failed: %v", configPath, err), Action: "Fix file permissions or move the broken file aside, then rerun lore install --target opencode."})
		checks = append(checks, inspectOpenCodeTUIPluginRefs(tuiPath))
		return checks
	}

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		checks = append(checks, output.Check{Name: "opencode-config", Status: output.StatusFail, Detail: fmt.Sprintf("%s is not valid JSON: %v", configPath, err), Action: "Restore the latest backup from ~/.config/opencode/backups or move opencode.json aside, then rerun lore install --target opencode."})
		checks = append(checks, inspectOpenCodeTUIPluginRefs(tuiPath))
		return checks
	}

	checks = append(checks, inspectOpenCodeConfigPayload(rootDir, configPath, payload)...)
	checks = append(checks, inspectOpenCodeTUIPluginRefs(tuiPath))
	return checks
}

func openCodeBackgroundSubagentsEnvCheck() output.Check {
	if os.Getenv("OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS") == "true" {
		return output.Check{Name: "opencode-background-subagents", Status: output.StatusOK, Detail: "OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS=true; OpenCode owns native background subagent execution"}
	}
	return output.Check{Name: "opencode-background-subagents", Status: output.StatusWarn, Detail: "OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS is not true; OpenCode native subagents may not run in the background", Action: "Start OpenCode with OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS=true when background subagent behavior is expected; Lore cannot enable it from opencode.json."}
}

func inspectOpenCodeConfigPayload(rootDir, configPath string, payload map[string]any) []output.Check {
	checks := make([]output.Check, 0, 3)
	issues := make([]string, 0)
	if _, present := payload["lore"]; present {
		issues = append(issues, "top-level lore block")
	}
	if legacyOpenCodePluginReference(payload["plugins"]) {
		issues = append(issues, "legacy plugins reference")
	}
	if legacyOpenCodePluginReference(payload["plugin"]) {
		issues = append(issues, "legacy plugin reference")
	}
	if hasLegacyOpenCodeSkillsPath(payload) {
		issues = append(issues, "skills.path is obsolete; use schema-safe skills.paths")
	}

	agents, hasAgentObject := payload["agent"].(map[string]any)
	hasManagedAgents := hasAgentObject && hasAnyOpenCodeManagedAgent(agents)
	hasLoreMCP := false
	if mcp, ok := payload["mcp"].(map[string]any); ok {
		_, hasLoreMCP = mcp["lore"]
	}
	if !hasManagedAgents && len(issues) == 0 {
		checks = append(checks, output.Check{Name: "opencode-config", Status: output.StatusOK, Detail: fmt.Sprintf("OpenCode config at %s is not Lore-managed or has no native Lore agent overlay; optional OpenCode install checks are informational", configPath)})
		if hasLoreMCP {
			checks = append(checks, inspectOpenCodeLoreMCP(payload))
		}
		return checks
	}

	if !hasAgentObject {
		issues = append(issues, "missing native agent object")
	} else {
		issues = append(issues, inspectOpenCodePromptRefs(rootDir, agents)...)
		issues = append(issues, inspectOpenCodeManagedAgentModels(agents)...)
	}
	if len(issues) > 0 {
		checks = append(checks, output.Check{Name: "opencode-config", Status: output.StatusFail, Detail: fmt.Sprintf("startup-risky %s: %s", configPath, strings.Join(issues, "; ")), Action: "Do not hand-edit the risky shape in place. Back up opencode.json, then run lore install --target opencode so Lore can render native prompt refs and remove legacy plugin references safely."})
	} else {
		checks = append(checks, output.Check{Name: "opencode-config", Status: output.StatusOK, Detail: fmt.Sprintf("native OpenCode config is startup-safe: agent prompts resolve under %s and no legacy managed plugin refs were found", rootDir)})
	}

	checks = append(checks, inspectOpenCodeLoreMCP(payload))
	return checks
}

func hasAnyOpenCodeManagedAgent(agents map[string]any) bool {
	for _, name := range []string{"lore", "lore-worker", "sdd-init", "sdd-explore", "sdd-propose", "sdd-spec", "sdd-design", "sdd-tasks", "sdd-apply", "sdd-verify", "sdd-archive"} {
		entry, ok := agents[name].(map[string]any)
		if !ok {
			continue
		}
		if isOpenCodeLoreManagedAgentEntry(name, entry) {
			return true
		}
	}
	return false
}

func isOpenCodeLoreManagedAgentEntry(name string, entry map[string]any) bool {
	prompt, _ := entry["prompt"].(string)
	if isLegacyOpenCodeLoreManagedPromptRef(name, prompt) {
		return true
	}
	promptPath, hasManagedPrompt := openCodePromptFilePath(prompt)
	if hasManagedPrompt {
		if name == "lore" && promptPath == "prompts/lore.md" {
			return true
		}
		if name == "lore-worker" && promptPath == "prompts/lore-worker.md" {
			return true
		}
		if strings.HasPrefix(name, "sdd-") && promptPath == filepath.ToSlash(filepath.Join("prompts", "sdd", strings.TrimPrefix(name, "sdd-")+".md")) {
			return true
		}
	}
	permission, _ := entry["permission"].(map[string]any)
	task, _ := permission["task"].(map[string]any)
	if name == "lore" && task["sdd-*"] == "allow" && task["lore-worker"] == "allow" {
		return true
	}
	return false
}

func hasLegacyOpenCodeSkillsPath(payload map[string]any) bool {
	skills, ok := payload["skills"].(map[string]any)
	if !ok {
		return false
	}
	_, present := skills["path"]
	return present
}

func isLegacyOpenCodeLoreManagedPromptRef(name, prompt string) bool {
	prompt = strings.TrimSpace(prompt)
	if name == "lore" && prompt == "{file:./AGENTS.md}" {
		return true
	}
	if name == "lore-worker" && prompt == "{file:./skills/lore-worker/SKILL.md}" {
		return true
	}
	if strings.HasPrefix(name, "sdd-") {
		phase := strings.TrimPrefix(name, "sdd-")
		return prompt == "{file:./skills/sdd-"+phase+"/SKILL.md}"
	}
	return false
}

func inspectOpenCodeManagedAgentModels(agents map[string]any) []string {
	issues := make([]string, 0)
	for _, name := range []string{"lore", "lore-worker", "sdd-init", "sdd-explore", "sdd-propose", "sdd-spec", "sdd-design", "sdd-tasks", "sdd-apply", "sdd-verify", "sdd-archive"} {
		entry, ok := agents[name].(map[string]any)
		if !ok {
			continue
		}
		model, _ := entry["model"].(string)
		if !isOpenCodeProviderModelIdentifier(model) {
			issues = append(issues, fmt.Sprintf("agent.%s.model must use provider/model form", name))
		}
	}
	return issues
}

func isOpenCodeProviderModelIdentifier(model string) bool {
	model = strings.TrimSpace(model)
	if model == "" || !strings.Contains(model, "/") {
		return false
	}
	parts := strings.SplitN(model, "/", 2)
	return strings.TrimSpace(parts[0]) != "" && strings.TrimSpace(parts[1]) != ""
}

func inspectOpenCodePromptRefs(rootDir string, agents map[string]any) []string {
	issues := make([]string, 0)
	for _, name := range []string{"lore", "lore-worker", "sdd-init", "sdd-explore", "sdd-propose", "sdd-spec", "sdd-design", "sdd-tasks", "sdd-apply", "sdd-verify", "sdd-archive"} {
		entry, ok := agents[name].(map[string]any)
		if !ok {
			issues = append(issues, fmt.Sprintf("missing agent.%s", name))
			continue
		}
		prompt, _ := entry["prompt"].(string)
		promptPath, ok := openCodePromptFilePath(prompt)
		if !ok {
			issues = append(issues, fmt.Sprintf("agent.%s.prompt is not a native ./prompts file ref", name))
			continue
		}
		if _, err := os.Stat(filepath.Join(rootDir, promptPath)); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				issues = append(issues, fmt.Sprintf("agent.%s.prompt target missing (%s)", name, promptPath))
			} else {
				issues = append(issues, fmt.Sprintf("agent.%s.prompt target unreadable (%s: %v)", name, promptPath, err))
			}
		}
	}
	return issues
}

func openCodePromptFilePath(prompt string) (string, bool) {
	prompt = strings.TrimSpace(prompt)
	if !strings.HasPrefix(prompt, "{file:./") || !strings.HasSuffix(prompt, "}") {
		return "", false
	}
	rel := strings.TrimSuffix(strings.TrimPrefix(prompt, "{file:./"), "}")
	if !strings.HasPrefix(rel, "prompts/") || strings.Contains(rel, "..") || filepath.IsAbs(rel) {
		return "", false
	}
	return filepath.Clean(rel), true
}

func inspectOpenCodeTUIPluginRefs(tuiPath string) output.Check {
	data, err := os.ReadFile(tuiPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return output.Check{Name: "opencode-tui", Status: output.StatusOK, Detail: fmt.Sprintf("tui.json not found at %s (optional)", tuiPath)}
		}
		return output.Check{Name: "opencode-tui", Status: output.StatusFail, Detail: fmt.Sprintf("read %s failed: %v", tuiPath, err), Action: "Fix file permissions or move tui.json aside, then rerun lore install --target opencode."}
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return output.Check{Name: "opencode-tui", Status: output.StatusFail, Detail: fmt.Sprintf("%s is not valid JSON: %v", tuiPath, err), Action: "Restore the latest backup from ~/.config/opencode/backups or move tui.json aside, then rerun lore install --target opencode."}
	}
	if _, present := payload["lore"]; present {
		return output.Check{Name: "opencode-tui", Status: output.StatusFail, Detail: "tui.json carries stale top-level Lore runtime metadata", Action: "Run lore install --target opencode so Lore can render native tui.json settings and remove legacy runtime metadata."}
	}
	if legacyOpenCodePluginReference(payload["plugin"]) || legacyOpenCodePluginReference(payload["plugins"]) {
		return output.Check{Name: "opencode-tui", Status: output.StatusFail, Detail: "tui.json references legacy OpenCode runtime-emulation plugins", Action: "Run lore install --target opencode so Lore can render native tui.json with no Lore-managed plugin registrations."}
	}
	return output.Check{Name: "opencode-tui", Status: output.StatusOK, Detail: "tui.json has no legacy managed plugin refs"}
}

func inspectOpenCodeLoreMCP(payload map[string]any) output.Check {
	mcp, ok := payload["mcp"].(map[string]any)
	if !ok {
		return output.Check{Name: "opencode-mcp", Status: output.StatusWarn, Detail: "mcp.lore is not configured in opencode.json", Action: "Run lore install --target opencode with saved Lore auth if OpenCode should use Lore MCP."}
	}
	lore, ok := mcp["lore"].(map[string]any)
	if !ok {
		return output.Check{Name: "opencode-mcp", Status: output.StatusWarn, Detail: "mcp block exists but mcp.lore is absent", Action: "Run lore install --target opencode to add the managed Lore MCP entry without changing unrelated MCP servers."}
	}
	urlValue, _ := lore["url"].(string)
	enabled := "unspecified"
	if value, ok := lore["enabled"].(bool); ok {
		enabled = fmt.Sprintf("%t", value)
	}
	if headers, ok := lore["headers"].(map[string]any); ok {
		if _, hasAuth := headers["Authorization"]; hasAuth {
			return output.Check{Name: "opencode-mcp", Status: output.StatusOK, Detail: fmt.Sprintf("mcp.lore present url=%s enabled=%s Authorization=<redacted>", safeOpenCodeMCPURL(urlValue), enabled)}
		}
	}
	return output.Check{Name: "opencode-mcp", Status: output.StatusWarn, Detail: fmt.Sprintf("mcp.lore present url=%s enabled=%s but Authorization header is missing", safeOpenCodeMCPURL(urlValue), enabled), Action: "Run lore install --target opencode after login so the managed MCP entry includes an Authorization bearer token."}
}

func legacyOpenCodePluginReference(value any) bool {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if legacyOpenCodePluginReference(item) {
				return true
			}
		}
	case map[string]any:
		for _, key := range []string{"id", "name", "path", "plugin"} {
			if legacyOpenCodePluginReference(typed[key]) {
				return true
			}
		}
	case string:
		normalized := strings.ToLower(strings.TrimSpace(typed))
		return strings.Contains(normalized, "background-agents") || strings.Contains(normalized, "lore-models") || strings.Contains(normalized, "model-variants") || strings.Contains(normalized, "opencode-subagent-statusline")
	}
	return false
}

func safeOpenCodeMCPURL(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "<missing>"
	}
	return strings.TrimSpace(raw)
}

func checksContainActionableOpenCodeFinding(checks []output.Check) bool {
	for _, check := range checks {
		if check.Status == output.StatusFail {
			return true
		}
	}
	return false
}

func localUpdateSafetyReason(currentVersion, execPath, pathPath string) (string, bool) {
	currentVersion = strings.TrimSpace(currentVersion)
	execPath = filepath.Clean(strings.TrimSpace(execPath))
	pathPath = filepath.Clean(strings.TrimSpace(pathPath))
	if currentVersion == "dev" {
		return "dev build refuses automatic self-update", true
	}
	if execPath != "" && pathPath != "" && execPath != pathPath {
		return fmt.Sprintf("PATH mismatch (running %s, PATH resolves %s)", execPath, pathPath), true
	}
	return "", false
}

func formatUpdatePlanSummary(plan cliupdate.Plan, dryRun bool) string {
	mode := "apply"
	if dryRun {
		mode = "dry-run"
	}
	parts := []string{
		fmt.Sprintf("mode=%s", mode),
		fmt.Sprintf("current=%s", plan.Current.Version),
		fmt.Sprintf("latest=%s", fallbackUpdateValue(plan.LatestTag, "unresolved")),
		fmt.Sprintf("target=%s", plan.Target.ExecutablePath),
		fmt.Sprintf("status=%s", plan.Status),
		fmt.Sprintf("cache=%s", plan.CacheSource),
		fmt.Sprintf("asset=%s", fallbackUpdateValue(plan.Asset.Name, "unresolved")),
		"scope=binary-only",
		"pi_runtime=untouched",
		"pi_dir=~/.pi untouched",
	}
	if reason := updatePlanReason(plan); reason != "" {
		parts = append(parts, fmt.Sprintf("unsafe=%s", reason))
	}
	return strings.Join(parts, " ")
}

func formatUpdateResultSummary(plan cliupdate.Plan, result cliupdate.Result) string {
	parts := []string{
		fmt.Sprintf("current=%s", plan.Current.Version),
		fmt.Sprintf("latest=%s", fallbackUpdateValue(plan.LatestTag, "unresolved")),
		fmt.Sprintf("target=%s", plan.Target.ExecutablePath),
		fmt.Sprintf("status=%s", result.Status),
		"scope=binary-only",
		"pi_runtime=untouched",
		"pi_dir=~/.pi untouched",
	}
	if result.BackupPath != "" {
		parts = append(parts, fmt.Sprintf("backup=%s", result.BackupPath))
	}
	if result.Installed.Version != "" {
		parts = append(parts, fmt.Sprintf("installed=%s", result.Installed.Version))
	}
	return strings.Join(parts, " ")
}

func updatePlanReason(plan cliupdate.Plan) string {
	if plan.Target.Status != cliupdate.TargetStatusOK {
		switch plan.Target.Reason {
		case cliupdate.ReasonPathMismatch:
			return "path mismatch"
		case cliupdate.ReasonSymlinkedTarget:
			return "symlinked target"
		default:
			return string(plan.Target.Reason)
		}
	}
	if plan.Status == cliupdate.StatusDevBuild {
		return "dev build refuses automatic self-update"
	}
	if plan.Status == cliupdate.StatusUnsupported {
		return "unsafe target"
	}
	return ""
}

func updatePlanAction(plan cliupdate.Plan) string {
	if reason := updatePlanReason(plan); reason != "" {
		return fmt.Sprintf("Resolve the %s condition before retrying lore update; Pi runtime and ~/.pi remain untouched.", reason)
	}
	return "Resolve the reported update precondition before retrying; Pi runtime and ~/.pi remain untouched."
}

func fallbackUpdateValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func explainLoginError(err error) string {
	var unauthorized *httpclient.UnauthorizedError
	if errors.As(err, &unauthorized) {
		return "normal user API token required; /v1/me rejected the provided token"
	}
	var unsupported *httpclient.UnsupportedServerError
	if errors.As(err, &unsupported) {
		return unsupported.Error()
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
