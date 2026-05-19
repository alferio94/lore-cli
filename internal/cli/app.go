package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/alferio94/lore-cli/internal/auth"
	"github.com/alferio94/lore-cli/internal/config"
	"github.com/alferio94/lore-cli/internal/httpclient"
	"github.com/alferio94/lore-cli/internal/output"
	"github.com/alferio94/lore-cli/internal/version"
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

type AuthManager interface {
	Save(serverURL, token string) error
	Load() (auth.Session, error)
	Logout() error
}

// App wires command IO and dependencies.
type App struct {
	Stdout         io.Writer
	Stderr         io.Writer
	Store          ConfigStore
	Auth           AuthManager
	ClientFactory  ClientFactory
	LookPath       func(string) (string, error)
	UserHomeDir    func() (string, error)
	ExecutablePath func() (string, error)
	TUIRunner      func(context.Context, InteractiveActions) error
	BuildInfo      version.Info
}

// New returns a CLI app with production defaults.
func New(configDir string, stdout, stderr io.Writer, buildInfo version.Info) *App {
	store := config.NewStore(configDir)
	return &App{
		Stdout: stdout,
		Stderr: stderr,
		Store:  store,
		Auth:   auth.Manager{ConfigStore: store},
		ClientFactory: func(baseURL string) (httpclient.Client, error) {
			return httpclient.New(baseURL, 0)
		},
		LookPath:       exec.LookPath,
		UserHomeDir:    os.UserHomeDir,
		ExecutablePath: os.Executable,
		BuildInfo:      buildInfo.Normalized(),
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
	if a.LookPath == nil {
		a.LookPath = exec.LookPath
	}
	if a.UserHomeDir == nil {
		a.UserHomeDir = os.UserHomeDir
	}
	if a.ExecutablePath == nil {
		a.ExecutablePath = os.Executable
	}
	if len(args) > 0 {
		if isHelpArg(args[0]) {
			a.printRootHelp()
			return 0
		}
		if args[0] == "help" {
			a.printRootHelp()
			return 0
		}
		if args[0] == "version" {
			return a.runVersion(args[1:])
		}
	}
	if a.Store == nil {
		fmt.Fprintln(a.Stderr, "internal error: config store is not configured")
		return 1
	}
	if a.ClientFactory == nil {
		fmt.Fprintln(a.Stderr, "internal error: client factory is not configured")
		return 1
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
	case "install":
		return a.runInstall(actions, args[1:])
	case "api":
		return a.runAPI(actions, args[1:])
	case "remember":
		return a.parseRemember(args[1:])
	case "recall":
		return a.parseRecall(args[1:])
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
		fmt.Fprintln(a.Stderr, "Validate a normal user API token with /v1/me before saving OS keychain-backed login metadata.")
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
		fmt.Fprintln(a.Stderr, "Show local login metadata, server health/readiness, and auth identity state.")
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
		fmt.Fprintln(a.Stderr, "Remove local login metadata and matching OS keychain credential only; no server-side token revocation is performed.")
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

func (a *App) runInstall(actions InteractiveActions, args []string) int {
	fs := newFlagSet("install", a.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(a.Stderr, "Usage: lore install")
		fmt.Fprintln(a.Stderr, "Install the Pi-first managed runtime using saved Lore login state.")
		fmt.Fprintln(a.Stderr, "Healthy saved OS keychain-backed login metadata is reused automatically; Claude Code, OpenCode, Codex, and Antigravity remain Coming soon.")
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 1
	}

	report := actions.Install(context.Background())
	fmt.Fprint(a.Stdout, output.RenderChecks(report.Title, report.Checks))
	return report.ExitCode
}

func (a *App) runAPI(_ InteractiveActions, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(a.Stderr, "Usage: lore api <request|mcp-call>")
		return 1
	}

	switch args[0] {
	case "request":
		fs := flag.NewFlagSet("api request", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		jsonOutput := fs.Bool("json", false, "Print machine-readable JSON output")
		method := fs.String("method", "", "HTTP method")
		path := fs.String("path", "", "Relative API path")
		bodyJSON := fs.String("body-json", "", "Optional JSON object/array request body")
		if err := fs.Parse(args[1:]); err != nil {
			return a.writeBrokerError(2, 400, "invalid_request", "invalid lore api request arguments", "")
		}
		if fs.NArg() != 0 {
			return a.writeBrokerError(2, 400, "invalid_request", "unexpected positional arguments", "")
		}
		return a.runAPIRequest(apiRequestOptions{JSONOutput: *jsonOutput, Method: *method, Path: *path, BodyJSON: *bodyJSON})
	case "mcp-call":
		fs := flag.NewFlagSet("api mcp-call", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		jsonOutput := fs.Bool("json", false, "Print machine-readable JSON output")
		tool := fs.String("tool", "", "Allowlisted MCP tool name")
		argsJSON := fs.String("args-json", "{}", "JSON object MCP tool arguments")
		if err := fs.Parse(args[1:]); err != nil {
			return a.writeBrokerError(2, 400, "invalid_request", "invalid lore api mcp-call arguments", "")
		}
		if fs.NArg() != 0 {
			return a.writeBrokerError(2, 400, "invalid_request", "unexpected positional arguments", "")
		}
		return a.runAPIMCPCall(apiMCPCallOptions{JSONOutput: *jsonOutput, Tool: *tool, ArgsJSON: *argsJSON})
	default:
		fmt.Fprintln(a.Stderr, "Usage: lore api <request|mcp-call>")
		return 1
	}
}

func (a *App) parseRemember(args []string) int {
	fs := newFlagSet("remember", a.Stderr)
	projectID := fs.String("project-id", "", "Lore project ID")
	memoryType := fs.String("type", "", "Memory type")
	title := fs.String("title", "", "Memory title")
	content := fs.String("content", "", "Memory content")
	scope := fs.String("scope", "project", "Memory scope")
	metadataJSON := fs.String("metadata-json", "", "Optional metadata JSON object")
	jsonOutput := fs.Bool("json", false, "Print server-shaped JSON output")
	fs.Usage = func() {
		fmt.Fprintln(a.Stderr, "Usage: lore remember --project-id <id> --type <type> --title <title> --content <text> [--scope project] [--metadata-json <json-object>] [--json]")
		fmt.Fprintln(a.Stderr, "Create one Lore memory via POST /v1/memories using saved login state.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 0 || strings.TrimSpace(*projectID) == "" || strings.TrimSpace(*memoryType) == "" || strings.TrimSpace(*title) == "" || strings.TrimSpace(*content) == "" {
		fs.Usage()
		return 1
	}
	if err := a.runRemember(rememberOptions{ProjectID: strings.TrimSpace(*projectID), Scope: strings.TrimSpace(*scope), Type: strings.TrimSpace(*memoryType), Title: strings.TrimSpace(*title), Content: strings.TrimSpace(*content), MetadataJSON: strings.TrimSpace(*metadataJSON), JSONOutput: *jsonOutput}); err != nil {
		fmt.Fprintf(a.Stderr, "remember failed: %s\n", explainEndpointError(err))
		return 1
	}
	return 0
}

func (a *App) parseRecall(args []string) int {
	fs := newFlagSet("recall", a.Stderr)
	projectID := fs.String("project-id", "", "Lore project ID")
	memoryType := fs.String("type", "", "Memory type filter")
	scope := fs.String("scope", "project", "Memory scope filter")
	limit := fs.Int("limit", 0, "Maximum number of memories to return (0 uses server default)")
	jsonOutput := fs.Bool("json", false, "Print server-shaped JSON output")
	fs.Usage = func() {
		fmt.Fprintln(a.Stderr, "Usage: lore recall --project-id <id> [--type <type>] [--scope <scope>] [--limit <n>] [--json]")
		fmt.Fprintln(a.Stderr, "List Lore memories by explicit filters only; no semantic search is performed.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 0 || strings.TrimSpace(*projectID) == "" || *limit < 0 {
		fs.Usage()
		return 1
	}
	if err := a.runRecall(recallOptions{ProjectID: strings.TrimSpace(*projectID), Scope: strings.TrimSpace(*scope), Type: strings.TrimSpace(*memoryType), Limit: *limit, JSONOutput: *jsonOutput}); err != nil {
		fmt.Fprintf(a.Stderr, "recall failed: %s\n", explainEndpointError(err))
		return 1
	}
	return 0
}

func (a *App) runVersion(args []string) int {
	fs := newFlagSet("version", a.Stderr)
	jsonOutput := fs.Bool("json", false, "Print version metadata as JSON")
	fs.Usage = func() {
		fmt.Fprintln(a.Stderr, "Usage: lore version [--json]")
		fmt.Fprintln(a.Stderr, "Print build metadata without requiring local config, auth, or network access.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 1
	}

	info := a.BuildInfo.Normalized()
	if *jsonOutput {
		if err := writeJSON(a.Stdout, info); err != nil {
			fmt.Fprintf(a.Stderr, "version output failed: %v\n", err)
			return 1
		}
		return 0
	}

	fmt.Fprintln(a.Stdout, info.String())
	return 0
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func isHelpArg(arg string) bool {
	return arg == "--help" || arg == "-h"
}

func writeJSON(dst io.Writer, value any) error {
	encoder := json.NewEncoder(dst)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
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
	fmt.Fprintln(w, "  tui       Start the interactive TUI explicitly")
	fmt.Fprintln(w, "  login     Validate a user API token with /v1/me and save OS keychain-backed login metadata")
	fmt.Fprintln(w, "  status    Show login metadata, health, readiness, and auth status")
	fmt.Fprintln(w, "  logout    Remove local login metadata and matching OS keychain credential only")
	fmt.Fprintln(w, "  doctor    Run actionable diagnostics, including optional Pi availability")
	fmt.Fprintln(w, "  install   Install the Pi-first managed runtime using saved Lore auth")
	fmt.Fprintln(w, "  remember  Create one memory via authenticated REST")
	fmt.Fprintln(w, "  api request  Hidden machine broker for allowlisted authenticated API calls")
	fmt.Fprintln(w, "  api mcp-call Hidden machine broker for allowlisted authenticated MCP tool calls")
	fmt.Fprintln(w, "  recall    List memories by explicit authenticated filters")
	fmt.Fprintln(w, "  version   Print build metadata for humans or scripts")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Use explicit subcommands for automation and scripts.")
	fmt.Fprintln(w, "Saved login state uses OS keychain-backed login metadata; raw API tokens are not written to config.json.")
}
