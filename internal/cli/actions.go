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

type apiMCPCallOptions struct {
	JSONOutput bool
	Tool       string
	ArgsJSON   string
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

func (a *App) planPiInstallAction(ctx context.Context, opts installCommandOptions) (install.PiInstallPlan, ActionReport, bool) {
	service := install.Service{Store: a.Store, Auth: a.authManager(), ClientFactory: install.ClientFactory(a.ClientFactory)}
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
		report.Checks = append(report.Checks, output.Check{Name: "install", Status: output.StatusFail, Detail: err.Error(), Action: "Keep Pi on the native Lore extensions path for now; Pi MCP remains disabled by default. Inspect the requested target/components and rerun lore install after fixing the reported issue."})
		report.ExitCode = 1
		return install.PiInstallPlan{}, report, false
	}
	return plan, report, true
}

func (a *App) executePiInstallAction(ctx context.Context, plan install.PiInstallPlan) ActionReport {
	service := install.Service{Store: a.Store, Auth: a.authManager(), ClientFactory: install.ClientFactory(a.ClientFactory)}
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
		}
		return checks, 1
	}

	cfg, err := a.Store.Load()
	if err != nil {
		if errors.Is(err, config.ErrNotFound) {
			checks := []output.Check{{Name: "config", Status: output.StatusWarn, Detail: fmt.Sprintf("no-config at %s", path), Action: "Run lore login --server <url> --email <email> for password login, or use --token for compatibility mode."}}
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
		}
		return checks, 1
	}

	client, err := a.ClientFactory(session.ServerURL)
	if err != nil {
		checks = append(checks, output.Check{Name: "server-url", Status: output.StatusFail, Detail: err.Error(), Action: "Fix the server URL with lore login --server <http(s)://host> --email <email> for password login, or use --token for compatibility mode."})
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

func formatManagedFileSummaryParts(paths []string, action string) []string {
	parts := make([]string, 0, len(paths))
	for _, path := range paths {
		parts = append(parts, fmt.Sprintf("managed_action=%s:%s", action, path))
	}
	return parts
}

func installPiRemotePackage() string {
	return "git:github.com/alferio94/lore-pi-subagents"
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

func (a *App) piCheck() output.Check {
	if _, err := a.LookPath("pi"); err != nil {
		return output.Check{Name: "pi", Status: output.StatusWarn, Detail: "pi binary not found on PATH", Action: "Install Pi or add it to PATH if Pi automation is expected on this machine."}
	}
	return output.Check{Name: "pi", Status: output.StatusOK, Detail: "pi binary available on PATH"}
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
