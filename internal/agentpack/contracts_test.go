package agentpack

import (
	"strings"
	"testing"
)

func TestCanonicalContractsCoverPhaseAliasLoreMCPAndStaleFields(t *testing.T) {
	if got := PhaseAgentName(PhaseProposal); got != "sdd-propose" {
		t.Fatalf("PhaseAgentName(proposal) = %q, want sdd-propose", got)
	}
	guidance := bulletize(LoreMCPGuidance())
	for _, want := range []string{"project_key", "lore_project_activity", "lore_memory_get", "do not pass query text"} {
		if !contains(guidance, want) {
			t.Fatalf("LoreMCPGuidance missing %q: %s", want, guidance)
		}
	}
	for _, repeated := range []string{"Use `lore_memory_search`", "accept exactly one of `project_id`"} {
		if strings.Count(guidance, repeated) != 1 {
			t.Fatalf("LoreMCPGuidance contains %q %d times; guidance=%s", repeated, strings.Count(guidance, repeated), guidance)
		}
	}
	fields := EnvelopeFieldList(FinalEnvelopeFields)
	for _, want := range []string{"`next_step`", "`continuation`", "`skill_resolution`"} {
		if !contains(fields, want) {
			t.Fatalf("FinalEnvelopeFields missing %q: %s", want, fields)
		}
	}
	stale := EnvelopeFieldList(StaleEnvelopeFields)
	for _, want := range []string{"`running`", "`next`", "`executive_summary`", "`next_recommended`"} {
		if !contains(stale, want) {
			t.Fatalf("StaleEnvelopeFields missing %q: %s", want, stale)
		}
	}
}

func TestRenderOpenCodeSDDPromptUsesCanonicalEnvelope(t *testing.T) {
	prompt, err := RenderOpenCodeSDDPrompt(PhaseApply)
	if err != nil {
		t.Fatalf("RenderOpenCodeSDDPrompt(apply) error = %v", err)
	}
	for _, want := range []string{"native OpenCode", "SDD phase: `apply`", "`next_step`", "OpenCode owns native agent execution", "lore_memory_search"} {
		if !contains(prompt, want) {
			t.Fatalf("OpenCode SDD prompt missing %q: %s", want, prompt)
		}
	}
	for _, forbidden := range []string{"lore-pi-runtime", "Pi Lore delegation adapter contract", "_shared/sdd-phase-common"} {
		if contains(prompt, forbidden) {
			t.Fatalf("OpenCode SDD prompt contains forbidden Pi/shared reference %q: %s", forbidden, prompt)
		}
	}
}
