package agentpack

import (
	"strings"
	"testing"
)

func TestDefaultExtendedSkillsReturnsThreeSkills(t *testing.T) {
	skills := DefaultExtendedSkills()
	if got, want := len(skills), 3; got != want {
		t.Fatalf("len(DefaultExtendedSkills()) = %d, want %d", got, want)
	}
	names := make([]string, 0, 3)
	for _, s := range skills {
		names = append(names, s.Name)
	}
	for _, want := range []string{"skill-creator", "skill-registry", "judgment-day"} {
		found := false
		for _, name := range names {
			if name == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("DefaultExtendedSkills() missing skill %q, got %v", want, names)
		}
	}
}

func TestSkillCreatorPortableHasRequiredFrontmatterFields(t *testing.T) {
	skill := SkillCreatorPortable()
	if skill.Name != "skill-creator" {
		t.Fatalf("SkillCreatorPortable().Name = %q, want skill-creator", skill.Name)
	}
	if skill.Description == "" {
		t.Fatal("SkillCreatorPortable().Description is empty")
	}
	if !strings.Contains(skill.Description, "skill") {
		t.Fatalf("SkillCreatorPortable().Description = %q, want trigger keyword", skill.Description)
	}
	if skill.Body == "" {
		t.Fatal("SkillCreatorPortable().Body is empty")
	}
}

func TestSkillRegistryPortableHasRequiredFrontmatterFields(t *testing.T) {
	skill := SkillRegistryPortable()
	if skill.Name != "skill-registry" {
		t.Fatalf("SkillRegistryPortable().Name = %q, want skill-registry", skill.Name)
	}
	if skill.Description == "" {
		t.Fatal("SkillRegistryPortable().Description is empty")
	}
	if !strings.Contains(skill.Description, "registry") {
		t.Fatalf("SkillRegistryPortable().Description = %q, want trigger keyword", skill.Description)
	}
	if skill.Body == "" {
		t.Fatal("SkillRegistryPortable().Body is empty")
	}
}

func TestJudgmentDayPortableIsHarnessAgnostic(t *testing.T) {
	skill := JudgmentDayPortable()
	if skill.Name != "judgment-day" {
		t.Fatalf("JudgmentDayPortable().Name = %q, want judgment-day", skill.Name)
	}
	if skill.Description == "" {
		t.Fatal("JudgmentDayPortable().Description is empty")
	}
	if skill.Body == "" {
		t.Fatal("JudgmentDayPortable().Body is empty")
	}

	// Assert harness-agnostic wording
	harnessAgnosticTerms := []string{
		"sub-agent",
		"delegation",
		"result retrieval",
		"harness-native",
		"parallel",
	}
	for _, term := range harnessAgnosticTerms {
		if !strings.Contains(strings.ToLower(skill.Body), term) {
			t.Errorf("JudgmentDayPortable().Body missing harness-agnostic term %q", term)
		}
	}

	// Assert no Pi-specific primitive names leak into installed content
	piSpecificTerms := []string{
		"lore_worker",
		"lore-search",
		"lore_get_observation",
		"contact_supervisor",
		"delegate(", // harness-agnostic "delegation" should suffice
	}
	for _, term := range piSpecificTerms {
		if strings.Contains(skill.Body, term) {
			t.Errorf("JudgmentDayPortable().Body contains Pi-specific term %q", term)
		}
	}

	// Assert .atl/skill-registry.md fallback is preserved
	if !strings.Contains(skill.Body, ".atl/skill-registry.md") {
		t.Error("JudgmentDayPortable().Body missing .atl/skill-registry.md fallback reference")
	}
}

func TestJudgmentDayPortableHasAllCriticalPatterns(t *testing.T) {
	skill := JudgmentDayPortable()
	patterns := []string{
		"Skill Resolution",
		"Parallel Blind Review",
		"Verdict Synthesis",
		"Warning Classification",
		"Fix and Re-judge",
		"Convergence Threshold",
		"Blocking Rules",
		"Self-Check",
	}
	for _, pattern := range patterns {
		if !strings.Contains(skill.Body, pattern) {
			t.Errorf("JudgmentDayPortable().Body missing critical pattern %q", pattern)
		}
	}
}

func TestExtendedSkillsProjectedForTargetResolvers(t *testing.T) {
	assets := OperationalAssets{}
	piSkills := assets.ExtendedSkills(PiSkillPathResolver())
	antiSkills := assets.ExtendedSkills(AntigravitySkillPathResolver())

	if len(piSkills) != 3 || len(antiSkills) != 3 {
		t.Fatalf("ExtendedSkills() returned wrong count: Pi=%d, Antigravity=%d, want 3 each", len(piSkills), len(antiSkills))
	}

	// Content should be identical regardless of resolver (no path substitution in body)
	for i := range piSkills {
		if piSkills[i].Body != antiSkills[i].Body {
			t.Errorf("ExtendedSkills body differs for skill %q between targets", piSkills[i].Name)
		}
	}
}

func TestManagedSkillFromExtendedSkill(t *testing.T) {
	skill := SkillCreatorPortable()
	managed := ManagedSkill{
		Name:        skill.Name,
		Description: skill.Description,
		Body:        skill.Body,
	}
	if managed.Name != skill.Name {
		t.Errorf("ManagedSkill.Name = %q, want %q", managed.Name, skill.Name)
	}
	if managed.Description != skill.Description {
		t.Errorf("ManagedSkill.Description mismatch")
	}
	if managed.Body != skill.Body {
		t.Errorf("ManagedSkill.Body mismatch")
	}
}

func TestExtendedSkillRefFormat(t *testing.T) {
	ref := ExtendedSkillRef("skill-creator")
	if ref.Name != "skills/skill-creator" {
		t.Errorf("ExtendedSkillRef().Name = %q, want skills/skill-creator", ref.Name)
	}
	if ref.Shared {
		t.Error("ExtendedSkillRef().Shared = true, want false")
	}
}

func TestSkillCreatorPortableHasRequiredSections(t *testing.T) {
	skill := SkillCreatorPortable()
	sections := []string{
		"When to Create a Skill",
		"Skill Structure",
		"SKILL.md Template",
		"Naming Conventions",
		"Frontmatter Fields",
		"Content Guidelines",
		"Checklist Before Creating",
	}
	for _, section := range sections {
		if !strings.Contains(skill.Body, section) {
			t.Errorf("SkillCreatorPortable().Body missing section %q", section)
		}
	}
}

func TestSkillRegistryPortableHasRequiredSections(t *testing.T) {
	skill := SkillRegistryPortable()
	sections := []string{
		"Purpose",
		"Source Priority",
		"What to Capture Per Skill",
		"Build Steps",
		"Output Shape",
		"Rules",
	}
	for _, section := range sections {
		if !strings.Contains(skill.Body, section) {
			t.Errorf("SkillRegistryPortable().Body missing section %q", section)
		}
	}
}
