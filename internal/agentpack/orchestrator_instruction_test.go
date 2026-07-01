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

// TestRenderOrchestratorSystemInstructionTeachesCanonicalMemoryToolSelection
// verifies the harness-neutral canonical context/memory-tool guidance is present in
// the generated orchestrator instruction. The guidance teaches the contract preferred
// by the Lore Server MCP surface (`lore_project_activity`, `lore_project_context`,
// `lore_memory_search`, and `lore_memory_get`) and explicitly prefers it over the
// deprecated Pi-native `lore-memory.ts` extension, which has been removed and is not
// available in any install path.
func TestRenderOrchestratorSystemInstructionTeachesCanonicalMemoryToolSelection(t *testing.T) {
	instruction := RenderOrchestratorSystemInstruction(DefaultDefinition())

	for _, want := range []string{
		"Lore MCP context and memory tool selection (harness-neutral canonical guidance):",
		"Prefer MCP Lore Server tools over deprecated harness-local memory extensions",
		"Tool names may be exposed with harness-specific namespace prefixes",
		"use `lore_project_activity` first when available",
		"Use `lore_project_context` when broader recent project context is needed.",
		"Use `lore_memory_search` for targeted memory discovery.",
		"`lore_project_activity`, `lore_project_context`, and `lore_memory_search` accept exactly one of `project_id` (UUID) or `project_key` per call.",
		"Prefer `project_key` when a stable key is known",
		"OMIT full `content`. Do not assume `content` is present",
		"call `lore_memory_get` with the memory `id` plus exactly one project identity: `project_id` (UUID) or `project_key`",
		"Harness-local or harness-native fallback tools",
		"MUST only be used when MCP Lore Server tools are unavailable.",
		// The deprecated Pi-native extension must be named explicitly so future
		// readers know it was removed (not just omitted).
		"The Pi-native `lore-memory.ts` extension was removed and is not available in any install path",
	} {
		if !strings.Contains(instruction, want) {
			t.Fatalf("instruction missing canonical memory-tool snippet %q", want)
		}
	}
}
