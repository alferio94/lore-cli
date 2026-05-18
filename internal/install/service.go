package install

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

type Service struct {
	Store         ConfigStore
	ClientFactory ClientFactory
}

type PreflightResult struct {
	Targets       []Target
	Checks        []output.Check
	CanContinue   bool
	LoginRequired bool
	ServerURL     string
	ConfigPath    string
}

func DefaultTargets() []Target {
	return []Target{
		{ID: TargetPi, Title: "Pi", Description: "Recommended for direct remote Lore access.", Recommended: true, Available: true},
		{ID: TargetClaudeCode, Title: "Claude Code", Description: "Listed for roadmap visibility.", Availability: "Coming soon"},
		{ID: TargetOpenCode, Title: "OpenCode", Description: "Listed for roadmap visibility.", Availability: "Coming soon"},
		{ID: TargetCodex, Title: "Codex", Description: "Listed for roadmap visibility.", Availability: "Coming soon"},
		{ID: TargetAntigravity, Title: "Antigravity", Description: "Listed for roadmap visibility.", Availability: "Coming soon"},
	}
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
	b.WriteString("\nOnly Pi is selectable in this slice.")
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
			result.Checks = []output.Check{{Name: "config", Status: output.StatusWarn, Detail: fmt.Sprintf("no-config at %s", path), Action: "Run lore login --server <url> --token <token>."}}
			return result
		}
		result.Checks = []output.Check{{Name: "config", Status: output.StatusFail, Detail: err.Error(), Action: "Inspect or remove the local config file and log in again."}}
		return result
	}

	result.ServerURL = cfg.ServerURL
	result.Checks = append(result.Checks, output.Check{Name: "config", Status: output.StatusOK, Detail: fmt.Sprintf("configured server=%s token=%s path=%s", cfg.ServerURL, config.RedactToken(cfg.APIToken), path)})
	if strings.TrimSpace(cfg.ServerURL) == "" || strings.TrimSpace(cfg.APIToken) == "" {
		result.LoginRequired = true
		result.Checks = append(result.Checks, output.Check{Name: "auth", Status: output.StatusFail, Detail: "saved login config is incomplete", Action: "Run lore login again with a valid server URL and normal user API token."})
		return result
	}
	if s.ClientFactory == nil {
		result.Checks = append(result.Checks, output.Check{Name: "server-url", Status: output.StatusFail, Detail: "install client factory is not configured", Action: "Retry with a configured Lore CLI app instance."})
		return result
	}

	client, err := s.ClientFactory(cfg.ServerURL)
	if err != nil {
		result.Checks = append(result.Checks, output.Check{Name: "server-url", Status: output.StatusFail, Detail: err.Error(), Action: "Fix the server URL with lore login --server <http(s)://host> --token <token>."})
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
	if subject, err := client.Me(ctx, cfg.APIToken); err != nil {
		result.LoginRequired = true
		result.Checks = append(result.Checks, output.Check{Name: "auth", Status: output.StatusFail, Detail: explainLoginError(err), Action: "Obtain a valid normal user API token and run lore login again."})
		return result
	} else {
		result.Checks = append(result.Checks, output.Check{Name: "auth", Status: output.StatusOK, Detail: output.FormatSubject(subject)})
	}
	result.CanContinue = true
	return result
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
