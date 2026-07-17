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
	if contains(guidance, "lore_memory_save") {
		t.Fatalf("LoreMCPGuidance incorrectly includes artifact-owner persistence: %s", guidance)
	}
	if persistence := bulletize(LoreArtifactPersistenceGuidance()); !contains(persistence, "lore_memory_save") {
		t.Fatalf("LoreArtifactPersistenceGuidance missing lore_memory_save: %s", persistence)
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

func TestSharedRoleContractFragmentsPreserveRuntimeAndSkillBoundaries(t *testing.T) {
	for _, want := range []string{"project-local skill registry", "Lore-wide managed skills", "legacy Claude-scoped skills"} {
		if !contains(bulletize(SkillResolutionGuidance()), want) {
			t.Fatalf("SkillResolutionGuidance missing %q", want)
		}
	}
	boundary := RepositoryMarkdownRuntimeBoundary()
	for _, want := range []string{"host runtime execution", "delegation", "retries", "artifact read-back", "interrupted-apply recovery", "not guarantees implemented here"} {
		if !contains(boundary, want) {
			t.Fatalf("RepositoryMarkdownRuntimeBoundary missing %q: %s", want, boundary)
		}
	}
	for _, prompt := range []string{
		RenderOrchestratorSystemInstruction(DefaultDefinition()),
		RenderLoreWorkerPrompt(PiSkillPathResolver()),
	} {
		if !contains(prompt, boundary) {
			t.Fatalf("role prompt missing runtime boundary: %s", prompt)
		}
	}
	prompt, err := RenderSDDPhasePrompt(PhaseApply, PiSkillPathResolver())
	if err != nil {
		t.Fatalf("RenderSDDPhasePrompt(apply) error = %v", err)
	}
	if !contains(prompt, boundary) {
		t.Fatalf("SDD prompt missing runtime boundary: %s", prompt)
	}
}

func TestRoleScopedPersistenceGuidance(t *testing.T) {
	for name, prompt := range map[string]string{
		"orchestrator":   RenderOrchestratorSystemInstruction(DefaultDefinition()),
		"generic worker": RenderLoreWorkerPrompt(PiSkillPathResolver()),
	} {
		if contains(prompt, "`lore_memory_save`") {
			t.Fatalf("%s prompt has an artifact-owner save duty: %s", name, prompt)
		}
	}
	for _, phase := range OrderedPhaseIDs() {
		prompt, err := RenderSDDPhasePrompt(phase, PiSkillPathResolver())
		if err != nil {
			t.Fatalf("RenderSDDPhasePrompt(%s) error = %v", phase, err)
		}
		for _, want := range []string{"Persist durable artifacts", "`lore_memory_save`", "configured artifact authority"} {
			if !contains(prompt, want) {
				t.Fatalf("SDD artifact owner %s missing %q: %s", phase, want, prompt)
			}
		}
	}
}

func TestSDDPromptsMandateResolvedSkillLoadingWithProjectPrecedence(t *testing.T) {
	for _, phase := range OrderedPhaseIDs() {
		piPrompt, err := RenderSDDPhasePrompt(phase, PiSkillPathResolver())
		if err != nil {
			t.Fatalf("RenderSDDPhasePrompt(%s) error = %v", phase, err)
		}
		openCodePrompt, err := RenderOpenCodeSDDPrompt(phase)
		if err != nil {
			t.Fatalf("RenderOpenCodeSDDPrompt(%s) error = %v", phase, err)
		}
		for target, prompt := range map[string]string{"pi": piPrompt, "opencode": openCodePrompt} {
			for _, want := range []string{"project-local before Lore-wide", "MUST load and follow", "phase skill"} {
				if !contains(prompt, want) {
					t.Fatalf("%s %s SDD prompt missing %q: %s", target, phase, want, prompt)
				}
			}
		}
	}
}

func TestSDDRenderersUseCanonicalProposalEnvelopeName(t *testing.T) {
	piPrompt, err := RenderSDDPhasePrompt(PhaseProposal, PiSkillPathResolver())
	if err != nil {
		t.Fatalf("RenderSDDPhasePrompt(proposal) error = %v", err)
	}
	if !contains(piPrompt, "set `phase` to `propose`") {
		t.Fatalf("Pi proposal prompt missing canonical envelope phase: %s", piPrompt)
	}

	openCodePrompt, err := RenderOpenCodeSDDPrompt(PhaseProposal)
	if err != nil {
		t.Fatalf("RenderOpenCodeSDDPrompt(proposal) error = %v", err)
	}
	for _, want := range []string{"# SDD propose Prompt for OpenCode", "native OpenCode `sdd-propose` subagent", "SDD phase: `propose`"} {
		if !contains(openCodePrompt, want) {
			t.Fatalf("OpenCode proposal prompt missing %q: %s", want, openCodePrompt)
		}
	}
}

func TestNativeHarnessProjectionsRemovePiRuntimeClaimsAndPreserveCanonicalEnvelope(t *testing.T) {
	assets := DefaultOperationalAssets().ManagedAgents(PiSkillPathResolver())
	var worker, apply string
	for _, asset := range assets {
		switch asset.Name {
		case RoleLoreWorker:
			worker = asset.Body
		case PhaseAgentName(PhaseApply):
			apply = asset.Body
		}
	}
	for _, harness := range []HarnessPrompt{HarnessCodex, HarnessAntigravity} {
		projectedWorker := ProjectNativeHarnessManagedAgentPrompt(harness, worker)
		for _, want := range []string{"You are the canonical Lore repository worker.", "exactly these keys: `status`, `summary`, `artifacts`, `files`, `validations`, `risks`, `next_step`, `continuation`, `question`, `options`, `skill_resolution`.", "`completed` | `needs_user_input` | `failed`", "Resolve project-local standards first.", "lore_memory_get"} {
			if !contains(projectedWorker, want) {
				t.Fatalf("%s worker projection missing %q: %s", harness, want, projectedWorker)
			}
		}
		projectedSDD := ProjectNativeHarnessManagedAgentPrompt(harness, apply)
		for _, want := range []string{"You execute the SDD apply phase.", "set `phase` to `apply`", "`status`, `phase`, `summary`, `artifacts`, `files`, `validations`, `risks`, `next_step`, `continuation`, `question`, `options`, `skill_resolution`", "MUST load and follow", "lore_memory_get"} {
			if !contains(projectedSDD, want) {
				t.Fatalf("%s SDD projection missing %q: %s", harness, want, projectedSDD)
			}
		}
		for _, projected := range []string{projectedWorker, projectedSDD} {
			for _, forbidden := range []string{"lore-pi-runtime", "Pi Lore delegation adapter contract", "runtime injects a response contract"} {
				if contains(projected, forbidden) {
					t.Fatalf("%s projection contains Pi-only runtime claim %q: %s", harness, forbidden, projected)
				}
			}
		}
	}
	if !contains(worker, "Delegation is provided by the `lore-pi-runtime` package") || !contains(apply, "This is the Pi Lore delegation adapter contract") {
		t.Fatal("Pi canonical managed-agent contracts lost their Pi-specific runtime semantics")
	}
}

func TestRenderOpenCodeSDDPromptUsesCanonicalEnvelope(t *testing.T) {
	prompt, err := RenderOpenCodeSDDPrompt(PhaseApply)
	if err != nil {
		t.Fatalf("RenderOpenCodeSDDPrompt(apply) error = %v", err)
	}
	for _, want := range []string{"native OpenCode", "SDD phase: `apply`", "`next_step`", "OpenCode owns native agent execution", "lore_memory_search", "Final compact JSON envelope"} {
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
