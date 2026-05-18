package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/alferio94/lore-cli/internal/config"
	"github.com/alferio94/lore-cli/internal/httpclient"
	"github.com/alferio94/lore-cli/internal/output"
)

// ConfigStore captures the config operations used by the CLI.
type ConfigStore interface {
	Load() (config.Config, error)
	Save(config.Config) error
	Delete() error
	Path() (string, error)
}

// ClientFactory creates an API client for a normalized server URL.
type ClientFactory func(baseURL string) (httpclient.Client, error)

// App wires command IO and dependencies.
type App struct {
	Stdout        io.Writer
	Stderr        io.Writer
	Store         ConfigStore
	ClientFactory ClientFactory
	LookPath      func(string) (string, error)
}

// New returns a CLI app with production defaults.
func New(configDir string, stdout, stderr io.Writer) *App {
	return &App{
		Stdout: stdout,
		Stderr: stderr,
		Store:  config.NewStore(configDir),
		ClientFactory: func(baseURL string) (httpclient.Client, error) {
			return httpclient.New(baseURL, 0)
		},
		LookPath: exec.LookPath,
	}
}

// Run executes the Lore CLI command.
func (a *App) Run(args []string) int {
	if a.Stdout == nil {
		a.Stdout = io.Discard
	}
	if a.Stderr == nil {
		a.Stderr = io.Discard
	}
	if a.Store == nil {
		fmt.Fprintln(a.Stderr, "internal error: config store is not configured")
		return 1
	}
	if a.ClientFactory == nil {
		fmt.Fprintln(a.Stderr, "internal error: client factory is not configured")
		return 1
	}
	if a.LookPath == nil {
		a.LookPath = exec.LookPath
	}

	if len(args) == 0 || isHelpArg(args[0]) {
		a.printRootHelp()
		return 0
	}

	switch args[0] {
	case "login":
		return a.runLogin(args[1:])
	case "status":
		return a.runStatus(args[1:])
	case "logout":
		return a.runLogout(args[1:])
	case "doctor":
		return a.runDoctor(args[1:])
	case "help":
		a.printRootHelp()
		return 0
	default:
		fmt.Fprintf(a.Stderr, "unknown command: %s\n\n", args[0])
		a.printRootHelpTo(a.Stderr)
		return 1
	}
}

func (a *App) runLogin(args []string) int {
	fs := newFlagSet("login", a.Stderr)
	server := fs.String("server", "", "Lore server base URL")
	token := fs.String("token", "", "User API token")
	fs.Usage = func() {
		fmt.Fprintln(a.Stderr, "Usage: lore login --server <url> --token <token>")
		fmt.Fprintln(a.Stderr, "Validate a normal user API token with /v1/me before saving local config.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 1
	}

	rawServer := strings.TrimSpace(*server)
	rawToken := strings.TrimSpace(*token)
	if rawServer == "" || rawToken == "" {
		fs.Usage()
		return 1
	}

	client, err := a.ClientFactory(rawServer)
	if err != nil {
		fmt.Fprintf(a.Stderr, "login failed: %v\n", err)
		return 1
	}
	subject, err := client.Me(context.Background(), rawToken)
	if err != nil {
		fmt.Fprintf(a.Stderr, "login failed: %s\n", explainLoginError(err))
		return 1
	}

	if err := a.Store.Save(config.Config{ServerURL: rawServer, APIToken: rawToken}); err != nil {
		fmt.Fprintf(a.Stderr, "login failed: %v\n", err)
		return 1
	}

	path, _ := a.Store.Path()
	fmt.Fprintln(a.Stdout, output.FormatLoginSuccess(subject, path))
	return 0
}

func (a *App) runStatus(args []string) int {
	fs := newFlagSet("status", a.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(a.Stderr, "Usage: lore status")
		fmt.Fprintln(a.Stderr, "Show local config, server health/readiness, and auth identity state.")
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 1
	}

	checks, exitCode := a.collectChecks(false)
	fmt.Fprint(a.Stdout, output.RenderChecks("Lore status", checks))
	return exitCode
}

func (a *App) runLogout(args []string) int {
	fs := newFlagSet("logout", a.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(a.Stderr, "Usage: lore logout")
		fmt.Fprintln(a.Stderr, "Remove local config only; no server-side token revocation is performed.")
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 1
	}

	hadConfig := true
	if _, err := a.Store.Load(); err != nil {
		if errors.Is(err, config.ErrNotFound) {
			hadConfig = false
		} else {
			fmt.Fprintf(a.Stderr, "logout failed: %v\n", err)
			return 1
		}
	}
	if err := a.Store.Delete(); err != nil {
		fmt.Fprintf(a.Stderr, "logout failed: %v\n", err)
		return 1
	}

	path, _ := a.Store.Path()
	fmt.Fprintln(a.Stdout, output.FormatLogoutResult(path, hadConfig))
	return 0
}

func (a *App) runDoctor(args []string) int {
	fs := newFlagSet("doctor", a.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(a.Stderr, "Usage: lore doctor")
		fmt.Fprintln(a.Stderr, "Run actionable config, network, readiness, auth, and Pi diagnostics.")
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 1
	}

	checks, exitCode := a.collectChecks(true)
	fmt.Fprint(a.Stdout, output.RenderChecks("Lore doctor", checks))
	return exitCode
}

func (a *App) collectChecks(includePi bool) ([]output.Check, int) {
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
	if err := client.Health(context.Background()); err != nil {
		checks = append(checks, output.Check{Name: "healthz", Status: output.StatusFail, Detail: explainEndpointError(err), Action: "Check server reachability and that the Lore API is running."})
		exitCode = 1
	} else {
		checks = append(checks, output.Check{Name: "healthz", Status: output.StatusOK, Detail: "server is live"})
	}

	if err := client.Ready(context.Background()); err != nil {
		checks = append(checks, output.Check{Name: "readyz", Status: output.StatusFail, Detail: explainEndpointError(err), Action: "Wait for the server to become ready or inspect server logs."})
		exitCode = 1
	} else {
		checks = append(checks, output.Check{Name: "readyz", Status: output.StatusOK, Detail: "server is ready"})
	}

	if strings.TrimSpace(cfg.APIToken) == "" {
		checks = append(checks, output.Check{Name: "auth", Status: output.StatusFail, Detail: "missing API token", Action: "Run lore login again with a valid normal user API token."})
		exitCode = 1
	} else if subject, err := client.Me(context.Background(), cfg.APIToken); err != nil {
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

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func isHelpArg(arg string) bool {
	return arg == "--help" || arg == "-h"
}

func (a *App) printRootHelp() {
	a.printRootHelpTo(a.Stdout)
}

func (a *App) printRootHelpTo(w io.Writer) {
	fmt.Fprintln(w, "Lore CLI")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  lore <command> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  login   Validate a user API token with /v1/me and save local config")
	fmt.Fprintln(w, "  status  Show config, health, readiness, and auth status")
	fmt.Fprintln(w, "  logout  Remove local config only; no remote revocation")
	fmt.Fprintln(w, "  doctor  Run actionable diagnostics, including optional Pi availability")
}
