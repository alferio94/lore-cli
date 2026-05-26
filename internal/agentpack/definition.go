package agentpack

import (
	"fmt"
	"strings"
)

const SchemaVersion1 = 1

type PhaseID string

const (
	PhaseInit     PhaseID = "init"
	PhaseExplore  PhaseID = "explore"
	PhaseProposal PhaseID = "proposal"
	PhaseSpec     PhaseID = "spec"
	PhaseDesign   PhaseID = "design"
	PhaseTasks    PhaseID = "tasks"
	PhaseApply    PhaseID = "apply"
	PhaseVerify   PhaseID = "verify"
	PhaseArchive  PhaseID = "archive"
)

var orderedPhaseIDs = []PhaseID{
	PhaseInit,
	PhaseExplore,
	PhaseProposal,
	PhaseSpec,
	PhaseDesign,
	PhaseTasks,
	PhaseApply,
	PhaseVerify,
	PhaseArchive,
}

const (
	RoleOrchestrator = "orchestrator"
	RoleLoreWorker   = "lore-worker"
)

type Definition struct {
	SchemaVersion int
	PackID        string
	Persona       Persona
	Workflow      Workflow
	Roles         []Role
	Profiles      []Profile
	ManagedAgents []ManagedAgent
}

type Persona struct {
	Name            string
	Identity        string
	Tone            string
	LanguagePolicy  string
	BehaviorRules   []string
	MentorTriggers  []string
	WorkerExecution string
}

type Workflow struct {
	Phases []Phase
}

type Phase struct {
	ID      PhaseID
	Title   string
	Summary string
}

type Role struct {
	Name    string
	Kind    string
	Summary string
}

type Profile struct {
	ID           string
	Description  string
	DefaultModel string
	RoleModels   map[string]string
}

type SkillPolicy struct {
	Mode  string
	Files []string
}

type ManagedAgent struct {
	Name                  string
	Description           string
	Tools                 []string
	Role                  string
	Phase                 PhaseID
	RequiredEnvelope      string
	SkillPolicy           SkillPolicy
	SystemPromptMode      string
	InheritProjectContext bool
	Body                  string
}

func OrderedPhaseIDs() []PhaseID {
	return append([]PhaseID(nil), orderedPhaseIDs...)
}

func PhaseAgentName(id PhaseID) string {
	if id == PhaseProposal {
		return "sdd-propose"
	}
	return "sdd-" + string(id)
}

func (d Definition) Validate() error {
	if d.SchemaVersion != SchemaVersion1 {
		return fmt.Errorf("schema_version = %d, want %d", d.SchemaVersion, SchemaVersion1)
	}
	if strings.TrimSpace(d.PackID) == "" {
		return fmt.Errorf("pack_id is required")
	}
	if len(d.Workflow.Phases) != len(orderedPhaseIDs) {
		return fmt.Errorf("workflow phases = %d, want %d", len(d.Workflow.Phases), len(orderedPhaseIDs))
	}
	for i, want := range orderedPhaseIDs {
		if got := d.Workflow.Phases[i].ID; got != want {
			return fmt.Errorf("workflow phase %d = %q, want %q", i, got, want)
		}
	}
	if len(d.Roles) == 0 {
		return fmt.Errorf("roles are required")
	}
	seenRoles := make(map[string]struct{}, len(d.Roles))
	for _, role := range d.Roles {
		name := strings.TrimSpace(role.Name)
		if name == "" {
			return fmt.Errorf("role name is required")
		}
		seenRoles[name] = struct{}{}
	}
	if _, ok := seenRoles[RoleOrchestrator]; !ok {
		return fmt.Errorf("role %q is required", RoleOrchestrator)
	}
	if _, ok := seenRoles[RoleLoreWorker]; !ok {
		return fmt.Errorf("role %q is required", RoleLoreWorker)
	}
	if len(d.Profiles) == 0 {
		return fmt.Errorf("profiles are required")
	}
	seenProfiles := make(map[string]struct{}, len(d.Profiles))
	for _, profile := range d.Profiles {
		if strings.TrimSpace(profile.ID) == "" {
			return fmt.Errorf("profile id is required")
		}
		if strings.TrimSpace(profile.DefaultModel) == "" {
			return fmt.Errorf("profile %q default model is required", profile.ID)
		}
		if _, exists := seenProfiles[profile.ID]; exists {
			return fmt.Errorf("duplicate profile %q", profile.ID)
		}
		seenProfiles[profile.ID] = struct{}{}
	}
	if len(d.ManagedAgents) == 0 {
		return fmt.Errorf("managed agents are required")
	}
	seenManagedAgents := make(map[string]struct{}, len(d.ManagedAgents))
	for _, agent := range d.ManagedAgents {
		if strings.TrimSpace(agent.Name) == "" {
			return fmt.Errorf("managed agent name is required")
		}
		if strings.TrimSpace(agent.Description) == "" {
			return fmt.Errorf("managed agent %q description is required", agent.Name)
		}
		if strings.TrimSpace(agent.SystemPromptMode) == "" {
			return fmt.Errorf("managed agent %q system prompt mode is required", agent.Name)
		}
		if strings.TrimSpace(agent.Body) == "" {
			return fmt.Errorf("managed agent %q body is required", agent.Name)
		}
		if _, exists := seenManagedAgents[agent.Name]; exists {
			return fmt.Errorf("duplicate managed agent %q", agent.Name)
		}
		seenManagedAgents[agent.Name] = struct{}{}
	}
	if _, ok := seenManagedAgents[RoleLoreWorker]; !ok {
		return fmt.Errorf("managed agent %q is required", RoleLoreWorker)
	}
	return nil
}

func (d Definition) Profile(id string) (Profile, error) {
	for _, profile := range d.Profiles {
		if profile.ID == id {
			return profile, nil
		}
	}
	return Profile{}, fmt.Errorf("profile %q not found", id)
}

func (p Profile) ModelForRole(role string) string {
	if model := strings.TrimSpace(p.RoleModels[role]); model != "" {
		return model
	}
	return p.DefaultModel
}
