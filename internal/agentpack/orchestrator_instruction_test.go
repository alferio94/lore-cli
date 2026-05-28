package agentpack

import (
	"strings"
	"testing"
)

func TestRenderOrchestratorSystemInstructionIncludesPortableLoreSemantics(t *testing.T) {
	instruction := RenderOrchestratorSystemInstruction(DefaultDefinition())

	for _, want := range []string{
		"# Lore Runtime",
		"You are Lore, the user's global orchestrator and technical partner",
		"Spanish input receives neutral Mexican Spanish",
		"You are the orchestrator: own decisions, pacing, and user-facing synthesis.",
		"Resolve a project-local skill registry first when present.",
		"Use Lore memory/project-context tooling when available",
		"The active SDD phases are `sdd-init`, `sdd-explore`, `sdd-propose`, `sdd-spec`, `sdd-design`, `sdd-tasks`, `sdd-apply`, `sdd-verify`, and `sdd-archive`.",
		"Do not manually author SDD phase artifacts from the orchestrator as a shortcut unless inline execution was explicitly requested or delegation is unavailable.",
		"If a blocker or user decision prevents safe progress, stop instead of guessing.",
	} {
		if !strings.Contains(instruction, want) {
			t.Fatalf("instruction = %q, want substring %q", instruction, want)
		}
	}
}

func TestRenderOrchestratorSystemInstructionFallsBackToDefaults(t *testing.T) {
	instruction := RenderOrchestratorSystemInstruction(Definition{})
	if !strings.Contains(instruction, "You are Lore") {
		t.Fatalf("instruction = %q, want default Lore instruction", instruction)
	}
}
