package compiler

import (
	"reflect"
	"testing"
)

func TestW1BProfileWinnerRetainsCompleteCanonicalShadows(t *testing.T) {
	input := w1bInput()
	input.Candidates = []Candidate{
		{Tier: TierLocal, ProfileID: "balanced", Model: "local-default", Location: "local", SourceKey: "z-local"},
		{Tier: TierCLI, ProfileID: "focused", Model: "cli-default", Location: "cli", SourceKey: "cli"},
		{Tier: TierLocal, ProfileID: "other", Model: "cli-default", Location: "local", SourceKey: "a-local"},
		{Tier: TierDefaults, ProfileID: "balanced", Model: "default", Location: "defaults", SourceKey: "defaults"},
	}

	ir := mustCompile(t, input)
	want := []Candidate{
		{Tier: TierLocal, ProfileID: "other", Model: "cli-default", Location: "local", SourceKey: "a-local"},
		{Tier: TierLocal, ProfileID: "balanced", Model: "local-default", Location: "local", SourceKey: "z-local"},
		{Tier: TierDefaults, ProfileID: "balanced", Model: "default", Location: "defaults", SourceKey: "defaults"},
	}
	if got := ir.Profile(); !reflect.DeepEqual(got, ProfileResolution{
		Winner:   Candidate{Tier: TierCLI, ProfileID: "focused", Model: "cli-default", Location: "cli", SourceKey: "cli"},
		Shadowed: want,
	}) {
		t.Fatalf("profile = %#v, want complete winner and canonical shadows", got)
	}
}

func TestW1BProfileProvenanceMaterializesSyntheticDefaultAndSealsCopies(t *testing.T) {
	input := w1bInput()
	input.Candidates = []Candidate{
		{Tier: TierCLI, ProfileID: "focused", Location: "cli", SourceKey: "cli"},
		{Tier: TierDefaults, ProfileID: "balanced", Location: "defaults", SourceKey: "defaults"},
	}
	ir := mustCompile(t, input)
	want := ProfileResolution{
		Winner: Candidate{Tier: TierCLI, ProfileID: "focused", Model: "focused-default", Location: "cli", SourceKey: "cli"},
		Shadowed: []Candidate{
			{Tier: TierDefaults, ProfileID: "balanced", Model: "balanced-default", Location: "defaults", SourceKey: "defaults"},
		},
	}
	if got := ir.Profile(); !reflect.DeepEqual(got, want) {
		t.Fatalf("effective profile provenance = %#v, want %#v", got, want)
	}
	returned := ir.Profile()
	returned.Winner.Model = "mutated"
	returned.Shadowed[0].Model = "mutated"
	if got := ir.Profile(); !reflect.DeepEqual(got, want) {
		t.Fatalf("profile accessor leaked effective provenance mutation: %#v", got)
	}
}

func TestW1BRoleResolutionPreservesValuesSourcesAndSelectedProfileUniverse(t *testing.T) {
	input := w1bInput()
	input.Candidates = []Candidate{
		{Tier: TierLocal, ProfileID: "balanced", Model: "local-default", Location: "local", SourceKey: "local-choice"},
		{Tier: TierCLI, ProfileID: "focused", Model: "cli-default", Location: "cli", SourceKey: "cli-choice"},
		{Tier: TierDefaults, ProfileID: "balanced", Model: "default", Location: "defaults", SourceKey: "defaults"},
	}
	input.RoleOverrides = []RoleOverride{
		{Role: "worker", Model: "project-worker", Location: "project", Tier: TierProject, SourceKey: "project-worker"},
		{Role: "worker", Model: "local-worker", Location: "local", Tier: TierLocal, SourceKey: "local-worker"},
		{Role: "worker", Model: "local-worker", Location: "local-2", Tier: TierLocal, SourceKey: "local-worker-equal"},
	}

	ir := mustCompile(t, input)
	got := ir.Roles()
	want := []ResolvedModel{
		{
			Role: "default", Model: "cli-default",
			Winner: Candidate{Tier: TierCLI, ProfileID: "focused", Model: "cli-default", Location: "cli", SourceKey: "cli-choice"},
			Shadowed: []Candidate{
				{Tier: TierLocal, ProfileID: "balanced", Model: "local-default", Location: "local", SourceKey: "local-choice"},
				{Tier: TierDefaults, ProfileID: "balanced", Model: "default", Location: "defaults", SourceKey: "defaults"},
			},
		},
		{
			Role: "worker", Model: "focused-worker",
			Winner: Candidate{Tier: TierCLI, ProfileID: "focused", Model: "focused-worker", Location: "cli", SourceKey: "cli-choice"},
			Shadowed: []Candidate{
				{Tier: TierProject, Model: "project-worker", Location: "project", SourceKey: "project-worker"},
				{Tier: TierLocal, ProfileID: "balanced", Model: "balanced-worker", Location: "local", SourceKey: "local-choice"},
				{Tier: TierLocal, Model: "local-worker", Location: "local", SourceKey: "local-worker"},
				{Tier: TierLocal, Model: "local-worker", Location: "local-2", SourceKey: "local-worker-equal"},
				{Tier: TierDefaults, ProfileID: "balanced", Model: "balanced-worker", Location: "defaults", SourceKey: "defaults"},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("roles = %#v, want %#v", got, want)
	}
}

func TestW1BLocalCandidateModelBeatsDefaultOnlyWithoutHigherSelection(t *testing.T) {
	input := w1bInput()
	input.Candidates = []Candidate{
		{Tier: TierLocal, ProfileID: "balanced", Model: "local-default", Location: "local", SourceKey: "local"},
		{Tier: TierDefaults, ProfileID: "balanced", Model: "canonical-default", Location: "defaults", SourceKey: "defaults"},
	}
	if got := roleByName(t, mustCompile(t, input), "default"); got.Model != "local-default" || got.Winner.SourceKey != "local" {
		t.Fatalf("local default = %#v, want local candidate", got)
	}

	input.Candidates = append(input.Candidates, Candidate{Tier: TierGlobal, ProfileID: "focused", Model: "global-default", Location: "global", SourceKey: "global"})
	if got := roleByName(t, mustCompile(t, input), "default"); got.Model != "global-default" || got.Winner.SourceKey != "global" {
		t.Fatalf("higher default = %#v, want global candidate", got)
	}
}

func TestW1BPublicAccessorsAndInputAreDeeplySealed(t *testing.T) {
	input := w1bInput()
	input.Candidates = []Candidate{{Tier: TierCLI, ProfileID: "focused", Model: "cli", Location: "cli", SourceKey: "cli"}, {Tier: TierDefaults, ProfileID: "balanced", Model: "default", Location: "defaults", SourceKey: "defaults"}}
	input.RoleOverrides = []RoleOverride{{Role: "worker", Model: "worker", Location: "project", Tier: TierProject, SourceKey: "worker"}}
	input.Requested = []CapabilityRequest{{ID: CapabilityPortable}}
	before := cloneForTest(input)
	ir := mustCompile(t, input)
	inputID, irID := ir.InputID(), ir.IRID()
	if !reflect.DeepEqual(input, before) {
		t.Fatal("successful compile mutated nested input")
	}

	input.Profiles[0].Roles["worker"] = "input-mutated"
	input.Candidates[0].Model = "input-mutated"
	profile := ir.Profile()
	profile.Winner.Model = "returned-mutated"
	profile.Shadowed[0].SourceKey = "returned-mutated"
	roles := ir.Roles()
	worker := roleByName(t, ir, "worker")
	for i := range roles {
		if roles[i].Role == "worker" {
			roles[i].Winner.Model = "returned-mutated"
			roles[i].Shadowed[0].Model = "returned-mutated"
		}
	}
	roles = append(roles, ResolvedModel{Role: "invented"})
	capabilities := ir.Capabilities()
	capabilities[0].Reason = "returned-mutated"

	if got := ir.Profile(); got.Winner.Model != "cli" || got.Shadowed[0].SourceKey != "defaults" {
		t.Fatalf("profile accessor leaked mutation: %#v", got)
	}
	if got := roleByName(t, ir, "worker"); got.Winner != worker.Winner || got.Shadowed[0] != worker.Shadowed[0] {
		t.Fatalf("role accessor leaked mutation: %#v", got)
	}
	if got := ir.Capabilities(); got[0].Reason == "returned-mutated" {
		t.Fatalf("capability accessor leaked mutation: %#v", got)
	}
	if ir.InputID() != inputID || ir.IRID() != irID || !ir.Sealed() {
		t.Fatalf("accessor mutation changed sealed identity: %#v", ir)
	}

	bad := cloneForTest(before)
	bad.Candidates = append(bad.Candidates, Candidate{Tier: TierDefaults, ProfileID: "balanced", SourceKey: "cli"})
	badBefore := cloneForTest(bad)
	if _, err := Compile(bad); err == nil || !reflect.DeepEqual(bad, badBefore) {
		t.Fatal("failed compile did not preserve caller nested input")
	}
}

func TestW1BCandidateModelOnlyDefinesSyntheticDefault(t *testing.T) {
	input := w1bInput()
	input.Candidates = []Candidate{
		{Tier: TierCLI, ProfileID: "focused", Location: "cli", SourceKey: "empty"},
		{Tier: TierDefaults, ProfileID: "balanced", Model: "ignored-for-named-role", Location: "defaults", SourceKey: "defaults"},
	}
	ir := mustCompile(t, input)
	if got := roleByName(t, ir, "default"); got.Model != "focused-default" {
		t.Fatalf("empty candidate default = %q, want profile fallback", got.Model)
	}
	if got := roleByName(t, ir, "worker"); got.Model != "focused-worker" || got.Shadowed[0].Model != "balanced-worker" {
		t.Fatalf("named role used candidate model: %#v", got)
	}
}

func TestW1BCrossKindSameSourceKeyUsesOverrideTieBreak(t *testing.T) {
	input := w1bInput()
	input.Candidates = []Candidate{{Tier: TierCLI, ProfileID: "focused", Location: "cli", SourceKey: "shared"}}
	input.RoleOverrides = []RoleOverride{{Role: "worker", Model: "worker", Location: "cli", Tier: TierCLI, SourceKey: "shared"}}
	got := roleByName(t, mustCompile(t, input), "worker")
	want := ResolvedModel{
		Role:   "worker",
		Model:  "worker",
		Winner: Candidate{Tier: TierCLI, Model: "worker", Location: "cli", SourceKey: "shared"},
		Shadowed: []Candidate{
			{Tier: TierCLI, ProfileID: "focused", Model: "focused-worker", Location: "cli", SourceKey: "shared"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("same-source role = %#v, want %#v", got, want)
	}
}

func TestW1BLocalNamedOverrideBeatsCanonicalRoleOnlyWithoutHigherSource(t *testing.T) {
	input := w1bInput()
	input.Candidates = []Candidate{{Tier: TierDefaults, ProfileID: "balanced", Location: "defaults", SourceKey: "defaults"}}
	input.RoleOverrides = []RoleOverride{{Role: "worker", Model: "local-worker", Location: "local", Tier: TierLocal, SourceKey: "local"}}
	got := roleByName(t, mustCompile(t, input), "worker")
	if got.Model != "local-worker" || got.Winner.SourceKey != "local" || len(got.Shadowed) != 1 || got.Shadowed[0] != (Candidate{Tier: TierDefaults, ProfileID: "balanced", Model: "balanced-worker", Location: "defaults", SourceKey: "defaults"}) {
		t.Fatalf("local named role provenance = %#v", got)
	}
}

func w1bInput() Input {
	input := validInput()
	input.Profiles = []Profile{
		{ID: "balanced", DefaultModel: "balanced-default", Roles: map[string]string{"worker": "balanced-worker"}},
		{ID: "focused", DefaultModel: "focused-default", Roles: map[string]string{"worker": "focused-worker"}},
		{ID: "other", DefaultModel: "other-default", Roles: map[string]string{"leaked": "other-leaked"}},
	}
	input.Extensions = map[string]string{}
	return input
}

func roleByName(t *testing.T, ir ResolvedIR, name string) ResolvedModel {
	t.Helper()
	for _, role := range ir.Roles() {
		if role.Role == name {
			return role
		}
	}
	t.Fatalf("role %q not found in %#v", name, ir.Roles())
	return ResolvedModel{}
}
