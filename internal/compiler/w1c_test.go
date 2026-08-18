package compiler

import (
	"reflect"
	"testing"
)

func TestW1CKnownRegistryAndTargetMatrix(t *testing.T) {
	wantIDs := []CapabilityID{CapabilityBoundedReview, CapabilityPortable}
	if got := knownCapabilityIDs(); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("known capability IDs = %#v, want %#v", got, wantIDs)
	}

	cases := []struct {
		target TargetID
		want   []CapabilityDecision
	}{
		{TargetPi, []CapabilityDecision{
			{ID: CapabilityBoundedReview, Target: TargetPi, State: StateStagedInactive, Reason: "Pi bounded review is staged and inactive", Evidence: "compiler-registry/v1", RuntimeActive: false},
			{ID: CapabilityPortable, Target: TargetPi, State: StateSupported, Reason: "portable compiler content is supported", Evidence: "compiler-registry/v1", RuntimeActive: true},
		}},
		{TargetOpenCode, targetMatrix(TargetOpenCode, StateUnsupported)},
		{TargetCodex, targetMatrix(TargetCodex, StateUnsupported)},
		{TargetAntigravity, targetMatrix(TargetAntigravity, StateUnsupported)},
		{TargetClaude, []CapabilityDecision{
			{ID: CapabilityBoundedReview, Target: TargetClaude, State: StateUnsupported, Reason: "Claude Code is unsupported and roadmap-only", Evidence: "compiler-registry/v1"},
			{ID: CapabilityPortable, Target: TargetClaude, State: StateUnsupported, Reason: "Claude Code is unsupported and roadmap-only", Evidence: "compiler-registry/v1"},
		}},
	}
	for _, tc := range cases {
		t.Run(string(tc.target), func(t *testing.T) {
			want := appendUnknownDecision(tc.target, tc.want)
			if got := capabilityMatrix(tc.target, []CapabilityRequest{{ID: "unknown-capability"}}); !reflect.DeepEqual(got, want) {
				t.Fatalf("matrix = %#v, want %#v", got, want)
			}
			for _, decision := range capabilityMatrix(tc.target, nil) {
				if decision.Evidence != "compiler-registry/v1" {
					t.Fatalf("evidence must be a safe identifier: %q", decision.Evidence)
				}
			}
		})
	}
}

func TestW1CMatrixUnionsRequestsAndMergesRequired(t *testing.T) {
	requests := []CapabilityRequest{
		{ID: "future-capability", Required: false},
		{ID: CapabilityPortable, Required: false},
		{ID: CapabilityBoundedReview, Required: false},
		{ID: "future-capability", Required: true},
		{ID: CapabilityPortable, Required: true},
	}
	got := capabilityMatrix(TargetPi, requests)
	want := []CapabilityDecision{
		{ID: CapabilityBoundedReview, Target: TargetPi, State: StateStagedInactive, Reason: "Pi bounded review is staged and inactive", Evidence: "compiler-registry/v1", RuntimeActive: false},
		{ID: "future-capability", Target: TargetPi, State: StateUnknown, Reason: "unknown capability; check spelling and compiler version", Evidence: "compiler-registry/v1", Required: true},
		{ID: CapabilityPortable, Target: TargetPi, State: StateSupported, Reason: "portable compiler content is supported", Evidence: "compiler-registry/v1", Required: true, RuntimeActive: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("matrix = %#v, want %#v", got, want)
	}

	reordered := append([]CapabilityRequest(nil), requests...)
	reverse(reordered)
	if !reflect.DeepEqual(capabilityMatrix(TargetPi, reordered), want) {
		t.Fatal("request order changed canonical matrix")
	}
}

func TestW1CAdmissionUsesRequestedStateAndClaudePrecedence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		target  TargetID
		request CapabilityRequest
		code    ErrorCode
	}{
		{"optional_unsupported", TargetOpenCode, CapabilityRequest{ID: CapabilityBoundedReview}, CodeCapabilityUnsupported},
		{"required_unsupported", TargetOpenCode, CapabilityRequest{ID: CapabilityBoundedReview, Required: true}, CodeCapabilityUnsupported},
		{"optional_unknown", TargetPi, CapabilityRequest{ID: "unknown"}, CodeCapabilityUnknown},
		{"required_unknown", TargetPi, CapabilityRequest{ID: "unknown", Required: true}, CodeCapabilityUnknown},
		{"claude_roadmap_precedes_capability", TargetClaude, CapabilityRequest{ID: "unknown", Required: true}, CodeTargetRoadmapUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := validInput()
			input.Target, input.Requested = tc.target, []CapabilityRequest{tc.request}
			_, err := Compile(input)
			requireCode(t, err, tc.code)
		})
	}

	input := validInput()
	input.Target = TargetOpenCode
	ir := mustCompile(t, input)
	if !ir.Admitted || len(ir.Capabilities()) != 2 || ir.Capabilities()[0].State != StateUnsupported {
		t.Fatalf("unrequested unsupported capability must be retained without blocking: %#v", ir)
	}
	input.Target, input.Requested = TargetPi, []CapabilityRequest{{ID: CapabilityBoundedReview}}
	ir = mustCompile(t, input)
	if !ir.Admitted || ir.Capabilities()[0].State != StateStagedInactive || ir.Capabilities()[0].RuntimeActive {
		t.Fatalf("requested Pi bounded review = %#v, want admitted staged/inactive", ir.Capabilities()[0])
	}
}

func TestW1CDecisionIdentityAndAccessorCopies(t *testing.T) {
	input := validInput()
	input.Requested = []CapabilityRequest{{ID: CapabilityPortable}, {ID: CapabilityPortable, Required: true}, {ID: CapabilityBoundedReview}}
	baseline := mustCompile(t, input)
	reordered := cloneForTest(input)
	reverse(reordered.Requested)
	reorderedIR := mustCompile(t, reordered)
	requireSameIdentity(t, baseline, reorderedIR)

	returned := baseline.Capabilities()
	returned[0].Reason = "mutated"
	if baseline.Capabilities()[0].Reason == "mutated" || baseline.IRID() != mustCompile(t, input).IRID() {
		t.Fatalf("capabilities accessor mutated sealed decision matrix: %#v", baseline.Capabilities())
	}
}

func TestW1CSensitiveProjectionRequiresSealedAdmittedDeclaredExactRef(t *testing.T) {
	ref := CredentialRef{Provider: "vault", Slot: "lore-token"}
	for _, tc := range []struct {
		name string
		ir   ResolvedIR
		ref  CredentialRef
	}{
		{"zero", ResolvedIR{}, ref},
		{"not_admitted", ResolvedIR{sealed: true}, ref},
		{"undeclared", ResolvedIR{sealed: true, Admitted: true}, ref},
		{"same_slot_other_provider", ResolvedIR{sealed: true, Admitted: true, credentialSlots: []CredentialRef{ref}}, CredentialRef{Provider: "other", Slot: ref.Slot}},
		{"same_provider_other_slot", ResolvedIR{sealed: true, Admitted: true, credentialSlots: []CredentialRef{ref}}, CredentialRef{Provider: ref.Provider, Slot: "other-slot"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewSensitiveProjection(tc.ir, "adapter-finalizer", tc.ref); err == nil {
				t.Fatal("NewSensitiveProjection succeeded for invalid sensitive boundary")
			}
		})
	}

	input := validInput()
	input.CredentialSlots = []CredentialRef{ref}
	ir := mustCompile(t, input)
	inputID, irID, capabilities := ir.InputID(), ir.IRID(), ir.Capabilities()
	returnedSlots := ir.CredentialSlots()
	returnedSlots[0].Provider, returnedSlots[0].Slot = "mutated", "mutated"
	returnedSlots = append(returnedSlots, CredentialRef{Provider: "invented", Slot: "invented"})
	if got := ir.CredentialSlots(); !reflect.DeepEqual(got, []CredentialRef{ref}) {
		t.Fatalf("credential slot accessor leaked mutation: %#v", got)
	}
	projection, err := NewSensitiveProjection(ir, "adapter-finalizer", ref)
	if err != nil {
		t.Fatal(err)
	}
	if projection.FinalizerID != "adapter-finalizer" || projection.Credential != ref || projection.RedactionToken != "<redacted:vault/lore-token>" {
		t.Fatalf("safe projection = %#v", projection)
	}
	if ir.InputID() != inputID || ir.IRID() != irID || !reflect.DeepEqual(ir.Capabilities(), capabilities) {
		t.Fatal("sensitive planning mutated plan identity or provenance")
	}

	// Two external token values are deliberately never supplied to compiler APIs.
	if other := mustCompile(t, input); other.InputID() != inputID || other.IRID() != irID {
		t.Fatal("token-independent compilation changed identity")
	}
}

func targetMatrix(target TargetID, boundedState CapabilityState) []CapabilityDecision {
	return []CapabilityDecision{
		{ID: CapabilityBoundedReview, Target: target, State: boundedState, Reason: "bounded review is currently Pi-only", Evidence: "compiler-registry/v1"},
		{ID: CapabilityPortable, Target: target, State: StateSupported, Reason: "portable compiler content is supported", Evidence: "compiler-registry/v1", RuntimeActive: true},
	}
}

func appendUnknownDecision(target TargetID, decisions []CapabilityDecision) []CapabilityDecision {
	result := append([]CapabilityDecision(nil), decisions...)
	result = append(result, CapabilityDecision{ID: "unknown-capability", Target: target, State: StateUnknown, Reason: "unknown capability; check spelling and compiler version", Evidence: "compiler-registry/v1"})
	return result
}
