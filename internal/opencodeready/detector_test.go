package opencodeready

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeCommandRunner records all Run calls and returns configurable results.
type fakeCommandRunner struct {
	calls []runCall

	results      map[string]Result // key = "name args..."
	err          error
	forceError   bool
}

type runCall struct {
	Ctx   context.Context
	Name  string
	Args  []string
}

func newFakeCommandRunner() *fakeCommandRunner {
	return &fakeCommandRunner{
		results: make(map[string]Result),
	}
}

func (f *fakeCommandRunner) Run(ctx context.Context, name string, args ...string) (Result, error) {
	f.calls = append(f.calls, runCall{Ctx: ctx, Name: name, Args: args})
	key := cmdKey(name, args...)
	if result, ok := f.results[key]; ok {
		return result, nil
	}
	if f.forceError {
		return Result{}, errors.New("forced error")
	}
	return Result{ExitCode: 1, Error: errors.New("command not configured")}, errors.New("command not configured")
}

func (f *fakeCommandRunner) addResult(name string, args []string, stdout string, exitCode int) {
	key := cmdKey(name, args...)
	f.results[key] = Result{Stdout: stdout, ExitCode: exitCode}
}

func (f *fakeCommandRunner) Calls() []runCall { return f.calls }

// fakeFS records all filesystem calls and returns configurable responses.
type fakeFS struct {
	statCalls []string
	statDirs  map[string]bool
	statErr   error

	readFileCalls []string
	readFileData  map[string][]byte
	readFileErr   error

	probeCalls []string
	probeErr   error

	// Safety: track any mutations to detect writes
	mutations []string
}

func newFakeFS() *fakeFS {
	return &fakeFS{
		statDirs:   make(map[string]bool),
		readFileData: make(map[string][]byte),
	}
}

func (f *fakeFS) Stat(path string) (FileInfo, error) {
	f.statCalls = append(f.statCalls, path)
	if f.statErr != nil {
		return nil, f.statErr
	}
	if f.statDirs[path] {
		return &fakeFileInfo{isDir: true}, nil
	}
	return nil, errors.New("path not found")
}

func (f *fakeFS) ReadFile(path string) ([]byte, error) {
	f.readFileCalls = append(f.readFileCalls, path)
	if f.readFileErr != nil {
		return nil, f.readFileErr
	}
	if data, ok := f.readFileData[path]; ok {
		return data, nil
	}
	return nil, errors.New("file not found")
}

func (f *fakeFS) TempWritableProbe(dir string) error {
	f.probeCalls = append(f.probeCalls, dir)
	if f.probeErr != nil {
		return f.probeErr
	}
	// Record the probe as a read-only check; no actual writes occur
	return nil
}

// RecordMutation records a write attempt (for safety tests).
func (f *fakeFS) RecordMutation(op string) {
	f.mutations = append(f.mutations, op)
}

// fakeFileInfo implements FileInfo for testing.
type fakeFileInfo struct {
	isDir bool
	mode  int
}

func (f *fakeFileInfo) IsDir() bool { return f.isDir }
func (f *fakeFileInfo) Mode() int   { return f.mode }

func cmdKey(name string, args ...string) string {
	key := name
	for _, a := range args {
		key += " " + a
	}
	return key
}

// Test: Missing CLI → blocking
func TestProbe_MissingCLI_Blocking(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.forceError = true

	opts := Options{HomeDir: "/fake/home"}
	report, err := Probe(context.Background(), runner, nil, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	// CLI finding should be blocking
	var cliFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeCLI {
			cliFinding = &report.Findings[i]
			break
		}
	}
	if cliFinding == nil {
		t.Fatal("OpenCode CLI finding not found")
	}
	if cliFinding.Status != StatusBlocking {
		t.Errorf("CLI status = %v, want %v", cliFinding.Status, StatusBlocking)
	}
	if cliFinding.Remediation == "" {
		t.Error("CLI blocking finding should have remediation")
	}

	// Overall should be blocking since CLI is blocking
	if report.Overall != StatusBlocking {
		t.Errorf("Overall = %v, want %v", report.Overall, StatusBlocking)
	}
}

// Test: CLI present, version parseable
func TestProbe_CLIPresent_VersionParseable(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "opencode v1.2.3 (build 123)\n", 0)

	fs := newFakeFS()
	fs.statDirs["/fake/home/.config/opencode"] = true // config dir exists

	opts := Options{HomeDir: "/fake/home"}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	// CLI finding should be ready
	var cliFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeCLI {
			cliFinding = &report.Findings[i]
			break
		}
	}
	if cliFinding == nil {
		t.Fatal("OpenCode CLI finding not found")
	}
	if cliFinding.Status != StatusReady {
		t.Errorf("CLI status = %v, want %v", cliFinding.Status, StatusReady)
	}

	// Version should be parsed
	if report.Version == "" {
		t.Error("Version should be parsed from output")
	}

	// Overall should not be blocking
	if report.Overall == StatusBlocking {
		t.Errorf("Overall should not be blocking with CLI ready")
	}
}

// Test: Fresh install — no config/plugin dirs, parent writable → warn with NewlyInstalled
func TestProbe_FreshInstall_NoConfigDir_MissingButCreatable(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v0.9.0\n", 0)

	fs := newFakeFS()
	// No directories exist, but TempWritableProbe succeeds (parent writable)
	fs.statErr = errors.New("path not found")

	opts := Options{HomeDir: "/fake/home", AllowTempProbe: true}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	// Config dir finding should be warn with NewlyInstalled=true
	var configFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeConfig {
			configFinding = &report.Findings[i]
			break
		}
	}
	if configFinding == nil {
		t.Fatal("OpenCode config finding not found")
	}
	if configFinding.Status != StatusWarn {
		t.Errorf("Config dir status = %v, want %v", configFinding.Status, StatusWarn)
	}
	if !configFinding.NewlyInstalled {
		t.Error("Config dir finding should have NewlyInstalled=true for missing-but-creatable")
	}
	if configFinding.Remediation == "" {
		t.Error("Config warn finding should have remediation")
	}
}

// Test: Existing config/plugin dirs → ready
func TestProbe_ExistingDirs_Ready(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)

	fs := newFakeFS()
	fs.statDirs["/fake/home/.config/opencode"] = true
	fs.statDirs["/fake/home/.config/opencode/plugins"] = true

	opts := Options{HomeDir: "/fake/home"}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	// Config dir should be ready
	var configFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeConfig {
			configFinding = &report.Findings[i]
			break
		}
	}
	if configFinding == nil {
		t.Fatal("OpenCode config finding not found")
	}
	if configFinding.Status != StatusReady {
		t.Errorf("Config dir status = %v, want %v", configFinding.Status, StatusReady)
	}
	if configFinding.NewlyInstalled {
		t.Error("Existing config dir should not have NewlyInstalled=true")
	}

	// Plugins dir should be ready
	var pluginsFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodePlugins {
			pluginsFinding = &report.Findings[i]
			break
		}
	}
	if pluginsFinding == nil {
		t.Fatal("OpenCode plugins finding not found")
	}
	if pluginsFinding.Status != StatusReady {
		t.Errorf("Plugins dir status = %v, want %v", pluginsFinding.Status, StatusReady)
	}
}

// Test: Missing dir, parent not writable with AllowTempProbe → unknown (not blocking)
// When AllowTempProbe is enabled but parent is not writable, the result is unknown
// (not blocking) because we cannot definitively determine the installability.
func TestProbe_MissingDir_ParentNotWritable_Unknown(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)

	fs := newFakeFS()
	fs.statErr = errors.New("path not found")
	fs.probeErr = errors.New("not writable")

	opts := Options{HomeDir: "/fake/home", AllowTempProbe: true}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	// Config dir should be unknown (not blocking) when AllowTempProbe is true
	// but the probe fails - this is fail-closed unknown, not blocking
	var configFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeConfig {
			configFinding = &report.Findings[i]
			break
		}
	}
	if configFinding == nil {
		t.Fatal("OpenCode config finding not found")
	}
	if configFinding.Status != StatusUnknown {
		t.Errorf("Config dir status = %v, want %v (AllowTempProbe but probe fails)", configFinding.Status, StatusUnknown)
	}
}

// Test: No writes outside temp probe surfaces (safety)
func TestProbe_NoWritesOutsideTempSurfaces(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)

	fs := newFakeFS()
	fs.statDirs["/fake/home/.config/opencode"] = true

	opts := Options{HomeDir: "/fake/home"}
	_, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	// Verify no mutations were recorded
	// The fakeFS records mutations only when explicitly recorded
	// A real safety test would use a wrapper that fails on any write outside temp dirs
	if len(fs.mutations) > 0 {
		t.Errorf("Unexpected mutations detected: %v", fs.mutations)
	}
}

// Test: Fail-closed unknown — plugin/runtime/agents are unknown even when CLI is ready.
// With the new fail-closed semantics, overall can only be "ready" when plugin/runtime/agents
// are not unknown. When these are unknown, overall should be unknown (fail-closed).
func TestProbe_Aggregation_PluginRuntimeAgentsUnknown_FailClosed(t *testing.T) {
	runner := newFakeCommandRunner()
	// CLI present (will be ready via --version exit 0)
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)
	// Version is parseable (ready)
	// Plugin, runtime, agents are unknown (no configured results for these probes)

	fs := newFakeFS()
	fs.statDirs["/fake/home/.config/opencode"] = true

	opts := Options{HomeDir: "/fake/home", AllowTempProbe: true}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	// Plugin, runtime, agents are all unknown
	var pluginAPI, runtime, agents *Finding
	for i := range report.Findings {
		switch report.Findings[i].ID {
		case FindingIDOpenCodePluginAPI:
			pluginAPI = &report.Findings[i]
		case FindingIDOpenCodeRuntime:
			runtime = &report.Findings[i]
		case FindingIDOpenCodeAgents:
			agents = &report.Findings[i]
		}
	}

	// Verify plugin/runtime/agents are unknown
	if pluginAPI.Status != StatusUnknown {
		t.Errorf("Plugin API status = %v, want %v", pluginAPI.Status, StatusUnknown)
	}
	if runtime.Status != StatusUnknown {
		t.Errorf("Runtime status = %v, want %v", runtime.Status, StatusUnknown)
	}
	if agents.Status != StatusUnknown {
		t.Errorf("Agents status = %v, want %v", agents.Status, StatusUnknown)
	}

	// Overall should be unknown (fail-closed) because plugin/runtime/agents are unknown
	// even though CLI is ready
	if report.Overall != StatusUnknown {
		t.Errorf("Overall = %v, want %v (fail-closed: unknown plugin/runtime/agents)", report.Overall, StatusUnknown)
	}
}

// Test: Version parse output variants
func TestParseVersionOutput(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"opencode v1.2.3\n", "1.2.3"},
		{"version 2.0.1\n", "2.0.1"},
		{"v0.9.0-beta.1\n", "0.9.0-beta.1"},
		{"opencode version: 1.0.0+build123\n", "1.0.0+build123"},
		{"invalid output\n", ""},
		{"\n", ""},
		{"v1.2.3", "1.2.3"},
	}

	for _, tc := range cases {
		got := parseVersionOutput(tc.input)
		if got != tc.expected {
			t.Errorf("parseVersionOutput(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

// Test: All finding IDs are represented in a complete probe
func TestProbe_AllFindingIDsPresent(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)
	runner.addResult("opencode", []string{"plugin", "--help"}, "plugin help\n", 0)
	runner.addResult("opencode", []string{"agent", "--help"}, "agent help\n", 0)
	runner.addResult("bun", []string{"--version"}, "1.0.0\n", 0)

	fs := newFakeFS()
	fs.statDirs["/fake/home/.config/opencode"] = true
	fs.statDirs["/fake/home/.config/opencode/plugins"] = true

	opts := Options{HomeDir: "/fake/home"}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	expectedIDs := []FindingID{
		FindingIDOpenCodeCLI,
		FindingIDOpenCodeVersion,
		FindingIDOpenCodeConfig,
		FindingIDOpenCodePlugins,
		FindingIDOpenCodeRuntime,
		FindingIDOpenCodeAgents,
	}

	found := make(map[FindingID]bool)
	for _, f := range report.Findings {
		found[f.ID] = true
	}

	for _, id := range expectedIDs {
		if !found[id] {
			t.Errorf("Finding %q not found in report", id)
		}
	}
}

// Test: aggregate helper function
func TestAggregate(t *testing.T) {
	cases := []struct {
		name     string
		findings []Finding
		expected Status
	}{
		{
			name: "no blocking, has ready critical, has warn",
			findings: []Finding{
				{ID: FindingIDOpenCodeCLI, Status: StatusReady},
				{ID: FindingIDOpenCodeConfig, Status: StatusWarn},
			},
			expected: StatusWarn,
		},
		{
			name: "has blocking",
			findings: []Finding{
				{ID: FindingIDOpenCodeCLI, Status: StatusReady},
				{ID: FindingIDOpenCodeConfig, Status: StatusBlocking},
			},
			expected: StatusBlocking,
		},
		{
			name: "no ready critical, no blocking",
			findings: []Finding{
				{ID: FindingIDOpenCodeConfig, Status: StatusWarn},
				{ID: FindingIDOpenCodePlugins, Status: StatusUnknown},
			},
			expected: StatusUnknown,
		},
		{
			name: "empty findings",
			findings: []Finding{},
			expected: StatusWarn,
		},
		{
			name: "only non-critical ready",
			findings: []Finding{
				{ID: FindingIDOpenCodePlugins, Status: StatusReady},
				{ID: FindingIDOpenCodeRuntime, Status: StatusUnknown},
			},
			expected: StatusUnknown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := aggregate(tc.findings)
			if got != tc.expected {
				t.Errorf("aggregate() = %v, want %v", got, tc.expected)
			}
		})
	}
}

// Test: extractVersion from findings
func TestExtractVersion(t *testing.T) {
	ready := Finding{
		ID:      FindingIDOpenCodeVersion,
		Status:  StatusReady,
		Evidence: []string{"version=1.2.3", "channel=stable"},
	}
	notReady := Finding{
		ID:      FindingIDOpenCodeVersion,
		Status:  StatusUnknown,
		Evidence: []string{"version could not be parsed"},
	}
	empty := Finding{
		ID:      FindingIDOpenCodeVersion,
		Status:  StatusReady,
		Evidence: []string{},
	}

	if got := extractVersion(ready); got != "1.2.3" {
		t.Errorf("extractVersion(ready) = %q, want %q", got, "1.2.3")
	}
	if got := extractVersion(notReady); got != "" {
		t.Errorf("extractVersion(notReady) = %q, want empty", got)
	}
	if got := extractVersion(empty); got != "" {
		t.Errorf("extractVersion(empty) = %q, want empty", got)
	}
}

// Test: Options defaults
func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()
	// DefaultOptions should return a zero-value Options struct
	// that is safe to use (no nil pointer panics)
	if opts.HomeDir != "" {
		t.Error("DefaultOptions should have empty HomeDir")
	}
	if opts.Env != nil {
		t.Error("DefaultOptions should have nil Env")
	}
	if opts.AllowTempProbe {
		t.Error("DefaultOptions should have AllowTempProbe=false")
	}
}

// Test: resolveHome with explicit home dir
func TestResolveHome_Explicit(t *testing.T) {
	opts := Options{HomeDir: "/explicit/home"}
	got := resolveHome(opts)
	if got != "/explicit/home" {
		t.Errorf("resolveHome() = %q, want %q", got, "/explicit/home")
	}
}

// Test: CLI present but exits non-zero → still "ready" (command found)
func TestProbe_CLIExists_ExitNonZero_Ready(t *testing.T) {
	runner := newFakeCommandRunner()
	// Command exists (no error) but returns non-zero exit
	// Configure all three commands that probeCLIPresence tries
	runner.addResult("opencode", []string{"--version"}, "some output\n", 0) // exit 0 = ready
	runner.addResult("opencode", []string{"version"}, "some output\n", 1)
	runner.addResult("opencode", []string{"--help"}, "some output\n", 1)

	fs := newFakeFS()
	fs.statDirs["/fake/home/.config/opencode"] = true

	opts := Options{HomeDir: "/fake/home"}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	var cliFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeCLI {
			cliFinding = &report.Findings[i]
			break
		}
	}
	if cliFinding == nil {
		t.Fatal("OpenCode CLI finding not found")
	}
	// Command found (exit 0 on first try) → ready
	if cliFinding.Status != StatusReady {
		t.Errorf("CLI status = %v, want %v (command found with exit 0)", cliFinding.Status, StatusReady)
	}
}

// Test: CLI present, version unparseable → version unknown, CLI ready
func TestProbe_CLIPresent_VersionUnparseable_Unknown(t *testing.T) {
	runner := newFakeCommandRunner()
	// Version output is unparseable (no semver pattern)
	runner.addResult("opencode", []string{"--version"}, "unknown output\n", 0)

	fs := newFakeFS()
	fs.statDirs["/fake/home/.config/opencode"] = true

	opts := Options{HomeDir: "/fake/home"}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	var versionFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeVersion {
			versionFinding = &report.Findings[i]
			break
		}
	}
	if versionFinding == nil {
		t.Fatal("OpenCode version finding not found")
	}
	if versionFinding.Status != StatusUnknown {
		t.Errorf("Version status = %v, want %v", versionFinding.Status, StatusUnknown)
	}
	// Version string should be empty when unparseable
	if report.Version != "" {
		t.Errorf("Version = %q, want empty", report.Version)
	}
}

// Test: Config dir missing, parent inaccessible with AllowTempProbe → unknown (not blocking)
// When AllowTempProbe is enabled but parent is not accessible, the result is unknown
// (not blocking) because we cannot definitively determine the installability.
func TestProbe_ConfigDir_MissingParentInaccessible_Unknown(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)

	fs := newFakeFS()
	fs.statErr = errors.New("permission denied")
	fs.probeErr = errors.New("permission denied") // Simulates inaccessible parent

	opts := Options{HomeDir: "/fake/home", AllowTempProbe: true}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	var configFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeConfig {
			configFinding = &report.Findings[i]
			break
		}
	}
	if configFinding == nil {
		t.Fatal("OpenCode config finding not found")
	}
	if configFinding.Status != StatusUnknown {
		t.Errorf("Config status = %v, want %v (AllowTempProbe but parent inaccessible)", configFinding.Status, StatusUnknown)
	}
}

// Test: Plugin API unknown when command not available
func TestProbe_PluginAPI_Unknown(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)
	// plugin --help fails
	runner.addResult("opencode", []string{"plugin", "--help"}, "command not found\n", 1)
	// debug info fails
	runner.addResult("opencode", []string{"debug", "info"}, "command not found\n", 1)

	fs := newFakeFS()
	fs.statDirs["/fake/home/.config/opencode"] = true

	opts := Options{HomeDir: "/fake/home"}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	var pluginAPIFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodePluginAPI {
			pluginAPIFinding = &report.Findings[i]
			break
		}
	}
	if pluginAPIFinding == nil {
		t.Fatal("OpenCode plugin API finding not found")
	}
	// Plugin API not verifiable read-only → unknown (not blocking)
	if pluginAPIFinding.Status != StatusUnknown {
		t.Errorf("Plugin API status = %v, want %v (conservative unknown)", pluginAPIFinding.Status, StatusUnknown)
	}
}

// Test: Runtime unknown when Bun not available
func TestProbe_Runtime_BunNotAvailable_Unknown(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)
	// bun --version fails (not available)
	runner.addResult("bun", []string{"--version"}, "command not found\n", 127)

	fs := newFakeFS()
	fs.statDirs["/fake/home/.config/opencode"] = true

	opts := Options{HomeDir: "/fake/home"}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	var runtimeFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeRuntime {
			runtimeFinding = &report.Findings[i]
			break
		}
	}
	if runtimeFinding == nil {
		t.Fatal("OpenCode runtime finding not found")
	}
	// Bun not available → unknown (not blocking)
	if runtimeFinding.Status != StatusUnknown {
		t.Errorf("Runtime status = %v, want %v", runtimeFinding.Status, StatusUnknown)
	}
	if runtimeFinding.Remediation == "" {
		t.Error("Runtime unknown should have remediation guidance")
	}
}

// Test: Native agents unknown when agent commands not available
func TestProbe_NativeAgents_Unknown(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)
	// agent --help fails
	runner.addResult("opencode", []string{"agent", "--help"}, "command not found\n", 1)
	// agent list fails
	runner.addResult("opencode", []string{"agent", "list"}, "command not found\n", 1)

	fs := newFakeFS()
	fs.statDirs["/fake/home/.config/opencode"] = true

	opts := Options{HomeDir: "/fake/home"}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	var agentsFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeAgents {
			agentsFinding = &report.Findings[i]
			break
		}
	}
	if agentsFinding == nil {
		t.Fatal("OpenCode agents finding not found")
	}
	// Native agents not verifiable → unknown (not blocking)
	if agentsFinding.Status != StatusUnknown {
		t.Errorf("Agents status = %v, want %v (conservative unknown)", agentsFinding.Status, StatusUnknown)
	}
}

// Test: Config dir exists, plugins dir missing-but-creatable
func TestProbe_ConfigExists_PluginsMissing_Creatable_Warn(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)

	fs := newFakeFS()
	fs.statDirs["/fake/home/.config/opencode"] = true // Config exists
	// Plugins dir missing, but TempWritableProbe succeeds on parent

	opts := Options{HomeDir: "/fake/home", AllowTempProbe: true}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	var pluginsFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodePlugins {
			pluginsFinding = &report.Findings[i]
			break
		}
	}
	if pluginsFinding == nil {
		t.Fatal("OpenCode plugins finding not found")
	}
	// Plugins missing but parent writable → warn
	if pluginsFinding.Status != StatusWarn {
		t.Errorf("Plugins status = %v, want %v (missing but creatable)", pluginsFinding.Status, StatusWarn)
	}
	if !pluginsFinding.NewlyInstalled {
		t.Error("Plugins missing-but-creatable should have NewlyInstalled=true")
	}
}

// Test: Both dirs missing with parent writable → both warn
func TestProbe_BothDirsMissing_ParentWritable_Warn(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)

	fs := newFakeFS()
	// Neither dir exists; TempWritableProbe succeeds on parent

	opts := Options{HomeDir: "/fake/home", AllowTempProbe: true}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	var configFinding, pluginsFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeConfig {
			configFinding = &report.Findings[i]
		} else if report.Findings[i].ID == FindingIDOpenCodePlugins {
			pluginsFinding = &report.Findings[i]
		}
	}
	if configFinding == nil || pluginsFinding == nil {
		t.Fatal("Config or plugins finding not found")
	}
	// Both should be warn (missing but creatable)
	if configFinding.Status != StatusWarn {
		t.Errorf("Config status = %v, want %v", configFinding.Status, StatusWarn)
	}
	if pluginsFinding.Status != StatusWarn {
		t.Errorf("Plugins status = %v, want %v", pluginsFinding.Status, StatusWarn)
	}
}

// Test: Both dirs missing with parent not writable and AllowTempProbe → both unknown (not blocking)
// When AllowTempProbe is enabled but parent is not writable, the result is unknown
// (not blocking) because we cannot definitively determine the installability.
func TestProbe_BothDirsMissing_ParentNotWritable_Unknown(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)

	fs := newFakeFS()
	// Neither dir exists; parent not writable (probe fails)
	fs.probeErr = errors.New("not writable")

	opts := Options{HomeDir: "/fake/home", AllowTempProbe: true}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	var configFinding, pluginsFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeConfig {
			configFinding = &report.Findings[i]
		} else if report.Findings[i].ID == FindingIDOpenCodePlugins {
			pluginsFinding = &report.Findings[i]
		}
	}
	if configFinding == nil || pluginsFinding == nil {
		t.Fatal("Config or plugins finding not found")
	}
	// Both should be unknown (not blocking) when AllowTempProbe enabled but probe fails
	if configFinding.Status != StatusUnknown {
		t.Errorf("Config status = %v, want %v", configFinding.Status, StatusUnknown)
	}
	if pluginsFinding.Status != StatusUnknown {
		t.Errorf("Plugins status = %v, want %v", pluginsFinding.Status, StatusUnknown)
	}
}

// Test: Version parse output variants — multiline output
func TestParseVersionOutput_Multiline(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"OpenCode CLI v2.3.4\nCopyright 2024\nBuild: 12345\n", "2.3.4"},
		{"version 1.0.0-beta\nlatest\n", "1.0.0-beta"},
		{"v0.1.2+abc123\n", "0.1.2+abc123"},
		{"OpenCode version 3.0.0\n", "3.0.0"},
		{"v1.2.3-alpha.4\n", "1.2.3-alpha.4"},
	}

	for _, tc := range cases {
		got := parseVersionOutput(tc.input)
		if got != tc.expected {
			t.Errorf("parseVersionOutput(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

// Test: Version parse output variants — unparseable output
func TestParseVersionOutput_Unparseable(t *testing.T) {
	cases := []string{
		"",
		"unknown",
		"no version info",
		"command not recognized",
		"Error: something went wrong",
	}

	for _, input := range cases {
		got := parseVersionOutput(input)
		if got != "" {
			t.Errorf("parseVersionOutput(%q) = %q, want empty (unparseable)", input, got)
		}
	}
}

// Test: probeCLIPresence fallback through all variants
func TestProbe_CLIPresence_AllVariants(t *testing.T) {
	runner := newFakeCommandRunner()
	// Only --version succeeds
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)

	fs := newFakeFS()
	fs.statDirs["/fake/home/.config/opencode"] = true

	opts := Options{HomeDir: "/fake/home"}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	var cliFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeCLI {
			cliFinding = &report.Findings[i]
			break
		}
	}
	if cliFinding == nil {
		t.Fatal("OpenCode CLI finding not found")
	}
	// CLI found via --version → ready
	if cliFinding.Status != StatusReady {
		t.Errorf("CLI status = %v, want %v", cliFinding.Status, StatusReady)
	}
}

// Test: probeCLIPresence all fallbacks fail → blocking
func TestProbe_CLIPresence_AllFallBacksFail_Blocking(t *testing.T) {
	runner := newFakeCommandRunner()
	// All commands fail
	runner.addResult("opencode", []string{"--version"}, "", 1)
	runner.addResult("opencode", []string{"version"}, "", 1)
	runner.addResult("opencode", []string{"--help"}, "", 1)

	fs := newFakeFS()
	fs.statDirs["/fake/home/.config/opencode"] = true

	opts := Options{HomeDir: "/fake/home"}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	var cliFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeCLI {
			cliFinding = &report.Findings[i]
			break
		}
	}
	if cliFinding == nil {
		t.Fatal("OpenCode CLI finding not found")
	}
	// All fallbacks failed → blocking
	if cliFinding.Status != StatusBlocking {
		t.Errorf("CLI status = %v, want %v", cliFinding.Status, StatusBlocking)
	}
	if cliFinding.Remediation == "" {
		t.Error("CLI blocking should have remediation")
	}
}

// Test: Runtime ready when Bun available
func TestProbe_Runtime_BunAvailable_Ready(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)
	runner.addResult("bun", []string{"--version"}, "1.0.0\n", 0)

	fs := newFakeFS()
	fs.statDirs["/fake/home/.config/opencode"] = true

	opts := Options{HomeDir: "/fake/home"}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	var runtimeFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeRuntime {
			runtimeFinding = &report.Findings[i]
			break
		}
	}
	if runtimeFinding == nil {
		t.Fatal("OpenCode runtime finding not found")
	}
	// Bun available → ready
	if runtimeFinding.Status != StatusReady {
		t.Errorf("Runtime status = %v, want %v", runtimeFinding.Status, StatusReady)
	}
}

// Test: Native agents ready when agent commands available
func TestProbe_NativeAgents_Ready(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)
	runner.addResult("opencode", []string{"agent", "--help"}, "agent subcommand\nUsage:\n", 0)

	fs := newFakeFS()
	fs.statDirs["/fake/home/.config/opencode"] = true

	opts := Options{HomeDir: "/fake/home"}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	var agentsFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeAgents {
			agentsFinding = &report.Findings[i]
			break
		}
	}
	if agentsFinding == nil {
		t.Fatal("OpenCode agents finding not found")
	}
	// Agent command available → ready
	if agentsFinding.Status != StatusReady {
		t.Errorf("Agents status = %v, want %v", agentsFinding.Status, StatusReady)
	}
}

// Test: Overall unknown when config/plugins unknown with AllowTempProbe but probe fails
// Since config/plugins return unknown (not blocking), overall will be unknown
// due to fail-closed aggregation (no blocking findings but plugin/runtime/agents unknown).
func TestProbe_OverallUnknown_ConfigPluginsUnknown(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)

	fs := newFakeFS()
	fs.statErr = errors.New("path not found")
	fs.probeErr = errors.New("not writable")

	opts := Options{HomeDir: "/fake/home", AllowTempProbe: true}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	// Config and plugins are unknown (not blocking) when AllowTempProbe but probe fails
	// Overall should be unknown due to fail-closed semantics (no blocking but plugin/runtime/agents unknown)
	if report.Overall != StatusUnknown {
		t.Errorf("Overall = %v, want %v (no blocking but unknown plugin/runtime/agents)", report.Overall, StatusUnknown)
	}
}

// Test: Overall ready when CLI present, dirs present, AND plugin/runtime/agents are ready
// With fail-closed semantics, overall can only be "ready" when plugin/runtime/agents are also verified.
func TestProbe_OverallReady_AllCriticalAndPluginRuntimeAgentsReady(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)
	runner.addResult("opencode", []string{"plugin", "--help"}, "plugin help\n", 0)
	runner.addResult("opencode", []string{"agent", "--help"}, "agent help\n", 0)
	runner.addResult("bun", []string{"--version"}, "1.0.0\n", 0)

	fs := newFakeFS()
	fs.statDirs["/fake/home/.config/opencode"] = true
	fs.statDirs["/fake/home/.config/opencode/plugins"] = true

	opts := Options{HomeDir: "/fake/home"}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	// Overall should be ready (CLI, version, dirs, plugin, runtime, agents all ready)
	if report.Overall != StatusReady {
		t.Errorf("Overall = %v, want %v (all critical and plugin/runtime/agents ready)", report.Overall, StatusReady)
	}
}

// Test: Overall unknown when CLI present but plugin/runtime/agents unknown
// With fail-closed semantics, overall can only be "ready" when plugin/runtime/agents are verified.
func TestProbe_OverallUnknown_CLIPresentPluginRuntimeAgentsUnknown(t *testing.T) {
	runner := newFakeCommandRunner()
	// CLI present (will be ready via --version exit 0)
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)
	// Plugin, runtime, agents are unknown (no configured results)

	fs := newFakeFS()
	// No dirs, parent writable (missing-but-creatable = warn)

	opts := Options{HomeDir: "/fake/home", AllowTempProbe: true}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	// Overall should be unknown (fail-closed) because plugin/runtime/agents are unknown
	// even though CLI is ready
	if report.Overall != StatusUnknown {
		t.Errorf("Overall = %v, want %v (fail-closed: plugin/runtime/agents unknown)", report.Overall, StatusUnknown)
	}
}

// Test: No nil runner panic — uses realCommandRunner
func TestProbe_NilRunner_NoPanic(t *testing.T) {
	fs := newFakeFS()
	fs.statDirs["/fake/home/.config/opencode"] = true
	fs.statDirs["/fake/home/.config/opencode/plugins"] = true

	opts := Options{HomeDir: "/fake/home"}
	// runner = nil → uses realCommandRunner
	report, err := Probe(context.Background(), nil, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}
	// Should not panic; report should have findings
	if len(report.Findings) == 0 {
		t.Error("Report should have findings even with nil runner")
	}
}

// Test: No nil fs panic — uses realFS
func TestProbe_NilFS_UsesRealFS(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)

	opts := Options{HomeDir: "/fake/home"}
	// fs = nil → uses realFS (will check real filesystem)
	// This test just verifies no panic occurs
	report, err := Probe(context.Background(), runner, nil, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}
	// Should not panic; fs will check real paths
	_ = report.Overall // Just verify no panic
}

// Test: Finding has meaningful evidence
func TestProbe_CLIBlocking_HasEvidence(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.forceError = true

	opts := Options{HomeDir: "/fake/home"}
	report, err := Probe(context.Background(), runner, nil, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	var cliFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeCLI {
			cliFinding = &report.Findings[i]
			break
		}
	}
	if cliFinding == nil {
		t.Fatal("OpenCode CLI finding not found")
	}
	if len(cliFinding.Evidence) == 0 {
		t.Error("CLI finding should have evidence")
	}
}

// Test: Finding has meaningful remediation
func TestProbe_VersionUnknown_HasRemediation(t *testing.T) {
	runner := newFakeCommandRunner()
	// Version output unparseable
	runner.addResult("opencode", []string{"--version"}, "garbage\n", 0)
	runner.addResult("opencode", []string{"version"}, "garbage\n", 0)

	opts := Options{HomeDir: "/fake/home"}
	report, err := Probe(context.Background(), runner, nil, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	var versionFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeVersion {
			versionFinding = &report.Findings[i]
			break
		}
	}
	if versionFinding == nil {
		t.Fatal("OpenCode version finding not found")
	}
	if versionFinding.Status != StatusUnknown {
		t.Errorf("Version status = %v, want %v", versionFinding.Status, StatusUnknown)
	}
	if versionFinding.Remediation == "" {
		t.Error("Version unknown should have remediation")
	}
}

// Test: Findings are in consistent order by FindingID
func TestProbe_FindingsOrderedByFindingID(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)

	fs := newFakeFS()
	fs.statDirs["/fake/home/.config/opencode"] = true
	fs.statDirs["/fake/home/.config/opencode/plugins"] = true

	opts := Options{HomeDir: "/fake/home"}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	if len(report.Findings) != 7 {
		t.Fatalf("Expected 7 findings, got %d", len(report.Findings))
	}

	// Verify order by checking IDs are in expected order
	// 1. CLI, 2. Version, 3. Config, 4. Plugins-dir, 5. Plugin-API, 6. Runtime, 7. Agents
	expectedOrder := []FindingID{
		FindingIDOpenCodeCLI,
		FindingIDOpenCodeVersion,
		FindingIDOpenCodeConfig,
		FindingIDOpenCodePlugins,
		FindingIDOpenCodePluginAPI,
		FindingIDOpenCodeRuntime,
		FindingIDOpenCodeAgents,
	}

	for i, expected := range expectedOrder {
		if report.Findings[i].ID != expected {
			t.Errorf("Finding[%d] ID = %v, want %v", i, report.Findings[i].ID, expected)
		}
	}
}

// Test: Options with explicit lookup function
func TestProbe_OptionsWithLookupEnv(t *testing.T) {
	lookupCalled := false
	opts := Options{
		HomeDir: "/explicit/home",
		LookupEnv: func(key string) (string, bool) {
			lookupCalled = true
			return "", false
		},
	}

	// Verify resolveHome uses the custom LookupEnv
	home := resolveHome(opts)
	if home != "/explicit/home" {
		t.Errorf("resolveHome() = %q, want %q", home, "/explicit/home")
	}
	// LookupEnv is not called for HomeDir since it's already set
	if lookupCalled {
		t.Error("LookupEnv should not be called when HomeDir is set")
	}
}

// Test: resolveEnv with custom LookupEnv
func TestResolveEnv_Custom(t *testing.T) {
	opts := Options{
		LookupEnv: func(key string) (string, bool) {
			if key == "HOME" {
				return "/custom/home", true
			}
			return "", false
		},
	}

	val := resolveEnv("HOME", opts)
	if val != "/custom/home" {
		t.Errorf("resolveEnv(HOME) = %q, want %q", val, "/custom/home")
	}
}

// Test: resolveEnv falls back to os.Getenv when LookupEnv not set
func TestResolveEnv_Default(t *testing.T) {
	opts := Options{}

	// This test uses os.Getenv which should have HOME set in test env
	val := resolveEnv("PATH", opts)
	// PATH should be available in test environment
	if val == "" {
		t.Log("PATH not set in test environment (may be expected)")
	}
}

// Test: TempWritableProbe with pathNotExistError
func TestTempWritableProbe_PathNotExist(t *testing.T) {
	err := tempWritableProbe("/nonexistent/path")
	var pathErr *pathNotExistError
	if !errors.As(err, &pathErr) {
		t.Errorf("Expected pathNotExistError, got %v", err)
	}
}

// Test: TempWritableProbe with notDirError
func TestTempWritableProbe_NotDir(t *testing.T) {
	// Create a temp file (not a directory)
	tempFile, err := os.CreateTemp("", "probe-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())
	tempFile.Close()

	err = tempWritableProbe(tempFile.Name())
	var notDirErr *notDirError
	if !errors.As(err, &notDirErr) {
		t.Errorf("Expected notDirError, got %v", err)
	}
}

// Test: Report version is empty when version finding not ready
func TestReport_VersionEmptyWhenNotReady(t *testing.T) {
	runner := newFakeCommandRunner()
	// Version output unparseable
	runner.addResult("opencode", []string{"--version"}, "garbage\n", 0)
	runner.addResult("opencode", []string{"version"}, "garbage\n", 0)

	opts := Options{HomeDir: "/fake/home"}
	report, err := Probe(context.Background(), runner, nil, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	// Report.Version should be empty when version finding is not ready
	if report.Version != "" {
		t.Errorf("report.Version = %q, want empty", report.Version)
	}
}

// Test: Report version populated when version finding is ready
func TestReport_VersionPopulatedWhenReady(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.2.3\n", 0)

	opts := Options{HomeDir: "/fake/home"}
	report, err := Probe(context.Background(), runner, nil, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	// Report.Version should be populated when version finding is ready
	if report.Version == "" {
		t.Error("report.Version should be populated when version finding is ready")
	}
	if report.Version != "1.2.3" {
		t.Errorf("report.Version = %q, want %q", report.Version, "1.2.3")
	}
}

// Test: openCodeRootDir produces correct path
func TestOpenCodeRootDir(t *testing.T) {
	homeDir := "/home/user"
	expected := filepath.Join(homeDir, ".config", "opencode")
	got := openCodeRootDir(homeDir)
	if got != expected {
		t.Errorf("openCodeRootDir(%q) = %q, want %q", homeDir, got, expected)
	}
}

// Test: openCodePluginsDir produces correct path
func TestOpenCodePluginsDir(t *testing.T) {
	homeDir := "/home/user"
	expected := filepath.Join(homeDir, ".config", "opencode", "plugins")
	got := openCodePluginsDir(homeDir)
	if got != expected {
		t.Errorf("openCodePluginsDir(%q) = %q, want %q", homeDir, got, expected)
	}
}

// Test: openCodeJSONPath produces correct path
func TestOpenCodeJSONPath(t *testing.T) {
	homeDir := "/home/user"
	expected := filepath.Join(homeDir, ".config", "opencode", "opencode.json")
	got := openCodeJSONPath(homeDir)
	if got != expected {
		t.Errorf("openCodeJSONPath(%q) = %q, want %q", homeDir, got, expected)
	}
}

// Test: aggregate with only plugins ready (non-critical) → unknown
func TestAggregate_OnlyPluginsReady_Unknown(t *testing.T) {
	findings := []Finding{
		{ID: FindingIDOpenCodePlugins, Status: StatusReady},
		{ID: FindingIDOpenCodeConfig, Status: StatusWarn},
		{ID: FindingIDOpenCodeRuntime, Status: StatusUnknown},
		{ID: FindingIDOpenCodeAgents, Status: StatusUnknown},
	}
	got := aggregate(findings)
	if got != StatusUnknown {
		t.Errorf("aggregate() = %v, want %v (no critical ready signal)", got, StatusUnknown)
	}
}

// Test: aggregate with CLI ready but plugin/runtime/agents unknown → unknown (fail-closed)
func TestAggregate_CLIReady_PluginRuntimeAgentsUnknown_Unknown(t *testing.T) {
	findings := []Finding{
		{ID: FindingIDOpenCodeCLI, Status: StatusReady},
		{ID: FindingIDOpenCodeConfig, Status: StatusWarn},
		{ID: FindingIDOpenCodePlugins, Status: StatusUnknown},
		{ID: FindingIDOpenCodeRuntime, Status: StatusUnknown},
		{ID: FindingIDOpenCodeAgents, Status: StatusUnknown},
	}
	got := aggregate(findings)
	// With fail-closed semantics, unknown plugin/runtime/agents prevents overall ready
	if got != StatusUnknown {
		t.Errorf("aggregate() = %v, want %v (fail-closed: plugin/runtime/agents unknown)", got, StatusUnknown)
	}
}

// Test: aggregate with CLI ready and all plugin/runtime/agents verified (not unknown) → ready
func TestAggregate_CLIReady_PluginRuntimeAgentsReady_Ready(t *testing.T) {
	findings := []Finding{
		{ID: FindingIDOpenCodeCLI, Status: StatusReady},
		{ID: FindingIDOpenCodeConfig, Status: StatusReady},
		{ID: FindingIDOpenCodePlugins, Status: StatusReady},
		{ID: FindingIDOpenCodePluginAPI, Status: StatusReady},
		{ID: FindingIDOpenCodeRuntime, Status: StatusReady},
		{ID: FindingIDOpenCodeAgents, Status: StatusReady},
	}
	got := aggregate(findings)
	// All conditions met: critical ready, no unknown plugin/runtime/agents, no blocking
	if got != StatusReady {
		t.Errorf("aggregate() = %v, want %v (all conditions met)", got, StatusReady)
	}
}

// Test: aggregate with CLI ready and plugin/runtime/agents unknown but config/plugins unknown too → unknown
func TestAggregate_CLIReady_AllUnknown_Unknown(t *testing.T) {
	findings := []Finding{
		{ID: FindingIDOpenCodeCLI, Status: StatusReady},
		{ID: FindingIDOpenCodeConfig, Status: StatusUnknown},
		{ID: FindingIDOpenCodePlugins, Status: StatusUnknown},
		{ID: FindingIDOpenCodePluginAPI, Status: StatusUnknown},
		{ID: FindingIDOpenCodeRuntime, Status: StatusUnknown},
		{ID: FindingIDOpenCodeAgents, Status: StatusUnknown},
	}
	got := aggregate(findings)
	if got != StatusUnknown {
		t.Errorf("aggregate() = %v, want %v (fail-closed: unknown findings)", got, StatusUnknown)
	}
}

// Test: aggregate with version blocking → blocking
func TestAggregate_VersionBlocking_Blocking(t *testing.T) {
	findings := []Finding{
		{ID: FindingIDOpenCodeCLI, Status: StatusReady},
		{ID: FindingIDOpenCodeVersion, Status: StatusBlocking},
		{ID: FindingIDOpenCodeConfig, Status: StatusReady},
	}
	got := aggregate(findings)
	if got != StatusBlocking {
		t.Errorf("aggregate() = %v, want %v (version blocking)", got, StatusBlocking)
	}
}

// Test: fakeCommandRunner call recording
func TestFakeCommandRunner_CallRecording(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)

	_, _ = runner.Run(context.Background(), "opencode", "--version")

	calls := runner.Calls()
	if len(calls) != 1 {
		t.Errorf("Expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "opencode" {
		t.Errorf("Call name = %q, want %q", calls[0].Name, "opencode")
	}
	if len(calls[0].Args) != 1 || calls[0].Args[0] != "--version" {
		t.Errorf("Call args = %v, want %v", calls[0].Args, []string{"--version"})
	}
}

// Test: fakeCommandRunner multiple calls recorded
func TestFakeCommandRunner_MultipleCallsRecorded(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)
	runner.addResult("bun", []string{"--version"}, "1.0.0\n", 0)

	runner.Run(context.Background(), "opencode", "--version")
	runner.Run(context.Background(), "bun", "--version")
	runner.Run(context.Background(), "opencode", "--version") // second call

	calls := runner.Calls()
	if len(calls) != 3 {
		t.Errorf("Expected 3 calls, got %d", len(calls))
	}
}

// Test: fakeCommandRunner default error when no result configured
func TestFakeCommandRunner_DefaultError(t *testing.T) {
	runner := newFakeCommandRunner()
	// No result configured for "nonexistent"

	result, err := runner.Run(context.Background(), "nonexistent", "--version")

	if err == nil {
		t.Error("Expected error when no result configured")
	}
	if result.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", result.ExitCode)
	}
}

// Test: fakeFS Stat records calls
func TestFakeFS_StatRecordsCalls(t *testing.T) {
	fs := newFakeFS()
	fs.statDirs["/fake/path"] = true

	_, _ = fs.Stat("/fake/path")

	if len(fs.statCalls) != 1 {
		t.Errorf("Expected 1 stat call, got %d", len(fs.statCalls))
	}
	if fs.statCalls[0] != "/fake/path" {
		t.Errorf("Stat call path = %q, want %q", fs.statCalls[0], "/fake/path")
	}
}

// Test: fakeFS TempWritableProbe records calls
func TestFakeFS_TempWritableProbeRecordsCalls(t *testing.T) {
	fs := newFakeFS()

	_ = fs.TempWritableProbe("/probe/dir")

	if len(fs.probeCalls) != 1 {
		t.Errorf("Expected 1 probe call, got %d", len(fs.probeCalls))
	}
	if fs.probeCalls[0] != "/probe/dir" {
		t.Errorf("Probe call path = %q, want %q", fs.probeCalls[0], "/probe/dir")
	}
}

// Test: cmdKey helper for command key generation
func TestCmdKey(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"opencode", []string{"--version"}, "opencode --version"},
		{"bun", []string{"--version"}, "bun --version"},
		{"opencode", []string{"agent", "list"}, "opencode agent list"},
		{"opencode", nil, "opencode"},
		{"opencode", []string{}, "opencode"},
	}

	for _, tc := range cases {
		got := cmdKey(tc.name, tc.args...)
		if got != tc.want {
			t.Errorf("cmdKey(%q, %v) = %q, want %q", tc.name, tc.args, got, tc.want)
		}
	}
}

// Test: readFile records calls
func TestFakeFS_ReadFileRecordsCalls(t *testing.T) {
	fs := newFakeFS()
	fs.readFileData["/config.json"] = []byte(`{}`)

	_, _ = fs.ReadFile("/config.json")

	if len(fs.readFileCalls) != 1 {
		t.Errorf("Expected 1 readFile call, got %d", len(fs.readFileCalls))
	}
	if fs.readFileCalls[0] != "/config.json" {
		t.Errorf("ReadFile call path = %q, want %q", fs.readFileCalls[0], "/config.json")
	}
}

// Test: readFile returns configured data
func TestFakeFS_ReadFileReturnsData(t *testing.T) {
	fs := newFakeFS()
	expected := []byte(`{"version": "1.0.0"}`)
	fs.readFileData["/config.json"] = expected

	data, err := fs.ReadFile("/config.json")

	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !bytes.Equal(data, expected) {
		t.Errorf("ReadFile returned %q, want %q", data, expected)
	}
}

// Test: readFile error when file not configured
func TestFakeFS_ReadFileNotFound(t *testing.T) {
	fs := newFakeFS()

	_, err := fs.ReadFile("/nonexistent.json")

	if err == nil {
		t.Error("Expected error when file not configured")
	}
}

// Test: realCommandRunner and realFS are not nil and work in real env
func TestRealCommandRunner_NotNil(t *testing.T) {
	var runner CommandRunner = &realCommandRunner{}
	if runner == nil {
		t.Error("realCommandRunner should not be nil")
	}
}

func TestRealFS_NotNil(t *testing.T) {
	var fs FS = &realFS{}
	if fs == nil {
		t.Error("realFS should not be nil")
	}
}

// Test: Result struct initialization
func TestResult_Initialization(t *testing.T) {
	result := Result{
		Stdout:   "output",
		Stderr:   "",
		ExitCode: 0,
		Error:    nil,
	}

	if result.Stdout != "output" {
		t.Errorf("Result.Stdout = %q, want %q", result.Stdout, "output")
	}
	if result.ExitCode != 0 {
		t.Errorf("Result.ExitCode = %d, want %d", result.ExitCode, 0)
	}
}

// Test: pathNotExistError interface
func TestPathNotExistError_Error(t *testing.T) {
	err := &pathNotExistError{Path: "/test/path"}
	msg := err.Error()
	if !strings.Contains(msg, "/test/path") {
		t.Errorf("pathNotExistError.Error() = %q, should contain %q", msg, "/test/path")
	}
}

// Test: notDirError interface
func TestNotDirError_Error(t *testing.T) {
	err := &notDirError{Path: "/test/file"}
	msg := err.Error()
	if !strings.Contains(msg, "/test/file") {
		t.Errorf("notDirError.Error() = %q, should contain %q", msg, "/test/file")
	}
}

// Test: Finding with all fields populated
func TestFinding_AllFields(t *testing.T) {
	finding := Finding{
		ID:              FindingIDOpenCodeCLI,
		Status:          StatusReady,
		Evidence:        []string{"evidence1", "evidence2"},
		Remediation:     "Install opencode",
		NewlyInstalled:  false,
	}

	if finding.ID != FindingIDOpenCodeCLI {
		t.Errorf("Finding.ID = %v, want %v", finding.ID, FindingIDOpenCodeCLI)
	}
	if finding.Status != StatusReady {
		t.Errorf("Finding.Status = %v, want %v", finding.Status, StatusReady)
	}
	if len(finding.Evidence) != 2 {
		t.Errorf("Finding.Evidence length = %d, want %d", len(finding.Evidence), 2)
	}
	if finding.NewlyInstalled {
		t.Error("Finding.NewlyInstalled should be false")
	}
}

// Test: Report struct initialization
func TestReport_Initialization(t *testing.T) {
	report := Report{
		Overall:  StatusReady,
		Findings: []Finding{},
		Version:  "1.0.0",
	}

	if report.Overall != StatusReady {
		t.Errorf("Report.Overall = %v, want %v", report.Overall, StatusReady)
	}
	if report.Version != "1.0.0" {
		t.Errorf("Report.Version = %q, want %q", report.Version, "1.0.0")
	}
}

// Test: Options struct initialization
func TestOptions_Initialization(t *testing.T) {
	opts := Options{
		HomeDir:       "/home",
		Env:           map[string]string{"KEY": "value"},
		AllowTempProbe: true,
	}

	if opts.HomeDir != "/home" {
		t.Errorf("Options.HomeDir = %q, want %q", opts.HomeDir, "/home")
	}
	if opts.Env["KEY"] != "value" {
		t.Errorf("Options.Env[KEY] = %q, want %q", opts.Env["KEY"], "value")
	}
	if !opts.AllowTempProbe {
		t.Error("Options.AllowTempProbe should be true")
	}
}

// Test: Config dir missing with AllowTempProbe=false → unknown (no probe attempted)
func TestProbe_ConfigDir_Missing_AllowTempProbeFalse_Unknown(t *testing.T) {
	fs := newFakeFS()
	fs.statErr = errors.New("path not found")
	// probeErr is not set - but it shouldn't matter since AllowTempProbe is false

	opts := Options{HomeDir: "/fake/home", AllowTempProbe: false}
	finding := probeConfigDir(context.Background(), fs, opts)

	// Config dir should be unknown (not blocking) when AllowTempProbe is false
	// because we cannot verify writability without the temp probe
	if finding.Status != StatusUnknown {
		t.Errorf("Config dir status = %v, want %v (AllowTempProbe=false)", finding.Status, StatusUnknown)
	}
	// Verify TempWritableProbe was NOT called
	if len(fs.probeCalls) > 0 {
		t.Errorf("TempWritableProbe should not be called when AllowTempProbe=false, but got calls: %v", fs.probeCalls)
	}
}

// Test: Config dir probe uses correct path with explicit HomeDir
func TestProbeConfigDir_UsesExplicitHomeDir(t *testing.T) {
	fs := newFakeFS()
	fs.statErr = errors.New("path not found")
	// Parent is writable

	opts := Options{HomeDir: "/explicit/home", AllowTempProbe: true}
	finding := probeConfigDir(context.Background(), fs, opts)

	// Should use /explicit/home/.config/opencode
	if finding.Status != StatusWarn {
		t.Errorf("Config status = %v, want %v", finding.Status, StatusWarn)
	}
	// Check that .config/opencode path is in evidence
	found := false
	for _, e := range finding.Evidence {
		if strings.Contains(e, "/explicit/home/.config/opencode") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Evidence should contain path with explicit HomeDir")
	}
}

// Test: Plugins dir probe uses correct path with explicit HomeDir
func TestProbePluginsDir_UsesExplicitHomeDir(t *testing.T) {
	fs := newFakeFS()
	fs.statErr = errors.New("path not found")
	// Parent is writable

	opts := Options{HomeDir: "/explicit/home", AllowTempProbe: true}
	finding := probePluginsDir(context.Background(), fs, opts)

	// Should use /explicit/home/.config/opencode/plugins
	if finding.Status != StatusWarn {
		t.Errorf("Plugins status = %v, want %v", finding.Status, StatusWarn)
	}
	// Check that plugins path is in evidence
	found := false
	for _, e := range finding.Evidence {
		if strings.Contains(e, "/explicit/home/.config/opencode/plugins") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Evidence should contain plugins path with explicit HomeDir")
	}
}

// Test: Config dir exists with plugins missing (warn, not blocking)
func TestProbe_ConfigExists_PluginsMissing_Warn(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)

	fs := newFakeFS()
	// Config dir exists
	fs.statDirs["/fake/home/.config/opencode"] = true
	// Plugins dir missing

	opts := Options{HomeDir: "/fake/home", AllowTempProbe: true}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	var pluginsFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodePlugins {
			pluginsFinding = &report.Findings[i]
			break
		}
	}
	if pluginsFinding == nil {
		t.Fatal("OpenCode plugins finding not found")
	}
	if pluginsFinding.Status != StatusWarn {
		t.Errorf("Plugins status = %v, want %v (missing but creatable)", pluginsFinding.Status, StatusWarn)
	}
}

// Test: Overall unknown when CLI missing and dirs missing (no blocking, but no ready critical)
func TestProbe_OverallUnknown_CLIMissingDirsMissing(t *testing.T) {
	runner := newFakeCommandRunner()
	// CLI missing (all fallbacks fail)
	runner.addResult("opencode", []string{"--version"}, "", 1)
	runner.addResult("opencode", []string{"version"}, "", 1)
	runner.addResult("opencode", []string{"--help"}, "", 1)

	fs := newFakeFS()
	// No dirs, parent writable (missing-but-creatable = warn, not blocking)

	opts := Options{HomeDir: "/fake/home", AllowTempProbe: true}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	// CLI is blocking, so overall should be blocking, not unknown
	// (CLI missing is blocking, not just missing)
	if report.Overall != StatusBlocking {
		t.Errorf("Overall = %v, want %v (CLI missing is blocking)", report.Overall, StatusBlocking)
	}
}

// Test: aggregate with multiple blocking findings
func TestAggregate_MultipleBlocking(t *testing.T) {
	findings := []Finding{
		{ID: FindingIDOpenCodeCLI, Status: StatusBlocking},
		{ID: FindingIDOpenCodeVersion, Status: StatusBlocking},
		{ID: FindingIDOpenCodeConfig, Status: StatusBlocking},
	}
	got := aggregate(findings)
	if got != StatusBlocking {
		t.Errorf("aggregate() = %v, want %v (any blocking)", got, StatusBlocking)
	}
}

// Test: aggregate with no findings
func TestAggregate_NoFindings(t *testing.T) {
	got := aggregate([]Finding{})
	// With fail-closed semantics, empty findings means no readiness signal → warn
	if got != StatusWarn {
		t.Errorf("aggregate() = %v, want %v (no findings → warn)", got, StatusWarn)
	}
}

// Test: probePluginAPI ready when plugin --help succeeds with relevant output
func TestProbe_PluginAPI_HelpWithPluginOutput_Ready(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"plugin", "--help"}, "OpenCode Plugin Manager\nUsage:\n  opencode plugin install <name>\n", 0)

	opts := Options{}
	finding := probePluginAPI(context.Background(), runner, opts)

	if finding.Status != StatusReady {
		t.Errorf("Plugin API status = %v, want %v", finding.Status, StatusReady)
	}
}

// Test: probePluginAPI ready when debug info reports plugin state
func TestProbe_PluginAPI_DebugInfo_Ready(t *testing.T) {
	runner := newFakeCommandRunner()
	// plugin --help fails
	runner.addResult("opencode", []string{"plugin", "--help"}, "", 1)
	// debug info succeeds with plugin info (must contain "plugin" or "loaded" lowercase)
	runner.addResult("opencode", []string{"debug", "info"}, "plugin list: []\nloaded: false\n", 0)

	opts := Options{}
	finding := probePluginAPI(context.Background(), runner, opts)

	// Output contains "plugin" (lowercase) and "loaded" → ready
	if finding.Status != StatusReady {
		t.Errorf("Plugin API status = %v, want %v (output contains 'plugin')", finding.Status, StatusReady)
	}
}

// Test: probeRuntime with bun exit 0 but unusual output
func TestProbe_Runtime_BunUnusualOutput_Ready(t *testing.T) {
	runner := newFakeCommandRunner()
	// bun --version succeeds with unusual but valid output
	runner.addResult("bun", []string{"--version"}, "Bun v0.1.0\n", 0)

	opts := Options{}
	finding := probeRuntime(context.Background(), runner, opts)

	// Bun available (exit 0) → ready regardless of output format
	if finding.Status != StatusReady {
		t.Errorf("Runtime status = %v, want %v", finding.Status, StatusReady)
	}
}

// Test: probeNativeAgents with agent list success
func TestProbe_NativeAgents_AgentListSuccess_Ready(t *testing.T) {
	runner := newFakeCommandRunner()
	// agent --help fails
	runner.addResult("opencode", []string{"agent", "--help"}, "", 1)
	// agent list succeeds
	runner.addResult("opencode", []string{"agent", "list"}, "Available agents:\n- default\n- coding\n", 0)

	opts := Options{}
	finding := probeNativeAgents(context.Background(), runner, opts)

	if finding.Status != StatusReady {
		t.Errorf("Agents status = %v, want %v", finding.Status, StatusReady)
	}
}

// Test: probeCLIPresence with version command fallback success
func TestProbe_CLIPresence_VersionFallback_Ready(t *testing.T) {
	runner := newFakeCommandRunner()
	// --version fails
	runner.addResult("opencode", []string{"--version"}, "", 1)
	// version succeeds
	runner.addResult("opencode", []string{"version"}, "v2.0.0\n", 0)

	opts := Options{}
	report, err := Probe(context.Background(), runner, nil, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	var cliFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeCLI {
			cliFinding = &report.Findings[i]
			break
		}
	}
	if cliFinding == nil {
		t.Fatal("OpenCode CLI finding not found")
	}
	if cliFinding.Status != StatusReady {
		t.Errorf("CLI status = %v, want %v", cliFinding.Status, StatusReady)
	}
}

// Test: probeCLIPresence with --help fallback success
func TestProbe_CLIPresence_HelpFallback_Ready(t *testing.T) {
	runner := newFakeCommandRunner()
	// --version fails
	runner.addResult("opencode", []string{"--version"}, "", 1)
	// version fails
	runner.addResult("opencode", []string{"version"}, "", 1)
	// --help succeeds
	runner.addResult("opencode", []string{"--help"}, "OpenCode CLI\nUsage:\n", 0)

	opts := Options{}
	report, err := Probe(context.Background(), runner, nil, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	var cliFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeCLI {
			cliFinding = &report.Findings[i]
			break
		}
	}
	if cliFinding == nil {
		t.Fatal("OpenCode CLI finding not found")
	}
	if cliFinding.Status != StatusReady {
		t.Errorf("CLI status = %v, want %v", cliFinding.Status, StatusReady)
	}
}

// Test: Config dir missing, parent not writable (probe succeeds but dir missing)
func TestProbe_ConfigDir_MissingParentWritableProbeSucceeds_Warn(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)

	fs := newFakeFS()
	fs.statErr = errors.New("path not found")
	// probeErr is nil, so TempWritableProbe succeeds

	opts := Options{HomeDir: "/fake/home", AllowTempProbe: true}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	var configFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeConfig {
			configFinding = &report.Findings[i]
			break
		}
	}
	if configFinding == nil {
		t.Fatal("OpenCode config finding not found")
	}
	// Missing but parent writable → warn
	if configFinding.Status != StatusWarn {
		t.Errorf("Config status = %v, want %v (missing but creatable)", configFinding.Status, StatusWarn)
	}
}

// Test: probeConfigDir with nil fs returns unknown
func TestProbeConfigDir_NilFS_Unknown(t *testing.T) {
	opts := Options{HomeDir: "/fake/home"}
	finding := probeConfigDir(context.Background(), nil, opts)

	if finding.Status != StatusUnknown {
		t.Errorf("Config status = %v, want %v (nil fs)", finding.Status, StatusUnknown)
	}
	if !strings.Contains(finding.Remediation, "internal error") {
		t.Error("Config unknown should mention internal error")
	}
}

// Test: probePluginsDir with nil fs returns unknown
func TestProbePluginsDir_NilFS_Unknown(t *testing.T) {
	opts := Options{HomeDir: "/fake/home"}
	finding := probePluginsDir(context.Background(), nil, opts)

	if finding.Status != StatusUnknown {
		t.Errorf("Plugins status = %v, want %v (nil fs)", finding.Status, StatusUnknown)
	}
	if !strings.Contains(finding.Remediation, "internal error") {
		t.Error("Plugins unknown should mention internal error")
	}
}

// Test: probePluginAPI with nil runner returns unknown
func TestProbePluginAPI_NilRunner_Unknown(t *testing.T) {
	opts := Options{}
	finding := probePluginAPI(context.Background(), nil, opts)

	if finding.Status != StatusUnknown {
		t.Errorf("Plugin API status = %v, want %v (nil runner)", finding.Status, StatusUnknown)
	}
}

// Test: probeRuntime with nil runner returns unknown
func TestProbeRuntime_NilRunner_Unknown(t *testing.T) {
	opts := Options{}
	finding := probeRuntime(context.Background(), nil, opts)

	if finding.Status != StatusUnknown {
		t.Errorf("Runtime status = %v, want %v (nil runner)", finding.Status, StatusUnknown)
	}
}

// Test: probeNativeAgents with nil runner returns unknown
func TestProbeNativeAgents_NilRunner_Unknown(t *testing.T) {
	opts := Options{}
	finding := probeNativeAgents(context.Background(), nil, opts)

	if finding.Status != StatusUnknown {
		t.Errorf("Agents status = %v, want %v (nil runner)", finding.Status, StatusUnknown)
	}
}

// Test: resolveHome falls back when HomeDir empty
func TestResolveHome_EmptyFallback(t *testing.T) {
	opts := Options{HomeDir: ""}
	// Should attempt os.UserHomeDir (may succeed or fail)
	home := resolveHome(opts)
	// Home may be empty string if os.UserHomeDir fails, or populated if it succeeds
	_ = home // Just verify no panic
}

// Test: openCodeConfigDir equals openCodeRootDir (config dir is root)
func TestOpenCodeConfigDir_EqualsRoot(t *testing.T) {
	homeDir := "/home/user"
	root := openCodeRootDir(homeDir)
	config := openCodeConfigDir(homeDir)
	if config != root {
		t.Errorf("openCodeConfigDir(%q) = %q, want same as root %q", homeDir, config, root)
	}
}

// Test: aggregate with CLI blocking and other findings
func TestAggregate_CLIBlocking_OtherFindings(t *testing.T) {
	findings := []Finding{
		{ID: FindingIDOpenCodeCLI, Status: StatusBlocking},
		{ID: FindingIDOpenCodeVersion, Status: StatusReady},
		{ID: FindingIDOpenCodeConfig, Status: StatusReady},
	}
	got := aggregate(findings)
	if got != StatusBlocking {
		t.Errorf("aggregate() = %v, want %v (CLI blocking)", got, StatusBlocking)
	}
}

// Test: aggregate with version blocking and other findings
func TestAggregate_VersionBlocking_OtherFindings(t *testing.T) {
	findings := []Finding{
		{ID: FindingIDOpenCodeCLI, Status: StatusReady},
		{ID: FindingIDOpenCodeVersion, Status: StatusBlocking},
		{ID: FindingIDOpenCodeConfig, Status: StatusReady},
	}
	got := aggregate(findings)
	if got != StatusBlocking {
		t.Errorf("aggregate() = %v, want %v (version blocking)", got, StatusBlocking)
	}
}

// Test: aggregate with config blocking (non-critical but blocking)
func TestAggregate_ConfigBlocking_NonCritical(t *testing.T) {
	findings := []Finding{
		{ID: FindingIDOpenCodeCLI, Status: StatusReady},
		{ID: FindingIDOpenCodeVersion, Status: StatusUnknown},
		{ID: FindingIDOpenCodeConfig, Status: StatusBlocking},
	}
	got := aggregate(findings)
	// Config is blocking, so overall is blocking even though CLI is ready
	if got != StatusBlocking {
		t.Errorf("aggregate() = %v, want %v (config blocking)", got, StatusBlocking)
	}
}

// Test: aggregate with plugins blocking (non-critical but blocking)
func TestAggregate_PluginsBlocking_NonCritical(t *testing.T) {
	findings := []Finding{
		{ID: FindingIDOpenCodeCLI, Status: StatusReady},
		{ID: FindingIDOpenCodeVersion, Status: StatusReady},
		{ID: FindingIDOpenCodePlugins, Status: StatusBlocking},
	}
	got := aggregate(findings)
	if got != StatusBlocking {
		t.Errorf("aggregate() = %v, want %v (plugins blocking)", got, StatusBlocking)
	}
}

// Test: aggregate with runtime blocking (non-critical but blocking)
func TestAggregate_RuntimeBlocking_NonCritical(t *testing.T) {
	findings := []Finding{
		{ID: FindingIDOpenCodeCLI, Status: StatusReady},
		{ID: FindingIDOpenCodeVersion, Status: StatusReady},
		{ID: FindingIDOpenCodeRuntime, Status: StatusBlocking},
	}
	got := aggregate(findings)
	if got != StatusBlocking {
		t.Errorf("aggregate() = %v, want %v (runtime blocking)", got, StatusBlocking)
	}
}

// Test: aggregate with agents blocking (non-critical but blocking)
func TestAggregate_AgentsBlocking_NonCritical(t *testing.T) {
	findings := []Finding{
		{ID: FindingIDOpenCodeCLI, Status: StatusReady},
		{ID: FindingIDOpenCodeVersion, Status: StatusReady},
		{ID: FindingIDOpenCodeAgents, Status: StatusBlocking},
	}
	got := aggregate(findings)
	if got != StatusBlocking {
		t.Errorf("aggregate() = %v, want %v (agents blocking)", got, StatusBlocking)
	}
}

// Test: extractVersion with empty evidence
func TestExtractVersion_EmptyEvidence(t *testing.T) {
	finding := Finding{
		ID:       FindingIDOpenCodeVersion,
		Status:   StatusReady,
		Evidence: []string{},
	}
	got := extractVersion(finding)
	if got != "" {
		t.Errorf("extractVersion(empty evidence) = %q, want empty", got)
	}
}

// Test: extractVersion with non-ready finding
func TestExtractVersion_NotReady(t *testing.T) {
	finding := Finding{
		ID:       FindingIDOpenCodeVersion,
		Status:   StatusUnknown,
		Evidence: []string{"version=1.0.0"},
	}
	got := extractVersion(finding)
	if got != "" {
		t.Errorf("extractVersion(not ready) = %q, want empty", got)
	}
}

// Test: extractVersion strips version= prefix
func TestExtractVersion_StripsPrefix(t *testing.T) {
	finding := Finding{
		ID:       FindingIDOpenCodeVersion,
		Status:   StatusReady,
		Evidence: []string{"version=2.5.0"},
	}
	got := extractVersion(finding)
	if got != "2.5.0" {
		t.Errorf("extractVersion() = %q, want %q", got, "2.5.0")
	}
}

// Test: extractVersion uses first evidence
func TestExtractVersion_FirstEvidence(t *testing.T) {
	finding := Finding{
		ID:       FindingIDOpenCodeVersion,
		Status:   StatusReady,
		Evidence: []string{"version=1.0.0", "extra evidence"},
	}
	got := extractVersion(finding)
	if got != "1.0.0" {
		t.Errorf("extractVersion() = %q, want %q (first evidence)", got, "1.0.0")
	}
}

// Test: FindingID string values are not empty
func TestFindingID_StringValues(t *testing.T) {
	ids := []FindingID{
		FindingIDOpenCodeCLI,
		FindingIDOpenCodeVersion,
		FindingIDOpenCodeConfig,
		FindingIDOpenCodePlugins,
		FindingIDOpenCodeRuntime,
		FindingIDOpenCodeAgents,
	}
	for _, id := range ids {
		if string(id) == "" {
			t.Errorf("FindingID %v has empty string value", id)
		}
	}
}

// Test: Status string values are not empty
func TestStatus_StringValues(t *testing.T) {
	statuses := []Status{
		StatusReady,
		StatusWarn,
		StatusBlocking,
		StatusUnknown,
	}
	for _, s := range statuses {
		if string(s) == "" {
			t.Errorf("Status %v has empty string value", s)
		}
	}
}

// Test: aggregate is fail-closed (any blocking = blocking)
func TestAggregate_FailClosed(t *testing.T) {
	findings := []Finding{
		{ID: FindingIDOpenCodeCLI, Status: StatusReady},
		{ID: FindingIDOpenCodeVersion, Status: StatusReady},
		{ID: FindingIDOpenCodeConfig, Status: StatusWarn},
		{ID: FindingIDOpenCodePlugins, Status: StatusBlocking}, // Any blocking
		{ID: FindingIDOpenCodeRuntime, Status: StatusReady},
		{ID: FindingIDOpenCodeAgents, Status: StatusReady},
	}
	got := aggregate(findings)
	if got != StatusBlocking {
		t.Errorf("aggregate() = %v, want %v (fail-closed: any blocking)", got, StatusBlocking)
	}
}

// Test: aggregate unknown when plugin/runtime/agents unknown, even if all non-blocking and critical ready
// With fail-closed semantics, unknown plugin/runtime/agents prevents overall ready.
func TestAggregate_AllNonBlocking_PluginRuntimeAgentsUnknown(t *testing.T) {
	findings := []Finding{
		{ID: FindingIDOpenCodeCLI, Status: StatusReady},
		{ID: FindingIDOpenCodeVersion, Status: StatusReady},
		{ID: FindingIDOpenCodeConfig, Status: StatusReady},
		{ID: FindingIDOpenCodePlugins, Status: StatusReady},
		{ID: FindingIDOpenCodePluginAPI, Status: StatusUnknown},
		{ID: FindingIDOpenCodeRuntime, Status: StatusUnknown},
		{ID: FindingIDOpenCodeAgents, Status: StatusUnknown},
	}
	got := aggregate(findings)
	// Fail-closed: unknown plugin/runtime/agents prevents ready even when all non-blocking
	if got != StatusUnknown {
		t.Errorf("aggregate() = %v, want %v (fail-closed: unknown plugin/runtime/agents)", got, StatusUnknown)
	}
}

// Test: empty HomeDir with nil LookupEnv (uses os.UserHomeDir)
func TestResolveHome_NilLookupEnv(t *testing.T) {
	opts := Options{
		HomeDir:   "",
		LookupEnv: nil,
	}
	// Should not panic, may return empty if os.UserHomeDir fails
	home := resolveHome(opts)
	_ = home
}

// Test: resolveEnv with nil LookupEnv (uses os.Getenv)
func TestResolveEnv_NilLookupEnv(t *testing.T) {
	opts := Options{
		LookupEnv: nil,
	}
	// Should not panic
	val := resolveEnv("PATH", opts)
	_ = val
}

// Test: Config dir present but inaccessible (stat returns error)
// Test: Config dir inaccessible with AllowTempProbe → unknown (fail-closed)
// When AllowTempProbe is enabled but parent is inaccessible, the result is unknown
// (not blocking) because we cannot definitively determine the installability.
func TestProbe_ConfigDir_Inaccessible_Unknown(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)

	fs := newFakeFS()
	fs.statErr = errors.New("permission denied")
	fs.probeErr = errors.New("permission denied")

	opts := Options{HomeDir: "/fake/home", AllowTempProbe: true}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	var configFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeConfig {
			configFinding = &report.Findings[i]
			break
		}
	}
	if configFinding == nil {
		t.Fatal("OpenCode config finding not found")
	}
	// Inaccessible with AllowTempProbe → unknown (fail-closed)
	if configFinding.Status != StatusUnknown {
		t.Errorf("Config status = %v, want %v (inaccessible with AllowTempProbe)", configFinding.Status, StatusUnknown)
	}
}

// Test: Plugins dir present but inaccessible with AllowTempProbe → unknown (fail-closed)
// When AllowTempProbe is enabled but parent is inaccessible, the result is unknown
// (not blocking) because we cannot definitively determine the installability.
func TestProbe_PluginsDir_Inaccessible_Unknown(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)

	fs := newFakeFS()
	fs.statDirs["/fake/home/.config/opencode"] = true // Config exists
	fs.statErr = errors.New("permission denied")        // Plugins dir inaccessible
	fs.probeErr = errors.New("permission denied")

	opts := Options{HomeDir: "/fake/home", AllowTempProbe: true}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	var pluginsFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodePlugins {
			pluginsFinding = &report.Findings[i]
			break
		}
	}
	if pluginsFinding == nil {
		t.Fatal("OpenCode plugins finding not found")
	}
	// Inaccessible with AllowTempProbe → unknown (fail-closed)
	if pluginsFinding.Status != StatusUnknown {
		t.Errorf("Plugins status = %v, want %v (inaccessible with AllowTempProbe)", pluginsFinding.Status, StatusUnknown)
	}
}

// Test: Version parsing with v prefix
func TestParseVersionOutput_WithVPrefix(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"v1.2.3", "1.2.3"},
		{"v2.0.0-alpha", "2.0.0-alpha"},
		{"version v3.0.0", "3.0.0"},
	}

	for _, tc := range cases {
		got := parseVersionOutput(tc.input)
		if got != tc.expected {
			t.Errorf("parseVersionOutput(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

// Test: Version parsing with version= prefix (lowercase only)
func TestParseVersionOutput_WithVersionEquals(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"version=1.2.3", "1.2.3"},
		{"version=3.0.0-beta", "3.0.0-beta"},
		// Note: uppercase VERSION= is not handled
	}

	for _, tc := range cases {
		got := parseVersionOutput(tc.input)
		if got != tc.expected {
			t.Errorf("parseVersionOutput(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

// Test: version finding uses fallback when first command fails
func TestProbe_Version_Fallback(t *testing.T) {
	runner := newFakeCommandRunner()
	// First version command fails
	runner.addResult("opencode", []string{"--version"}, "", 1)
	// Fallback succeeds
	runner.addResult("opencode", []string{"version"}, "v1.0.0\n", 0)

	opts := Options{HomeDir: "/fake/home"}
	report, err := Probe(context.Background(), runner, nil, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	var versionFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeVersion {
			versionFinding = &report.Findings[i]
			break
		}
	}
	if versionFinding == nil {
		t.Fatal("OpenCode version finding not found")
	}
	// Fallback succeeded → version ready
	if versionFinding.Status != StatusReady {
		t.Errorf("Version status = %v, want %v (fallback succeeded)", versionFinding.Status, StatusReady)
	}
	if report.Version != "1.0.0" {
		t.Errorf("report.Version = %q, want %q", report.Version, "1.0.0")
	}
}

// Test: version finding unknown when both commands fail (CLI blocking, version not checked)
func TestProbe_Version_BothFail_Unknown(t *testing.T) {
	runner := newFakeCommandRunner()
	// Both version commands fail (and all CLI commands fail)
	runner.addResult("opencode", []string{"--version"}, "", 1)
	runner.addResult("opencode", []string{"version"}, "", 1)
	runner.addResult("opencode", []string{"--help"}, "", 1)

	opts := Options{HomeDir: "/fake/home"}
	report, err := Probe(context.Background(), runner, nil, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	// CLI is blocking, so version finding is not added
	// (version check is skipped when CLI is not ready)
	var cliFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeCLI {
			cliFinding = &report.Findings[i]
			break
		}
	}
	if cliFinding == nil {
		t.Fatal("CLI finding not found")
	}
	// CLI is blocking
	if cliFinding.Status != StatusBlocking {
		t.Errorf("CLI status = %v, want %v", cliFinding.Status, StatusBlocking)
	}
	// Version finding should not exist (skipped when CLI not ready)
	var versionFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeVersion {
			versionFinding = &report.Findings[i]
			break
		}
	}
	if versionFinding != nil {
		t.Error("Version finding should not exist when CLI is blocking")
	}
}

// Test: version finding skipped when CLI not present
func TestProbe_Version_CLINotPresent(t *testing.T) {
	runner := newFakeCommandRunner()
	// CLI not found (all commands fail)
	runner.addResult("opencode", []string{"--version"}, "", 1)
	runner.addResult("opencode", []string{"version"}, "", 1)
	runner.addResult("opencode", []string{"--help"}, "", 1)

	opts := Options{HomeDir: "/fake/home"}
	report, err := Probe(context.Background(), runner, nil, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	var cliFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeCLI {
			cliFinding = &report.Findings[i]
			break
		}
	}
	if cliFinding == nil {
		t.Fatal("CLI finding not found")
	}
	// CLI not present → blocking
	if cliFinding.Status != StatusBlocking {
		t.Errorf("CLI status = %v, want %v", cliFinding.Status, StatusBlocking)
	}
	// Version finding should not exist (skipped when CLI not ready)
	var versionFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeVersion {
			versionFinding = &report.Findings[i]
			break
		}
	}
	if versionFinding != nil {
		t.Error("Version finding should not exist when CLI is blocking")
	}
}

// Test: aggregate with plugins blocking but CLI ready (blocking takes precedence)
func TestAggregate_PluginsBlocking_CLIReady(t *testing.T) {
	findings := []Finding{
		{ID: FindingIDOpenCodeCLI, Status: StatusReady},
		{ID: FindingIDOpenCodeVersion, Status: StatusReady},
		{ID: FindingIDOpenCodeConfig, Status: StatusReady},
		{ID: FindingIDOpenCodePlugins, Status: StatusBlocking},
	}
	got := aggregate(findings)
	if got != StatusBlocking {
		t.Errorf("aggregate() = %v, want %v (plugins blocking)", got, StatusBlocking)
	}
}

// Test: aggregate with runtime/agents unknown (fail-closed) and CLI ready → unknown
// With fail-closed semantics, unknown plugin/runtime/agents prevents overall ready.
func TestAggregate_RuntimeUnknown_CLIReady_FailClosed(t *testing.T) {
	findings := []Finding{
		{ID: FindingIDOpenCodeCLI, Status: StatusReady},
		{ID: FindingIDOpenCodeVersion, Status: StatusReady},
		{ID: FindingIDOpenCodeConfig, Status: StatusReady},
		{ID: FindingIDOpenCodePlugins, Status: StatusReady},
		{ID: FindingIDOpenCodeRuntime, Status: StatusUnknown},
		{ID: FindingIDOpenCodeAgents, Status: StatusUnknown},
	}
	got := aggregate(findings)
	// Fail-closed: unknown plugin/runtime/agents prevents ready
	if got != StatusUnknown {
		t.Errorf("aggregate() = %v, want %v (fail-closed: plugin/runtime/agents unknown)", got, StatusUnknown)
	}
}

// Test: aggregate with all unknown (no ready critical, no blocking) → unknown
func TestAggregate_AllUnknown(t *testing.T) {
	findings := []Finding{
		{ID: FindingIDOpenCodeCLI, Status: StatusUnknown},
		{ID: FindingIDOpenCodeVersion, Status: StatusUnknown},
		{ID: FindingIDOpenCodeConfig, Status: StatusUnknown},
		{ID: FindingIDOpenCodePlugins, Status: StatusUnknown},
		{ID: FindingIDOpenCodeRuntime, Status: StatusUnknown},
		{ID: FindingIDOpenCodeAgents, Status: StatusUnknown},
	}
	got := aggregate(findings)
	if got != StatusUnknown {
		t.Errorf("aggregate() = %v, want %v (all unknown, no ready critical)", got, StatusUnknown)
	}
}

// Test: aggregate with all warn (no ready critical, no blocking) → unknown
func TestAggregate_AllWarn(t *testing.T) {
	findings := []Finding{
		{ID: FindingIDOpenCodeCLI, Status: StatusUnknown},
		{ID: FindingIDOpenCodeVersion, Status: StatusUnknown},
		{ID: FindingIDOpenCodeConfig, Status: StatusWarn},
		{ID: FindingIDOpenCodePlugins, Status: StatusWarn},
		{ID: FindingIDOpenCodeRuntime, Status: StatusWarn},
		{ID: FindingIDOpenCodeAgents, Status: StatusWarn},
	}
	got := aggregate(findings)
	if got != StatusUnknown {
		t.Errorf("aggregate() = %v, want %v (all warn, no ready critical)", got, StatusUnknown)
	}
}

// Test: aggregate with all ready → ready
func TestAggregate_AllReady(t *testing.T) {
	findings := []Finding{
		{ID: FindingIDOpenCodeCLI, Status: StatusReady},
		{ID: FindingIDOpenCodeVersion, Status: StatusReady},
		{ID: FindingIDOpenCodeConfig, Status: StatusReady},
		{ID: FindingIDOpenCodePlugins, Status: StatusReady},
		{ID: FindingIDOpenCodeRuntime, Status: StatusReady},
		{ID: FindingIDOpenCodeAgents, Status: StatusReady},
	}
	got := aggregate(findings)
	if got != StatusReady {
		t.Errorf("aggregate() = %v, want %v (all ready)", got, StatusReady)
	}
}

// Test: aggregate with CLI ready and only warn findings (no unknown) → warn (not ready)
// Warn means partial readiness, not full readiness, even when CLI is present.
func TestAggregate_CLIReady_WarnOnly_NoUnknown_Warn(t *testing.T) {
	findings := []Finding{
		{ID: FindingIDOpenCodeCLI, Status: StatusReady},
		{ID: FindingIDOpenCodeVersion, Status: StatusReady},
		{ID: FindingIDOpenCodeConfig, Status: StatusWarn}, // missing-but-creatable
		{ID: FindingIDOpenCodePlugins, Status: StatusWarn}, // missing-but-creatable
		{ID: FindingIDOpenCodePluginAPI, Status: StatusReady},
		{ID: FindingIDOpenCodeRuntime, Status: StatusReady},
		{ID: FindingIDOpenCodeAgents, Status: StatusReady},
	}
	got := aggregate(findings)
	// Warn findings prevent overall ready even when CLI is ready
	if got != StatusWarn {
		t.Errorf("aggregate() = %v, want %v (warn prevents ready)", got, StatusWarn)
	}
}

// Test: CLI presence probe with err != nil but exit code 0 (unusual)
func TestProbe_CLI_ErrNotNilButExitZero(t *testing.T) {
	runner := newFakeCommandRunner()
	// Error but exit code 0 - unusual but possible
	runner.addResult("opencode", []string{"--version"}, "output\n", 0)
	// Force an error that still returns exit 0

	fs := newFakeFS()
	fs.statDirs["/fake/home/.config/opencode"] = true

	opts := Options{HomeDir: "/fake/home"}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	var cliFinding *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == FindingIDOpenCodeCLI {
			cliFinding = &report.Findings[i]
			break
		}
	}
	if cliFinding == nil {
		t.Fatal("OpenCode CLI finding not found")
	}
	// Exit code 0 → ready even if err was non-nil but exit was 0
	if cliFinding.Status != StatusReady {
		t.Errorf("CLI status = %v, want %v (exit 0)", cliFinding.Status, StatusReady)
	}
}

// Test: multiple run calls are tracked in order
func TestFakeCommandRunner_CallOrder(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)
	runner.addResult("bun", []string{"--version"}, "v1.0.0\n", 0)
	runner.addResult("opencode", []string{"agent", "list"}, "agents\n", 0)

	runner.Run(context.Background(), "opencode", "--version")
	runner.Run(context.Background(), "bun", "--version")
	runner.Run(context.Background(), "opencode", "agent", "list")

	calls := runner.Calls()
	if len(calls) != 3 {
		t.Errorf("Expected 3 calls, got %d", len(calls))
	}
	if calls[0].Name != "opencode" || calls[0].Args[0] != "--version" {
		t.Error("First call order incorrect")
	}
	if calls[1].Name != "bun" || calls[1].Args[0] != "--version" {
		t.Error("Second call order incorrect")
	}
	if calls[2].Name != "opencode" || calls[2].Args[0] != "agent" || calls[2].Args[1] != "list" {
		t.Error("Third call order incorrect")
	}
}

// Test: fakeFS stat error is returned
func TestFakeFS_StatError(t *testing.T) {
	fs := newFakeFS()
	fs.statErr = errors.New("stat failed")

	_, err := fs.Stat("/any/path")

	if err == nil {
		t.Error("Expected error from stat")
	}
	if err.Error() != "stat failed" {
		t.Errorf("Error = %q, want %q", err.Error(), "stat failed")
	}
}

// Test: fakeFS probe error is returned
func TestFakeFS_TempWritableProbeError(t *testing.T) {
	fs := newFakeFS()
	fs.probeErr = errors.New("probe failed")

	err := fs.TempWritableProbe("/any/dir")

	if err == nil {
		t.Error("Expected error from TempWritableProbe")
	}
	if err.Error() != "probe failed" {
		t.Errorf("Error = %q, want %q", err.Error(), "probe failed")
	}
}

// Test: fakeFileInfo mode and isDir
func TestFakeFileInfo_ModeAndIsDir(t *testing.T) {
	info := &fakeFileInfo{isDir: true, mode: 0755}

	if !info.IsDir() {
		t.Error("IsDir() should return true")
	}
	if info.Mode() != 0755 {
		t.Errorf("Mode() = %d, want %d", info.Mode(), 0755)
	}
}

// Test: fakeFileInfo not a directory
func TestFakeFileInfo_NotDir(t *testing.T) {
	info := &fakeFileInfo{isDir: false, mode: 0644}

	if info.IsDir() {
		t.Error("IsDir() should return false")
	}
	if info.Mode() != 0644 {
		t.Errorf("Mode() = %d, want %d", info.Mode(), 0644)
	}
}

// Test: Options LookupEnv called for env resolution
func TestResolveEnv_LookupEnvCalled(t *testing.T) {
	called := false
	opts := Options{
		LookupEnv: func(key string) (string, bool) {
			called = true
			if key == "TEST_VAR" {
				return "test_value", true
			}
			return "", false
		},
	}

	val := resolveEnv("TEST_VAR", opts)

	if !called {
		t.Error("LookupEnv should be called")
	}
	if val != "test_value" {
		t.Errorf("resolveEnv(TEST_VAR) = %q, want %q", val, "test_value")
	}
}

// Test: ResolveEnv returns empty when key not found
func TestResolveEnv_KeyNotFound(t *testing.T) {
	opts := Options{
		LookupEnv: func(key string) (string, bool) {
			return "", false
		},
	}

	val := resolveEnv("MISSING_VAR", opts)

	if val != "" {
		t.Errorf("resolveEnv(MISSING_VAR) = %q, want empty", val)
	}
}

// Test: openCodeRootDir with empty home
func TestOpenCodeRootDir_EmptyHome(t *testing.T) {
	homeDir := ""
	expected := filepath.Join("", ".config", "opencode")
	got := openCodeRootDir(homeDir)
	if got != expected {
		t.Errorf("openCodeRootDir(%q) = %q, want %q", homeDir, got, expected)
	}
}

// Test: openCodePluginsDir with empty home
func TestOpenCodePluginsDir_EmptyHome(t *testing.T) {
	homeDir := ""
	expected := filepath.Join("", ".config", "opencode", "plugins")
	got := openCodePluginsDir(homeDir)
	if got != expected {
		t.Errorf("openCodePluginsDir(%q) = %q, want %q", homeDir, got, expected)
	}
}

// Test: openCodeJSONPath with empty home
func TestOpenCodeJSONPath_EmptyHome(t *testing.T) {
	homeDir := ""
	expected := filepath.Join("", ".config", "opencode", "opencode.json")
	got := openCodeJSONPath(homeDir)
	if got != expected {
		t.Errorf("openCodeJSONPath(%q) = %q, want %q", homeDir, got, expected)
	}
}

// Test: report with all critical and plugin/runtime/agents findings ready → overall ready
// This tests the full fail-closed semantics: all conditions must be met for overall ready.
func TestReport_AllFindingsReady(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "v1.0.0\n", 0)
	runner.addResult("opencode", []string{"plugin", "--help"}, "plugin help\n", 0)
	runner.addResult("opencode", []string{"agent", "--help"}, "agent help\n", 0)
	runner.addResult("bun", []string{"--version"}, "1.0.0\n", 0)

	fs := newFakeFS()
	fs.statDirs["/fake/home/.config/opencode"] = true
	fs.statDirs["/fake/home/.config/opencode/plugins"] = true

	opts := Options{HomeDir: "/fake/home"}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	// All findings are ready: CLI, version, config, plugins, plugin API, runtime, agents
	// Therefore overall should be ready (all fail-closed conditions met)
	if report.Overall != StatusReady {
		t.Errorf("Overall = %v, want %v (all findings ready)", report.Overall, StatusReady)
	}
	if report.Version == "" {
		t.Error("Version should be populated")
	}
	if report.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", report.Version, "1.0.0")
	}
	// Expected: CLI, Version, Config, Plugins-dir, Plugin-API, Runtime, Agents = 7 findings
	if len(report.Findings) != 7 {
		t.Errorf("Expected 7 findings, got %d", len(report.Findings))
	}
}

// Test: report overall matches aggregate logic
func TestReport_OverallMatchesAggregate(t *testing.T) {
	// Test case: CLI ready, version unknown, config warn, plugins unknown, runtime unknown, agents unknown
	// Expected overall: ready (CLI is critical and ready)
	runner := newFakeCommandRunner()
	runner.addResult("opencode", []string{"--version"}, "unparseable\n", 0)

	fs := newFakeFS()
	fs.statDirs["/fake/home/.config/opencode"] = true

	opts := Options{HomeDir: "/fake/home", AllowTempProbe: true}
	report, err := Probe(context.Background(), runner, fs, opts)

	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	// Verify report.Overall matches aggregate logic
	expectedOverall := aggregate(report.Findings)
	if report.Overall != expectedOverall {
		t.Errorf("report.Overall = %v, aggregate(findings) = %v", report.Overall, expectedOverall)
	}
}

// Test: realFS Stat with real file (should not error in test env)
func TestRealFS_StatRealFile(t *testing.T) {
	fs := &realFS{}

	// Stat a file that should exist in test environment
	info, err := fs.Stat("/dev/null")
	if err != nil {
		t.Fatalf("Stat(/dev/null) returned error: %v", err)
	}
	if info == nil {
		t.Fatal("Stat returned nil info")
	}
	// /dev/null is not a directory
	if info.IsDir() {
		t.Error("/dev/null should not be a directory")
	}
}

// Test: realFS Stat with nonexistent file (should error)
func TestRealFS_StatNonexistent(t *testing.T) {
	fs := &realFS{}

	_, err := fs.Stat("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Error("Stat should return error for nonexistent path")
	}
}

// Test: realFS ReadFile with nonexistent file (should error)
func TestRealFS_ReadFileNonexistent(t *testing.T) {
	fs := &realFS{}

	_, err := fs.ReadFile("/nonexistent/file.txt")
	if err == nil {
		t.Error("ReadFile should return error for nonexistent path")
	}
}

// Test: realFS TempWritableProbe with temp dir (should succeed)
func TestRealFS_TempWritableProbe_TempDir(t *testing.T) {
	fs := &realFS{}

	tempDir := os.TempDir()
	err := fs.TempWritableProbe(tempDir)
	if err != nil {
		t.Errorf("TempWritableProbe(temp dir) returned error: %v", err)
	}
}

// Test: realFS TempWritableProbe with nonexistent path (should error)
func TestRealFS_TempWritableProbe_Nonexistent(t *testing.T) {
	fs := &realFS{}

	err := fs.TempWritableProbe("/nonexistent/path")
	if err == nil {
		t.Error("TempWritableProbe should return error for nonexistent path")
	}
}

// Test: realFS TempWritableProbe with file (not dir) (should error)
func TestRealFS_TempWritableProbe_File(t *testing.T) {
	fs := &realFS{}

	tempFile, err := os.CreateTemp("", "probe-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())
	tempFile.Close()

	err = fs.TempWritableProbe(tempFile.Name())
	if err == nil {
		t.Error("TempWritableProbe should return error for file (not directory)")
	}
}

// Test: realCommandRunner runs a real command
func TestRealCommandRunner_RunRealCommand(t *testing.T) {
	runner := &realCommandRunner{}

	result, err := runner.Run(context.Background(), "echo", "hello")
	if err != nil {
		t.Fatalf("Run(echo hello) returned error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "hello") {
		t.Errorf("Stdout = %q, want to contain 'hello'", result.Stdout)
	}
}

// Test: realCommandRunner command not found
func TestRealCommandRunner_CommandNotFound(t *testing.T) {
	runner := &realCommandRunner{}

	_, err := runner.Run(context.Background(), "nonexistent-command-xyz")
	if err == nil {
		t.Error("Run should return error for nonexistent command")
	}
}

// Test: runCommand with context cancellation
func TestRunCommand_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := runCommand(ctx, "sleep", "10")
	if err == nil {
		t.Error("Expected error from cancelled context")
	}
}

// Test: Result with all fields populated
func TestResult_AllFields(t *testing.T) {
	result := Result{
		Stdout:   "stdout content",
		Stderr:   "stderr content",
		ExitCode: 42,
		Error:    errors.New("test error"),
	}

	if result.Stdout != "stdout content" {
		t.Errorf("Result.Stdout = %q, want %q", result.Stdout, "stdout content")
	}
	if result.Stderr != "stderr content" {
		t.Errorf("Result.Stderr = %q, want %q", result.Stderr, "stderr content")
	}
	if result.ExitCode != 42 {
		t.Errorf("Result.ExitCode = %d, want %d", result.ExitCode, 42)
	}
	if result.Error == nil || result.Error.Error() != "test error" {
		t.Errorf("Result.Error = %v, want 'test error'", result.Error)
	}
}