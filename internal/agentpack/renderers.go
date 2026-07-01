package agentpack

import (
	"fmt"
	"strings"
)

type HarnessPrompt string

const (
	HarnessOpenCode    HarnessPrompt = "opencode"
	HarnessPi          HarnessPrompt = "pi"
	HarnessCodex       HarnessPrompt = "codex"
	HarnessAntigravity HarnessPrompt = "antigravity"
)

func RenderLoreWorkerPrompt(resolver SkillPathResolver) string {
	assets := DefaultOperationalAssets().ManagedAgents(resolver)
	for _, agent := range assets {
		if agent.Name == RoleLoreWorker {
			return ensureTrailingNewline(agent.Body)
		}
	}
	return ""
}

func RenderSDDPhasePrompt(phase PhaseID, resolver SkillPathResolver) (string, error) {
	name := PhaseAgentName(phase)
	assets := DefaultOperationalAssets().ManagedAgents(resolver)
	for _, agent := range assets {
		if agent.Name == name {
			return ensureTrailingNewline(agent.Body), nil
		}
	}
	return "", fmt.Errorf("unknown SDD phase %q", phase)
}

func RenderOpenCodeOrchestratorPrompt(definition Definition) string {
	if definition.SchemaVersion == 0 {
		definition = DefaultDefinition()
	}
	return ensureTrailingNewline(strings.Join([]string{
		"# Lore Orchestrator Prompt for OpenCode",
		"",
		"You are Lore, the user's technical partner inside OpenCode. You are the primary orchestrator, not the repository worker.",
		"",
		"## Native OpenCode role",
		"- Use native OpenCode subagents for repository inspection, implementation, review, and SDD phases; do not emulate delegation with local runtime plugins.",
		"- Own user-facing synthesis, pacing, risk calls, and decisions. Workers own repository execution.",
		"- Choose the safest visible mode: Direct for tiny local fixes, Direct + LoreWorker for bounded repo work, SDD for architecture/persistence/API/auth/rollout or explicit `/sdd-*` work.",
		"",
		"## SDD orchestration",
		"Delegate each SDD phase to the matching native OpenCode subagent and use the canonical phase dependency graph.",
		"",
		"## Canonical Lore instruction",
		strings.TrimRight(RenderOrchestratorSystemInstruction(definition), "\n"),
	}, "\n"))
}

func RenderOpenCodeWorkerPrompt() string {
	workerContract := projectOpenCodeWorkerContract(RenderLoreWorkerPrompt(openCodePromptSkillPathResolver{}))
	return ensureTrailingNewline(strings.Join([]string{
		"# Lore Worker Prompt for OpenCode",
		"",
		"You are the canonical Lore repository worker running as a native OpenCode subagent.",
		"",
		"## Final compact JSON envelope",
		"Return the canonical compact worker JSON envelope described below.",
		"",
		"## Native OpenCode role",
		"- Execute the assigned repository task yourself; do not orchestrate, delegate, or launch other workers.",
		"- Inspect current status/diffs before editing. Stay bounded to the request and make the smallest safe change set.",
		"- Run focused validation for touched packages only unless the user asks for broader checks.",
		"- Use native OpenCode task/question behavior only when the primary agent invokes you; do not emulate Pi runtime delegation.",
		"",
		"## Canonical worker contract",
		strings.TrimRight(workerContract, "\n"),
	}, "\n"))
}

func RenderOpenCodeSDDPrompt(phase PhaseID) (string, error) {
	body, err := RenderSDDPhasePrompt(phase, openCodePromptSkillPathResolver{})
	if err != nil {
		return "", err
	}
	body = projectOpenCodeSDDContract(body, phase)
	return ensureTrailingNewline(strings.Join([]string{
		fmt.Sprintf("# SDD %s Prompt for OpenCode", renderSDDPhaseName(phase)),
		"",
		fmt.Sprintf("You execute the SDD `%s` phase as the native OpenCode `%s` subagent.", renderSDDPhaseName(phase), PhaseAgentName(phase)),
		"",
		"## Phase identity",
		fmt.Sprintf("- Agent identity: `%s`.", PhaseAgentName(phase)),
		fmt.Sprintf("- SDD phase: `%s` only.", renderSDDPhaseName(phase)),
		"",
		"## SDD graph",
		"Use the SDD graph: `" + SDDDependencyGraph() + "`.",
		"Persist the full phase artifact before returning.",
		"",
		"## Final compact JSON envelope",
		"Return the canonical compact SDD JSON envelope described below.",
		"",
		"## Native OpenCode role",
		"- Use OpenCode native task/question/subagent behavior only; do not emulate Pi delegation or plugin runtime behavior.",
		"- Preserve Lore MCP when available and follow the canonical SDD phase skill contract below.",
		"",
		"## Canonical SDD phase contract",
		strings.TrimRight(body, "\n"),
	}, "\n")), nil
}

func projectOpenCodeWorkerContract(canonical string) string {
	contract := strings.TrimRight(canonical, "\n")
	contract = replaceMarkdownSection(contract, "## Response contract (Pi Lore delegation adapter contract)", strings.Join([]string{
		"## Final compact JSON envelope",
		"Return ONLY one compact JSON object with exactly these keys: " + EnvelopeFieldList(WorkerEnvelopeFields) + ".",
		"- `status`: `completed` | `needs_user_input` | `failed` (final only; `running` is reserved for host-side transient state).",
		"- `summary`: one compact operational line, <= 280 chars.",
		"- `artifacts`: string array with <= 8 artifact references, each <= 160 chars.",
		"- `files`: string array with <= 16 file references touched in this work, each <= 200 chars.",
		"- `validations`: string array with <= 16 focused validation commands/observations, each <= 200 chars.",
		"- `risks`: string array with <= 5 compact items, each <= 180 chars.",
		"- `next_step`: string <= 160 chars or null.",
		"- `continuation`: string <= 240 chars or null.",
		"- `question`: string <= 220 chars or null.",
		"- `options`: string array with <= 5 compact choices.",
		"- `skill_resolution`: `injected` | `fallback-registry` | `fallback-path` | `none`.",
		"- This is the OpenCode native subagent handoff envelope. Do not use `next`, `executive_summary`, or `next_recommended` as response-contract fields.",
	}, "\n"))
	contract = strings.ReplaceAll(contract, "## Runtime ownership\n"+RuntimeOwnershipGuidance(), "## Native OpenCode runtime ownership\n"+OpenCodeRuntimeOwnershipGuidance())
	return contract
}

func projectOpenCodeSDDContract(body string, phase PhaseID) string {
	phaseSkillPath := openCodePromptSkillPathResolver{}.ResolveSkillRef(Skill(PhaseAgentName(phase)))
	sharedSkillPath := openCodePromptSkillPathResolver{}.ResolveSkillRef(SharedSkill("_shared/sdd-phase-common"))
	body = strings.ReplaceAll(body,
		fmt.Sprintf("Before substantial work, load and follow exactly:\n- `%s`\n- `%s`", phaseSkillPath, sharedSkillPath),
		fmt.Sprintf("Before substantial work, use this prompt as the self-contained OpenCode SDD contract and load the phase skill when skill loading is available:\n- `%s`\n\nThe phase obligations, Lore MCP rules, and final envelope are inlined here; do not reference a separate shared phase-common file.", phaseSkillPath),
	)
	body = strings.ReplaceAll(body,
		"This is the Pi Lore delegation adapter contract; Codex/Antigravity do not consume this exact JSON shape.",
		"This is the compact OpenCode SDD handoff envelope for native OpenCode subagents.",
	)
	body = strings.ReplaceAll(body, RuntimeOwnershipGuidance(), OpenCodeRuntimeOwnershipGuidance())
	return body
}

func replaceMarkdownSection(text, heading, replacement string) string {
	start := strings.Index(text, heading)
	if start == -1 {
		return text
	}
	searchFrom := start + len(heading)
	relativeNext := strings.Index(text[searchFrom:], "\n\n## ")
	if relativeNext == -1 {
		return strings.TrimRight(text[:start], "\n") + "\n\n" + replacement
	}
	end := searchFrom + relativeNext
	prefix := strings.TrimRight(text[:start], "\n")
	suffix := strings.TrimLeft(text[end:], "\n")
	if prefix == "" {
		return replacement + "\n\n" + suffix
	}
	return prefix + "\n\n" + replacement + "\n\n" + suffix
}

type openCodePromptSkillPathResolver struct{}

func (openCodePromptSkillPathResolver) ResolveSkillRef(ref SkillRef) string {
	if ref.Shared {
		return "~/.config/opencode/skills/" + ref.Name + ".md"
	}
	return "~/.config/opencode/skills/" + ref.Name + "/SKILL.md"
}

func ensureTrailingNewline(text string) string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return ""
	}
	return text + "\n"
}
