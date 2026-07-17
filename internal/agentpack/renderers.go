package agentpack

import (
	"fmt"
	"strings"
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
		fmt.Sprintf("# SDD %s Prompt for OpenCode", PhaseEnvelopeName(phase)),
		"",
		fmt.Sprintf("You execute the SDD `%s` phase as the native OpenCode `%s` subagent. SDD phase: `%s`.", PhaseEnvelopeName(phase), PhaseAgentName(phase), PhaseEnvelopeName(phase)),
		"",
		"## Phase identity",
		fmt.Sprintf("- Agent identity: `%s`.", PhaseAgentName(phase)),
		fmt.Sprintf("- SDD phase: `%s` only.", PhaseEnvelopeName(phase)),
		"",
		"## SDD graph",
		"Use the SDD graph: `" + SDDDependencyGraph() + "`.",
		"Persist the full phase artifact before returning.",
		"",
		"## Final compact JSON envelope",
		"Return the canonical compact SDD JSON envelope below.",
		"",
		"## Canonical SDD phase contract",
		strings.TrimRight(body, "\n"),
	}, "\n")), nil
}

// ProjectNativeHarnessManagedAgentPrompt projects the canonical Pi-authored
// managed-agent body for harnesses that do not consume Pi runtime contracts.
// It preserves the portable role and envelope obligations while removing
// Pi-runtime ownership and injected-contract assertions.
func ProjectNativeHarnessManagedAgentPrompt(harness HarnessPrompt, canonical string) string {
	switch harness {
	case HarnessCodex, HarnessAntigravity:
		return projectNativeHarnessContract(canonical)
	default:
		return ensureTrailingNewline(canonical)
	}
}

func projectNativeHarnessContract(canonical string) string {
	contract := strings.TrimRight(canonical, "\n")
	contract = replaceMarkdownSection(contract, "## Response contract (Pi Lore delegation adapter contract)", strings.Join([]string{
		"## Final compact JSON envelope",
		"Return ONLY one compact JSON object with exactly these keys: " + EnvelopeFieldList(WorkerEnvelopeFields) + ".",
		"- `status`: `completed` | `needs_user_input` | `failed` (final only; Do not use `running`).",
		"- `summary`: one compact operational line, <= 280 chars.",
		"- `artifacts`, `files`, `validations`, `risks`, and `options`: string arrays.",
		"- `next_step`, `continuation`, and `question`: string or null.",
		"- `skill_resolution`: `injected` | `fallback-registry` | `fallback-path` | `none`.",
		"- Persist or reference long details in artifacts; do not embed long logs, diffs, or narratives in the envelope itself.",
		"- The canonical envelope is a repository handoff format; do not use `next`, `executive_summary`, or `next_recommended` as response-contract fields.",
	}, "\n"))
	contract = strings.ReplaceAll(contract, "This is the Pi Lore delegation adapter contract.", "This is the canonical compact SDD envelope.")
	contract = strings.ReplaceAll(contract, "## Runtime ownership\n"+RuntimeOwnershipGuidance()+"\n"+RepositoryMarkdownRuntimeBoundary(), "## Runtime boundary\n"+RepositoryMarkdownRuntimeBoundary())
	contract = strings.ReplaceAll(contract, RuntimeOwnershipGuidance()+"\n"+RepositoryMarkdownRuntimeBoundary(), RepositoryMarkdownRuntimeBoundary())
	return ensureTrailingNewline(contract)
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
	contract = strings.ReplaceAll(contract, "## Runtime ownership\n"+RuntimeOwnershipGuidance(), "## Native OpenCode runtime ownership\n"+OpenCodeRuntimeContractGuidance())
	return contract
}

func projectOpenCodeSDDContract(body string, phase PhaseID) string {
	phaseSkillPath := openCodePromptSkillPathResolver{}.ResolveSkillRef(Skill(PhaseAgentName(phase)))
	sharedSkillPath := openCodePromptSkillPathResolver{}.ResolveSkillRef(SharedSkill("_shared/sdd-phase-common"))
	body = strings.ReplaceAll(body,
		fmt.Sprintf("You execute the SDD %s phase.\nBefore work, MUST load and follow the resolved phase skill; resolve project-local before Lore-wide:\n- `%s`\n- `%s`", PhaseEnvelopeName(phase), phaseSkillPath, sharedSkillPath),
		fmt.Sprintf("SDD %s. Before work, MUST load and follow the resolved phase skill; resolve project-local before Lore-wide:\n- `%s`\n\nPhase obligations, Lore MCP rules, and the final envelope are inlined here; do not reference a shared phase-common file.", PhaseEnvelopeName(phase), phaseSkillPath),
	)
	body = strings.ReplaceAll(body,
		"This is the Pi Lore delegation adapter contract.",
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
