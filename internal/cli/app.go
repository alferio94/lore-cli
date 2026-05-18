package cli

import (
	"context"
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
	TUIRunner     func(context.Context, InteractiveActions) error
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
	if len(args) == 0 {
		if a.TUIRunner == nil {
			fmt.Fprintln(a.Stderr, "internal error: TUI runner is not configured")
			return 1
		}
		if err := a.TUIRunner(context.Background(), a.InteractiveActions()); err != nil {
			fmt.Fprintf(a.Stderr, "failed to start interactive UI: %v\n", err)
			return 1
		}
		return 0
	}
	if isHelpArg(args[0]) {
		a.printRootHelp()
		return 0
	}

	actions := a.InteractiveActions()

	switch args[0] {
	case "tui":
		if len(args) != 1 {
			fmt.Fprintln(a.Stderr, "Usage: lore tui")
			return 1
		}
		if a.TUIRunner == nil {
			fmt.Fprintln(a.Stderr, "internal error: TUI runner is not configured")
			return 1
		}
		if err := a.TUIRunner(context.Background(), actions); err != nil {
			fmt.Fprintf(a.Stderr, "failed to start interactive UI: %v\n", err)
			return 1
		}
		return 0
	case "login":
		return a.runLogin(actions, args[1:])
	case "status":
		return a.runStatus(actions, args[1:])
	case "logout":
		return a.runLogout(actions, args[1:])
	case "doctor":
		return a.runDoctor(actions, args[1:])
	case "help":
		a.printRootHelp()
		return 0
	default:
		fmt.Fprintf(a.Stderr, "unknown command: %s\n\n", args[0])
		a.printRootHelpTo(a.Stderr)
		return 1
	}
}

func (a *App) runLogin(actions InteractiveActions, args []string) int {
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

	result, err := actions.Login(context.Background(), rawServer, rawToken)
	if err != nil {
		if rawServer == "" || rawToken == "" {
			fs.Usage()
			return 1
		}
		if _, ok := err.(*httpclient.UnauthorizedError); ok {
			fmt.Fprintf(a.Stderr, "login failed: %s\n", explainLoginError(err))
			return 1
		}
		fmt.Fprintf(a.Stderr, "login failed: %v\n", err)
		return 1
	}

	fmt.Fprintln(a.Stdout, result.Summary)
	return 0
}

func (a *App) runStatus(actions InteractiveActions, args []string) int {
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

	report := actions.Status(context.Background())
	fmt.Fprint(a.Stdout, output.RenderChecks(report.Title, report.Checks))
	return report.ExitCode
}

func (a *App) runLogout(actions InteractiveActions, args []string) int {
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

	result, err := actions.Logout(context.Background())
	if err != nil {
		fmt.Fprintf(a.Stderr, "logout failed: %v\n", err)
		return 1
	}

	fmt.Fprintln(a.Stdout, result.Summary)
	return 0
}

func (a *App) runDoctor(actions InteractiveActions, args []string) int {
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

	report := actions.Doctor(context.Background())
	fmt.Fprint(a.Stdout, output.RenderChecks(report.Title, report.Checks))
	return report.ExitCode
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
	fmt.Fprintln(w, "  lore                  Start the interactive TUI")
	fmt.Fprintln(w, "  lore <command> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  tui     Start the interactive TUI explicitly")
	fmt.Fprintln(w, "  login   Validate a user API token with /v1/me and save local config")
	fmt.Fprintln(w, "  status  Show config, health, readiness, and auth status")
	fmt.Fprintln(w, "  logout  Remove local config only; no remote revocation")
	fmt.Fprintln(w, "  doctor  Run actionable diagnostics, including optional Pi availability")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Use explicit subcommands for automation and scripts.")
}
