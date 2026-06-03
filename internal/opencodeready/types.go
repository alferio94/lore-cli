// Package opencodeready provides read-only OpenCode readiness detection for future
// plugin-backed subagent support. It inspects CLI presence, version, and config/plugin
// directory state without performing any persistent writes to user config or state.
//
// Readiness detection is fail-closed: findings classified as "unknown" are treated as
// not ready. No plugin files, agent definitions, or configuration are written by this
// package. Future gated work is required before plugin installation or runtime wiring.
package opencodeready

import (
	"context"
	"strings"
)

// Status represents the readiness classification of an individual finding or the overall
// report. "unknown" is treated as not ready by the aggregation logic.
type Status string

const (
	StatusReady    Status = "ready"
	StatusWarn     Status = "warn"
	StatusBlocking Status = "blocking"
	StatusUnknown  Status = "unknown"
)

// FindingID identifies the source of a readiness finding.
type FindingID string

const (
	FindingIDOpenCodeCLI      FindingID = "opencode-cli"
	FindingIDOpenCodeVersion  FindingID = "opencode-version"
	FindingIDOpenCodeConfig   FindingID = "opencode-config"
	FindingIDOpenCodePlugins  FindingID = "opencode-plugins-dir"
	FindingIDOpenCodePluginAPI FindingID = "opencode-plugin-api"
	FindingIDOpenCodeRuntime  FindingID = "opencode-runtime"
	FindingIDOpenCodeAgents   FindingID = "opencode-native-agents"
)

// Finding represents a single readiness check result with evidence and remediation guidance.
type Finding struct {
	// ID identifies which check produced this finding.
	ID FindingID
	// Status is the readiness classification.
	Status Status
	// Evidence contains non-sensitive diagnostic details from the check.
	Evidence []string
	// Remediation provides actionable guidance when status is not "ready".
	Remediation string
	// NewlyInstalled is true when the check reveals a missing-but-creatable path
	// (e.g., config directory not present but its parent is writable). This means
	// the path can be created by a future install, not that lore-cli created it.
	NewlyInstalled bool
}

// Report is the aggregated result of all OpenCode readiness checks.
type Report struct {
	// Overall is the aggregated readiness status. It is "ready" only when no findings
	// are "blocking" and at least one critical finding is "ready".
	Overall Status
	// Findings contains the individual check results, ordered by FindingID.
	Findings []Finding
	// Version is the parsed OpenCode version string when available, otherwise empty.
	Version string
}

// CommandRunner abstracts command execution for deterministic testing.
// Implementations must be read-only and must not produce side effects.
type CommandRunner interface {
	// Run executes a command with the given context, name, and arguments.
	// It returns the combined stdout and stderr content plus any execution error.
	// Implementations should not mutate system state beyond ephemeral process execution.
	Run(ctx context.Context, name string, args ...string) (Result, error)
}

// Result captures the outcome of a command execution.
type Result struct {
	// Stdout is the captured standard output.
	Stdout string
	// Stderr is the captured standard error.
	Stderr string
	// ExitCode is the process exit code; 0 indicates success.
	ExitCode int
	// Error is the execution error when the process could not run, or nil on success.
	Error error
}

// FS abstracts filesystem access for deterministic testing.
// All operations are read-only; no implementations should write to non-temp surfaces.
type FS interface {
	// Stat returns file information for the given path.
	// It returns an error when the path does not exist or is inaccessible.
	Stat(path string) (FileInfo, error)
	// ReadFile returns the file content for the given path.
	// It returns an error when the file cannot be read.
	ReadFile(path string) ([]byte, error)
	// TempWritableProbe verifies that a directory is writable without creating files.
	// It returns nil on success or an error describing the probe result.
	// This must only be used for intentionally probe-safe surfaces (temp dirs, safe targets).
	TempWritableProbe(dir string) error
}

// FileInfo abstracts os.FileInfo for testability.
type FileInfo interface {
	// IsDir returns true when the path is a directory.
	IsDir() bool
	// Mode returns the file mode bits.
	Mode() int
}

// Options configures the OpenCode readiness probe.
type Options struct {
	// HomeDir is the user's home directory for resolving OpenCode paths.
	HomeDir string
	// Env is the environment variable map passed to command runners.
	// When nil, os.Environ() is used.
	Env map[string]string
	// AllowTempProbe permits filesystem write probes on specific safe surfaces
	// (e.g., parent directory writability checks). Default is false.
	AllowTempProbe bool
	// LookupEnv is used to resolve environment variables in path expansion.
	// When nil, os.LookupEnv is used.
	LookupEnv func(key string) (string, bool)
}

// DefaultOptions returns a safe default Options using the current environment.
func DefaultOptions() Options {
	return Options{}
}

// Probe runs all OpenCode readiness checks and returns an aggregated report.
// It uses the provided CommandRunner and FS; if either is nil, real implementations are used.
// Probe is read-only: it inspects the environment and reports findings without writing
// to user config, plugin directories, or agent definitions.
func Probe(ctx context.Context, runner CommandRunner, fs FS, opts Options) (Report, error) {
	if runner == nil {
		runner = &realCommandRunner{}
	}
	if fs == nil {
		fs = &realFS{}
	}

	var allFindings []Finding

	// 1. OpenCode CLI presence check
	cliFinding := probeCLIPresence(ctx, runner, opts)
	allFindings = append(allFindings, cliFinding)

	// 2. OpenCode version check (only if CLI is present)
	var version string
	if cliFinding.Status == StatusReady {
		versionFinding := probeVersion(ctx, runner, opts)
		allFindings = append(allFindings, versionFinding)
		version = extractVersion(versionFinding)
	}

	// 3. Config directory check
	configFinding := probeConfigDir(ctx, fs, opts)
	allFindings = append(allFindings, configFinding)

	// 4. Plugins directory check
	pluginsFinding := probePluginsDir(ctx, fs, opts)
	allFindings = append(allFindings, pluginsFinding)

	// 5. Plugin API/load evidence (conservative — returns unknown when not safely verifiable)
	pluginAPIFinding := probePluginAPI(ctx, runner, opts)
	allFindings = append(allFindings, pluginAPIFinding)

	// 6. Runtime prerequisites (conservative — returns unknown when not safely verifiable)
	runtimeFinding := probeRuntime(ctx, runner, opts)
	allFindings = append(allFindings, runtimeFinding)

	// 7. Native agent support (conservative — returns unknown when not safely verifiable)
	agentsFinding := probeNativeAgents(ctx, runner, opts)
	allFindings = append(allFindings, agentsFinding)

	overall := aggregate(allFindings)

	return Report{
		Overall:   overall,
		Findings:  allFindings,
		Version:   version,
	}, nil
}

// aggregate computes the overall readiness status from individual findings.
// It is fail-closed: any "blocking" finding makes overall "blocking".
// Any "unknown" finding prevents "ready" — unknown means we cannot confirm readiness.
// Only when no blocking exists AND no unknown findings exist AND at least one critical
// signal is ready does overall become "ready".
// "warn" is used when there are no blocking/unknown findings but no critical signals are ready.
// "warn" is also used when there are warn findings but no critical signals are ready
// AND no unknown findings are present — warn does not imply full readiness.
func aggregate(findings []Finding) Status {
	hasBlocking := false
	hasUnknown := false
	hasWarn := false
	hasReadyCritical := false

	for _, f := range findings {
		switch f.Status {
		case StatusBlocking:
			hasBlocking = true
		case StatusUnknown:
			hasUnknown = true
		case StatusWarn:
			hasWarn = true
		case StatusReady:
			// Critical findings: CLI and version are the primary readiness signals
			if f.ID == FindingIDOpenCodeCLI || f.ID == FindingIDOpenCodeVersion {
				hasReadyCritical = true
			}
		}
	}

	if hasBlocking {
		return StatusBlocking
	}
	if hasUnknown {
		return StatusUnknown
	}
	// Any warn finding prevents overall "ready" — warn means partial readiness,
	// not full readiness. The ordering is: blocking > unknown > warn > ready.
	if hasWarn {
		return StatusWarn
	}
	if hasReadyCritical {
		return StatusReady
	}
	return StatusWarn
}

// extractVersion returns the parsed version string from a version finding.
func extractVersion(f Finding) string {
	if f.Status != StatusReady || len(f.Evidence) == 0 {
		return ""
	}
	// Evidence format: "version=1.2.3" or raw output containing version tokens.
	// Strip the version= prefix if present to return just the version string.
	v := f.Evidence[0]
	if strings.HasPrefix(v, "version=") {
		return strings.TrimPrefix(v, "version=")
	}
	return v
}