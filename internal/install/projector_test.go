package install

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/compiler"
	"github.com/alferio94/lore-cli/internal/reconcile"
)

func TestW3ProjectSemanticPlanNormalizesPiAndOpenCodeFacts(t *testing.T) {
	for _, target := range []TargetID{TargetPi, TargetOpenCode} {
		t.Run(string(target), func(t *testing.T) {
			ir := projectorIR(t, target)
			first := projectorInput(target, false)
			second := projectorInput(target, true)

			got, err := ProjectSemanticPlan(ir, first)
			if err != nil {
				t.Fatalf("ProjectSemanticPlan(first) error = %v", err)
			}
			again, err := ProjectSemanticPlan(ir, second)
			if err != nil {
				t.Fatalf("ProjectSemanticPlan(second) error = %v", err)
			}
			if !reflect.DeepEqual(got.TargetFacts(), again.TargetFacts()) || !reflect.DeepEqual(got.Intents(), again.Intents()) {
				t.Fatalf("permuted projection differs:\nfirst=%#v\nsecond=%#v", got.TargetFacts(), again.TargetFacts())
			}

			facts := got.TargetFacts()
			if facts.Target != target || facts.IRID != ir.IRID() || facts.Profile.Winner.ProfileID != "project" || facts.Profile.Winner.Scope != compiler.ProfileScopeProject || facts.Profile.Winner.ProjectID != "project:alpha" {
				t.Fatalf("target/profile facts = %#v", facts)
			}
			if !facts.Persistence.Requested || facts.Persistence.Scope != compiler.ProfileScopeProject || facts.Persistence.ProjectID != "project:alpha" || facts.Persistence.ProfileID != "project" {
				t.Fatalf("persistence fact = %#v", facts.Persistence)
			}
			if len(facts.Roles) != 2 || facts.Roles[0].Role != "default" || facts.Roles[1].Role != "worker" || facts.Roles[1].Winner.SourceKey != "project-source" {
				t.Fatalf("role provenance = %#v", facts.Roles)
			}
			wantState, wantActive := compiler.StateUnsupported, false
			if target == TargetPi {
				wantState = compiler.StateStagedInactive
			}
			bounded := capabilityFact(t, facts.Capabilities, compiler.CapabilityBoundedReview)
			if bounded.State != wantState || bounded.RuntimeActive != wantActive || bounded.Evidence != "compiler-registry/v1" {
				t.Fatalf("bounded-review fact = %#v", bounded)
			}

			if len(facts.Resources) != 3 {
				t.Fatalf("resource facts = %#v", facts.Resources)
			}
			if got := []reconcile.Mode{facts.Resources[0].Mode, facts.Resources[1].Mode, facts.Resources[2].Mode}; !reflect.DeepEqual(got, []reconcile.Mode{reconcile.ModeAdditive, reconcile.ModeReplace, reconcile.ModeMarkerMerge}) {
				t.Fatalf("normalized modes = %v", got)
			}
			marker := facts.Resources[2]
			if marker.Resource != "prompts/managed.md" || marker.Marker != reconcile.MarkerValid || !reflect.DeepEqual(marker.OwnershipMarkers, []OwnershipMarker{{Name: "managed_by", Value: "lore-cli"}, {Name: "managed_layer", Value: "agent-pack"}}) {
				t.Fatalf("marker fact = %#v", marker)
			}
			replace := facts.Resources[1]
			if len(replace.SensitiveReferences) != 1 || replace.SensitiveReferences[0] != (SensitiveReference{FinalizerID: "mcp-finalizer", Provider: "vault", Slot: "lore"}) {
				t.Fatalf("sensitive references = %#v", replace.SensitiveReferences)
			}
			reportA, err := reconcile.Reconcile(ir, got.Intents())
			if err != nil {
				t.Fatalf("Reconcile(first) error = %v", err)
			}
			reportB, err := reconcile.Reconcile(ir, again.Intents())
			if err != nil {
				t.Fatalf("Reconcile(second) error = %v", err)
			}
			if !reflect.DeepEqual(reportA.Decisions(), reportB.Decisions()) || !reportA.AllAdmitted() {
				t.Fatalf("reconciliation differs or rejected: A=%#v B=%#v", reportA.Decisions(), reportB.Decisions())
			}
		})
	}
}

func TestW3ProjectSemanticPlanDefensiveCopiesAndSecretRejection(t *testing.T) {
	ir := projectorIR(t, TargetPi)
	input := projectorInput(TargetPi, false)
	original := cloneProjectorInput(input)
	plan, err := ProjectSemanticPlan(ir, input)
	if err != nil {
		t.Fatalf("ProjectSemanticPlan error = %v", err)
	}
	if !reflect.DeepEqual(input, original) {
		t.Fatal("ProjectSemanticPlan mutated caller input")
	}

	facts := plan.TargetFacts()
	facts.Resources[0].Desired[0] = 'X'
	facts.Resources[2].OwnershipMarkers[0].Value = "mutated"
	facts.Roles[0].Shadowed[0].SourceKey = "mutated"
	intents := plan.Intents()
	intents[0].Desired.Content[0] = 'X'
	intents[0].Observed.AdditiveClaims[0].Subject = "mutated"
	intents[1].Desired.SensitiveLinks[0].Credential.Slot = "mutated"
	if !reflect.DeepEqual(plan.TargetFacts(), projectMust(t, ir, original).TargetFacts()) || !reflect.DeepEqual(plan.Intents(), projectMust(t, ir, original).Intents()) {
		t.Fatal("plan accessors alias caller or prior return storage")
	}

	secret := projectorInput(TargetPi, false)
	secret.Resources[0].Desired = []byte("Authorization: Bearer raw-secret-value")
	zero, err := ProjectSemanticPlan(ir, secret)
	assertProjectorError(t, err, CodeProjectorSecret, "resources.config/settings.json.desired")
	if !zero.IsZero() || strings.Contains(err.Error(), "raw-secret-value") {
		t.Fatalf("secret rejection output/error = %#v / %q", zero, err)
	}
}

func TestW3ProjectSemanticPlanRejectsSecretOwnershipMarkerOverlay(t *testing.T) {
	const secret = "raw-marker-secret-W3S1"
	cases := []struct{ name, marker, value string }{
		{"nested name", "ownership.metadata.Authorization.value", secret},
		{"hyphen variant", "managed.api-key", secret},
		{"case variant", "CrEdEnTiAl", secret},
		{"name whitespace", "  token  ", secret},
		{"bearer value whitespace", "managed_by", " \tBeArEr \t" + secret + " \t"},
		{"unquoted assignment value", "managed_by", "password = " + secret},
	}
	for _, target := range []TargetID{TargetPi, TargetOpenCode} {
		for _, tc := range cases {
			t.Run(string(target)+"/"+tc.name, func(t *testing.T) {
				input := projectorInput(target, false)
				marker := reverseIndex(input.Resources, "prompts/managed.md")
				input.Resources[marker].OwnershipMarkers[tc.marker] = tc.value

				got, err := ProjectSemanticPlan(projectorIR(t, target), input)
				assertProjectorError(t, err, CodeProjectorSecret, "resources.prompts/managed.md.ownership_markers")
				if !got.IsZero() || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), tc.marker) {
					t.Fatalf("secret marker rejection leaked output/error: %#v / %q", got, err)
				}
			})
		}
	}
}

func TestW3ProjectSemanticPlanRejectsUnsupportedInvalidAndMismatchedTargets(t *testing.T) {
	pi := projectorIR(t, TargetPi)
	codex := projectorIR(t, TargetCodex)
	unsupportedComponent := projectorInput(TargetOpenCode, false)
	unsupportedComponent.Resources[0].Component = ComponentBoundedReviewProjection
	cases := []struct {
		name string
		ir   compiler.ResolvedIR
		in   ProjectorInput
		code ProjectorCode
		path string
	}{
		{"invalid target", pi, ProjectorInput{}, CodeProjectorInvalidTarget, "target"},
		{"mismatch", pi, projectorInput(TargetOpenCode, false), CodeProjectorTargetMismatch, "target"},
		{"unsupported", codex, projectorInput(TargetCodex, false), CodeProjectorUnsupportedTarget, "target"},
		{"unsupported component", projectorIR(t, TargetOpenCode), unsupportedComponent, CodeProjectorInvalidFact, "resources.config/settings.json.component"},
		{"invalid mode", pi, projectorInput(TargetPi, false), CodeProjectorInvalidFact, "resources.config/settings.json.mode"},
	}
	cases[4].in.Resources[0].Mode = "unknown"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ProjectSemanticPlan(tc.ir, tc.in)
			assertProjectorError(t, err, tc.code, tc.path)
			if !got.IsZero() {
				t.Fatalf("ProjectSemanticPlan output = %#v, want zero", got)
			}
		})
	}
}

func projectorIR(t *testing.T, target TargetID) compiler.ResolvedIR {
	t.Helper()
	ir, err := compiler.Compile(compiler.Input{
		Schema: compiler.SchemaV1, Compiler: compiler.CompilerV1,
		Pack: compiler.PackSnapshot{ID: "portable-agent-pack", Version: "1.0.0"}, Target: compiler.TargetID(target), ProjectID: "project:alpha",
		Profiles:   []compiler.Profile{{ID: "global", DefaultModel: "global-model", Roles: map[string]string{"worker": "global-worker"}}, {ID: "project", DefaultModel: "project-model", Roles: map[string]string{"worker": "project-worker"}}},
		Candidates: []compiler.Candidate{{Tier: compiler.TierGlobal, ProfileID: "global", SourceKey: "global-source", Scope: compiler.ProfileScopeGlobal, Location: "global/config"}, {Tier: compiler.TierProject, ProfileID: "project", SourceKey: "project-source", Scope: compiler.ProfileScopeProject, ProjectID: "project:alpha", Location: "project/config"}},
		Requested:  []compiler.CapabilityRequest{{ID: compiler.CapabilityPortable, Required: true}}, CredentialSlots: []compiler.CredentialRef{{Provider: "vault", Slot: "lore"}},
		Persistence: compiler.RequestedProfilePersistence{Scope: compiler.ProfileScopeProject, ProjectID: "project:alpha", ProfileID: "project"},
	})
	if err != nil {
		t.Fatalf("Compile(%s) error = %v", target, err)
	}
	return ir
}

func projectorInput(target TargetID, reverse bool) ProjectorInput {
	resources := []SemanticResource{
		{Resource: "config/settings.json", Component: ComponentCorePack, Mode: MergeModeAdditiveJSON, Present: true, Observed: []byte("settings-old"), Desired: []byte("settings-new"), Evidence: reconcile.Evidence{Managed: true}, AdditiveClaims: []reconcile.AdditiveClaim{{Subject: "theme", Equivalent: true, Evidence: reconcile.Evidence{Managed: true}}, {Subject: "agents", Equivalent: true, Evidence: reconcile.Evidence{Managed: true}}}},
		{Resource: "prompts/managed.md", Component: ComponentCorePack, Mode: MergeModeMarkerMerge, Present: true, Observed: []byte("prompt-old"), Desired: []byte("prompt-new"), Evidence: reconcile.Evidence{Managed: true}, Marker: reconcile.MarkerValid, OwnershipMarkers: map[string]string{"managed_layer": "agent-pack", "managed_by": "lore-cli"}},
		{Resource: "mcp/lore.json", Component: ComponentLoreServerMCP, Mode: MergeModeReplace, Present: false, Desired: []byte(`{"credential":{"provider":"vault","slot":"lore"}}`), SensitiveReferences: []SensitiveReference{{FinalizerID: "mcp-finalizer", Provider: "vault", Slot: "lore"}}},
	}
	if reverse {
		resources[0], resources[2] = resources[2], resources[0]
		resources[1].OwnershipMarkers = map[string]string{"managed_by": "lore-cli", "managed_layer": "agent-pack"}
		resources[reverseIndex(resources, "config/settings.json")].AdditiveClaims[0], resources[reverseIndex(resources, "config/settings.json")].AdditiveClaims[1] = resources[reverseIndex(resources, "config/settings.json")].AdditiveClaims[1], resources[reverseIndex(resources, "config/settings.json")].AdditiveClaims[0]
	}
	return ProjectorInput{Target: target, Resources: resources}
}

func reverseIndex(resources []SemanticResource, resource string) int {
	for i := range resources {
		if resources[i].Resource == resource {
			return i
		}
	}
	return -1
}

func cloneProjectorInput(in ProjectorInput) ProjectorInput {
	out := ProjectorInput{Target: in.Target, Resources: append([]SemanticResource(nil), in.Resources...)}
	for i := range out.Resources {
		out.Resources[i].Observed = append([]byte(nil), in.Resources[i].Observed...)
		out.Resources[i].Desired = append([]byte(nil), in.Resources[i].Desired...)
		out.Resources[i].AdditiveClaims = append([]reconcile.AdditiveClaim(nil), in.Resources[i].AdditiveClaims...)
		out.Resources[i].SensitiveReferences = append([]SensitiveReference(nil), in.Resources[i].SensitiveReferences...)
		if in.Resources[i].OwnershipMarkers != nil {
			out.Resources[i].OwnershipMarkers = map[string]string{}
			for key, value := range in.Resources[i].OwnershipMarkers {
				out.Resources[i].OwnershipMarkers[key] = value
			}
		}
	}
	return out
}

func capabilityFact(t *testing.T, facts []compiler.CapabilityDecision, id compiler.CapabilityID) compiler.CapabilityDecision {
	t.Helper()
	for _, fact := range facts {
		if fact.ID == id {
			return fact
		}
	}
	t.Fatalf("capability %q missing from %#v", id, facts)
	return compiler.CapabilityDecision{}
}

func projectMust(t *testing.T, ir compiler.ResolvedIR, in ProjectorInput) SemanticPlan {
	t.Helper()
	plan, err := ProjectSemanticPlan(ir, in)
	if err != nil {
		t.Fatalf("ProjectSemanticPlan error = %v", err)
	}
	return plan
}

func assertProjectorError(t *testing.T, err error, code ProjectorCode, path string) {
	t.Helper()
	if err == nil || !errors.Is(err, code) {
		t.Fatalf("error = %v, want errors.Is(%q)", err, code)
	}
	var typed *ProjectorError
	if !errors.As(err, &typed) || typed.Code() != code || typed.Path() != path {
		t.Fatalf("error = %#v, want code=%q path=%q", err, code, path)
	}
}
