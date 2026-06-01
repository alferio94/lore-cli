package agentconfig

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSchemaVersion(t *testing.T) {
	if SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", SchemaVersion)
	}
}

func TestFileName(t *testing.T) {
	if FileName != "agent-config.json" {
		t.Errorf("FileName = %q, want agent-config.json", FileName)
	}
}

func TestDefaultSDDModel(t *testing.T) {
	if DefaultSDDModel != "gpt-5.4" {
		t.Errorf("DefaultSDDModel = %q, want gpt-5.4", DefaultSDDModel)
	}
}

func TestCanonicalSDDPhases(t *testing.T) {
	phases := CanonicalSDDPhases()
	want := []string{
		"sdd-init", "sdd-explore", "sdd-propose", "sdd-spec",
		"sdd-design", "sdd-tasks", "sdd-apply", "sdd-verify", "sdd-archive",
	}
	if len(phases) != len(want) {
		t.Fatalf("len(CanonicalSDDPhases()) = %d, want %d", len(phases), len(want))
	}
	for i := range want {
		if phases[i] != want[i] {
			t.Errorf("phase[%d] = %q, want %q", i, phases[i], want[i])
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", cfg.SchemaVersion, SchemaVersion)
	}
	if len(cfg.SDDAgents) != len(canonicalSDDPhases) {
		t.Errorf("len(SDDAgents) = %d, want %d", len(cfg.SDDAgents), len(canonicalSDDPhases))
	}
	for _, name := range canonicalSDDPhases {
		agent, ok := cfg.SDDAgents[name]
		if !ok {
			t.Errorf("DefaultConfig missing SDD agent %q", name)
			continue
		}
		if agent.Model != DefaultSDDModel {
			t.Errorf("SDDAgents[%q].Model = %q, want %q", name, agent.Model, DefaultSDDModel)
		}
	}
}

func TestConfigValidateOK(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("DefaultConfig.Validate() = %v, want nil", err)
	}
}

func TestConfigValidateWrongSchemaVersion(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SchemaVersion = 99
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() should reject wrong schema version")
	} else if !strings.Contains(err.Error(), "schema version") {
		t.Errorf("Validate() error = %q, want schema version error", err.Error())
	}
}

func TestConfigValidateNilSDDAgents(t *testing.T) {
	cfg := Config{SchemaVersion: SchemaVersion, SDDAgents: nil}
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() should reject nil SDDAgents")
	}
}

func TestConfigValidateMissingAgent(t *testing.T) {
	cfg := DefaultConfig()
	delete(cfg.SDDAgents, "sdd-verify")
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() should reject missing canonical agent")
	} else if !strings.Contains(err.Error(), "sdd-verify") {
		t.Errorf("Validate() error = %q, want missing agent error", err.Error())
	}
}

func TestConfigValidateBlankModel(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SDDAgents["sdd-apply"] = Agent{Model: "   "}
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() should reject blank model")
	} else if !strings.Contains(err.Error(), "blank model") {
		t.Errorf("Validate() error = %q, want blank model error", err.Error())
	}
}

func TestConfigValidateUnknownAgent(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SDDAgents["sdd-fake"] = Agent{Model: "gpt-5.4"}
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() should reject unknown agent key")
	} else if !strings.Contains(err.Error(), "sdd-fake") {
		t.Errorf("Validate() error = %q, want unknown agent error", err.Error())
	}
}

func TestFromJSON(t *testing.T) {
	cfg := DefaultConfig()
	data, err := cfg.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}
	restored, err := FromJSON(data)
	if err != nil {
		t.Fatalf("FromJSON() error = %v", err)
	}
	if err := restored.Validate(); err != nil {
		t.Errorf("Restored config Validate() = %v, want nil", err)
	}
}

func TestFromJSONInvalid(t *testing.T) {
	_, err := FromJSON("not json")
	if err == nil {
		t.Error("FromJSON() should reject invalid JSON")
	}
}

func TestToJSONIsCanonical(t *testing.T) {
	// Two logically identical configs must produce byte-identical output.
	cfg1 := DefaultConfig()
	cfg2 := DefaultConfig()
	cfg2.SDDAgents["sdd-init"] = Agent{Model: "gpt-5.4"} // already that value

	json1, err := cfg1.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}
	json2, err := cfg2.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	if json1 != json2 {
		t.Errorf("Canonical output not stable: json1 != json2")
	}
}

func TestToJSONSchemaVersionPresent(t *testing.T) {
	cfg := DefaultConfig()
	data, err := cfg.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}
	if !strings.Contains(data, `"schema_version"`) {
		t.Error("ToJSON() output should contain schema_version")
	}
	if !strings.Contains(data, `"sdd_agents"`) {
		t.Error("ToJSON() output should contain sdd_agents")
	}
}

func TestConfigIsSecretFree(t *testing.T) {
	cfg := DefaultConfig()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	jsonStr := strings.ToLower(string(data))
	sensitive := []string{"token", "secret", "password", "credential", "apikey", "bearer", "auth"}
	for _, s := range sensitive {
		if strings.Contains(jsonStr, s) {
			t.Errorf("Config JSON contains sensitive field %q", s)
		}
	}
}

func TestSortedAgentKeys(t *testing.T) {
	cfg := Config{
		SchemaVersion: SchemaVersion,
		SDDAgents: map[string]Agent{
			"zebra":    {Model: "gpt-5.4"},
			"alpha":    {Model: "gpt-5.4"},
			"sdd-init": {Model: "gpt-5.4"},
		},
	}
	keys := SortedAgentKeys(cfg)
	want := []string{"alpha", "sdd-init", "zebra"}
	if len(keys) != len(want) {
		t.Fatalf("SortedAgentKeys len = %d, want %d", len(keys), len(want))
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("SortedAgentKeys()[%d] = %q, want %q", i, keys[i], want[i])
		}
	}
}

func TestSchemaAgents(t *testing.T) {
	agents := SchemaAgents()
	if len(agents) != len(canonicalSDDPhases) {
		t.Errorf("len(SchemaAgents()) = %d, want %d", len(agents), len(canonicalSDDPhases))
	}
}
