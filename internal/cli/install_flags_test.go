package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/config"
	"github.com/alferio94/lore-cli/internal/httpclient"
	"github.com/alferio94/lore-cli/internal/version"
)

func TestInstallCommandDryRunAcceptsExplicitPiTargetAndComponents(t *testing.T) {
	homeDir, piAgentDir := setIsolatedPiHome(t)
	configDir := t.TempDir()
	store := &fakeStore{path: filepath.Join(configDir, "config.json"), loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token=target-components"}}
	client := &fakeClient{subject: httpclient.Subject{UserID: "user-1", Kind: "user"}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.ExecutablePath = func() (string, error) { return "/usr/local/bin/lore", nil }
	app.BuildInfo = version.Info{Version: "v1.2.3"}

	if exitCode := app.Run([]string{"install", "--dry-run", "--target", "pi", "--component", "pi-extensions"}); exitCode != 0 {
		t.Fatalf("install --dry-run --target pi --component pi-extensions exitCode = %d, want 0, stderr=%q stdout=%q", exitCode, stderr.String(), stdout.String())
	}
	if _, err := os.Stat(filepath.Join(piAgentDir, "lore-install.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest stat err = %v, want not exist after dry-run", err)
	}
	out := stdout.String()
	for _, want := range []string{"install_target=pi", "runtime=pi-remote-package", "remote_package=git:github.com/nicobailon/pi-mcp-adapter@1091b34da83d58bd2d9fcaff2dc31f449a94bf1f", "components=core-pack,pi-extensions", "mode=dry-run", "managed_action="} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout = %q, want substring %q", out, want)
		}
	}
	assertNoTokenLeak(t, out, stderr.String(), "secret-token=target-components")
	_ = homeDir
}

func TestInstallCommandRejectsUnsupportedInstallTarget(t *testing.T) {
	_, piAgentDir := setIsolatedPiHome(t)
	configDir := t.TempDir()
	store := &fakeStore{path: filepath.Join(configDir, "config.json"), loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token=unsupported-target"}}
	client := &fakeClient{subject: httpclient.Subject{UserID: "user-1", Kind: "user"}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.ExecutablePath = func() (string, error) { return "/usr/local/bin/lore", nil }
	app.BuildInfo = version.Info{Version: "v1.2.3"}

	if exitCode := app.Run([]string{"install", "--target", "claude-code"}); exitCode != 1 {
		t.Fatalf("install --target claude-code exitCode = %d, want 1, stderr=%q stdout=%q", exitCode, stderr.String(), stdout.String())
	}
	out := stdout.String()
	for _, want := range []string{"target \"claude-code\" is Coming soon", "uses hosted Lore MCP", "Choose an install target:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout = %q, want substring %q", out, want)
		}
	}
	if _, err := os.Stat(filepath.Join(piAgentDir, "lore-install.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest stat err = %v, want no install writes on unsupported target", err)
	}
	assertNoTokenLeak(t, out, stderr.String(), "secret-token=unsupported-target")
}

func TestInstallCommandRejectsOpenCodeTarget(t *testing.T) {
	// Hard-removed OpenCode target: `lore install --target opencode` is
	// rejected as unsupported, with no installer side effects.
	homeDir := t.TempDir()
	configDir := t.TempDir()
	store := &fakeStore{path: filepath.Join(configDir, "config.json"), loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token=opencode-rejected"}}
	client := &fakeClient{subject: httpclient.Subject{UserID: "user-1", Kind: "user"}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.UserHomeDir = func() (string, error) { return homeDir, nil }
	app.BuildInfo = version.Info{Version: "v1.2.3"}

	if exitCode := app.Run([]string{"install", "--target", "opencode"}); exitCode != 1 {
		t.Fatalf("install --target opencode exitCode = %d, want 1, stderr=%q stdout=%q", exitCode, stderr.String(), stdout.String())
	}
	out := stdout.String()
	for _, want := range []string{`unsupported target "opencode"`, "supported targets: pi, codex, antigravity"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout = %q, want substring %q", out, want)
		}
	}
	// No OpenCode managed surface written to disk.
	for _, path := range []string{
		filepath.Join(homeDir, ".config", "opencode", "AGENTS.md"),
		filepath.Join(homeDir, ".config", "opencode", "opencode.json"),
		filepath.Join(homeDir, ".config", "opencode", "lore-install.json"),
		filepath.Join(homeDir, ".config", "opencode", "skills", "sdd-apply", "SKILL.md"),
	} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stat %s err = %v, want no installer writes for rejected OpenCode target", path, err)
		}
	}
	assertNoTokenLeak(t, out, stderr.String(), "secret-token=opencode-rejected")
}

func TestInstallCommandSupportsAntigravityDryRunAndApply(t *testing.T) {
	homeDir := t.TempDir()
	configDir := t.TempDir()
	store := &fakeStore{path: filepath.Join(configDir, "config.json"), loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token=antigravity-wording"}}
	client := &fakeClient{subject: httpclient.Subject{UserID: "user-1", Kind: "user"}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.UserHomeDir = func() (string, error) { return homeDir, nil }
	app.ExecutablePath = func() (string, error) { return "/usr/local/bin/lore", nil }
	app.BuildInfo = version.Info{Version: "v1.2.3"}

	t.Run("default antigravity install includes managed MCP config", func(t *testing.T) {
		if exitCode := app.Run([]string{"install", "--dry-run", "--target", "antigravity"}); exitCode != 0 {
			t.Fatalf("install --dry-run --target antigravity exitCode = %d, want 0, stderr=%q stdout=%q", exitCode, stderr.String(), stdout.String())
		}
		out := stdout.String()
		for _, want := range []string{"install_target=antigravity", "runtime=antigravity-prompt-skills", "components=core-pack,lore-server-mcp", "mode=dry-run", "managed_action=create:../GEMINI.md", "managed_action=create:../config/agents/lore.json", "managed_action=create:../config/mcp_config.json", "managed_action=create:skills/sdd-apply/SKILL.md", "managed_action=create:lore-install.json"} {
			if !strings.Contains(out, want) {
				t.Fatalf("stdout = %q, want substring %q", out, want)
			}
		}
		if _, err := os.Stat(filepath.Join(homeDir, ".gemini", "GEMINI.md")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("GEMINI.md stat err = %v, want no prompt writes after dry-run", err)
		}
		assertNoTokenLeak(t, out, stderr.String(), "secret-token=antigravity-wording")
		stdout.Reset()
		stderr.Reset()
	})

	t.Run("managed mcp config uses direct server URL and bearer header", func(t *testing.T) {
		if exitCode := app.Run([]string{"install", "--target", "antigravity", "--component", "lore-server-mcp"}); exitCode != 0 {
			t.Fatalf("install --target antigravity --component lore-server-mcp exitCode = %d, want 0, stderr=%q stdout=%q", exitCode, stderr.String(), stdout.String())
		}
		out := stdout.String()
		for _, want := range []string{"install_target=antigravity", "runtime=antigravity-prompt-skills", "components=core-pack,lore-server-mcp", "managed_action=create:../config/agents/lore.json", "managed_action=create:../config/mcp_config.json", "managed_action=create:lore-install.json", "managed MCP config path="} {
			if !strings.Contains(out, want) {
				t.Fatalf("stdout = %q, want substring %q", out, want)
			}
		}
		if !strings.Contains(out, filepath.ToSlash(filepath.Join(homeDir, ".gemini", "config", "mcp_config.json"))) {
			t.Fatalf("stdout = %q, want managed mcp_config.json path", out)
		}
		if strings.Contains(out, "Coming soon") {
			t.Fatalf("stdout = %q, want real Antigravity install flow instead of roadmap wording", out)
		}
		for _, path := range []string{
			filepath.Join(homeDir, ".gemini", "GEMINI.md"),
			filepath.Join(homeDir, ".gemini", "antigravity-cli", "skills", "sdd-apply", "SKILL.md"),
			filepath.Join(homeDir, ".gemini", "antigravity-cli", "lore-install.json"),
			filepath.Join(homeDir, ".gemini", "config", "mcp_config.json"),
			filepath.Join(homeDir, ".gemini", "config", "agents", "lore.json"),
		} {
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("stat %s error = %v, want installed Antigravity artifact", path, err)
			}
		}
		skillBody, err := os.ReadFile(filepath.Join(homeDir, ".gemini", "antigravity-cli", "skills", "sdd-apply", "SKILL.md"))
		if err != nil {
			t.Fatalf("ReadFile(installed antigravity skill) error = %v", err)
		}
		skillText := string(skillBody)
		for _, want := range []string{"~/.gemini/antigravity-cli/skills/sdd-apply/SKILL.md", "~/.gemini/antigravity-cli/skills/_shared/sdd-phase-common.md"} {
			if !strings.Contains(skillText, want) {
				t.Fatalf("installed Antigravity skill = %q, want substring %q", skillText, want)
			}
		}
		for _, forbidden := range []string{"~/.pi/agent/skills/", "agents/lore-managed", "managedBy:", "phase:", "skillPolicyMode:"} {
			if strings.Contains(skillText, forbidden) {
				t.Fatalf("installed Antigravity skill = %q, want %q omitted", skillText, forbidden)
			}
		}
		agentBody, err := os.ReadFile(filepath.Join(homeDir, ".gemini", "config", "agents", "lore.json"))
		if err != nil {
			t.Fatalf("ReadFile(lore.json) error = %v", err)
		}
		agentText := string(agentBody)
		var agentProfile map[string]any
		if err := json.Unmarshal(agentBody, &agentProfile); err != nil {
			t.Fatalf("json.Unmarshal(lore.json) error = %v", err)
		}
		for key, want := range map[string]string{"id": "lore", "name": "Lore", "description": "Global Lore orchestrator specialized in SDD workflows and persistent context through Lore MCP"} {
			if got, _ := agentProfile[key].(string); got != want {
				t.Fatalf("lore.json %s = %q, want %q", key, got, want)
			}
		}
		if got, _ := agentProfile["default"].(bool); !got {
			t.Fatalf("lore.json default = %v, want true", agentProfile["default"])
		}
		if _, ok := agentProfile["tools"]; ok {
			t.Fatalf("lore.json = %q, want no tools field", agentText)
		}
		if instruction, _ := agentProfile["systemInstruction"].(string); !strings.Contains(instruction, "Lore MCP tools are exposed according to the user's current role and permissions") {
			t.Fatalf("lore.json systemInstruction = %q, want embedded role-based tool guidance", instruction)
		}
		mcpBody, err := os.ReadFile(filepath.Join(homeDir, ".gemini", "config", "mcp_config.json"))
		if err != nil {
			t.Fatalf("ReadFile(mcp_config.json) error = %v", err)
		}
		mcpText := string(mcpBody)
		for _, want := range []string{`"serverUrl": "https://example.test/v1/mcp"`, `"headers": {`, `"Authorization": "Bearer secret-token=antigravity-wording"`} {
			if !strings.Contains(mcpText, want) {
				t.Fatalf("mcp_config.json = %q, want substring %q", mcpText, want)
			}
		}
		for _, forbidden := range []string{"\"command\"", "--auth-file", "lore_mcp_auth.json", "daemon", "autostart"} {
			if strings.Contains(mcpText, forbidden) {
				t.Fatalf("mcp_config.json = %q, want %q omitted", mcpText, forbidden)
			}
		}
		assertNoTokenLeak(t, out, stderr.String(), "secret-token=antigravity-wording")
	})
}

func TestInstallCommandAcceptsLoreServerMCPWithPiTarget(t *testing.T) {
	_, piAgentDir := setIsolatedPiHome(t)
	configDir := t.TempDir()
	store := &fakeStore{path: filepath.Join(configDir, "config.json"), loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token=pi-mcp"}}
	client := &fakeClient{subject: httpclient.Subject{UserID: "user-1", Kind: "user"}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.ExecutablePath = func() (string, error) { return "/usr/local/bin/lore", nil }
	app.BuildInfo = version.Info{Version: "v1.2.3"}

	// lore-server-mcp is now the default Pi backend and is accepted.
	if exitCode := app.Run([]string{"install", "--dry-run", "--target", "pi", "--component", "lore-server-mcp"}); exitCode != 0 {
		t.Fatalf("install --dry-run --target pi --component lore-server-mcp exitCode = %d, want 0, stderr=%q stdout=%q", exitCode, stderr.String(), stdout.String())
	}
	out := stdout.String()
	for _, want := range []string{"install_target=pi", "remote_package=git:github.com/nicobailon/pi-mcp-adapter@1091b34da83d58bd2d9fcaff2dc31f449a94bf1f", "components=core-pack,lore-server-mcp"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout = %q, want substring %q", out, want)
		}
	}
	if _, err := os.Stat(filepath.Join(piAgentDir, "lore-install.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest stat err = %v, want no manifest after dry-run", err)
	}
	assertNoTokenLeak(t, out, stderr.String(), "secret-token=pi-mcp")
}

func TestInstallUsageIncludesTargetAndComponentFlags(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loadErr: config.ErrNotFound}
	app, _, stderr := newTestApp(store, nil)

	if exitCode := app.Run([]string{"install", "--help"}); exitCode != 1 {
		t.Fatalf("install --help exitCode = %d, want 1 with usage output", exitCode)
	}
	for _, want := range []string{"--target", "--component", "Pi-first managed runtime", "portable Lore agent pack", "core-pack", "pi-extensions", "Antigravity", "prompt + skills MVP", "plaintext Authorization bearer token", "~/.gemini/config/mcp_config.json", "~/.gemini/config/agents/lore.json", "codex", "~/.codex/config.toml", "/v1/mcp"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want substring %q", stderr.String(), want)
		}
	}
}
