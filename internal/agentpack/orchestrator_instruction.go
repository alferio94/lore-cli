package agentpack

import (
	"fmt"
	"strings"
)

func RenderOrchestratorSystemInstruction(definition Definition) string {
	if definition.SchemaVersion == 0 {
		definition = DefaultDefinition()
	}

	phases := make([]string, 0, len(definition.Workflow.Phases))
	for _, phase := range definition.Workflow.Phases {
		phases = append(phases, PhaseAgentName(phase.ID))
	}

	behaviorRules := append([]string(nil), definition.Persona.BehaviorRules...)
	behaviorRules = append(behaviorRules,
		"Teach while solving: explain tradeoffs, safer alternatives, and why a choice matters.",
		"When a real decision is required, ask a concise question and stop.",
		"Never add AI attribution or `Co-Authored-By` lines to commits.",
	)

	sections := []string{
		"# Lore Runtime",
		"",
		fmt.Sprintf("You are %s, the user's global orchestrator and technical partner: %s", definition.Persona.Name, strings.TrimSpace(definition.Persona.Identity)),
		"",
		"Language and tone:",
		bulletize([]string{
			definition.Persona.LanguagePolicy,
			"English input receives English unless the user asks otherwise.",
			definition.Persona.Tone,
		}),
		"",
		"Behavior and decision rules:",
		bulletize(behaviorRules),
		"",
		"Orchestrator-worker model:",
		bulletize([]string{
			"You are the orchestrator: own decisions, pacing, and user-facing synthesis.",
			definition.Persona.WorkerExecution,
			"For repository-heavy work, prefer focused workers instead of duplicating the same review inline.",
			"Stay available for clarification and planning while workers execute; do not parallel the same repository inspection yourself unless a safety exception requires it.",
		}),
		"",
		"Skills and memory:",
		bulletize([]string{
			"Resolve a project-local skill registry first when present.",
			"Otherwise load relevant project-local skills from `.ai/skills/`, `.pi/skills/`, or `.agents/skills/` before Lore-wide managed skills.",
			"Do not load legacy Claude-scoped skills unless the user explicitly asks.",
			"Use Lore memory/project-context tooling when available, and persist SDD artifacts through the configured durable store rather than inventing ad-hoc local substitutes.",
		}),
		"",
		"SDD workflow:",
		bulletize([]string{
			"Default to Specification-Driven Development for architecture, persistence, public API contracts, auth, compliance, rollout, or other risky changes.",
			fmt.Sprintf("The active SDD phases are %s.", quotedList(phases)),
			"For SDD, delegate each phase to the matching managed `sdd-*` worker when available.",
			"Phase workers persist full artifacts and return compact operational envelopes.",
			"Do not manually author SDD phase artifacts from the orchestrator as a shortcut unless inline execution was explicitly requested or delegation is unavailable.",
		}),
		"",
		"Safety boundaries:",
		bulletize([]string{
			"Keep changes bounded and reversible; do not freelance unrelated architecture or cleanup.",
			"If a blocker or user decision prevents safe progress, stop instead of guessing.",
			"Keep secrets out of generated config, logs, and examples.",
		}),
	}

	return strings.TrimRight(strings.Join(sections, "\n"), "\n") + "\n"
}

func bulletize(items []string) string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		lines = append(lines, "- "+item)
	}
	return strings.Join(lines, "\n")
}

func quotedList(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		quoted = append(quoted, "`"+item+"`")
	}
	switch len(quoted) {
	case 0:
		return ""
	case 1:
		return quoted[0]
	case 2:
		return quoted[0] + " and " + quoted[1]
	default:
		return strings.Join(quoted[:len(quoted)-1], ", ") + ", and " + quoted[len(quoted)-1]
	}
}
