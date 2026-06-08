package install

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/alferio94/lore-cli/internal/agentconfig"
	"github.com/alferio94/lore-cli/internal/auth"
	"github.com/alferio94/lore-cli/internal/config"
	"github.com/alferio94/lore-cli/internal/httpclient"
	"github.com/alferio94/lore-cli/internal/output"
)

type TargetID string

const (
	TargetPi          TargetID = "pi"
	TargetClaudeCode  TargetID = "claude-code"
	TargetOpenCode    TargetID = "opencode"
	TargetCodex       TargetID = "codex"
	TargetAntigravity TargetID = "antigravity"
)

type Target struct {
	ID           TargetID
	Title        string
	Description  string
	Recommended  bool
	Available    bool
	Availability string
}

type ConfigStore interface {
	Load() (config.Config, error)
	Path() (string, error)
}

type ClientFactory func(baseURL string) (httpclient.Client, error)

type AuthLoader interface {
	Load() (auth.Session, error)
}

type Service struct {
	Store            ConfigStore
	Auth             AuthLoader
	ClientFactory    ClientFactory
	AgentConfigStore AgentConfigStore
}

// AgentConfigStore abstracts the agent-config store so it can be injected in tests.
type AgentConfigStore interface {
	Path() (string, error)
	Load() (agentconfig.Config, error)
	EnsureDefault() (agentconfig.Config, bool, error)
}

type PreflightResult struct {
	Targets          []Target
	Checks           []output.Check
	CanContinue      bool
	LoginRequired    bool
	ServerURL        string
	Token            string
	ConfigPath       string
	AgentConfigPath  string
	AgentConfigValid bool
}

func DefaultInstallTarget() TargetID {
	return TargetPi
}

func DefaultTargets() []Target {
	registry, _ := defaultInstallRegistry()
	known := []TargetID{DefaultInstallTarget(), TargetClaudeCode, TargetOpenCode, TargetCodex, TargetAntigravity}
	targets := make([]Target, 0, len(known))
	for _, id := range known {
		target := roadmapTarget(id)
		if registry != nil {
			if adapter, err := registry.Resolve(id); err == nil {
				target = supportedTarget(adapter)
			}
		}
		targets = append(targets, target)
	}
	return targets
}

// SupportedTargets returns the set of currently supported install target IDs.
// The set is the four supported install targets (Pi, OpenCode, Codex,
// Antigravity). Claude Code stays on the roadmap list and is not routable.
func SupportedTargets() []TargetID {
	return []TargetID{TargetPi, TargetOpenCode, TargetCodex, TargetAntigravity}
}

func supportedTarget(adapter HarnessAdapter) Target {
	target := Target{ID: adapter.ID(), Title: adapter.Title(), Available: true, Recommended: adapter.ID() == DefaultInstallTarget()}
	switch adapter.ID() {
	case TargetPi:
		target.Description = "Recommended today; uses hosted Lore MCP via pi-mcp-adapter as the default backend, with optional explicit pi-extensions (lore-footer.ts only) via --component pi-extensions. The deprecated lore-memory extension has been removed and is not available."
	case TargetAntigravity:
		target.Description = "prompt + skills MVP target with managed Gemini lore agent profile and optional direct MCP config; Pi remains the default recommended path while Antigravity keeps harness-owned prompt, skills, and manifest semantics."
	case TargetOpenCode:
		target.Description = "Bounded OpenCode projection: manages ~/.config/opencode/AGENTS.md, skills/<phase>/SKILL.md, a native opencode.json (with the documented $schema, native `agent` overlay wiring each SDD phase to its SKILL.md via {file:...} references, and a native `skills` block — no top-level Lore-only `lore` metadata), and manifest. Lore MCP renders as a documented top-level mcp.lore remote entry with a managed_by: lore-cli ownership marker; the installer fails closed with a typed conflict error and a backup when the existing mcp.lore block is foreign. tui.json uses the native OpenCode $schema and a singular `plugin` string array registering only the community opencode-subagent-statusline. Local plugin .ts files (background-agents.ts, model-variants.ts, opencode-subagent-statusline.ts) are copied to ~/.config/opencode/plugins/; only the community statusline is registered in tui.json. Explicit sdd-engram and logo plugins are never bundled or registered. Legacy installs that shipped the previous top-level `lore` block (in opencode.json) or the plural `plugins` array of objects (in tui.json) are silently repaired to the native shape on the next run. config-only projection: no profiles, bootstrap, or runtime subagents."
	case TargetCodex:
		target.Description = "Managed Codex projection into ~/.codex with remote Lore MCP config, managed agents.md, skills/*.md, and manifest. No codex exec runner or bootstrap behavior is installed."
	default:
		target.Description = "Supported target."
	}
	return target
}

func roadmapTarget(id TargetID) Target {
	switch id {
	case TargetClaudeCode:
		return Target{ID: id, Title: "Claude Code", Description: "Listed for roadmap visibility.", Availability: "Coming soon"}
	default:
		return Target{ID: id, Title: string(id), Description: "Listed for roadmap visibility.", Availability: "Coming soon"}
	}
}

func ResolveInstallTarget(target TargetID) (Target, error) {
	selected := target
	if strings.TrimSpace(string(selected)) == "" {
		selected = DefaultInstallTarget()
	}
	supported := SupportedTargets()
	supportedNames := make([]string, 0, len(supported))
	for _, id := range supported {
		supportedNames = append(supportedNames, string(id))
	}
	available := make([]string, 0, len(DefaultTargets()))
	for _, candidate := range DefaultTargets() {
		if candidate.Available {
			available = append(available, string(candidate.ID))
		}
		if candidate.ID != selected {
			continue
		}
		if !candidate.Available {
			return Target{}, fmt.Errorf("target %q is %s; supported targets: %s", selected, candidate.Availability, strings.Join(supportedNames, ", "))
		}
		return candidate, nil
	}
	if strings.TrimSpace(string(selected)) != "" {
		return Target{}, fmt.Errorf("unsupported target %q; supported targets: %s", selected, strings.Join(supportedNames, ", "))
	}
	return Target{}, fmt.Errorf("unknown target %q", selected)
}

func FormatTargetSelection(targets []Target) string {
	var b strings.Builder
	b.WriteString("Choose an install target:\n")
	for _, target := range targets {
		label := target.Title
		if target.Recommended {
			label += " — Recommended"
		}
		if target.Available {
			fmt.Fprintf(&b, "- %s: %s\n", label, target.Description)
			continue
		}
		fmt.Fprintf(&b, "- %s: %s (%s)\n", label, target.Description, target.Availability)
	}
	b.WriteString("\nPi remains the default recommended path and uses hosted Lore MCP by default. Codex writes managed remote MCP + skills. Antigravity can write ~/.gemini/config/agents/lore.json and optionally write direct MCP config.")
	return b.String()
}

func (s Service) Preflight(ctx context.Context) PreflightResult {
	result := PreflightResult{Targets: DefaultTargets()}
	if s.Store == nil {
		result.Checks = []output.Check{{Name: "config", Status: output.StatusFail, Detail: "install preflight is not configured", Action: "Retry with a configured Lore CLI app instance."}}
		return result
	}

	path, pathErr := s.Store.Path()
	if pathErr != nil {
		result.Checks = []output.Check{{Name: "config-path", Status: output.StatusFail, Detail: pathErr.Error(), Action: "Fix the local config directory permissions or override LORE_CONFIG_DIR."}}
		return result
	}
	result.ConfigPath = path

	cfg, err := s.Store.Load()
	if err != nil {
		if errors.Is(err, config.ErrNotFound) {
			result.LoginRequired = true
			result.Checks = []output.Check{{Name: "config", Status: output.StatusWarn, Detail: fmt.Sprintf("no-config at %s", path), Action: "Run lore login --server <url> --email <email> for password login, or use --token for compatibility mode."}}
			return result
		}
		result.Checks = []output.Check{{Name: "config", Status: output.StatusFail, Detail: err.Error(), Action: "Inspect or remove the local config file and log in again."}}
		return result
	}

	result.ServerURL = cfg.ServerURL
	result.Checks = append(result.Checks, output.Check{Name: "config", Status: output.StatusOK, Detail: fmt.Sprintf("saved login state server=%s path=%s auth=OS keychain-backed metadata only", cfg.ServerURL, path)})
	if s.Auth == nil {
		result.Checks = append(result.Checks, output.Check{Name: "auth", Status: output.StatusFail, Detail: "install auth loader is not configured", Action: "Retry with a configured Lore CLI app instance."})
		return result
	}
	session, err := s.Auth.Load()
	if err != nil {
		result.LoginRequired = true
		action := "Run lore login again with password login or a valid compatibility token."
		var authErr *auth.Error
		if errors.As(err, &authErr) && authErr.Code == auth.ErrCredentialUnavailable {
			action = unavailableCredentialAction()
		}
		result.Checks = append(result.Checks, output.Check{Name: "auth", Status: output.StatusFail, Detail: explainAuthLoadError(err), Action: action})
		return result
	}
	result.ServerURL = session.ServerURL
	result.Token = session.Token
	if s.ClientFactory == nil {
		result.Checks = append(result.Checks, output.Check{Name: "server-url", Status: output.StatusFail, Detail: "install client factory is not configured", Action: "Retry with a configured Lore CLI app instance."})
		return result
	}

	client, err := s.ClientFactory(session.ServerURL)
	if err != nil {
		result.Checks = append(result.Checks, output.Check{Name: "server-url", Status: output.StatusFail, Detail: err.Error(), Action: "Fix the server URL with lore login --server <http(s)://host> --email <email> for password login, or use --token for compatibility mode."})
		return result
	}
	if err := client.Health(ctx); err != nil {
		result.Checks = append(result.Checks, output.Check{Name: "healthz", Status: output.StatusFail, Detail: explainEndpointError(err), Action: "Check server reachability and that the Lore API is running."})
		return result
	}
	result.Checks = append(result.Checks, output.Check{Name: "healthz", Status: output.StatusOK, Detail: "server is live"})
	if err := client.Ready(ctx); err != nil {
		result.Checks = append(result.Checks, output.Check{Name: "readyz", Status: output.StatusFail, Detail: explainEndpointError(err), Action: "Wait for the server to become ready or inspect server logs."})
		return result
	}
	result.Checks = append(result.Checks, output.Check{Name: "readyz", Status: output.StatusOK, Detail: "server is ready"})
	if subject, err := client.Me(ctx, session.Token); err != nil {
		result.LoginRequired = true
		result.Checks = append(result.Checks, output.Check{Name: "auth", Status: output.StatusFail, Detail: explainLoginError(err), Action: "Obtain a valid password-login session or compatibility token and run lore login again."})
		return result
	} else {
		result.Checks = append(result.Checks, output.Check{Name: "auth", Status: output.StatusOK, Detail: output.FormatSubject(subject)})
	}
	result.CanContinue = true
	s.checkAgentConfig(&result)
	return result
}

// checkAgentConfig performs a read-only check of the agent-config contract.
// It reports path, validity, and declared models without implying Codex execution support.
// If an AgentConfigStore is not configured, it is skipped silently.
func (s *Service) checkAgentConfig(result *PreflightResult) {
	if s.AgentConfigStore == nil {
		return
	}

	path, pathErr := s.AgentConfigStore.Path()
	if pathErr != nil {
		result.Checks = append(result.Checks, output.Check{
			Name:   "agent-config",
			Status: output.StatusWarn,
			Detail: fmt.Sprintf("agent-config path could not be resolved: %v", pathErr),
			Action: "Fix the local config directory permissions or override LORE_CONFIG_DIR.",
		})
		return
	}
	result.AgentConfigPath = path

	cfg, err := s.AgentConfigStore.Load()
	if err != nil {
		if errors.Is(err, agentconfig.ErrNotFound) {
			result.Checks = append(result.Checks, output.Check{
				Name:   "agent-config",
				Status: output.StatusWarn,
				Detail: fmt.Sprintf("agent-config not found at %s", path),
				Action: "Agent config is optional; run lore install to generate a default one.",
			})
			return
		}
		result.Checks = append(result.Checks, output.Check{
			Name:   "agent-config",
			Status: output.StatusFail,
			Detail: fmt.Sprintf("agent-config invalid: %v", err),
			Action: "Inspect or remove the agent-config.json file and rerun lore install.",
		})
		return
	}

	if err := cfg.Validate(); err != nil {
		result.AgentConfigValid = false
		result.Checks = append(result.Checks, output.Check{
			Name:   "agent-config",
			Status: output.StatusFail,
			Detail: fmt.Sprintf("agent-config validation failed: %v", err),
			Action: "Inspect or remove the agent-config.json file and rerun lore install.",
		})
		return
	}

	result.AgentConfigValid = true
	// Report path + validity + model count without exposing model names.
	modelCount := len(cfg.SDDAgents)
	result.Checks = append(result.Checks, output.Check{
		Name:   "agent-config",
		Status: output.StatusOK,
		Detail: fmt.Sprintf("agent-config path=%s schema_version=%d sdd_agents=%d", path, cfg.SchemaVersion, modelCount),
	})
}

func explainAuthLoadError(err error) string {
	var authErr *auth.Error
	if errors.As(err, &authErr) {
		switch authErr.Code {
		case auth.ErrCredentialMissing:
			return "saved login state is incomplete"
		case auth.ErrCredentialUnavailable:
			return unavailableCredentialMessage("saved login state could not access secure credential storage")
		case auth.ErrConfigNotFound:
			return "no saved login state"
		default:
			return "saved login state could not be read"
		}
	}
	return err.Error()
}

func explainLoginError(err error) string {
	var unauthorized *httpclient.UnauthorizedError
	if errors.As(err, &unauthorized) {
		return "normal user API token required; /v1/me rejected the provided token"
	}
	return explainEndpointError(err)
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
