package agentpack

import (
	"strings"
	"testing"
)

func TestBoundedReviewAssetsArePiOnlyAndPortableFallbackRemainsSelfContained(t *testing.T) {
	assets := OperationalAssets{}
	fallback := JudgmentDayPortable().Body
	for _, target := range []struct {
		name     string
		resolver SkillPathResolver
	}{
		{name: "pi", resolver: PiSkillPathResolver()},
		{name: "opencode", resolver: openCodePromptSkillPathResolver{}},
		{name: "codex", resolver: skillPathResolverFunc(func(SkillRef) string { return "~/.codex/skills" })},
		{name: "antigravity", resolver: AntigravitySkillPathResolver()},
	} {
		t.Run(target.name, func(t *testing.T) {
			var judgmentDay string
			for _, skill := range assets.ExtendedSkills(target.resolver) {
				if skill.Name == "judgment-day" {
					judgmentDay = skill.Body
				}
				if strings.Contains(skill.Name, "bounded-review") || strings.Contains(skill.Body, "lore.judgment-day.bounded-review") {
					t.Fatalf("%s extended skills leaked Pi-only bounded-review asset", target.name)
				}
			}
			if judgmentDay != fallback {
				t.Fatalf("%s fallback drifted from portable Judgment Day", target.name)
			}
			for _, forbidden := range []string{"native lifecycle", "native activation", "mandatory-rejudgment-v1", "judgment-review-result/v1", "~/.pi/", "lore/judgment-review"} {
				if strings.Contains(judgmentDay, forbidden) {
					t.Fatalf("%s fallback claimed Pi-native behavior or path %q", target.name, forbidden)
				}
			}
			if !strings.Contains(judgmentDay, "Every code-modifying Fix Agent action, including later rounds, MUST be followed by a fresh pair of two blind Judges before APPROVED.") {
				t.Fatalf("%s fallback lost universal fresh-pair correction", target.name)
			}
		})
	}
}

func TestBoundedReviewPairContainsNoSecretsOrMachinePaths(t *testing.T) {
	pair := BoundedReviewPair()
	for _, asset := range []BoundedReviewAsset{pair.Judge, pair.Fix} {
		for _, forbidden := range []string{"/Users/", "C:\\Users\\", "secret-token", "Bearer ", "{{", "<diff>", "<target>"} {
			if strings.Contains(asset.Body, forbidden) {
				t.Fatalf("%s asset contains sensitive or dynamic material %q", asset.Role, forbidden)
			}
		}
	}
}
