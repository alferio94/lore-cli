package install

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alferio94/lore-cli/internal/agentconfig"
	"github.com/alferio94/lore-cli/internal/agentpack"
)

const (
	opencodeConfigRootDirName = ".config"
	opencodeRootDirName       = "opencode"
	opencodeAgentsFileName    = "AGENTS.md"
	opencodeConfigFileName    = "opencode.json"
	opencodeSkillsDirName     = "skills"
	opencodeCommandsDirName   = "commands"
	opencodeManifestFileName  = "lore-install.json"
)

// opencodeMCPBlockKey is the top-level key for the OpenCode remote MCP config.
// OpenCode uses `mcp` (not `mcpServers`) as the canonical top-level key.
const opencodeMCPBlockKey = "mcp"

type opencodeAdapter struct {
	target       TargetID
	title        string
	capabilities map[CapabilityID]Capability
}

func defaultOpenCodeAdapter() HarnessAdapter {
	return opencodeAdapter{
		target: TargetOpenCode,
		title:  "OpenCode",
		capabilities: map[CapabilityID]Capability{
			CapabilityAgentPack: {
				ID:               CapabilityAgentPack,
				Component:        ComponentCorePack,
				Description:      "Render Lore-managed OpenCode AGENTS.md and managed skill files from the portable agent pack.",
				EnabledByDefault: true,
			},
			CapabilityExtendedSkills: {
				ID:          CapabilityExtendedSkills,
				Component:   ComponentExtendedSkills,
				Description: "Portable extended skill bundle for CLI-managed non-agent skills.",
				Optional:    true,
			},
			CapabilityLoreServerMCP: {
				ID:          CapabilityLoreServerMCP,
				Component:   ComponentLoreServerMCP,
				Description: "Optional Lore MCP configuration support for OpenCode.",
				Optional:    true,
			},
			CapabilityOpenCodeSDDAssets: {
				ID:          CapabilityOpenCodeSDDAssets,
				Component:   ComponentOpenCodeSDDAssets,
				Description: "Optional SDD command and prompt asset support for OpenCode.",
				Optional:    true,
			},
		},
	}
}

func (a opencodeAdapter) ID() TargetID  { return a.target }
func (a opencodeAdapter) Title() string { return a.title }

func (a opencodeAdapter) Capabilities() map[CapabilityID]Capability {
	copyMap := make(map[CapabilityID]Capability, len(a.capabilities))
	for key, value := range a.capabilities {
		copyMap[key] = value
	}
	return copyMap
}

func (a opencodeAdapter) Supports(component ComponentID) bool {
	for _, capability := range a.capabilities {
		if capability.Component == component {
			return true
		}
	}
	return false
}

func ResolveOpenCodeLayout(homeDir string) HarnessLayout {
	rootDir := filepath.Join(homeDir, opencodeConfigRootDirName, opencodeRootDirName)
	manifestPath := filepath.Join(rootDir, opencodeManifestFileName)
	skillsDir := filepath.Join(rootDir, opencodeSkillsDirName)
	commandsDir := filepath.Join(rootDir, opencodeCommandsDirName)
	return HarnessLayout{
		Target:       TargetOpenCode,
		RootDir:      rootDir,
		ManifestPath: manifestPath,
		Paths: map[string]string{
			opencodeConfigRootPathKey:  filepath.Join(homeDir, opencodeConfigRootDirName),
			opencodeDirPathKey:         rootDir,
			opencodeAgentsPathKey:      filepath.Join(rootDir, opencodeAgentsFileName),
			opencodeJSONPathKey:        filepath.Join(rootDir, opencodeConfigFileName),
			opencodeSkillsDirPathKey:   skillsDir,
			opencodeCommandsDirPathKey: commandsDir,
			opencodeManifestPathKey:    manifestPath,
			"harness_root":             rootDir,
		},
	}
}

func (a opencodeAdapter) Render(_ context.Context, req RenderRequest) ([]RenderedFile, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	components, err := NormalizeComponentSelection(req.Target, req.Components)
	if err != nil {
		return nil, err
	}
	for _, component := range components {
		if !a.Supports(component) {
			return nil, fmt.Errorf("component %q is not supported by target %q", component, a.target)
		}
	}

	rendered := make([]RenderedFile, 0, 1+len(req.effectiveManagedAgents(OpenCodeSkillPathResolver())))
	if containsComponent(components, ComponentCorePack) {
		agentsContent, err := renderOpenCodeAgentsMD(req)
		if err != nil {
			return nil, err
		}
		rendered = append(rendered, RenderedFile{
			Component:    ComponentCorePack,
			RelativePath: opencodeAgentsFileName,
			MergeMode:    MergeModeReplace,
			Content:      agentsContent,
		})
		rendered = append(rendered, renderOpenCodeManagedSkills(req)...)
	}
	if containsComponent(components, ComponentExtendedSkills) {
		rendered = append(rendered, renderOpenCodeExtendedSkills(req)...)
	}
	if containsComponent(components, ComponentLoreServerMCP) {
		// Skip opencode.json rendering here; renderOpenCodeFiles in opencode_install.go
		// will produce the complete opencode.json (lore + mcp.lore) when lore-server-mcp
		// is selected and auth is available. Do not error—the non-MCP files (AGENTS.md,
		// skills, etc.) still need to be returned.
	}
	// Phase 2: SDD assets content — commands and prompts rendered when opencode-sdd-assets
	// component is explicitly selected.
	if containsComponent(components, ComponentOpenCodeSDDAssets) {
		rendered = append(rendered, renderOpenCodeSDDCommands(req)...)
		rendered = append(rendered, renderOpenCodeSDDPrompts(req)...)
	}
	sort.Slice(rendered, func(i, j int) bool { return rendered[i].RelativePath < rendered[j].RelativePath })
	return rendered, nil
}

func (a opencodeAdapter) RenderManagedAgents(context.Context, RenderRequest) ([]RenderedFile, error) {
	return nil, nil
}

func (a opencodeAdapter) RenderExtendedSkills(_ context.Context, req RenderRequest, _ PiLayout) ([]RenderedFile, error) {
	return renderOpenCodeExtendedSkills(req), nil
}

func renderOpenCodeAgentsMD(req RenderRequest) ([]byte, error) {
	definition := req.effectiveDefinition()
	models := openCodeAgentModels(req.AgentConfig)
	modelLines := make([]string, 0, len(models))
	for _, name := range agentpack.SDDPhaseAgentNames() {
		modelLines = append(modelLines, "- "+name+": `"+models[name]+"`")
	}

	instruction := strings.TrimRight(agentpack.RenderOrchestratorSystemInstruction(definition), "\n")
	text := strings.Join([]string{
		"# Lore Runtime",
		"",
		"This file is managed by `lore install --target opencode` and should not be edited manually.",
		"",
		"## OpenCode managed surface",
		"- Managed skills directory: `~/.config/opencode/skills`",
		"- Managed settings merge target: `~/.config/opencode/opencode.json` (Lore owns the top-level `lore` block and, when lore-server-mcp is selected, the `mcp.lore` remote entry)",
		"- Managed manifest: `~/.config/opencode/lore-install.json`",
		"- Optional managed commands directory: `~/.config/opencode/commands` (omitted until an approved explicit command boundary exists)",
		"- Scope boundary: config-only Lore projection; no plugins, profiles, bootstrap/package-manager behavior, or native/runtime subagents.",
		"- Lore server MCP token: when lore-server-mcp is selected, the bearer token is persisted in opencode.json under `mcp.lore.headers.Authorization` and a plaintext-token warning appears at install time.",
		"",
		"## Managed SDD model declarations",
		strings.Join(modelLines, "\n"),
		"",
		"## Orchestrator instruction",
		"",
		instruction,
	}, "\n") + "\n"

	return []byte(text), nil
}

func renderOpenCodeManagedSkills(req RenderRequest) []RenderedFile {
	managedAgents := req.effectiveManagedAgents(OpenCodeSkillPathResolver())
	rendered := make([]RenderedFile, 0, len(managedAgents))
	for _, agent := range managedAgents {
		// Map PhaseProposal to the approved canonical "propose" name so skill paths
		// and filenames use the canonical "sdd-propose" form instead of "sdd-proposal".
		canonicalName := canonicalPhaseName(agentpack.PhaseID(agent.Name))
		skillDirName := strings.TrimPrefix(agent.Name, "sdd-")
		if strings.HasPrefix(agent.Name, "sdd-") {
			// For SDD phase agents, use the canonical name mapping.
			skillDirName = canonicalName
		}
		content := strings.Join([]string{
			"---",
			"name: " + skillDirName,
			"description: " + agent.Description,
			"---",
			agent.Body,
		}, "\n")
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		rendered = append(rendered, RenderedFile{
			Component:    ComponentCorePack,
			RelativePath: filepath.ToSlash(filepath.Join(opencodeSkillsDirName, skillDirName, "SKILL.md")),
			MergeMode:    MergeModeReplace,
			Content:      []byte(content),
		})
	}
	return rendered
}

func renderOpenCodeExtendedSkills(req RenderRequest) []RenderedFile {
	extendedSkills := req.effectiveExtendedSkills(OpenCodeSkillPathResolver())
	if len(extendedSkills) == 0 {
		return nil
	}
	rendered := make([]RenderedFile, 0, len(extendedSkills))
	for _, skill := range extendedSkills {
		content := renderManagedSkillMarkdown(skill)
		rendered = append(rendered, RenderedFile{
			Component:    ComponentExtendedSkills,
			RelativePath: filepath.ToSlash(filepath.Join(opencodeSkillsDirName, skill.Name, "SKILL.md")),
			MergeMode:    MergeModeReplace,
			Content:      []byte(content),
		})
	}
	return rendered
}

func OpenCodeSkillPathResolver() agentpack.SkillPathResolver {
	return agentpackSkillPathResolverFunc(func(ref agentpack.SkillRef) string {
		return filepath.ToSlash(filepath.Join("~/.config/opencode/skills", ref.Name, "SKILL.md"))
	})
}

func openCodeAgentModels(cfg agentconfig.Config) map[string]string {
	models := make(map[string]string, len(agentpack.SDDPhaseAgentNames()))
	for _, name := range agentpack.SDDPhaseAgentNames() {
		models[name] = agentpack.DefaultSDDModel
	}
	for name, agent := range cfg.SDDAgents {
		if strings.TrimSpace(agent.Model) == "" {
			continue
		}
		if _, ok := models[name]; ok {
			models[name] = strings.TrimSpace(agent.Model)
		}
	}
	return models
}

// canonicalPhaseName returns the approved canonical phase name for use in filenames
// and path components. PhaseProposal maps to "propose"; all other phases use their
// raw PhaseID string. This ensures consistent naming across commands, prompts, and
// skills for the approved OpenCode SDD asset surface.
func canonicalPhaseName(phase agentpack.PhaseID) string {
	switch phase {
	case agentpack.PhaseProposal:
		return "propose"
	default:
		return string(phase)
	}
}

// agentSkillNameToPhase maps a managed agent name (e.g., "sdd-propose") back to its
// PhaseID. This is used to map canonical skill names back to PhaseID for rendering.
func agentSkillNameToPhase(name string) agentpack.PhaseID {
	switch name {
	case "sdd-propose":
		return agentpack.PhaseProposal
	case "sdd-init":
		return agentpack.PhaseInit
	case "sdd-explore":
		return agentpack.PhaseExplore
	case "sdd-spec":
		return agentpack.PhaseSpec
	case "sdd-design":
		return agentpack.PhaseDesign
	case "sdd-tasks":
		return agentpack.PhaseTasks
	case "sdd-apply":
		return agentpack.PhaseApply
	case "sdd-verify":
		return agentpack.PhaseVerify
	case "sdd-archive":
		return agentpack.PhaseArchive
	default:
		return agentpack.PhaseID(name)
	}
}

// renderOpenCodeMCPConfig produces the opencode.json config file with both the top-level
// `lore` block and the `mcp.lore` remote entry. It combines the lore block from the
// agent config with the MCP remote config. This produces a complete opencode.json
// that can be used directly without additional merging in planOpenCodeManagedFileActions.
func renderOpenCodeMCPConfig(cfg agentconfig.Config, serverURL, token string) ([]byte, error) {
	if strings.TrimSpace(serverURL) == "" {
		return nil, fmt.Errorf("server-url is required for OpenCode MCP config")
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("token is required for OpenCode MCP config")
	}

	models := openCodeAgentModels(cfg)
	agents := make(map[string]map[string]string, len(models))
	for _, name := range agentpack.SDDPhaseAgentNames() {
		agents[name] = map[string]string{"model": models[name]}
	}

	lore := map[string]any{
		opencodeManagedByKey:     opencodeManagedByValue,
		opencodeSchemaVersionKey: 1,
		opencodeAgentsKey:        agents,
		opencodeSkillsDirKey:     "~/.config/opencode/skills",
	}

	mcpPayload := map[string]any{
		"type":    "remote",
		"url":     strings.TrimSpace(serverURL),
		"enabled": true,
		"headers": map[string]any{
			"Authorization": "Bearer " + strings.TrimSpace(token),
		},
	}

	payload := map[string]any{
		opencodeLoreBlockKey: lore,
		opencodeMCPBlockKey:  map[string]any{"lore": mcpPayload},
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode OpenCode MCP config: %w", err)
	}
	return append(data, '\n'), nil
}

// renderOpenCodeSDDCommands produces deterministic SDD phase command assets for OpenCode.
// Each command is a standalone markdown file with frontmatter and bounded Lore/OpenCode-
// adapted content. No Gentle references, no agent/runtime claims, no runtime subagents.
// The canonical command name for the proposal phase is "sdd-propose", not "sdd-proposal".
func renderOpenCodeSDDCommands(req RenderRequest) []RenderedFile {
	const phasePrefix = "sdd-"
	rendered := make([]RenderedFile, 0, 9)
	for _, phase := range agentpack.OrderedPhaseIDs() {
		// Map PhaseProposal to the approved canonical "propose" name.
		// All other phases use their raw name as-is.
		canonicalName := string(phase)
		if phase == agentpack.PhaseProposal {
			canonicalName = "propose"
		}
		name := phasePrefix + canonicalName
		title := phaseLabel(name)
		description := phaseDescription(phase)
		trigger := phaseTrigger(phase)
		content := renderSDDCommandContent(phase, name, title, description, trigger)
		rendered = append(rendered, RenderedFile{
			Component:    ComponentOpenCodeSDDAssets,
			RelativePath: filepath.ToSlash(filepath.Join(opencodeCommandsDirName, name+".md")),
			MergeMode:    MergeModeReplace,
			Content:      []byte(content),
		})
	}
	return rendered
}

func phaseLabel(name string) string {
	// Handle canonical SDD phase naming: "proposal" maps to "propose" (verb form),
	// "archive" maps to "archive" (no change). All other phases use their raw name.
	// The approved canonical command name is "sdd-propose", not "sdd-proposal".
	// This function receives the already-constructed name (e.g. "sdd-proposal")
	// and returns the display label using the canonical verb form.
	canonicalMap := map[string]string{
		"sdd-proposal": "SDD P — Proposal",
	}
	if label, ok := canonicalMap[name]; ok {
		return label
	}
	if len(name) >= 5 {
		return "SDD " + strings.ToUpper(string(name[4])) + name[5:]
	}
	return name
}

func phaseDescription(phase agentpack.PhaseID) string {
	switch phase {
	case agentpack.PhaseInit:
		return "Initialize Spec-Driven Development context in any project. Detects stack, conventions, testing capabilities, and bootstraps the active persistence backend."
	case agentpack.PhaseExplore:
		return "Explore and investigate ideas before committing to a change. Trigger: When the orchestrator launches you to think through a feature, investigate the codebase, or clarify requirements."
	case agentpack.PhaseProposal:
		return "Create a change proposal with intent, scope, and approach. Trigger: When the orchestrator launches you to create or update a proposal for a change."
	case agentpack.PhaseSpec:
		return "Write specifications with requirements and scenarios (delta specs for changes). Trigger: When the orchestrator launches you to write or update specs for a change."
	case agentpack.PhaseDesign:
		return "Create technical design document with architecture decisions and approach. Trigger: When the orchestrator launches you to write or update the technical design for a change."
	case agentpack.PhaseTasks:
		return "Break down a change into an implementation task checklist. Trigger: When the orchestrator launches you to create or update the task breakdown for a change."
	case agentpack.PhaseApply:
		return "Implement tasks from the change, writing actual code following the specs and design. Trigger: When the orchestrator launches you to implement one or more tasks from a change."
	case agentpack.PhaseVerify:
		return "Validate that implementation matches specs, design, and tasks. Trigger: When the orchestrator launches you to verify a completed (or partially completed) change."
	case agentpack.PhaseArchive:
		return "Sync delta specs to main specs and archive a completed change. Trigger: When the orchestrator launches you to archive a change after implementation and verification."
	default:
		return "SDD phase"
	}
}

func phaseTrigger(phase agentpack.PhaseID) string {
	switch phase {
	case agentpack.PhaseInit:
		return "sdd init, sdd-init, iniciar sdd, openspec init"
	case agentpack.PhaseExplore:
		return "sdd explore, sdd-explore, investigar"
	case agentpack.PhaseProposal:
		return "sdd propose, sdd-propose, proposal, propuesta"
	case agentpack.PhaseSpec:
		return "sdd spec, sdd-spec, especificacion"
	case agentpack.PhaseDesign:
		return "sdd design, sdd-design, diseno"
	case agentpack.PhaseTasks:
		return "sdd tasks, sdd-tasks"
	case agentpack.PhaseApply:
		return "sdd apply, sdd-apply, implementar"
	case agentpack.PhaseVerify:
		return "sdd verify, sdd-verify, verificar"
	case agentpack.PhaseArchive:
		return "sdd archive, sdd-archive, archivar"
	default:
		return string(phase)
	}
}

func renderSDDCommandContent(phase agentpack.PhaseID, name, title, description, trigger string) string {
	var body string
	switch phase {
	case agentpack.PhaseInit:
		body = "## When to Use\n\n- User wants to initialize SDD in a project\n- User says \"sdd init\", \"sdd-init\", \"iniciar sdd\", or \"openspec init\"\n\n## What to Do\n\n1. **Detect project conventions**: Identify stack, languages, test frameworks, and conventions\n2. **Resolve skill registry**: Check for .pi/skills/ or .agents/skills/ and load project standards\n3. **Bootstrap persistence backend**: Initialize Lore memory if writes are healthy; fall back to OpenSpec filesystem\n4. **Create change context**: Create a named change directory under openspec/changes/{change-name}/\n5. **Write state.yaml**: Schema version, target harness, components, and phase status\n\n## Output\n\nPersist SDD initialization artifact to Lore memory (topic_key: `sdd/{change-name}/init`) or to OpenSpec files.\n\n## Key Boundaries\n\n- Do NOT implement anything in init phase; only initialize context\n- Do NOT make irreversible changes; init must be safe to rerun\n- Do NOT claim runtime or subagent behavior"
	case agentpack.PhaseExplore:
		body = "## When to Use\n\n- User wants to explore a feature or problem space\n- User says \"sdd explore\", \"sdd-explore\", or \"investigar\"\n- Orchestrator launches explore phase for a proposed change\n\n## What to Do\n\n1. **Understand the problem**: Read existing code, understand current patterns, identify constraints\n2. **Gather context**: Investigate related files, dependencies, and potential impacts\n3. **Identify risks**: Note any architectural concerns, breaking changes, or unknowns\n4. **Evaluate approaches**: Consider multiple solutions before recommending one\n\n## Output\n\nExplore findings are the basis for the proposal phase. Persist to Lore memory (topic_key: `sdd/{change-name}/exploration`) or to openspec/changes/{change-name}/exploration.md.\n\n## Key Boundaries\n\n- Explore is investigation only; do NOT write specs or implementation\n- Do NOT make changes to the codebase\n- Do NOT commit anything; findings are temporary until proposal is approved"
	case agentpack.PhaseProposal:
		body = "## When to Use\n\n- User wants to formalize a proposed change\n- User says \"sdd propose\", \"sdd-propose\", \"proposal\", or \"propuesta\"\n- Orchestrator launches propose phase after explore is complete\n\n## What to Do\n\n1. **Define intent**: What is the change trying to achieve?\n2. **Scope the change**: What is in scope? What is explicitly out of scope?\n3. **Identify stakeholders and risks**: Who is affected? What could go wrong?\n4. **Propose approach**: High-level implementation strategy\n5. **Define rollback**: How do we undo this if it goes wrong?\n\n## Output\n\nProposal document persisted to Lore memory (topic_key: `sdd/{change-name}/proposal`) or to openspec/changes/{change-name}/proposal.md.\n\n## Key Boundaries\n\n- Proposal is intent and approach only; do NOT write detailed specs\n- Do NOT implement anything; proposal must be approved before spec\n- Do NOT skip explore phase; explore informs proposal"
	case agentpack.PhaseSpec:
		body = "## When to Use\n\n- User wants to write or update specifications\n- User says \"sdd spec\", \"sdd-spec\", \"especificacion\", or \"specs\"\n- Orchestrator launches spec phase after proposal is approved\n\n## What to Do\n\n1. **Write delta specs**: Focus on what is changing, not the entire system\n2. **Define requirements**: Functional and non-functional requirements\n3. **Write acceptance scenarios**: How do we verify the change works?\n4. **Document constraints**: External dependencies, API contracts, boundaries\n\n## Output\n\nDelta specs persisted to Lore memory (topic_key: `sdd/{change-name}/spec`) or to openspec/changes/{change-name}/specs/.\n\n## Key Boundaries\n\n- Specs define WHAT, not HOW; design covers implementation approach\n- Do NOT implement anything in spec phase\n- Do NOT reference specific files or functions; describe behavior generically\n- Do NOT skip proposal; specs build on approved proposal"
	case agentpack.PhaseDesign:
		body = "## When to Use\n\n- User wants to write technical design\n- User says \"sdd design\", \"sdd-design\", or \"diseno\"\n- Orchestrator launches design phase after specs are approved\n\n## What to Do\n\n1. **Describe approach**: How will we implement the change?\n2. **Define interfaces**: Public APIs, data structures, contracts\n3. **Document decisions**: Why this approach over alternatives?\n4. **Identify dependencies**: What does this change depend on?\n5. **Define validation**: How do we verify the design works?\n\n## Output\n\nDesign document persisted to Lore memory (topic_key: `sdd/{change-name}/design`) or to openspec/changes/{change-name}/design.md.\n\n## Key Boundaries\n\n- Design defines HOW, not implementation details\n- Do NOT write actual code in design phase\n- Do NOT skip spec phase; design builds on approved specs\n- Do NOT make architectural decisions that were not in scope"
	case agentpack.PhaseTasks:
		body = "## When to Use\n\n- User wants to create or update task breakdown\n- User says \"sdd tasks\" or \"sdd-tasks\"\n- Orchestrator launches tasks phase after design is complete\n\n## What to Do\n\n1. **Identify tasks**: Break the change into logical implementation units\n2. **Order tasks**: Group by dependency and risk\n3. **Define slice boundaries**: Each slice should be bounded and safe to checkpoint\n4. **Write task descriptions**: Clear acceptance criteria for each task\n\n## Output\n\nTasks document persisted to Lore memory (topic_key: `sdd/{change-name}/tasks`) or to openspec/changes/{change-name}/tasks.md.\n\n## Key Boundaries\n\n- Tasks define implementation order, not actual implementation\n- Do NOT implement tasks; apply phase handles implementation\n- Do NOT create too many tasks in one slice; keep slices small\n- Do NOT skip design; tasks build on approved design"
	case agentpack.PhaseApply:
		body = "## When to Use\n\n- User wants to implement tasks from approved tasks.md\n- User says \"sdd apply\", \"sdd-apply\", or \"implementar\"\n- Orchestrator launches apply phase with specific task scope\n\n## What to Do\n\n1. **Read specs and design**: Understand WHAT and HOW before writing code\n2. **Read existing patterns**: Match the project conventions and style\n3. **Implement in bounded slices**: Work on one logical chunk at a time\n4. **Persist checkpoints**: Save apply-partial after each completed task\n5. **Run focused validation**: Test the slice before moving on\n\n## Output\n\nImplementation artifacts persisted to Lore memory:\n- apply-started (topic_key: `sdd/{change-name}/apply-started`)\n- apply-partial (topic_key: `sdd/{change-name}/apply-partial`)\n- apply-progress (topic_key: `sdd/{change-name}/apply-progress`)\n- apply-report (topic_key: `sdd/{change-name}/apply-report`)\n\n## Key Boundaries\n\n- Apply implements ONE bounded slice, not the entire change\n- Do NOT skip specs or design; implementation must follow approved documents\n- Do NOT run broad tests during apply; verify phase handles validation\n- Do NOT commit changes; apply phase is implementation only"
	case agentpack.PhaseVerify:
		body = "## When to Use\n\n- User wants to verify implementation against specs\n- User says \"sdd verify\", \"sdd-verify\", or \"verificar\"\n- Orchestrator launches verify phase after apply completes\n\n## What to Do\n\n1. **Read specs and design**: Understand the acceptance criteria\n2. **Read implementation**: Understand what was actually written\n3. **Validate requirements**: Check each spec requirement against implementation\n4. **Run tests**: Execute test suite and verify coverage\n5. **Check boundaries**: Ensure no out-of-scope changes were made\n\n## Output\n\nVerification report persisted to Lore memory (topic_key: `sdd/{change-name}/verify-report`) or to openspec/changes/{change-name}/verify-report.md.\n\n## Key Boundaries\n\n- Verify validates implementation, does not implement\n- Do NOT write implementation code in verify phase\n- Do NOT skip apply phase; verify builds on completed implementation\n- Do NOT ignore test failures or spec violations"
	case agentpack.PhaseArchive:
		body = "## When to Use\n\n- User wants to archive a completed change\n- User says \"sdd archive\" or \"sdd-archive\"\n- Orchestrator launches archive phase after verify passes\n\n## What to Do\n\n1. **Verify all phases complete**: Ensure init through verify are done\n2. **Sync delta specs**: Merge delta specs into main specs directory\n3. **Archive change artifacts**: Move change directory to archive location\n4. **Update main spec index**: Ensure main specs reference the change\n\n## Output\n\nArchive persisted to Lore memory (topic_key: `sdd/{change-name}/archive`) or to openspec/changes/{change-name}/archive.md.\n\n## Key Boundaries\n\n- Archive is final phase; do NOT archive until all prior phases complete\n- Do NOT modify implementation after archive\n- Do NOT skip verify phase; archive builds on verified implementation\n- Do NOT delete change artifacts; archive preserves traceability"
	default:
		body = "## SDD Phase\n\nPlaceholder content."
	}

	frontmatter := strings.Join([]string{
		"---",
		"name: " + name,
		"description: >",
		"  " + description,
		"  Trigger: When user says '" + trigger + "'.",
		"license: MIT",
		"metadata:",
		"  author: gentleman-programming",
		"  version: \"1.0\"",
		"---",
		"",
		"# " + title,
		"",
		body,
	}, "\n") + "\n"

	return frontmatter
}

// renderOpenCodeSDDPrompts produces inert SDD per-phase prompt guidance for OpenCode.
// Each prompt is a standalone markdown file under prompts/sdd/ with bounded Lore/OpenCode-
// adapted content. No Gentle references, no agent/runtime claims, no runtime subagents,
// no opencode.json wiring. These are install-time assets only.
func renderOpenCodeSDDPrompts(req RenderRequest) []RenderedFile {
	const phasePrefix = "sdd-"
	rendered := make([]RenderedFile, 0, 9)
	for _, phase := range agentpack.OrderedPhaseIDs() {
		// Map PhaseProposal to the approved canonical "propose" name.
		canonicalName := canonicalPhaseName(phase)
		name := phasePrefix + canonicalName
		content := renderSDDPromptContent(phase)
		rendered = append(rendered, RenderedFile{
			Component:    ComponentOpenCodeSDDAssets,
			RelativePath: filepath.ToSlash(filepath.Join("prompts", "sdd", name+".md")),
			MergeMode:    MergeModeReplace,
			Content:      []byte(content),
		})
	}
	return rendered
}

func renderSDDPromptContent(phase agentpack.PhaseID) string {
	var title, triggers, summary string
	var boundaries []string

	switch phase {
	case agentpack.PhaseInit:
		title = "SDD Init"
		triggers = "sdd init, sdd-init, iniciar sdd, openspec init"
		summary = "Initialize Spec-Driven Development context in any project. Detects stack, conventions, testing capabilities, and bootstraps the active persistence backend."
		boundaries = []string{
			"Do NOT implement anything in init phase; only initialize context",
			"Do NOT make irreversible changes; init must be safe to rerun",
			"Do NOT claim runtime or subagent behavior",
		}
	case agentpack.PhaseExplore:
		title = "SDD Explore"
		triggers = "sdd explore, sdd-explore, investigar"
		summary = "Explore and investigate ideas before committing to a change. Trigger: When the orchestrator launches you to think through a feature, investigate the codebase, or clarify requirements."
		boundaries = []string{
			"Explore is investigation only; do NOT write specs or implementation",
			"Do NOT make changes to the codebase",
			"Do NOT commit anything; findings are temporary until proposal is approved",
		}
	case agentpack.PhaseProposal:
		title = "SDD Propose"
		triggers = "sdd propose, sdd-propose, proposal, propuesta"
		summary = "Create a change proposal with intent, scope, and approach. Trigger: When the orchestrator launches you to create or update a proposal for a change."
		boundaries = []string{
			"Proposal is intent and approach only; do NOT write detailed specs",
			"Do NOT implement anything; proposal must be approved before spec",
			"Do NOT skip explore phase; explore informs proposal",
		}
	case agentpack.PhaseSpec:
		title = "SDD Spec"
		triggers = "sdd spec, sdd-spec, especificacion"
		summary = "Write specifications with requirements and scenarios (delta specs for changes). Trigger: When the orchestrator launches you to write or update specs for a change."
		boundaries = []string{
			"Specs define WHAT, not HOW; design covers implementation approach",
			"Do NOT implement anything in spec phase",
			"Do NOT reference specific files or functions; describe behavior generically",
		}
	case agentpack.PhaseDesign:
		title = "SDD Design"
		triggers = "sdd design, sdd-design, diseno"
		summary = "Create technical design document with architecture decisions and approach. Trigger: When the orchestrator launches you to write or update the technical design for a change."
		boundaries = []string{
			"Design defines HOW, not implementation details",
			"Do NOT write actual code in design phase",
			"Do NOT make architectural decisions that were not in scope",
		}
	case agentpack.PhaseTasks:
		title = "SDD Tasks"
		triggers = "sdd tasks, sdd-tasks"
		summary = "Break down a change into an implementation task checklist. Trigger: When the orchestrator launches you to create or update the task breakdown for a change."
		boundaries = []string{
			"Tasks define implementation order, not actual implementation",
			"Do NOT implement tasks; apply phase handles implementation",
			"Do NOT create too many tasks in one slice; keep slices small",
		}
	case agentpack.PhaseApply:
		title = "SDD Apply"
		triggers = "sdd apply, sdd-apply, implementar"
		summary = "Implement tasks from the change, writing actual code following the specs and design. Trigger: When the orchestrator launches you to implement one or more tasks from a change."
		boundaries = []string{
			"Apply implements ONE bounded slice, not the entire change",
			"Do NOT skip specs or design; implementation must follow approved documents",
			"Do NOT run broad tests during apply; verify phase handles validation",
		}
	case agentpack.PhaseVerify:
		title = "SDD Verify"
		triggers = "sdd verify, sdd-verify, verificar"
		summary = "Validate that implementation matches specs, design, and tasks. Trigger: When the orchestrator launches you to verify a completed (or partially completed) change."
		boundaries = []string{
			"Verify validates implementation, does not implement",
			"Do NOT write implementation code in verify phase",
			"Do NOT skip apply phase; verify builds on completed implementation",
		}
	case agentpack.PhaseArchive:
		title = "SDD Archive"
		triggers = "sdd archive, sdd-archive, archivar"
		summary = "Sync delta specs to main specs and archive a completed change. Trigger: When the orchestrator launches you to archive a change after implementation and verification."
		boundaries = []string{
			"Archive is final phase; do NOT archive until all prior phases complete",
			"Do NOT modify implementation after archive",
			"Do NOT skip verify phase; archive builds on verified implementation",
		}
	default:
		title = "SDD Phase"
		triggers = string(phase)
		summary = "Bounded SDD phase for OpenCode/Lore workflows."
		boundaries = []string{"Do NOT make changes outside the approved phase scope"}
	}

	var boundariesLines string
	for _, b := range boundaries {
		boundariesLines += "- " + b + "\n"
	}

	frontmatter := strings.Join([]string{
		"---",
		"name: " + title,
		"description: >",
		"  " + summary,
		"  Trigger: When user says '" + triggers + "'.",
		"license: MIT",
		"metadata:",
		"  author: gentleman-programming",
		"  version: \"1.0\"",
		"---",
		"",
		"# " + title,
		"",
		"## Summary",
		"",
		summary,
		"",
		"## Triggers",
		"",
		triggers,
		"",
		"## Key Boundaries (OpenCode/Lore Bounded)",
		"",
		boundariesLines,
		"## Prompt Asset Note",
		"",
		"This file is an inert install-time prompt asset for OpenCode/Lore bounded SDD",
		"workflows. It is not runtime-wired and does not claim full orchestrator",
		"readiness. Commands are triggered by explicit user phrases. No runtime",
		"subagent, plugin, profile, bootstrap, or package-manager behavior is claimed.",
		"Prompts are staged assets only; no opencode.json wiring is active in this scope.",
	}, "\n") + "\n"

	return frontmatter
}
