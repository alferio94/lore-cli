package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/alferio94/lore-cli/internal/config"
	"github.com/alferio94/lore-cli/internal/httpclient"
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

// InteractiveActions exposes reusable command behavior for the TUI.
type InteractiveActions struct {
	Login  func(ctx context.Context, serverURL, token string) (ActionMessage, error)
	Logout func(ctx context.Context) (ActionMessage, error)
	Status func(ctx context.Context) ActionReport
	Doctor func(ctx context.Context) ActionReport
}

// InteractiveActions returns the shared action set used by the CLI and future TUI.
func (a *App) InteractiveActions() InteractiveActions {
	return InteractiveActions{
		Login:  a.loginAction,
		Logout: a.logoutAction,
		Status: a.statusAction,
		Doctor: a.doctorAction,
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

	if err := a.Store.Save(config.Config{ServerURL: rawServer, APIToken: rawToken}); err != nil {
		return ActionMessage{}, err
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
	if err := a.Store.Delete(); err != nil {
		return ActionMessage{}, err
	}

	path, _ := a.Store.Path()
	return ActionMessage{Summary: output.FormatLogoutResult(path, hadConfig)}, nil
}

func (a *App) statusAction(ctx context.Context) ActionReport {
	checks, exitCode := a.collectChecks(ctx, false)
	return ActionReport{Title: "Lore status", Checks: checks, ExitCode: exitCode}
}

func (a *App) doctorAction(ctx context.Context) ActionReport {
	checks, exitCode := a.collectChecks(ctx, true)
	return ActionReport{Title: "Lore doctor", Checks: checks, ExitCode: exitCode}
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

	checks := []output.Check{{Name: "config", Status: output.StatusOK, Detail: fmt.Sprintf("configured server=%s token=%s path=%s", cfg.ServerURL, config.RedactToken(cfg.APIToken), path)}}

	client, err := a.ClientFactory(cfg.ServerURL)
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

	if strings.TrimSpace(cfg.APIToken) == "" {
		checks = append(checks, output.Check{Name: "auth", Status: output.StatusFail, Detail: "missing API token", Action: "Run lore login again with a valid normal user API token."})
		exitCode = 1
	} else if subject, err := client.Me(ctx, cfg.APIToken); err != nil {
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
