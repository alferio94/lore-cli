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
// verifies the harness-neutral canonical memory-tool guidance is present in the
// generated orchestrator instruction. The guidance teaches the contract preferred
// by the lore-server MCP surface (`lore_memory_*` tools) and explicitly prefers
// it over the deprecated Pi-native `lore-memory.ts` extension, which has been
// removed and is not available in any install path.
func TestRenderOrchestratorSystemInstructionTeachesCanonicalMemoryToolSelection(t *testing.T) {
	instruction := RenderOrchestratorSystemInstruction(DefaultDefinition())

	for _, want := range []string{
		"Lore memory tool selection (harness-neutral canonical guidance):",
		"Prefer the MCP Lore Server tools (`lore_memory_*`) over any deprecated harness-local memory extension",
		"Use `lore_memory_search` for memory discovery.",
		"`lore_memory_search` accepts exactly one of `project_id` or `project_key` per call.",
		"Prefer `project_key` when a stable key is known",
		"`lore_memory_search` returns compact `content_preview` results and omits the full memory content.",
		"call `lore_memory_get` with `project_id` (UUID) plus the memory `id`",
		"`lore_memory_get` requires a `project_id`; passing a `project_key` is not a supported substitute.",
		"Harness-local or harness-native fallback tools",
		"MUST only be used when MCP Lore Server tools are unavailable.",
		// The deprecated Pi-native extension must be named explicitly so future
		// readers know it was removed (not just omitted).
		"the Pi-native `lore-memory.ts` extension, which is removed and not available in any install path",
	} {
		if !strings.Contains(instruction, want) {
			t.Fatalf("instruction missing canonical memory-tool snippet %q", want)
		}
	}
}
