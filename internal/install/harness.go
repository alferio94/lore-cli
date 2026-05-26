package install

import (
	"fmt"
	"time"

	"github.com/alferio94/lore-cli/internal/agentpack"
)

type HarnessLayout struct {
	Target       TargetID
	RootDir      string
	ManifestPath string
	Paths        map[string]string
}

type PlanFileAction struct {
	Component    ComponentID
	RelativePath string
	AbsolutePath string
	Action       string
	MergeMode    MergeMode
	BackupPath   string
}

type InstallRequest struct {
	HomeDir         string
	ServerURL       string
	LoreBinaryPath  string
	LoreConfigDir   string
	LoreCLIVersion  string
	Target          TargetID
	Components      []ComponentID
	Definition      agentpack.Definition
	RuntimeContract RuntimeContract
	Now             time.Time
}

type InstallPlan struct {
	Request    InstallRequest
	Layout     HarnessLayout
	Components []ComponentID
	Files      []PlanFileAction
}

type InstallResult struct {
	Target   TargetID
	Layout   HarnessLayout
	Summary  InstallSummary
	Manifest Manifest
}

func (r InstallRequest) Validate() error {
	if r.Target == "" {
		return fmt.Errorf("target is required")
	}
	if stringsTrimSpace(r.HomeDir) == "" {
		return fmt.Errorf("home dir is required")
	}
	definition := r.Definition
	if definition.SchemaVersion == 0 {
		definition = agentpack.DefaultDefinition()
	}
	if err := definition.Validate(); err != nil {
		return fmt.Errorf("definition: %w", err)
	}
	if _, err := NormalizeComponentSelection(r.Target, r.Components); err != nil {
		return err
	}
	return nil
}

func stringsTrimSpace(value string) string {
	start, end := 0, len(value)
	for start < end {
		switch value[start] {
		case ' ', '\t', '\n', '\r':
			start++
		default:
			goto trimRight
		}
	}
	return ""

trimRight:
	for end > start {
		switch value[end-1] {
		case ' ', '\t', '\n', '\r':
			end--
		default:
			return value[start:end]
		}
	}
	return value[start:end]
}
