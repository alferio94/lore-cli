package install

import (
	"context"
	"fmt"

	"github.com/alferio94/lore-cli/internal/agentconfig"
	"github.com/alferio94/lore-cli/internal/agentpack"
)

type CapabilityID string

type MergeMode string

const (
	CapabilityAgentPack      CapabilityID = "agent-pack"
	CapabilityPiExtensions   CapabilityID = "pi-extensions"
	CapabilityPrompt         CapabilityID = "prompt"
	CapabilitySkills         CapabilityID = "skills"
	CapabilityLoreServerMCP  CapabilityID = "lore-server-mcp"
	CapabilityExtendedSkills CapabilityID = "extended-skills"

	MergeModeReplace      MergeMode = "replace"
	MergeModeAdditiveJSON MergeMode = "additive-json"
	MergeModeMarkerMerge  MergeMode = "marker-merge"
)

type Capability struct {
	ID               CapabilityID
	Component        ComponentID
	Description      string
	Optional         bool
	EnabledByDefault bool
}

type RenderRequest struct {
	Target          TargetID
	Definition      agentpack.Definition
	Assets          agentpack.OperationalAssets
	Components      []ComponentID
	ServerURL       string
	SavedToken      string
	LoreBinaryPath  string
	LoreConfigDir   string
	LoreCLIVersion  string
	SettingsPath    string
	RuntimeContract RuntimeContract
	AgentConfig     agentconfig.Config
}

type RenderedFile struct {
	Component    ComponentID
	RelativePath string
	MergeMode    MergeMode
	Content      []byte
}

type HarnessAdapter interface {
	ID() TargetID
	Title() string
	Capabilities() map[CapabilityID]Capability
	Supports(ComponentID) bool
	Render(context.Context, RenderRequest) ([]RenderedFile, error)
	RenderManagedAgents(context.Context, RenderRequest) ([]RenderedFile, error)
	// RenderExtendedSkills renders extended skills for the target harness.
	// For Antigravity, this returns nil since extended skills are handled in Render.
	// For Pi, this renders to CLI-managed skill paths only, excluding user-owned paths.
	RenderExtendedSkills(context.Context, RenderRequest, PiLayout) ([]RenderedFile, error)
}

type Registry struct {
	adapters map[TargetID]HarnessAdapter
}

func NewRegistry(adapters ...HarnessAdapter) (*Registry, error) {
	registry := &Registry{adapters: make(map[TargetID]HarnessAdapter, len(adapters))}
	for _, adapter := range adapters {
		if adapter == nil {
			return nil, fmt.Errorf("adapter is nil")
		}
		if _, exists := registry.adapters[adapter.ID()]; exists {
			return nil, fmt.Errorf("duplicate adapter %q", adapter.ID())
		}
		registry.adapters[adapter.ID()] = adapter
	}
	return registry, nil
}

func (r *Registry) Resolve(target TargetID) (HarnessAdapter, error) {
	if r == nil {
		return nil, fmt.Errorf("adapter registry is not configured")
	}
	adapter, ok := r.adapters[target]
	if !ok {
		return nil, fmt.Errorf("target %q is not registered", target)
	}
	return adapter, nil
}

func (r RenderRequest) Validate() error {
	if r.Target == "" {
		return fmt.Errorf("target is required")
	}
	if err := r.effectiveDefinition().Validate(); err != nil {
		return fmt.Errorf("definition: %w", err)
	}
	components, err := NormalizeComponentSelection(r.Target, r.Components)
	if err != nil {
		return err
	}
	if (r.Target == TargetAntigravity || r.Target == TargetCodex) && containsComponent(components, ComponentLoreServerMCP) {
		if stringsTrimSpace(r.ServerURL) == "" {
			return fmt.Errorf("server url is required for target %q component %q", r.Target, ComponentLoreServerMCP)
		}
		if stringsTrimSpace(r.SavedToken) == "" {
			return fmt.Errorf("saved login token is required for target %q component %q", r.Target, ComponentLoreServerMCP)
		}
	}
	return nil
}

func (r RenderRequest) effectiveDefinition() agentpack.Definition {
	if r.Assets.PackID != "" {
		return r.Assets.Definition()
	}
	if r.Definition.SchemaVersion == 0 {
		return agentpack.DefaultDefinition()
	}
	return r.Definition
}

func (r RenderRequest) effectiveManagedAgents(resolver agentpack.SkillPathResolver) []agentpack.ManagedAgent {
	if r.Assets.PackID != "" {
		return r.Assets.ManagedAgents(resolver)
	}
	definition := r.effectiveDefinition()
	return append([]agentpack.ManagedAgent(nil), definition.ManagedAgents...)
}

// effectiveExtendedSkills returns the extended skills bundle resolved for the target harness.
// It only produces output when ComponentExtendedSkills is selected in the request.
func (r RenderRequest) effectiveExtendedSkills(resolver agentpack.SkillPathResolver) []agentpack.ManagedSkill {
	if !containsComponent(r.Components, ComponentExtendedSkills) {
		return nil
	}
	if r.Assets.PackID != "" {
		return r.Assets.ExtendedSkills(resolver)
	}
	assets := agentpack.OperationalAssets{}
	return assets.ExtendedSkills(resolver)
}

func defaultInstallRegistry() (*Registry, error) {
	return NewRegistry(defaultPiAdapter(), defaultAntigravityAdapter(), defaultCodexAdapter())
}

func containsComponent(components []ComponentID, target ComponentID) bool {
	for _, component := range components {
		if component == target {
			return true
		}
	}
	return false
}
