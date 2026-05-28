package agentpack

import (
	"path/filepath"
	"strings"
)

type OperationalAssets struct {
	PackID   string
	Persona  Persona
	Workflow Workflow
	Roles    []Role
	Profiles []Profile
	Agents   []AgentInstructionAsset
}

type PromptAsset struct {
	Template  string
	SkillRefs []SkillRef
}

type AgentInstructionAsset struct {
	Name                  string
	Description           string
	Tools                 []string
	Role                  string
	Phase                 PhaseID
	RequiredEnvelope      string
	SkillPolicy           SkillLoadPolicy
	SystemPromptMode      string
	InheritProjectContext bool
	Body                  PromptAsset
}

type SkillLoadPolicy struct {
	Mode string
	Refs []SkillRef
}

type SkillRef struct {
	Name   string
	Shared bool
}

type SkillPathResolver interface {
	ResolveSkillRef(SkillRef) string
}

type skillPathResolverFunc func(SkillRef) string

func (f skillPathResolverFunc) ResolveSkillRef(ref SkillRef) string {
	return f(ref)
}

func Skill(name string) SkillRef {
	return SkillRef{Name: name}
}

func SharedSkill(name string) SkillRef {
	return SkillRef{Name: name, Shared: true}
}

func PiSkillPathResolver() SkillPathResolver {
	return skillPathResolverFunc(resolvePiSkillRef)
}

func AntigravitySkillPathResolver() SkillPathResolver {
	return skillPathResolverFunc(resolveAntigravitySkillRef)
}

func ResolveSkillPaths(resolver SkillPathResolver, refs []SkillRef) []string {
	resolved := make([]string, 0, len(refs))
	for _, ref := range refs {
		resolved = append(resolved, resolver.ResolveSkillRef(ref))
	}
	return resolved
}

func (a PromptAsset) Render(resolver SkillPathResolver) string {
	text := a.Template
	for _, ref := range a.SkillRefs {
		text = strings.ReplaceAll(text, ref.placeholder(), resolver.ResolveSkillRef(ref))
	}
	return text
}

func (a AgentInstructionAsset) ManagedAgent(resolver SkillPathResolver) ManagedAgent {
	return ManagedAgent{
		Name:                  a.Name,
		Description:           a.Description,
		Tools:                 append([]string(nil), a.Tools...),
		Role:                  a.Role,
		Phase:                 a.Phase,
		RequiredEnvelope:      a.RequiredEnvelope,
		SkillPolicy:           SkillPolicy{Mode: a.SkillPolicy.Mode, Files: ResolveSkillPaths(resolver, a.SkillPolicy.Refs)},
		SystemPromptMode:      a.SystemPromptMode,
		InheritProjectContext: a.InheritProjectContext,
		Body:                  a.Body.Render(resolver),
	}
}

func (a OperationalAssets) ManagedAgents(resolver SkillPathResolver) []ManagedAgent {
	managed := make([]ManagedAgent, 0, len(a.Agents))
	for _, agent := range a.Agents {
		managed = append(managed, agent.ManagedAgent(resolver))
	}
	return managed
}

// ManagedSkill represents a rendered non-agent skill ready for installation
// to a harness-specific skills directory.
type ManagedSkill struct {
	Name        string
	Description string
	Body        string
}

func (a OperationalAssets) Definition() Definition {
	return Definition{
		SchemaVersion: SchemaVersion1,
		PackID:        a.PackID,
		Persona:       a.Persona,
		Workflow:      Workflow{Phases: append([]Phase(nil), a.Workflow.Phases...)},
		Roles:         append([]Role(nil), a.Roles...),
		Profiles:      cloneProfiles(a.Profiles),
		ManagedAgents: a.ManagedAgents(PiSkillPathResolver()),
	}
}

// ExtendedSkills returns the extended skills bundle rendered for the target harness.
func (a OperationalAssets) ExtendedSkills(resolver SkillPathResolver) []ManagedSkill {
	assets := DefaultExtendedSkills()
	resolved := make([]ManagedSkill, 0, len(assets))
	for _, asset := range assets {
		resolved = append(resolved, ManagedSkill{
			Name:        asset.Name,
			Description: asset.Description,
			Body:        asset.Body,
		})
	}
	return resolved
}

// ExtendedSkillRef returns a SkillRef for the extended skill with the given name.
func ExtendedSkillRef(name string) SkillRef {
	return SkillRef{Name: "skills/" + name}
}

func cloneProfiles(profiles []Profile) []Profile {
	cloned := make([]Profile, 0, len(profiles))
	for _, profile := range profiles {
		roleModels := make(map[string]string, len(profile.RoleModels))
		for role, model := range profile.RoleModels {
			roleModels[role] = model
		}
		cloned = append(cloned, Profile{
			ID:           profile.ID,
			Description:  profile.Description,
			DefaultModel: profile.DefaultModel,
			RoleModels:   roleModels,
		})
	}
	return cloned
}

func resolvePiSkillRef(ref SkillRef) string {
	if ref.Shared {
		return filepath.ToSlash(filepath.Join("~/.pi/agent/skills", ref.Name+".md"))
	}
	return filepath.ToSlash(filepath.Join("~/.pi/agent/skills", ref.Name, "SKILL.md"))
}

func resolveAntigravitySkillRef(ref SkillRef) string {
	if ref.Shared {
		return filepath.ToSlash(filepath.Join("~/.gemini/antigravity-cli/skills", ref.Name+".md"))
	}
	return filepath.ToSlash(filepath.Join("~/.gemini/antigravity-cli/skills", ref.Name, "SKILL.md"))
}

func (r SkillRef) placeholder() string {
	return "{{skill:" + r.Name + "}}"
}
