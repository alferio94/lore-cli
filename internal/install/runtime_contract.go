package install

import "fmt"

type RuntimeContract struct {
	Version         int                     `json:"version"`
	AgentResolution AgentResolutionContract `json:"agentResolution"`
}

type AgentResolutionContract struct {
	ManagedFilenamePrefix      string   `json:"managedFilenamePrefix"`
	SupportsManagedFrontmatter bool     `json:"supportsManagedFrontmatter,omitempty"`
	ManagedBy                  string   `json:"managedBy"`
	ManagedLayer               string   `json:"managedLayer"`
	Precedence                 []string `json:"precedence"`
	ProjectAgentsDefault       string   `json:"projectAgentsDefault"`
	ProjectAgentsSettingPath   string   `json:"projectAgentsSettingPath"`
}

func defaultRuntimeContract() RuntimeContract {
	return RuntimeContract{
		Version: 1,
		AgentResolution: AgentResolutionContract{
			ManagedFilenamePrefix:      "lore-managed-",
			SupportsManagedFrontmatter: false,
			ManagedBy:                  "lore-cli",
			ManagedLayer:               "global-overlay",
			Precedence:                 []string{"builtin", "managed", "user", "project"},
			ProjectAgentsDefault:       "enabled",
			ProjectAgentsSettingPath:   "lore.agent_resolution.project_agents",
		},
	}
}

func (r PiInstallRequest) WithRuntimeContract(contract RuntimeContract) PiInstallRequest {
	r.RuntimeContract = contract
	return r
}

func (r PiInstallRequest) runtimeContractOrDefault() RuntimeContract {
	if r.RuntimeContract.Version == 0 {
		return defaultRuntimeContract()
	}
	return r.RuntimeContract
}

func validateRuntimeContractCompatibility(contract RuntimeContract) error {
	if contract.Version != 1 {
		return fmt.Errorf("runtime contract compatibility failed: unsupported contract version %d for agentResolution overlays", contract.Version)
	}
	resolution := contract.AgentResolution
	if resolution.ManagedFilenamePrefix == "" {
		return fmt.Errorf("runtime contract compatibility failed: agentResolution.managedFilenamePrefix is required")
	}
	if resolution.SupportsManagedFrontmatter && (resolution.ManagedBy == "" || resolution.ManagedLayer == "") {
		return fmt.Errorf("runtime contract compatibility failed: agentResolution managed markers are required when supportsManagedFrontmatter=true")
	}
	if len(resolution.Precedence) != 4 || resolution.Precedence[0] != "builtin" || resolution.Precedence[1] != "managed" || resolution.Precedence[2] != "user" || resolution.Precedence[3] != "project" {
		return fmt.Errorf("runtime contract compatibility failed: agentResolution.precedence must be [builtin managed user project]")
	}
	if resolution.ProjectAgentsSettingPath == "" {
		return fmt.Errorf("runtime contract compatibility failed: agentResolution.projectAgentsSettingPath is required")
	}
	return nil
}
