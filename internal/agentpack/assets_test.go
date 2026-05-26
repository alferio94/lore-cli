package agentpack

import (
	"reflect"
	"strings"
	"testing"
)

func TestDefaultOperationalAssetsProjectsCurrentDefinition(t *testing.T) {
	assets := DefaultOperationalAssets()
	definition := assets.Definition()

	if assets.PackID != definition.PackID {
		t.Fatalf("PackID = %q, want %q", definition.PackID, assets.PackID)
	}
	if got, want := len(assets.Agents), len(definition.ManagedAgents); got != want {
		t.Fatalf("len(Agents) = %d, want %d projected managed agents", got, want)
	}
	if err := definition.Validate(); err != nil {
		t.Fatalf("Definition().Validate() error = %v, want nil", err)
	}

	applyAsset := findAgentAsset(t, assets.Agents, "sdd-apply")
	applyProjected := applyAsset.ManagedAgent(PiSkillPathResolver())
	if !reflect.DeepEqual(applyProjected, definition.ManagedAgents[7]) {
		t.Fatalf("projected sdd-apply managed agent drifted from Definition() projection\nprojected=%+v\ndefinition=%+v", applyProjected, definition.ManagedAgents[7])
	}
}

func TestCanonicalSkillRefsStayLogicalUntilProjected(t *testing.T) {
	assets := DefaultOperationalAssets()
	applyAsset := findAgentAsset(t, assets.Agents, "sdd-apply")

	if len(applyAsset.SkillPolicy.Refs) != 2 {
		t.Fatalf("len(SkillPolicy.Refs) = %d, want 2", len(applyAsset.SkillPolicy.Refs))
	}
	if got, want := applyAsset.SkillPolicy.Refs[0], Skill("sdd-apply"); got != want {
		t.Fatalf("SkillPolicy.Refs[0] = %+v, want %+v", got, want)
	}
	if got, want := applyAsset.SkillPolicy.Refs[1], SharedSkill("_shared/sdd-phase-common"); got != want {
		t.Fatalf("SkillPolicy.Refs[1] = %+v, want %+v", got, want)
	}

	assertNoRawPiPathLeakage(t, assets)

	piProjected := applyAsset.ManagedAgent(PiSkillPathResolver())
	for _, want := range []string{
		"~/.pi/agent/skills/sdd-apply/SKILL.md",
		"~/.pi/agent/skills/_shared/sdd-phase-common.md",
	} {
		if !strings.Contains(piProjected.Body, want) {
			t.Fatalf("Pi projected body = %q, want %q", piProjected.Body, want)
		}
	}
	if !reflect.DeepEqual(piProjected.SkillPolicy.Files, []string{
		"~/.pi/agent/skills/sdd-apply/SKILL.md",
		"~/.pi/agent/skills/_shared/sdd-phase-common.md",
	}) {
		t.Fatalf("Pi projected skill files = %v, want Pi paths", piProjected.SkillPolicy.Files)
	}

	antigravityProjected := applyAsset.ManagedAgent(AntigravitySkillPathResolver())
	for _, want := range []string{
		"~/.gemini/antigravity-cli/skills/sdd-apply/SKILL.md",
		"~/.gemini/antigravity-cli/skills/_shared/sdd-phase-common.md",
	} {
		if !strings.Contains(antigravityProjected.Body, want) {
			t.Fatalf("Antigravity projected body = %q, want %q", antigravityProjected.Body, want)
		}
	}
}

func TestSkillResolversMapRefsPerHarness(t *testing.T) {
	refs := []SkillRef{Skill("sdd-apply"), SharedSkill("_shared/sdd-phase-common")}

	if got, want := ResolveSkillPaths(PiSkillPathResolver(), refs), []string{
		"~/.pi/agent/skills/sdd-apply/SKILL.md",
		"~/.pi/agent/skills/_shared/sdd-phase-common.md",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveSkillPaths(Pi) = %v, want %v", got, want)
	}

	if got, want := ResolveSkillPaths(AntigravitySkillPathResolver(), refs), []string{
		"~/.gemini/antigravity-cli/skills/sdd-apply/SKILL.md",
		"~/.gemini/antigravity-cli/skills/_shared/sdd-phase-common.md",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveSkillPaths(Antigravity) = %v, want %v", got, want)
	}
}

func assertNoRawPiPathLeakage(t *testing.T, assets OperationalAssets) {
	t.Helper()
	for _, asset := range assets.Agents {
		if strings.Contains(asset.Body.Template, "~/.pi/agent/skills/") {
			t.Fatalf("agent %q Body.Template = %q, want logical skill placeholders instead of raw Pi paths", asset.Name, asset.Body.Template)
		}
		for _, ref := range asset.SkillPolicy.Refs {
			if strings.Contains(ref.Name, "~/.pi/agent/skills/") {
				t.Fatalf("agent %q SkillPolicy ref = %+v, want logical skill ids instead of raw Pi paths", asset.Name, ref)
			}
		}
	}
}

func findAgentAsset(t *testing.T, assets []AgentInstructionAsset, name string) AgentInstructionAsset {
	t.Helper()
	for _, asset := range assets {
		if asset.Name == name {
			return asset
		}
	}
	t.Fatalf("agent asset %q not found", name)
	return AgentInstructionAsset{}
}
