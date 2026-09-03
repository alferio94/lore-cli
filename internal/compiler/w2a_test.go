package compiler

import (
	"errors"
	"reflect"
	"testing"
)

func TestW2AScopeAndPersistenceValidationCodes(t *testing.T) {
	validProject := ProjectID("project:alpha-01")
	for _, tc := range []struct {
		name string
		edit func(*Input)
		code ErrorCode
	}{
		{"invalid_candidate_scope", func(in *Input) { in.Candidates[0].Scope = ProfileScope("workspace") }, CodeInvalidProfileScope},
		{"project_candidate_missing_id", func(in *Input) { in.Candidates[0].Scope = ProfileScopeProject }, CodeInvalidProjectID},
		{"invalid_project_id", func(in *Input) {
			in.Candidates[0].Scope, in.Candidates[0].ProjectID = ProfileScopeProject, "project:bad/value"
		}, CodeInvalidProjectID},
		{"project_intent_missing_context", func(in *Input) {
			in.Persistence = RequestedProfilePersistence{Scope: ProfileScopeProject, ProfileID: "balanced"}
		}, CodeInvalidPersistenceIntent},
		{"project_intent_wrong_winner", func(in *Input) {
			in.ProjectID, in.Persistence = validProject, RequestedProfilePersistence{Scope: ProfileScopeProject, ProjectID: validProject, ProfileID: "other"}
		}, CodeInvalidPersistenceIntent},
		{"global_intent_has_project", func(in *Input) {
			in.Persistence = RequestedProfilePersistence{Scope: ProfileScopeGlobal, ProjectID: validProject, ProfileID: "balanced"}
		}, CodeInvalidPersistenceIntent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := validInput()
			tc.edit(&in)
			_, err := Compile(in)
			requireCode(t, err, tc.code)
			if !errors.Is(err, tc.code) {
				t.Fatalf("errors.Is(%v) = false", tc.code)
			}
			var typed *ValidationError
			if !errors.As(err, &typed) || typed.Code() != tc.code {
				t.Fatalf("typed code = %#v", typed)
			}
		})
	}
}

func TestW2AMatchingProjectPrecedenceExplainAndIdentity(t *testing.T) {
	input := w2aInput()
	input.ProjectID = "project:alpha-01"
	input.Candidates = []Candidate{
		{Tier: TierGlobal, Scope: ProfileScopeGlobal, ProfileID: "balanced", Location: "global", SourceKey: "global"},
		{Tier: TierProject, Scope: ProfileScopeProject, ProjectID: "project:alpha-01", ProfileID: "focused", Location: "project", SourceKey: "alpha"},
		{Tier: TierProject, Scope: ProfileScopeProject, ProjectID: "project:beta-02", ProfileID: "other", Location: "project", SourceKey: "beta"},
	}
	report, err := Explain(input)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Admitted || report.Profile().Winner.ProfileID != "focused" || len(report.Profile().Shadowed) != 1 || report.Profile().Shadowed[0].SourceKey != "global" {
		t.Fatalf("profile report = %#v", report.Profile())
	}
	wantEvents := []ExplainEvent{
		{Code: ExplainScopeMismatch, SourceKey: "beta", ProfileID: "other", Scope: ProfileScopeProject, ProjectID: "project:beta-02", Remediation: "select a candidate for the active project"},
		{Code: ExplainProfileWinner, SourceKey: "alpha", ProfileID: "focused", Scope: ProfileScopeProject, ProjectID: "project:alpha-01"},
		{Code: ExplainProfileShadow, SourceKey: "global", ProfileID: "balanced", Scope: ProfileScopeGlobal},
		{Code: ExplainRoleWinner, Role: "default", SourceKey: "alpha", ProfileID: "focused"},
		{Code: ExplainRoleShadow, Role: "default", SourceKey: "global", ProfileID: "balanced"},
		{Code: ExplainCapability, Capability: CapabilityBoundedReview, Remediation: "Pi bounded review is staged and inactive"},
		{Code: ExplainCapability, Capability: CapabilityPortable, Remediation: "portable compiler content is supported"},
		{Code: ExplainAdmission, Remediation: "admitted"},
	}
	if !reflect.DeepEqual(report.Events(), wantEvents) {
		t.Fatalf("events = %#v", report.Events())
	}
	alpha := mustCompile(t, input)
	beta := cloneForTest(input)
	beta.ProjectID = "project:beta-02"
	betaIR := mustCompile(t, beta)
	if betaIR.Profile().Winner.ProfileID != "other" {
		t.Fatalf("beta winner = %#v", betaIR.Profile())
	}
	requireDifferentIdentity(t, alpha, betaIR)
	input.Candidates = append(input.Candidates, Candidate{Tier: TierCLI, Scope: ProfileScopeGlobal, ProfileID: "balanced", Location: "cli", SourceKey: "cli"})
	if got := mustCompile(t, input).Profile().Winner; got.SourceKey != "cli" {
		t.Fatalf("CLI did not win: %#v", got)
	}
}

func TestW2ARejectedExplainAndImmutablePersistenceIntent(t *testing.T) {
	input := w2aInput()
	input.ProjectID = "project:alpha-01"
	input.Candidates = []Candidate{{Tier: TierProject, Scope: ProfileScopeProject, ProjectID: "project:beta-02", ProfileID: "focused", Location: "project", SourceKey: "beta"}}
	input.Extensions = map[string]string{"x:test": "safe"}
	for i := range input.Profiles {
		input.Profiles[i].Roles = map[string]string{}
	}
	input.RoleOverrides = []RoleOverride{{Role: "default", Model: "override", Tier: TierLocal, SourceKey: "override"}}
	input.Requested = []CapabilityRequest{{ID: CapabilityPortable}}
	input.CredentialSlots = []CredentialRef{{Provider: "vault", Slot: "slot"}}
	beforeRejected := cloneForTest(input)
	report, err := Explain(input)
	requireCode(t, err, CodeNoMatchingProfile)
	wantRejected := []ExplainEvent{
		{Code: ExplainScopeMismatch, SourceKey: "beta", ProfileID: "focused", Scope: ProfileScopeProject, ProjectID: "project:beta-02", Remediation: "select a candidate for the active project"},
		{Code: ExplainCapability, Capability: CapabilityBoundedReview, Remediation: "Pi bounded review is staged and inactive"},
		{Code: ExplainCapability, Capability: CapabilityPortable, Remediation: "portable compiler content is supported"},
		{Code: ExplainAdmission, ErrorCode: CodeNoMatchingProfile, Remediation: "no profile candidate matches the active project; select a global candidate or matching project"},
	}
	if report.Admitted || !reflect.DeepEqual(report.Events(), wantRejected) || len(report.Roles()) != 0 || report.Profile().Winner != (Candidate{}) || !reflect.DeepEqual(input, beforeRejected) {
		t.Fatalf("rejected report = %#v", report)
	}
	if _, err := Compile(input); err == nil {
		t.Fatal("Compile admitted mismatch-only input")
	}

	input = w2aInput()
	input.ProjectID = "project:alpha-01"
	input.Candidates = []Candidate{{Tier: TierGlobal, Scope: ProfileScopeGlobal, ProfileID: "balanced", SourceKey: "global"}, {Tier: TierProject, Scope: ProfileScopeProject, ProjectID: input.ProjectID, ProfileID: "focused", SourceKey: "project"}}
	input.Persistence = RequestedProfilePersistence{Scope: ProfileScopeProject, ProjectID: input.ProjectID, ProfileID: "focused"}
	input.Extensions = map[string]string{"x:test": "safe"}
	for i := range input.Profiles {
		input.Profiles[i].Roles = map[string]string{}
	}
	input.RoleOverrides = []RoleOverride{{Role: "default", Model: "override", Tier: TierLocal, SourceKey: "override"}}
	input.Requested = []CapabilityRequest{{ID: CapabilityPortable}}
	input.CredentialSlots = []CredentialRef{{Provider: "vault", Slot: "slot"}}
	before := cloneForTest(input)
	report, err = Explain(input)
	if err != nil || !report.Admitted {
		t.Fatalf("admitted report err=%v report=%#v", err, report)
	}
	intent, ok := report.ProfilePersistenceIntent()
	if !ok || intent.Scope() != ProfileScopeProject || intent.ProjectID() != input.ProjectID || intent.ProfileID() != "focused" {
		t.Fatalf("intent = %#v, %v", intent, ok)
	}
	if again, _ := report.ProfilePersistenceIntent(); again.ProjectID() != input.ProjectID || !reflect.DeepEqual(input, before) {
		t.Fatalf("intent or input mutation leaked: %#v", again)
	}
	ir := mustCompile(t, input)
	if got, ok := ir.ProfilePersistenceIntent(); !ok || got.ProjectID() != input.ProjectID || got.ProfileID() != "focused" {
		t.Fatalf("IR persistence intent = %#v, %v", got, ok)
	}
}

func TestW2ADeterministicMismatchEventsAndGlobalPersistence(t *testing.T) {
	input := w2aInput()
	input.ProjectID = "project:alpha-01"
	input.Candidates = []Candidate{
		{Tier: TierProject, Scope: ProfileScopeProject, ProjectID: "project:gamma-03", ProfileID: "other", SourceKey: "gamma"},
		{Tier: TierGlobal, Scope: ProfileScopeGlobal, ProfileID: "balanced", SourceKey: "global"},
		{Tier: TierProject, Scope: ProfileScopeProject, ProjectID: "project:beta-02", ProfileID: "focused", SourceKey: "beta"},
	}
	baseline, err := Explain(input)
	if err != nil {
		t.Fatal(err)
	}
	permuted := cloneForTest(input)
	permuted.Candidates[0], permuted.Candidates[2] = permuted.Candidates[2], permuted.Candidates[0]
	other, err := Explain(permuted)
	if err != nil || !reflect.DeepEqual(baseline.Events(), other.Events()) {
		t.Fatalf("permuted events differ: %#v %#v", baseline.Events(), other.Events())
	}
	wantMismatches := []ExplainEvent{
		{Code: ExplainScopeMismatch, SourceKey: "beta", ProfileID: "focused", Scope: ProfileScopeProject, ProjectID: "project:beta-02", Remediation: "select a candidate for the active project"},
		{Code: ExplainScopeMismatch, SourceKey: "gamma", ProfileID: "other", Scope: ProfileScopeProject, ProjectID: "project:gamma-03", Remediation: "select a candidate for the active project"},
	}
	if got := baseline.Events()[:2]; !reflect.DeepEqual(got, wantMismatches) {
		t.Fatalf("mismatches = %#v", got)
	}
	input.Persistence = RequestedProfilePersistence{Scope: ProfileScopeGlobal, ProfileID: "balanced"}
	ir := mustCompile(t, input)
	intent, ok := ir.ProfilePersistenceIntent()
	if !ok || intent.Scope() != ProfileScopeGlobal || intent.ProjectID() != "" || intent.ProfileID() != "balanced" {
		t.Fatalf("global intent = %#v, %v", intent, ok)
	}
}

func TestW2AExplainRejectedPreservesValidationPrecedenceAndPartialEvidence(t *testing.T) {
	input := w2aInput()
	input.Compiler = "broken"
	input.ProjectID = "project:alpha-01"
	input.Candidates = []Candidate{{Tier: TierProject, Scope: ProfileScopeProject, ProjectID: "project:beta-02", ProfileID: "focused", SourceKey: "beta"}}
	report, explainErr := Explain(input)
	_, compileErr := Compile(input)
	requireCode(t, compileErr, CodeInvalidCompilerVersion)
	requireCode(t, explainErr, CodeInvalidCompilerVersion)
	if report.Admitted || len(report.Capabilities()) != 0 || report.Events()[len(report.Events())-1].ErrorCode != CodeInvalidCompilerVersion {
		t.Fatalf("invalid compiler report = %#v", report)
	}

	for _, tc := range []struct {
		name, remediation string
		request           CapabilityRequest
		code              ErrorCode
	}{
		{"unsupported", "requested capability is unsupported for target; select portable-content or a supported target", CapabilityRequest{ID: CapabilityBoundedReview}, CodeCapabilityUnsupported},
		{"unknown", "requested capability is unknown; check spelling and compiler version", CapabilityRequest{ID: "future"}, CodeCapabilityUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := w2aInput()
			in.Target = TargetOpenCode
			in.Requested = []CapabilityRequest{tc.request}
			report, err := Explain(in)
			requireCode(t, err, tc.code)
			wantCapabilities := 2
			if tc.code == CodeCapabilityUnknown {
				wantCapabilities = 3
			}
			if report.Admitted || report.Profile().Winner.ProfileID != "balanced" || len(report.Roles()) == 0 || len(report.Capabilities()) != wantCapabilities {
				t.Fatalf("partial report = %#v", report)
			}
			admission := report.Events()[len(report.Events())-1]
			if admission.Code != ExplainAdmission || admission.ErrorCode != tc.code || admission.Remediation != tc.remediation {
				t.Fatalf("admission = %#v", admission)
			}
			if tc.code == CodeCapabilityUnknown {
				want := []ExplainEvent{
					{Code: ExplainProfileWinner, SourceKey: "defaults", ProfileID: "balanced"},
					{Code: ExplainRoleWinner, Role: "default", SourceKey: "defaults", ProfileID: "balanced"},
					{Code: ExplainCapability, Capability: CapabilityBoundedReview, Remediation: "bounded review is currently Pi-only"},
					{Code: ExplainCapability, Capability: "future", Remediation: "unknown capability; check spelling and compiler version"},
					{Code: ExplainCapability, Capability: CapabilityPortable, Remediation: "portable compiler content is supported"},
					{Code: ExplainAdmission, ErrorCode: CodeCapabilityUnknown, Remediation: tc.remediation},
				}
				if !reflect.DeepEqual(report.Events(), want) {
					t.Fatalf("unknown events = %#v", report.Events())
				}
			}
			if ir, err := Compile(in); err == nil || ir.Admitted {
				t.Fatalf("Compile result = %#v, %v", ir, err)
			}
		})
	}
}

func TestW2AExplainInvalidPersistenceExactEventsIdentityAndCopies(t *testing.T) {
	in := w2aInput()
	in.ProjectID = "project:alpha-01"
	in.Candidates = []Candidate{{Tier: TierGlobal, Scope: ProfileScopeGlobal, ProfileID: "balanced", SourceKey: "global"}, {Tier: TierProject, Scope: ProfileScopeProject, ProjectID: in.ProjectID, ProfileID: "focused", SourceKey: "project"}}
	in.Persistence = RequestedProfilePersistence{Scope: ProfileScopeProject, ProjectID: in.ProjectID, ProfileID: "balanced"}
	in.Extensions = map[string]string{"x:test": "safe"}
	for i := range in.Profiles {
		in.Profiles[i].Roles = map[string]string{}
	}
	before := cloneForTest(in)
	report, err := Explain(in)
	requireCode(t, err, CodeInvalidPersistenceIntent)
	want := []ExplainEvent{
		{Code: ExplainProfileWinner, SourceKey: "project", ProfileID: "focused", Scope: ProfileScopeProject, ProjectID: in.ProjectID},
		{Code: ExplainProfileShadow, SourceKey: "global", ProfileID: "balanced", Scope: ProfileScopeGlobal},
		{Code: ExplainRoleWinner, Role: "default", SourceKey: "project", ProfileID: "focused"},
		{Code: ExplainRoleShadow, Role: "default", SourceKey: "global", ProfileID: "balanced"},
		{Code: ExplainCapability, Capability: CapabilityBoundedReview, Remediation: "Pi bounded review is staged and inactive"},
		{Code: ExplainCapability, Capability: CapabilityPortable, Remediation: "portable compiler content is supported"},
		{Code: ExplainAdmission, ErrorCode: CodeInvalidPersistenceIntent, Remediation: "persistence intent must select the resolved profile"},
	}
	if !reflect.DeepEqual(report.Events(), want) || !reflect.DeepEqual(in, before) {
		t.Fatalf("events/input = %#v %#v", report.Events(), in)
	}
	if intent, ok := report.ProfilePersistenceIntent(); ok || intent != (ProfilePersistenceIntent{}) {
		t.Fatalf("invalid intent = %#v, %v", intent, ok)
	}

	accepted := cloneForTest(in)
	accepted.Persistence.ProfileID = "focused"
	base := mustCompile(t, accepted)
	changedScope := cloneForTest(accepted)
	changedScope.Persistence = RequestedProfilePersistence{Scope: ProfileScopeGlobal, ProfileID: "focused"}
	changedProject := cloneForTest(accepted)
	changedProject.ProjectID, changedProject.Candidates[1].ProjectID, changedProject.Persistence.ProjectID = "project:beta-02", "project:beta-02", "project:beta-02"
	requireDifferentIdentity(t, base, mustCompile(t, changedScope))
	requireDifferentIdentity(t, base, mustCompile(t, changedProject))
	report, _ = Explain(accepted)
	returnedProfile := report.Profile()
	returnedProfile.Shadowed[0].ProfileID = "mutated"
	report.Events()[0].ProfileID = "mutated"
	report.Roles()[0].Winner.Model = "mutated"
	report.Capabilities()[0].Reason = "mutated"
	fresh, _ := Explain(accepted)
	if fresh.Profile().Shadowed[0].ProfileID == "mutated" || fresh.Events()[0].ProfileID == "mutated" || fresh.Roles()[0].Winner.Model == "mutated" || fresh.Capabilities()[0].Reason == "mutated" {
		t.Fatalf("report accessor leaked: %#v", fresh)
	}
	ir := mustCompile(t, accepted)
	irProfile, irRoles, irCapabilities := ir.Profile(), ir.Roles(), ir.Capabilities()
	irProfile.Shadowed[0].ProfileID, irRoles[0].Winner.Model, irCapabilities[0].Reason = "mutated", "mutated", "mutated"
	freshIR := mustCompile(t, accepted)
	if freshIR.Profile().Shadowed[0].ProfileID == "mutated" || freshIR.Roles()[0].Winner.Model == "mutated" || freshIR.Capabilities()[0].Reason == "mutated" {
		t.Fatalf("IR accessor leaked: %#v", freshIR)
	}
}

func TestW2ASameValueAccessorsAreDefensiveAndRejectedSequenceIsExact(t *testing.T) {
	in := w2aInput()
	in.ProjectID = "project:alpha-01"
	in.Target = TargetOpenCode
	in.Candidates = []Candidate{
		{Tier: TierGlobal, Scope: ProfileScopeGlobal, ProfileID: "balanced", SourceKey: "global"},
		{Tier: TierProject, Scope: ProfileScopeProject, ProjectID: "project:beta-02", ProfileID: "other", SourceKey: "beta"},
	}
	in.Requested = []CapabilityRequest{{ID: CapabilityBoundedReview}}
	in.Extensions = map[string]string{"x:test": "safe"}
	for i := range in.Profiles {
		in.Profiles[i].Roles = map[string]string{}
	}
	before := cloneForTest(in)
	report, err := Explain(in)
	requireCode(t, err, CodeCapabilityUnsupported)
	want := []ExplainEvent{
		{Code: ExplainScopeMismatch, SourceKey: "beta", ProfileID: "other", Scope: ProfileScopeProject, ProjectID: "project:beta-02", Remediation: "select a candidate for the active project"},
		{Code: ExplainProfileWinner, SourceKey: "global", ProfileID: "balanced", Scope: ProfileScopeGlobal},
		{Code: ExplainRoleWinner, Role: "default", SourceKey: "global", ProfileID: "balanced"},
		{Code: ExplainCapability, Capability: CapabilityBoundedReview, Remediation: "bounded review is currently Pi-only"},
		{Code: ExplainCapability, Capability: CapabilityPortable, Remediation: "portable compiler content is supported"},
		{Code: ExplainAdmission, ErrorCode: CodeCapabilityUnsupported, Remediation: "requested capability is unsupported for target; select portable-content or a supported target"},
	}
	if !reflect.DeepEqual(report.Events(), want) || !reflect.DeepEqual(in, before) {
		t.Fatalf("rejected report/input = %#v %#v", report.Events(), in)
	}
	ir, compileErr := Compile(in)
	requireCode(t, compileErr, CodeCapabilityUnsupported)
	if !reflect.DeepEqual(ir, ResolvedIR{}) {
		t.Fatalf("rejected IR = %#v", ir)
	}

	profile, events, roles, capabilities := report.Profile(), report.Events(), report.Roles(), report.Capabilities()
	profile.Winner.ProfileID, events[0].ProfileID, roles[0].Winner.Model, capabilities[0].Reason = "mutated", "mutated", "mutated", "mutated"
	if got := report.Profile(); got.Winner.ProfileID == "mutated" {
		t.Fatal("same report profile accessor leaked")
	}
	if got := report.Events(); got[0].ProfileID == "mutated" {
		t.Fatal("same report event accessor leaked")
	}
	if got := report.Roles(); got[0].Winner.Model == "mutated" {
		t.Fatal("same report role accessor leaked")
	}
	if got := report.Capabilities(); got[0].Reason == "mutated" {
		t.Fatal("same report capability accessor leaked")
	}

	accepted := cloneForTest(in)
	accepted.Target, accepted.Requested = TargetPi, nil
	acceptedIR := mustCompile(t, accepted)
	ip, ie, irs, ic := acceptedIR.Profile(), acceptedIR.CredentialSlots(), acceptedIR.Roles(), acceptedIR.Capabilities()
	ip.Winner.ProfileID = "mutated"
	ie = append(ie, CredentialRef{Provider: "mutated", Slot: "mutated"})
	irs[0].Winner.Model, ic[0].Reason = "mutated", "mutated"
	if acceptedIR.Profile().Winner.ProfileID == "mutated" || len(acceptedIR.CredentialSlots()) != len(ie)-1 || acceptedIR.Roles()[0].Winner.Model == "mutated" || acceptedIR.Capabilities()[0].Reason == "mutated" {
		t.Fatal("same IR accessor leaked")
	}
}

func w2aInput() Input {
	in := validInput()
	in.Profiles = []Profile{{ID: "balanced", DefaultModel: "balanced"}, {ID: "focused", DefaultModel: "focused"}, {ID: "other", DefaultModel: "other"}}
	return in
}
