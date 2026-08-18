package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func validInput() Input {
	return Input{Schema: SchemaV1, Compiler: CompilerV1, Pack: PackSnapshot{ID: "portable-pack", Version: "1"}, Target: TargetPi, Profiles: []Profile{{ID: "balanced", DefaultModel: "gpt-5", Roles: map[string]string{"worker": "gpt-5-mini"}}}, Candidates: []Candidate{{Tier: TierDefaults, ProfileID: "balanced", SourceKey: "defaults"}}}
}

func TestCompilerVersionExactSemverV1(t *testing.T) {
	for _, version := range []string{
		"canonical-capability-profile-compiler/v1.0.0",
		"canonical-capability-profile-compiler/v1.4294967295.4294967295",
	} {
		t.Run("valid_"+version, func(t *testing.T) {
			input := validInput()
			input.Compiler = version
			mustCompile(t, input)
		})
	}
	invalid := []struct{ name, version string }{
		{"empty_input", ""},
		{"prefix_only", "canonical-capability-profile-compiler/v"},
		{"empty_major", "canonical-capability-profile-compiler/v.0.0"},
		{"empty_minor", "canonical-capability-profile-compiler/v1..0"},
		{"empty_patch", "canonical-capability-profile-compiler/v1.0."},
		{"too_few", "canonical-capability-profile-compiler/v1.0"},
		{"too_many", "canonical-capability-profile-compiler/v1.0.0.0"},
		{"leading_zero_major", "canonical-capability-profile-compiler/v01.0.0"},
		{"leading_zero_minor", "canonical-capability-profile-compiler/v1.01.0"},
		{"leading_zero_patch", "canonical-capability-profile-compiler/v1.0.01"},
		{"overflow_major", "canonical-capability-profile-compiler/v4294967296.0.0"},
		{"overflow_minor", "canonical-capability-profile-compiler/v1.4294967296.0"},
		{"overflow_patch", "canonical-capability-profile-compiler/v1.0.4294967296"},
		{"prerelease", "canonical-capability-profile-compiler/v1.0.0-rc.1"},
		{"build", "canonical-capability-profile-compiler/v1.0.0+build"},
		{"trailing", "canonical-capability-profile-compiler/v1.0.0x"},
		{"whitespace", " canonical-capability-profile-compiler/v1.0.0 "},
		{"major_0", "canonical-capability-profile-compiler/v0.0.0"},
		{"major_2", "canonical-capability-profile-compiler/v2.0.0"},
		{"plus_sign", "canonical-capability-profile-compiler/v+1.0.0"},
		{"minus_sign", "canonical-capability-profile-compiler/v1.-1.0"},
		{"nondecimal", "canonical-capability-profile-compiler/v1.x.0"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			input := validInput()
			input.Compiler = tc.version
			_, err := Compile(input)
			requireCode(t, err, CodeInvalidCompilerVersion)
		})
	}
}

func TestKnownTargetValidation(t *testing.T) {
	want := []TargetID{TargetPi, TargetOpenCode, TargetCodex, TargetAntigravity, TargetClaude}
	if got := knownTargetIDs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("known targets = %#v, want %#v", got, want)
	}
	for _, target := range want[:4] {
		t.Run("supported_"+string(target), func(t *testing.T) {
			input := validInput()
			input.Target = target
			mustCompile(t, input)
		})
	}
	input := validInput()
	input.Target = TargetClaude
	_, err := Compile(input)
	requireCode(t, err, CodeTargetRoadmapUnsupported)

	input.Target = ""
	_, err = Compile(input)
	requireCode(t, err, CodeTargetRequired)
	for _, target := range []TargetID{"claude", "Claude", "open-code", "OPENCODE", "random-target"} {
		t.Run("unknown_"+string(target), func(t *testing.T) {
			input := validInput()
			input.Target = target
			_, err := Compile(input)
			requireCode(t, err, CodeUnknownTarget)
		})
	}
}

func TestCompileDoesNotMutateInput(t *testing.T) {
	input := canonicalInput()
	before := cloneForTest(input)
	mustCompile(t, input)
	if !reflect.DeepEqual(input, before) {
		t.Fatalf("success mutated input:\n got %#v\nwant %#v", input, before)
	}

	input.Candidates = append(input.Candidates, Candidate{Tier: TierDefaults, ProfileID: "balanced", SourceKey: input.Candidates[0].SourceKey})
	before = cloneForTest(input)
	_, err := Compile(input)
	requireCode(t, err, CodeDuplicateCandidateSource)
	if !reflect.DeepEqual(input, before) {
		t.Fatalf("post-normalization failure mutated input:\n got %#v\nwant %#v", input, before)
	}
}

func TestCredentialProviderAndSlotAffectIdentity(t *testing.T) {
	input := identityInput()
	baseline := mustCompile(t, input)

	reordered := cloneForTest(input)
	reordered.CredentialSlots[0], reordered.CredentialSlots[1] = reordered.CredentialSlots[1], reordered.CredentialSlots[0]
	requireSameIdentity(t, baseline, mustCompile(t, reordered))

	provider := cloneForTest(input)
	provider.CredentialSlots[0].Provider = "provider-changed"
	requireDifferentIdentity(t, baseline, mustCompile(t, provider))
	slot := cloneForTest(input)
	slot.CredentialSlots[0].Slot = "slot-changed"
	requireDifferentIdentity(t, baseline, mustCompile(t, slot))

	duplicate := CredentialRef{Provider: "duplicate-provider", Slot: "duplicate-slot"}
	for name, slots := range map[string][]CredentialRef{
		"duplicate_before_surrounding": {{Provider: "a", Slot: "one"}, duplicate, duplicate, {Provider: "z", Slot: "three"}},
		"duplicate_after_surrounding":  {{Provider: "z", Slot: "three"}, duplicate, duplicate, {Provider: "a", Slot: "one"}},
	} {
		t.Run(name, func(t *testing.T) {
			input := identityInput()
			input.CredentialSlots = slots
			_, err := Compile(input)
			requireCode(t, err, CodeDuplicateCredentialRef)
		})
	}
}

func TestCanonicalInputIdentityNormalizesSemanticReordering(t *testing.T) {
	input := canonicalInput()
	baseline := mustCompile(t, input)
	permutations := []struct {
		name   string
		mutate func(*Input)
	}{
		{"profiles", func(in *Input) { reverse(in.Profiles) }},
		{"candidates_equal_tier_source_keys", func(in *Input) { reverse(in.Candidates) }},
		{"overrides_equal_tier_multi_role_source_keys", func(in *Input) { reverse(in.RoleOverrides) }},
		{"requests_mixed_ids_duplicate_required", func(in *Input) { reverse(in.Requested) }},
		{"extensions_insertion", func(in *Input) {
			in.Extensions = make(map[string]string, 2)
			in.Extensions["z"] = "9"
			in.Extensions["a"] = "1"
		}},
		{"credentials", func(in *Input) { reverse(in.CredentialSlots) }},
	}
	for _, tc := range permutations {
		t.Run("reorders_"+tc.name, func(t *testing.T) {
			permuted := cloneForTest(input)
			tc.mutate(&permuted)
			requireSameIdentity(t, baseline, mustCompile(t, permuted))
		})
	}

	view := normalizedSnapshot(input)
	wantView := normalizedView{
		candidates: []Candidate{
			{Tier: TierCLI, ProfileID: "balanced", Location: "cli", SourceKey: "cli"},
			{Tier: TierLocal, ProfileID: "balanced", Location: "local-a", SourceKey: "a"},
			{Tier: TierLocal, ProfileID: "balanced", Location: "local-z", SourceKey: "z"},
		},
		overrides: []RoleOverride{
			{Role: "worker", Model: "cli-worker", Location: "cli", Tier: TierCLI, SourceKey: "cli-override"},
			{Role: "reviewer", Model: "local-reviewer", Location: "local", Tier: TierLocal, SourceKey: "reviewer-z"},
			{Role: "worker", Model: "local-worker", Location: "local", Tier: TierLocal, SourceKey: "worker-a"},
		},
		requests: []CapabilityRequest{
			{ID: CapabilityBoundedReview},
			{ID: CapabilityPortable},
			{ID: CapabilityPortable, Required: true},
		},
		credentials: []CredentialRef{
			{Slot: "one", Provider: "a"},
			{Slot: "one", Provider: "z"},
			{Slot: "two", Provider: "b"},
		},
	}
	if !reflect.DeepEqual(view, wantView) {
		t.Fatalf("normalized snapshot = %#v, want %#v", view, wantView)
	}

	t.Run("global_duplicate_candidate_across_tiers", func(t *testing.T) {
		duplicate := canonicalInput()
		duplicate.Candidates[1].SourceKey = duplicate.Candidates[0].SourceKey
		_, err := Compile(duplicate)
		requireCode(t, err, CodeDuplicateCandidateSource)
	})
	t.Run("global_duplicate_override_across_tiers", func(t *testing.T) {
		duplicate := canonicalInput()
		duplicate.RoleOverrides[1].SourceKey = duplicate.RoleOverrides[0].SourceKey
		_, err := Compile(duplicate)
		requireCode(t, err, CodeDuplicateOverrideSource)
	})

	behavior := identityInput()
	behaviorBaseline := mustCompile(t, behavior)
	changes := []struct {
		name   string
		mutate func(*Input)
	}{
		{"compiler", func(in *Input) { in.Compiler = "canonical-capability-profile-compiler/v1.0.1" }},
		{"pack", func(in *Input) { in.Pack.Version = "2" }},
		{"target", func(in *Input) { in.Target = TargetCodex }},
		{"profile", func(in *Input) { in.Profiles[0].Roles["reviewer"] = "profile-changed" }},
		{"candidate", func(in *Input) { in.Candidates[0].Location = "candidate-changed" }},
		{"override", func(in *Input) { in.RoleOverrides[0].Model = "override-changed" }},
		{"request", func(in *Input) { in.Requested[0].Required = true }},
		{"extension", func(in *Input) { in.Extensions["feature.example"] = "changed" }},
		{"credential_provider", func(in *Input) { in.CredentialSlots[0].Provider = "provider-changed" }},
		{"credential_slot", func(in *Input) { in.CredentialSlots[0].Slot = "slot-changed" }},
	}
	for _, tc := range changes {
		t.Run("behavior_change_"+tc.name, func(t *testing.T) {
			changed := cloneForTest(behavior)
			tc.mutate(&changed)
			requireDifferentIdentity(t, behaviorBaseline, mustCompile(t, changed))
		})
	}

	t.Run("independent_domain_digest", func(t *testing.T) {
		const exactInputDomain = "lore.compiler.input.v1\x00"
		const exactIRDomain = "lore.compiler.ir.v1\x00"
		if inputDomain != exactInputDomain || irDomain != exactIRDomain {
			t.Fatalf("domains = %q/%q", inputDomain, irDomain)
		}
		payload := struct {
			Schema uint16
			Name   string
		}{Schema: 1, Name: "fixed-payload"}
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		expected := func(domain string) string {
			digest := sha256.Sum256(append([]byte(domain), data...))
			return hex.EncodeToString(digest[:])
		}
		inputDigest, err := identity(inputDomain, payload)
		if err != nil || inputDigest != expected(exactInputDomain) {
			t.Fatalf("input digest = %q, error = %v, want %q", inputDigest, err, expected(exactInputDomain))
		}
		irDigest, err := identity(irDomain, payload)
		if err != nil || irDigest != expected(exactIRDomain) {
			t.Fatalf("IR digest = %q, error = %v, want %q", irDigest, err, expected(exactIRDomain))
		}
		if inputDigest == irDigest {
			t.Fatal("same payload collided across identity domains")
		}
	})
}

func canonicalInput() Input {
	input := validInput()
	input.Profiles = append(input.Profiles, Profile{ID: "other", DefaultModel: "other-model", Roles: map[string]string{"reviewer": "other-reviewer", "worker": "other-worker"}})
	input.Profiles[0].Roles["reviewer"] = "gpt-5"
	input.Candidates = []Candidate{
		{Tier: TierLocal, ProfileID: "balanced", Location: "local-z", SourceKey: "z"},
		{Tier: TierCLI, ProfileID: "balanced", Location: "cli", SourceKey: "cli"},
		{Tier: TierLocal, ProfileID: "balanced", Location: "local-a", SourceKey: "a"},
	}
	input.RoleOverrides = []RoleOverride{
		{Role: "worker", Model: "local-worker", Location: "local", Tier: TierLocal, SourceKey: "worker-a"},
		{Role: "worker", Model: "cli-worker", Location: "cli", Tier: TierCLI, SourceKey: "cli-override"},
		{Role: "reviewer", Model: "local-reviewer", Location: "local", Tier: TierLocal, SourceKey: "reviewer-z"},
	}
	input.Requested = []CapabilityRequest{{ID: CapabilityPortable, Required: true}, {ID: CapabilityBoundedReview}, {ID: CapabilityPortable}}
	input.Extensions = map[string]string{"a": "1", "z": "9"}
	input.CredentialSlots = []CredentialRef{{Slot: "two", Provider: "b"}, {Slot: "one", Provider: "z"}, {Slot: "one", Provider: "a"}}
	return input
}

func identityInput() Input {
	input := validInput()
	input.Profiles[0].Roles["reviewer"] = "gpt-5"
	input.Candidates[0].Location = "defaults"
	input.RoleOverrides = []RoleOverride{{Role: "worker", Model: "override", Location: "project", Tier: TierProject, SourceKey: "project-worker"}}
	input.Requested = []CapabilityRequest{{ID: CapabilityPortable}}
	input.Extensions = map[string]string{"feature.example": "enabled"}
	input.CredentialSlots = []CredentialRef{{Provider: "a", Slot: "one"}, {Provider: "b", Slot: "two"}}
	return input
}

func mustCompile(t *testing.T, input Input) ResolvedIR {
	t.Helper()
	ir, err := Compile(input)
	if err != nil {
		t.Fatal(err)
	}
	return ir
}

func requireCode(t *testing.T, err error, want ErrorCode) {
	t.Helper()
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %T %v, want *ValidationError with code %q", err, err, want)
	}
	if validation.Code() != want {
		t.Fatalf("error code = %q, want %q", validation.Code(), want)
	}
	if !errors.Is(err, want) {
		t.Fatalf("errors.Is(error, code %q) = false", want)
	}
}

func requireSameIdentity(t *testing.T, want, got ResolvedIR) {
	t.Helper()
	if got.InputID() != want.InputID() || got.IRID() != want.IRID() {
		t.Fatalf("identity = %q/%q, want %q/%q", got.InputID(), got.IRID(), want.InputID(), want.IRID())
	}
}

func requireDifferentIdentity(t *testing.T, baseline, changed ResolvedIR) {
	t.Helper()
	if changed.InputID() == baseline.InputID() || changed.IRID() == baseline.IRID() {
		t.Fatalf("identity did not change: %q/%q", changed.InputID(), changed.IRID())
	}
}

func cloneForTest(input Input) Input {
	result := input
	result.Profiles = append([]Profile(nil), input.Profiles...)
	for i := range result.Profiles {
		result.Profiles[i].Roles = make(map[string]string, len(input.Profiles[i].Roles))
		for role, model := range input.Profiles[i].Roles {
			result.Profiles[i].Roles[role] = model
		}
	}
	result.Candidates = append([]Candidate(nil), input.Candidates...)
	result.RoleOverrides = append([]RoleOverride(nil), input.RoleOverrides...)
	result.Requested = append([]CapabilityRequest(nil), input.Requested...)
	result.Extensions = make(map[string]string, len(input.Extensions))
	for key, value := range input.Extensions {
		result.Extensions[key] = value
	}
	result.CredentialSlots = append([]CredentialRef(nil), input.CredentialSlots...)
	return result
}

func reverse[T any](values []T) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}
