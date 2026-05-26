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
	"github.com/alferio94/lore-cli/internal/install"
	"github.com/alferio94/lore-cli/internal/mcp"
	"github.com/alferio94/lore-cli/internal/output"
	cliupdate "github.com/alferio94/lore-cli/internal/update"
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
	Stdout               io.Writer
	Stderr               io.Writer
	Stdin                io.Reader
	PasswordPrompt       func() (string, error)
	Store                ConfigStore
	Auth                 AuthManager
	ClientFactory        ClientFactory
	LookPath             func(string) (string, error)
	UserHomeDir          func() (string, error)
	ExecutablePath       func() (string, error)
	TUIRunner            func(context.Context, InteractiveActions) error
	UpdateServiceFactory func() (cliupdate.Service, error)
	BuildInfo            version.Info
}

// New returns a CLI app with production defaults.
func New(configDir string, stdout, stderr io.Writer, buildInfo version.Info) *App {
	store := config.NewStore(configDir)
	return &App{
		Stdout: stdout,
		Stderr: stderr,
		Store:  store,
		Stdin:  os.Stdin,
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
	if a.Stdin == nil {
		a.Stdin = os.Stdin
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
	case "update":
		return a.runUpdate(args[1:])
	case "api":
		return a.runAPI(actions, args[1:])
	case "mcp":
		return a.runMCP(args[1:])
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
	email := fs.String("email", "", "Lore account email for password login")
	token := fs.String("token", "", "User API token compatibility path")
	passwordStdin := fs.Bool("password-stdin", false, "Read one password line from stdin for non-interactive automation")
	fs.Usage = func() {
		fmt.Fprintln(a.Stderr, "Usage: lore login --server <url> --email <email> [--password-stdin]")
		fmt.Fprintln(a.Stderr, "   or: lore login --server <url> --token <token>")
		fmt.Fprintln(a.Stderr, "Primary login uses email + hidden password to mint a reusable API token; only the minted token is saved in the OS keychain.")
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
	rawEmail := strings.TrimSpace(*email)
	rawToken := strings.TrimSpace(*token)
	if rawServer == "" {
		fs.Usage()
		return 1
	}

	mode := ""
	var (
		result ActionMessage
		err    error
	)
	switch {
	case rawToken != "":
		mode = "token"
		if rawEmail != "" || *passwordStdin {
			fmt.Fprintln(a.Stderr, "login failed: --token compatibility mode cannot be combined with --email or --password-stdin")
			return 1
		}
		result, err = actions.Login(context.Background(), rawServer, rawToken)
	case rawEmail != "":
		mode = "password"
		password, passwordErr := a.readLoginPassword(*passwordStdin)
		if passwordErr != nil {
			fmt.Fprintf(a.Stderr, "login failed: %v\n", passwordErr)
			return 1
		}
		result, err = a.loginActionWithInput(context.Background(), LoginInput{Mode: "password", ServerURL: rawServer, Email: rawEmail, Password: password})
	default:
		fs.Usage()
		return 1
	}
	if err != nil {
		if mode == "password" {
			fmt.Fprintf(a.Stderr, "login failed: %s\n", explainEndpointError(err))
		} else {
			fmt.Fprintf(a.Stderr, "login failed: %s\n", explainLoginError(err))
		}
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

func (a *App) runInstall(_ InteractiveActions, args []string) int {
	fs := newFlagSet("install", a.Stderr)
	dryRun := fs.Bool("dry-run", false, "Show the selected install plan without mutating managed runtime files")
	yes := fs.Bool("yes", false, "Accept the safe default full-backup behavior without prompting")
	target := fs.String("target", string(install.DefaultInstallTarget()), "Install target (Pi stays the default recommended target; Antigravity is the prompt + skills MVP target)")
	var components componentFlag
	fs.Var(&components, "component", "Optional component override; repeat or use a comma-separated list (Pi supports core-pack and pi-extensions only)")
	fs.Usage = func() {
		fmt.Fprintln(a.Stderr, "Usage: lore install [--dry-run] [--yes] [--target pi|antigravity] [--component <id>]")
		fmt.Fprintln(a.Stderr, "Install the Pi-first managed runtime using saved Lore login state.")
		fmt.Fprintln(a.Stderr, "Pi remains the default recommended path with the portable Lore agent pack plus pi-extensions; Antigravity is the supported prompt + skills MVP target with harness-owned prompt, skills, and optional MCP config semantics.")
		fmt.Fprintln(a.Stderr, "Healthy saved OS keychain-backed login metadata is reused automatically after password-first login or a compatibility token via --token; Claude Code, OpenCode, and Codex remain Coming soon.")
		fmt.Fprintln(a.Stderr, "Pi keeps the native Lore extensions path by default; Antigravity keeps prompt + skills first, does not emulate Pi overlays, makes no auto-install guarantee, and keeps MCP optional.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 1
	}

	report := a.installActionWithOptions(context.Background(), installCommandOptions{DryRun: *dryRun, Yes: *yes, Target: install.TargetID(strings.TrimSpace(*target)), Components: components.ComponentIDs()})
	fmt.Fprint(a.Stdout, output.RenderChecks(report.Title, report.Checks))
	return report.ExitCode
}

func (a *App) runUpdate(args []string) int {
	fs := newFlagSet("update", a.Stderr)
	dryRun := fs.Bool("dry-run", false, "Show the binary-only update plan without replacing the lore executable")
	yes := fs.Bool("yes", false, "Skip the interactive confirmation prompt after all safety checks pass")
	fs.Usage = func() {
		fmt.Fprintln(a.Stderr, "Usage: lore update [--dry-run] [--yes]")
		fmt.Fprintln(a.Stderr, "Update only the active Lore CLI binary; Pi runtime and ~/.pi remain untouched.")
		fmt.Fprintln(a.Stderr, "The command fails closed for unsafe targets and preserves the same safety checks in dry-run and --yes modes.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 1
	}

	report := a.updateActionWithOptions(context.Background(), updateCommandOptions{DryRun: *dryRun, Yes: *yes})
	fmt.Fprint(a.Stdout, output.RenderChecks(report.Title, report.Checks))
	return report.ExitCode
}

func (a *App) runMCP(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(a.Stderr, "Usage: lore mcp <serve|proxy>")
		fmt.Fprintln(a.Stderr, "Use the canonical local Lore Server MCP stdio bridge; it remains separate from the default Pi install path.")
		return 1
	}

	runServe := func(commandName string, alias bool, subargs []string) int {
		fs := newFlagSet("mcp "+commandName, a.Stderr)
		fs.Usage = func() {
			fmt.Fprintf(a.Stderr, "Usage: lore mcp %s\n", commandName)
			fmt.Fprintln(a.Stderr, "Start the local auth-safe Lore Server MCP stdio bridge using saved Lore login state.")
			if alias {
				fmt.Fprintln(a.Stderr, "This deprecated compatibility alias forwards to the canonical lore mcp serve command.")
			} else {
				fmt.Fprintln(a.Stderr, "Use this canonical bridge instead of wiring raw tokens into a harness.")
			}
			fmt.Fprintln(a.Stderr, "This bridge is intentionally separate from the default Pi install path.")
		}
		if err := fs.Parse(subargs); err != nil {
			return 1
		}
		if fs.NArg() != 0 {
			fs.Usage()
			return 1
		}
		client, session, err := a.loadAuthenticatedClient()
		if err != nil {
			fmt.Fprintf(a.Stderr, "mcp %s failed: %s\n", commandName, err)
			return 1
		}
		if err := mcp.Serve(context.Background(), a.Stdin, a.Stdout, mcp.UpstreamFunc(func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
			return client.MCPForward(ctx, session.Token, method, params)
		})); err != nil {
			fmt.Fprintf(a.Stderr, "mcp %s failed: %v\n", commandName, err)
			return 1
		}
		return 0
	}

	switch args[0] {
	case "serve":
		return runServe("serve", false, args[1:])
	case "proxy":
		return runServe("proxy", true, args[1:])
	default:
		fmt.Fprintln(a.Stderr, "Usage: lore mcp <serve|proxy>")
		fmt.Fprintln(a.Stderr, "Use the canonical local Lore Server MCP stdio bridge; it remains separate from the default Pi install path.")
		return 1
	}
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

type componentFlag []string

func (f *componentFlag) String() string {
	if f == nil {
		return ""
	}
	return strings.Join(*f, ",")
}

func (f *componentFlag) Set(value string) error {
	for _, raw := range strings.Split(value, ",") {
		component := strings.TrimSpace(raw)
		if component == "" {
			return fmt.Errorf("component value cannot be empty")
		}
		*f = append(*f, component)
	}
	return nil
}

func (f componentFlag) ComponentIDs() []install.ComponentID {
	components := make([]install.ComponentID, 0, len(f))
	for _, component := range f {
		components = append(components, install.ComponentID(component))
	}
	return components
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
	fmt.Fprintln(w, "  login     Use email + hidden password to mint a reusable token, or use --password-stdin / --token for automation and compatibility")
	fmt.Fprintln(w, "  status    Show login metadata, health, readiness, and auth status")
	fmt.Fprintln(w, "  logout    Remove local login metadata and matching OS keychain credential only")
	fmt.Fprintln(w, "  doctor    Run actionable diagnostics, including optional Pi availability")
	fmt.Fprintln(w, "  install   Install the Pi-first managed runtime using saved Lore auth")
	fmt.Fprintln(w, "  update    Check or apply a binary-only Lore CLI update without touching ~/.pi")
	fmt.Fprintln(w, "  remember  Create one memory via authenticated REST")
	fmt.Fprintln(w, "  api request  Hidden machine broker for allowlisted authenticated API calls")
	fmt.Fprintln(w, "  api mcp-call Hidden machine broker for allowlisted authenticated MCP tool calls")
	fmt.Fprintln(w, "  recall    List memories by explicit authenticated filters")
	fmt.Fprintln(w, "  version   Print build metadata for humans or scripts")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Use explicit subcommands for automation and scripts.")
	fmt.Fprintln(w, "Saved login state uses OS keychain-backed login metadata; raw API tokens are not written to config.json.")
}
