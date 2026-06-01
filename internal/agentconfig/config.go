// Package agentconfig provides the harness-neutral agent configuration contract
// for canonical SDD agents. It defines a secret-free, versioned schema stored as
// agent-config.json beside the auth-owned config.json.
//
// This package is intentionally scoped to configuration only: it does NOT enable
// live Codex execution, subagent invocation, per-harness projection, or active
// Lore MCP runtime behavior.
package agentconfig

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/alferio94/lore-cli/internal/agentpack"
)

// SchemaVersion is the current agent-config schema version.
const SchemaVersion = 1

// FileName is the agent-config filename, a sibling to config.json.
const FileName = "agent-config.json"

// DefaultSDDModel is the initial model for every canonical SDD agent.
// It aliases agentpack.DefaultSDDModel to avoid duplicate definitions.
const DefaultSDDModel = agentpack.DefaultSDDModel

// canonicalSDDPhases holds the canonical phase names derived from agentpack.
// It is a local variable alias to satisfy existing tests that reference this name.
var canonicalSDDPhases = agentpack.SDDPhaseAgentNames()

// Config is the agent-config.json document.
// It is secret-free: no tokens, passwords, bearer headers, or credentials.
type Config struct {
	SchemaVersion int               `json:"schema_version"`
	SDDAgents    map[string]Agent  `json:"sdd_agents"`
}

// Agent describes one declared SDD agent.
type Agent struct {
	Model string `json:"model"`
}

// CanonicalSDDPhases returns the canonical phase names for SDD agents.
// It delegates to agentpack.SDDPhaseAgentNames to avoid duplicate definitions
// and eliminate drift between agentconfig and agentpack.
func CanonicalSDDPhases() []string {
	return agentpack.SDDPhaseAgentNames()
}

// DefaultConfig returns a new Config with schema v1 and all canonical SDD
// agents declared with model DefaultSDDModel.
func DefaultConfig() Config {
	// Build agent map using the canonical phase list from agentpack.
	agentNames := agentpack.SDDPhaseAgentNames()
	agents := make(map[string]Agent, len(agentNames))
	for _, name := range agentNames {
		agents[name] = Agent{Model: agentpack.DefaultSDDModel}
	}
	return Config{
		SchemaVersion: SchemaVersion,
		SDDAgents:      agents,
	}
}

// Validate checks the config for schema and coverage correctness.
// It fails closed: malformed JSON, wrong schema version, missing canonical
// agents, unknown agent keys, or blank model values all produce errors.
func (c Config) Validate() error {
	if c.SchemaVersion != SchemaVersion {
		return fmt.Errorf("agent-config schema version %d is not supported (want %d)", c.SchemaVersion, SchemaVersion)
	}
	if c.SDDAgents == nil {
		return fmt.Errorf("agent-config sdd_agents is required")
	}

	// Validate coverage using canonical phases from agentpack.
	for _, name := range agentpack.SDDPhaseAgentNames() {
		agent, ok := c.SDDAgents[name]
		if !ok {
			return fmt.Errorf("agent-config missing canonical SDD agent %q", name)
		}
		if strings.TrimSpace(agent.Model) == "" {
			return fmt.Errorf("agent-config SDD agent %q has a blank model", name)
		}
	}

	// Reject any unknown agent keys to fail closed.
	for name := range c.SDDAgents {
		if !agentpack.IsKnownSDDAgent(name) {
			return fmt.Errorf("agent-config contains unknown SDD agent %q", name)
		}
	}

	return nil
}

// renderConfig is the internal struct used for deterministic JSON output.
// Keys are written in canonical order so repeated saves are byte-stable.
type renderConfig struct {
	SchemaVersion int               `json:"schema_version"`
	SDDAgents     map[string]Agent `json:"sdd_agents"`
}

// ToJSON returns a canonical JSON string with stable key ordering.
// This is used by Store.Save to produce deterministic/idempotent output.
func (c Config) ToJSON() (string, error) {
	// Canonicalise: copy agents into a new map ordered by phase.
	agentNames := agentpack.SDDPhaseAgentNames()
	ordered := make(map[string]Agent, len(agentNames))
	for _, name := range agentNames {
		if agent, ok := c.SDDAgents[name]; ok {
			ordered[name] = agent
		}
	}
	r := renderConfig{
		SchemaVersion: c.SchemaVersion,
		SDDAgents:     ordered,
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal agent-config: %w", err)
	}
	// Normalise trailing newline.
	if len(data) > 0 && data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	return string(data), nil
}

// FromJSON deserialises a Config. Call Validate() separately to enforce schema rules.
func FromJSON(data string) (Config, error) {
	var cfg Config
	if err := json.Unmarshal([]byte(data), &cfg); err != nil {
		return Config{}, fmt.Errorf("unmarshal agent-config: %w", err)
	}
	return cfg, nil
}

// SchemaAgents returns the set of canonical agent keys.
// It is an alias for CanonicalSDDPhases for callers that expect a "schema agents" label.
func SchemaAgents() []string {
	return agentpack.SDDPhaseAgentNames()
}

// SortedAgentKeys returns the agent keys sorted alphabetically.
// Useful for deterministic iteration in diagnostics.
func SortedAgentKeys(c Config) []string {
	keys := make([]string, 0, len(c.SDDAgents))
	for k := range c.SDDAgents {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
