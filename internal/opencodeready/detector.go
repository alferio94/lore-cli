package opencodeready

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// realCommandRunner uses os/exec for actual command execution.
type realCommandRunner struct{}

// Run executes the named command with the given arguments using the current environment.
func (r *realCommandRunner) Run(ctx context.Context, name string, args ...string) (Result, error) {
	return runCommand(ctx, name, args...)
}

// realFS uses os package functions for actual filesystem access.
type realFS struct{}

// Stat returns file information for the given path.
func (r *realFS) Stat(path string) (FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &osFileInfo{info: info}, nil
}

// ReadFile returns the file content for the given path.
func (r *realFS) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// TempWritableProbe verifies that dir is writable by creating and removing a temp file.
// This is only used for explicitly probe-safe surfaces.
func (r *realFS) TempWritableProbe(dir string) error {
	return tempWritableProbe(dir)
}

// osFileInfo wraps os.FileInfo for the FileInfo interface.
type osFileInfo struct {
	info os.FileInfo
}

func (i *osFileInfo) IsDir() bool  { return i.info.IsDir() }
func (i *osFileInfo) Mode() int    { return int(i.info.Mode()) }

// resolveHome returns the effective home directory, using LookupEnv when set.
func resolveHome(opts Options) string {
	if opts.HomeDir != "" {
		return opts.HomeDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// resolveEnv resolves an environment variable, using LookupEnv when set.
func resolveEnv(key string, opts Options) string {
	if opts.LookupEnv != nil {
		if val, ok := opts.LookupEnv(key); ok {
			return val
		}
	}
	return os.Getenv(key)
}

// openCodeRootDir returns the OpenCode config root for the given home directory.
func openCodeRootDir(homeDir string) string {
	return filepath.Join(homeDir, ".config", "opencode")
}

// openCodeConfigDir returns the OpenCode config directory path.
func openCodeConfigDir(homeDir string) string {
	return openCodeRootDir(homeDir)
}

// openCodePluginsDir returns the OpenCode plugins directory path.
func openCodePluginsDir(homeDir string) string {
	return filepath.Join(openCodeRootDir(homeDir), "plugins")
}

// openCodeJSONPath returns the OpenCode config file path.
func openCodeJSONPath(homeDir string) string {
	return filepath.Join(openCodeRootDir(homeDir), "opencode.json")
}

// probeCLIPresence checks whether the OpenCode CLI is available on PATH.
func probeCLIPresence(ctx context.Context, runner CommandRunner, opts Options) Finding {
	if runner == nil {
		return Finding{ID: FindingIDOpenCodeCLI, Status: StatusUnknown, Remediation: "internal error: no command runner"}
	}

	// Try `opencode --version` first
	result, err := runner.Run(ctx, "opencode", "--version")
	if err == nil && result.ExitCode == 0 {
		return Finding{
			ID:          FindingIDOpenCodeCLI,
			Status:      StatusReady,
			Evidence:    []string{"opencode --version available on PATH"},
			Remediation: "",
		}
	}

	// Fall back to `opencode version`
	result, err = runner.Run(ctx, "opencode", "version")
	if err == nil && result.ExitCode == 0 {
		return Finding{
			ID:          FindingIDOpenCodeCLI,
			Status:      StatusReady,
			Evidence:    []string{"opencode version available on PATH"},
			Remediation: "",
		}
	}

	// Fall back to `opencode --help` as a last resort
	result, err = runner.Run(ctx, "opencode", "--help")
	if err == nil && result.ExitCode == 0 {
		return Finding{
			ID:          FindingIDOpenCodeCLI,
			Status:      StatusReady,
			Evidence:    []string{"opencode --help available on PATH"},
			Remediation: "",
		}
	}

	return Finding{
		ID:          FindingIDOpenCodeCLI,
		Status:      StatusBlocking,
		Evidence:    []string{"opencode not found on PATH"},
		Remediation: "Install OpenCode and ensure the binary is on PATH: https://opencode.ai",
	}
}

// probeVersion attempts to parse the OpenCode version string.
func probeVersion(ctx context.Context, runner CommandRunner, opts Options) Finding {
	if runner == nil {
		return Finding{ID: FindingIDOpenCodeVersion, Status: StatusUnknown, Remediation: "internal error: no command runner"}
	}

	// Try `opencode --version`
	result, err := runner.Run(ctx, "opencode", "--version")
	if err == nil && result.ExitCode == 0 {
		version := parseVersionOutput(result.Stdout + result.Stderr)
		if version != "" {
			return Finding{
				ID:          FindingIDOpenCodeVersion,
				Status:      StatusReady,
				Evidence:    []string{"version=" + version},
				Remediation: "",
			}
		}
	}

	// Fall back to `opencode version`
	result, err = runner.Run(ctx, "opencode", "version")
	if err == nil && result.ExitCode == 0 {
		version := parseVersionOutput(result.Stdout + result.Stderr)
		if version != "" {
			return Finding{
				ID:          FindingIDOpenCodeVersion,
				Status:      StatusReady,
				Evidence:    []string{"version=" + version},
				Remediation: "",
			}
		}
	}

	return Finding{
		ID:          FindingIDOpenCodeVersion,
		Status:      StatusUnknown,
		Evidence:    []string{"version could not be parsed from output"},
		Remediation: "Ensure OpenCode version command produces parseable output",
	}
}

// parseVersionOutput extracts a semver-like version string from command output.
// It returns the first token matching N.N.N or N.N.N-beta/alpha pattern.
func parseVersionOutput(output string) string {
	output = strings.TrimSpace(output)
	// Look for version patterns like "v1.2.3" or "1.2.3" or "version 1.2.3" or "version=1.2.3"
	fields := strings.Fields(output)
	for _, field := range fields {
		// Strip prefixes in correct order
		if strings.HasPrefix(field, "version=") {
			field = strings.TrimPrefix(field, "version=")
		} else if strings.HasPrefix(field, "version") {
			// "version X.Y.Z" -> just "X.Y.Z"
			field = strings.TrimPrefix(field, "version")
			field = strings.TrimSpace(field)
		}
		field = strings.TrimPrefix(field, "v")
		// Simple semver-ish check: N.N.N
		if len(field) >= 5 && field[0] >= '0' && field[0] <= '9' {
			dots := 0
			valid := true
			for i, ch := range field {
				if ch == '.' {
					dots++
					continue
				}
				if ch < '0' || ch > '9' {
					if i > 0 && (ch == '-' || ch == '+' || (ch >= 'a' && ch <= 'z')) {
						continue
					}
					valid = false
					break
				}
			}
			if valid && dots >= 2 {
				return field
			}
		}
	}
	return ""
}

// probeConfigDir checks the OpenCode config directory state.
func probeConfigDir(ctx context.Context, fs FS, opts Options) Finding {
	if fs == nil {
		return Finding{ID: FindingIDOpenCodeConfig, Status: StatusUnknown, Remediation: "internal error: no filesystem"}
	}

	homeDir := resolveHome(opts)
	if homeDir == "" {
		return Finding{ID: FindingIDOpenCodeConfig, Status: StatusUnknown, Remediation: "could not resolve home directory"}
	}

	configDir := openCodeConfigDir(homeDir)

	// Check if config dir exists
	_, err := fs.Stat(configDir)
	if err == nil {
		// Config directory exists
		return Finding{
			ID:             FindingIDOpenCodeConfig,
			Status:         StatusReady,
			Evidence:       []string{"config dir exists at " + configDir},
			Remediation:    "",
			NewlyInstalled: false,
		}
	}

	// Config dir does not exist - check if parent is writable (missing-but-creatable)
	// Only perform temp probe if explicitly allowed
	if opts.AllowTempProbe {
		parentDir := filepath.Dir(configDir)
		if err := fs.TempWritableProbe(parentDir); err == nil {
			return Finding{
				ID:             FindingIDOpenCodeConfig,
				Status:         StatusWarn,
				Evidence:       []string{"config dir missing but parent writable", "path=" + configDir},
				Remediation:    "OpenCode config directory will be created on first run or install",
				NewlyInstalled:  true,
			}
		}
	}

	// Parent not writable or AllowTempProbe is false - unknown (not blocking)
	return Finding{
		ID:             FindingIDOpenCodeConfig,
		Status:         StatusUnknown,
		Evidence:       []string{"config dir missing and parent not writable or temp probes disabled", "path=" + configDir},
		Remediation:    "Configure OPENCODE_HOME or ensure ~/.config is accessible",
		NewlyInstalled: false,
	}
}

// probePluginsDir checks the OpenCode plugins directory state.
func probePluginsDir(ctx context.Context, fs FS, opts Options) Finding {
	if fs == nil {
		return Finding{ID: FindingIDOpenCodePlugins, Status: StatusUnknown, Remediation: "internal error: no filesystem"}
	}

	homeDir := resolveHome(opts)
	if homeDir == "" {
		return Finding{ID: FindingIDOpenCodePlugins, Status: StatusUnknown, Remediation: "could not resolve home directory"}
	}

	pluginsDir := openCodePluginsDir(homeDir)

	// Check if plugins dir exists
	_, err := fs.Stat(pluginsDir)
	if err == nil {
		// Plugins directory exists
		return Finding{
			ID:             FindingIDOpenCodePlugins,
			Status:         StatusReady,
			Evidence:       []string{"plugins dir exists at " + pluginsDir},
			Remediation:    "",
			NewlyInstalled: false,
		}
	}

	// Plugins dir does not exist - check if parent is writable (missing-but-creatable)
	// Only perform temp probe if explicitly allowed
	if opts.AllowTempProbe {
		parentDir := filepath.Dir(pluginsDir)
		if err := fs.TempWritableProbe(parentDir); err == nil {
			return Finding{
				ID:             FindingIDOpenCodePlugins,
				Status:         StatusWarn,
				Evidence:       []string{"plugins dir missing but parent writable", "path=" + pluginsDir},
				Remediation:    "Plugins directory can be created when installing plugins",
				NewlyInstalled:  true,
			}
		}
	}

	// Parent not writable or AllowTempProbe is false - unknown (not blocking)
	return Finding{
		ID:             FindingIDOpenCodePlugins,
		Status:         StatusUnknown,
		Evidence:       []string{"plugins dir missing and parent not writable or temp probes disabled", "path=" + pluginsDir},
		Remediation:    "Ensure ~/.config/opencode is accessible",
		NewlyInstalled: false,
	}
}

// probePluginAPI performs a conservative read-only check for plugin API evidence.
// It returns "unknown" unless evidence can be gathered without writes or unsafe probes.
func probePluginAPI(ctx context.Context, runner CommandRunner, opts Options) Finding {
	if runner == nil {
		return Finding{ID: FindingIDOpenCodePluginAPI, Status: StatusUnknown, Remediation: "internal error: no command runner"}
	}

	// Try `opencode plugin --help` as a read-only check for plugin command presence
	result, err := runner.Run(ctx, "opencode", "plugin", "--help")
	if err == nil && result.ExitCode == 0 {
		output := result.Stdout + result.Stderr
		if strings.Contains(output, "plugin") || strings.Contains(output, "install") {
			return Finding{
				ID:          FindingIDOpenCodePluginAPI,
				Status:      StatusReady,
				Evidence:    []string{"opencode plugin command available"},
				Remediation: "",
			}
		}
	}

	// Try `opencode debug info` for plugin loading evidence
	result, err = runner.Run(ctx, "opencode", "debug", "info")
	if err == nil && result.ExitCode == 0 {
		output := result.Stdout + result.Stderr
		if strings.Contains(output, "plugin") || strings.Contains(output, "loaded") {
			return Finding{
				ID:          FindingIDOpenCodePluginAPI,
				Status:      StatusReady,
				Evidence:    []string{"opencode debug info reports plugin state"},
				Remediation: "",
			}
		}
	}

	// Cannot verify safely - conservative unknown
	return Finding{
		ID:          FindingIDOpenCodePluginAPI,
		Status:      StatusUnknown,
		Evidence:    []string{"plugin API could not be verified read-only"},
		Remediation: "Ensure opencode plugin command or debug info is available for plugin verification",
	}
}

// probeRuntime performs a conservative check for runtime prerequisites (e.g., Bun).
// It returns "unknown" when verification is not safely possible.
func probeRuntime(ctx context.Context, runner CommandRunner, opts Options) Finding {
	if runner == nil {
		return Finding{ID: FindingIDOpenCodeRuntime, Status: StatusUnknown, Remediation: "internal error: no command runner"}
	}

	// Check for Bun availability (read-only PATH check)
	result, err := runner.Run(ctx, "bun", "--version")
	if err == nil && result.ExitCode == 0 {
		return Finding{
			ID:          FindingIDOpenCodeRuntime,
			Status:      StatusReady,
			Evidence:    []string{"bun runtime available for plugin execution"},
			Remediation: "",
		}
	}

	// Bun not available - conservative unknown (not blocking, but not ready)
	return Finding{
		ID:          FindingIDOpenCodeRuntime,
		Status:      StatusUnknown,
		Evidence:    []string{"bun runtime not available on PATH"},
		Remediation: "Install Bun for local plugin support: https://bun.sh",
	}
}

// probeNativeAgents performs a conservative check for native agent support.
// It returns "unknown" when evidence cannot be gathered safely.
func probeNativeAgents(ctx context.Context, runner CommandRunner, opts Options) Finding {
	if runner == nil {
		return Finding{ID: FindingIDOpenCodeAgents, Status: StatusUnknown, Remediation: "internal error: no command runner"}
	}

	// Try `opencode agent --help` or `opencode agent list` for agent command presence
	result, err := runner.Run(ctx, "opencode", "agent", "--help")
	if err == nil && result.ExitCode == 0 {
		return Finding{
			ID:          FindingIDOpenCodeAgents,
			Status:      StatusReady,
			Evidence:    []string{"opencode agent command available"},
			Remediation: "",
		}
	}

	result, err = runner.Run(ctx, "opencode", "agent", "list")
	if err == nil && result.ExitCode == 0 {
		return Finding{
			ID:          FindingIDOpenCodeAgents,
			Status:      StatusReady,
			Evidence:    []string{"opencode agent list available"},
			Remediation: "",
		}
	}

	// Cannot verify safely - conservative unknown
	return Finding{
		ID:          FindingIDOpenCodeAgents,
		Status:      StatusUnknown,
		Evidence:    []string{"native agent support could not be verified read-only"},
		Remediation: "Ensure opencode agent commands are available for native agent verification",
	}
}