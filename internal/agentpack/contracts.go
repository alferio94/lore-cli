package agentpack

import "strings"

// FinalEnvelopeFields is the canonical compact worker/SDD handoff field set.
var FinalEnvelopeFields = []string{
	"status",
	"phase",
	"summary",
	"artifacts",
	"files",
	"validations",
	"risks",
	"next_step",
	"continuation",
	"question",
	"options",
	"skill_resolution",
}

// WorkerEnvelopeFields is the canonical non-SDD worker handoff field set.
var WorkerEnvelopeFields = []string{
	"status",
	"summary",
	"artifacts",
	"files",
	"validations",
	"risks",
	"next_step",
	"continuation",
	"question",
	"options",
	"skill_resolution",
}

var FinalStatusValues = []string{"completed", "needs_user_input", "failed"}

var StaleEnvelopeFields = []string{"running", "next", "executive_summary", "next_recommended"}

func SDDDependencyGraph() string {
	return "init -> explore -> propose -> [spec || design] -> tasks -> apply -> verify -> archive"
}

func LoreMCPGuidance() []string {
	return []string{
		"Prefer MCP Lore Server tools over deprecated harness-local memory extensions. The Pi-native `lore-memory.ts` extension was removed and is not available in any install path. Tool names may be exposed with harness-specific namespace prefixes; follow the Lore MCP descriptions for the active harness.",
		"For initial project orientation, use `lore_project_activity` first when available. It returns a bounded, metadata-first activity surface grouped by topic/change and omits full memory content by design.",
		"Use `lore_project_context` when broader recent project context is needed. It returns compact recent-memory DTOs and omits full memory content.",
		"Use `lore_memory_search` for targeted memory discovery. Search is filter-driven: pass `type`, `scope`, and `limit`; do not pass query text.",
		"`lore_project_activity`, `lore_project_context`, and `lore_memory_search` accept exactly one of `project_id` (UUID) or `project_key` per call. Prefer `project_key` when a stable key is known; only use `project_id` (UUID) when no key is available.",
		"Project activity, project context, and memory search return compact previews/metadata and OMIT full `content`. Do not assume `content` is present in those payloads.",
		"To load the full body, call `lore_memory_get` with the memory `id` plus exactly one project identity: `project_id` (UUID) or `project_key`. Prefer `project_key` when available and supported by the active MCP tool description.",
		"Harness-local or harness-native fallback tools may have older schemas and MUST only be used when MCP Lore Server tools are unavailable. Do not mix the two surfaces in the same workflow.",
	}
}

func RuntimeOwnershipGuidance() string {
	return "Delegation is provided by the `lore-pi-runtime` package (active Pi runtime). The legacy `lore-delegation.ts` Pi extension is currently disabled/blocked in `~/.pi/agent/extensions/`. The package runtime injects the canonical final response contract when the child launches; if the injected section is present, follow it as the authoritative contract."
}

func OpenCodeRuntimeOwnershipGuidance() string {
	return "OpenCode owns native agent execution through `agent` entries, `task`, and `question`; do not describe or depend on Pi runtime ownership or Pi-injected child contracts."
}

func EnvelopeFieldList(fields []string) string {
	quoted := make([]string, 0, len(fields))
	for _, field := range fields {
		quoted = append(quoted, "`"+field+"`")
	}
	return strings.Join(quoted, ", ")
}

func FinalStatusList() string {
	return strings.Join(FinalStatusValues, "`, `")
}
