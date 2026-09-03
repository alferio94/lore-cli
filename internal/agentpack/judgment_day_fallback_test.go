package agentpack

import (
	"strings"
	"testing"
)

func TestJudgmentDayFallbackRequiresFreshBlindPairAfterEveryCodeChangingFix(t *testing.T) {
	body := JudgmentDayPortable().Body

	for _, want := range []string{
		"Every code-modifying Fix Agent action, including later rounds, MUST be followed by a fresh pair of two blind Judges before APPROVED.",
		"After every code-modifying Fix Agent action, immediately launch two fresh blind judges in parallel before any terminal judgment.",
		"MUST NOT declare JUDGMENT: APPROVED until the latest fresh Judge A and Judge B both return CLEAN after every code-modifying Fix Agent action",
		"If ANY JD had a code-modifying Fix Agent action, did a fresh two-Judge re-judgment complete afterward?",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("portable Judgment Day fallback missing mandatory rejudgment rule %q", want)
		}
	}

	for _, forbidden := range []string{
		"Only re-judge if there are confirmed CRITICALs.",
		"Fix inline, do NOT re-launch judges",
		"Fix inline if trivial. Do NOT re-judge",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("portable Judgment Day fallback retains contradictory shortcut %q", forbidden)
		}
	}
}

func TestJudgmentDayFallbackPreservesPortableSafetySemantics(t *testing.T) {
	body := JudgmentDayPortable().Body

	for _, want := range []string{
		"search for skill-registry observation, fallback to .atl/skill-registry.md",
		"Inject this block into BOTH Judge prompts AND the Fix Agent prompt (identical for all)",
		"Neither agent knows about the other — no cross-contamination",
		"Confirmed   -> found by BOTH agents",
		"Suspect A   -> found ONLY by Judge A",
		"Contradiction -> agents DISAGREE",
		"WARNING (real)",
		"WARNING (theoretical)",
		"ASK: 'Fix confirmed issues?' Only fix after user confirms.",
		"The Fix Agent is a separate delegation",
		"After 2 fix iterations, ASK the user before continuing",
		"MUST NOT save a session summary or tell the user 'done' until every JD reaches a terminal state",
		"Fix ONLY the confirmed issues listed above",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("portable Judgment Day fallback lost safety semantic %q", want)
		}
	}
}

func TestJudgmentDayFallbackIsSelfContainedAcrossHarnessProjections(t *testing.T) {
	assets := OperationalAssets{}
	fallback := JudgmentDayPortable().Body
	for _, projection := range []struct {
		name   string
		skills []ManagedSkill
	}{
		{name: "pi", skills: assets.ExtendedSkills(PiSkillPathResolver())},
		{name: "antigravity", skills: assets.ExtendedSkills(AntigravitySkillPathResolver())},
	} {
		var body string
		for _, skill := range projection.skills {
			if skill.Name == "judgment-day" {
				body = skill.Body
				break
			}
		}
		if body != fallback {
			t.Errorf("%s Judgment Day projection differs from self-contained fallback", projection.name)
		}
	}

	for _, forbidden := range []string{
		"native lifecycle",
		"native activation",
		"mandatory-rejudgment-v1",
		"judgment-review-result/v1",
		"~/.pi/",
		"lore_worker",
	} {
		if strings.Contains(fallback, forbidden) {
			t.Errorf("portable Judgment Day fallback leaks runtime-specific claim or path %q", forbidden)
		}
	}
}
