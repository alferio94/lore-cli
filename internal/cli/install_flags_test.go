package cli

import (
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
	for _, want := range []string{"install_target=pi", "runtime=pi-remote-package", "remote_package=git:github.com/alferio94/lore-pi-subagents", "components=core-pack,pi-extensions", "managed_local_files=3", "mode=dry-run", "managed_action="} {
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
	for _, want := range []string{"target \"claude-code\" is Coming soon", "Pi-native Lore extensions path", "Choose an install target:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout = %q, want substring %q", out, want)
		}
	}
	if _, err := os.Stat(filepath.Join(piAgentDir, "lore-install.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest stat err = %v, want no install writes on unsupported target", err)
	}
	assertNoTokenLeak(t, out, stderr.String(), "secret-token=unsupported-target")
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

	if exitCode := app.Run([]string{"install", "--dry-run", "--target", "antigravity"}); exitCode != 0 {
		t.Fatalf("install --dry-run --target antigravity exitCode = %d, want 0, stderr=%q stdout=%q", exitCode, stderr.String(), stdout.String())
	}
	out := stdout.String()
	for _, want := range []string{"install_target=antigravity", "runtime=antigravity-prompt-skills", "components=core-pack", "mode=dry-run", "managed_action=create:../GEMINI.md", "managed_action=create:skills/sdd-apply/SKILL.md", "managed_action=create:lore-install.json"} {
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
	if exitCode := app.Run([]string{"install", "--target", "antigravity"}); exitCode != 0 {
		t.Fatalf("install --target antigravity exitCode = %d, want 0, stderr=%q stdout=%q", exitCode, stderr.String(), stdout.String())
	}
	out = stdout.String()
	for _, want := range []string{"install_target=antigravity", "runtime=antigravity-prompt-skills", "managed_action=create:../GEMINI.md", "managed_action=create:skills/sdd-apply/SKILL.md", "managed_action=create:lore-install.json"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout = %q, want substring %q", out, want)
		}
	}
	if strings.Contains(out, "Coming soon") {
		t.Fatalf("stdout = %q, want real Antigravity install flow instead of roadmap wording", out)
	}
	for _, path := range []string{
		filepath.Join(homeDir, ".gemini", "GEMINI.md"),
		filepath.Join(homeDir, ".gemini", "antigravity-cli", "skills", "sdd-apply", "SKILL.md"),
		filepath.Join(homeDir, ".gemini", "antigravity-cli", "lore-install.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("stat %s error = %v, want installed Antigravity artifact", path, err)
		}
	}
	assertNoTokenLeak(t, out, stderr.String(), "secret-token=antigravity-wording")
}

func TestInstallCommandRejectsUnsupportedPiMCPComponent(t *testing.T) {
	_, piAgentDir := setIsolatedPiHome(t)
	configDir := t.TempDir()
	store := &fakeStore{path: filepath.Join(configDir, "config.json"), loaded: config.Config{ServerURL: "https://example.test", APIToken: "secret-token=pi-mcp"}}
	client := &fakeClient{subject: httpclient.Subject{UserID: "user-1", Kind: "user"}}
	app, stdout, stderr := newTestApp(store, func(baseURL string) (httpclient.Client, error) { return client, nil })
	app.ExecutablePath = func() (string, error) { return "/usr/local/bin/lore", nil }
	app.BuildInfo = version.Info{Version: "v1.2.3"}

	if exitCode := app.Run([]string{"install", "--dry-run", "--target", "pi", "--component", "lore-server-mcp"}); exitCode != 1 {
		t.Fatalf("install --dry-run --target pi --component lore-server-mcp exitCode = %d, want 1, stderr=%q stdout=%q", exitCode, stderr.String(), stdout.String())
	}
	out := stdout.String()
	for _, want := range []string{"lore-server-mcp", "target \"pi\"", "Pi-native Lore extensions path"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout = %q, want substring %q", out, want)
		}
	}
	if _, err := os.Stat(filepath.Join(piAgentDir, "lore-install.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest stat err = %v, want no install writes on unsupported component", err)
	}
	assertNoTokenLeak(t, out, stderr.String(), "secret-token=pi-mcp")
}

func TestInstallUsageIncludesTargetAndComponentFlags(t *testing.T) {
	store := &fakeStore{path: "/tmp/lore/config.json", loadErr: config.ErrNotFound}
	app, _, stderr := newTestApp(store, nil)

	if exitCode := app.Run([]string{"install", "--help"}); exitCode != 1 {
		t.Fatalf("install --help exitCode = %d, want 1 with usage output", exitCode)
	}
	for _, want := range []string{"--target", "--component", "Pi-first managed runtime", "portable Lore agent pack", "core-pack", "pi-extensions", "Antigravity", "prompt + skills MVP"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want substring %q", stderr.String(), want)
		}
	}
}
