package agentpack

import (
	"strings"
	"testing"
)

func TestDefaultDefinitionProvidesPortableLorePack(t *testing.T) {
	definition := DefaultDefinition()

	if definition.SchemaVersion != SchemaVersion1 {
		t.Fatalf("SchemaVersion = %d, want %d", definition.SchemaVersion, SchemaVersion1)
	}
	if definition.PackID != "portable-agent-pack" {
		t.Fatalf("PackID = %q, want portable-agent-pack", definition.PackID)
	}
	if len(definition.Workflow.Phases) != len(OrderedPhaseIDs()) {
		t.Fatalf("len(Workflow.Phases) = %d, want %d", len(definition.Workflow.Phases), len(OrderedPhaseIDs()))
	}
	if got := definition.Workflow.Phases[0].ID; got != PhaseInit {
		t.Fatalf("first phase = %q, want %q", got, PhaseInit)
	}
	if got := definition.Workflow.Phases[len(definition.Workflow.Phases)-1].ID; got != PhaseArchive {
		t.Fatalf("last phase = %q, want %q", got, PhaseArchive)
	}
	if _, err := definition.Profile("balanced"); err != nil {
		t.Fatalf("Profile(balanced) error = %v, want nil", err)
	}
	if _, err := definition.Profile("speed"); err != nil {
		t.Fatalf("Profile(speed) error = %v, want nil", err)
	}
	if err := definition.Validate(); err != nil {
		t.Fatalf("Validate error = %v, want nil", err)
	}
}

func TestDefaultDefinitionIncludesCanonicalRolesAndPhaseOrder(t *testing.T) {
	definition := DefaultDefinition()

	wantPhases := OrderedPhaseIDs()
	if len(definition.Workflow.Phases) != len(wantPhases) {
		t.Fatalf("len(Workflow.Phases) = %d, want %d", len(definition.Workflow.Phases), len(wantPhases))
	}
	for i, want := range wantPhases {
		if got := definition.Workflow.Phases[i].ID; got != want {
			t.Fatalf("Workflow.Phases[%d] = %q, want %q", i, got, want)
		}
	}

	roles := map[string]bool{}
	for _, role := range definition.Roles {
		roles[role.Name] = true
	}
	for _, want := range []string{RoleOrchestrator, RoleLoreWorker, "sdd-propose"} {
		if !roles[want] {
			t.Fatalf("role %q missing from default definition", want)
		}
	}
	if roles["sdd-proposal"] {
		t.Fatal("legacy role sdd-proposal unexpectedly present in default definition")
	}
}

func TestDefaultDefinitionIncludesManagedAgentOverlays(t *testing.T) {
	definition := DefaultDefinition()

	if len(definition.ManagedAgents) != 10 {
		t.Fatalf("len(ManagedAgents) = %d, want 10 canonical overlays", len(definition.ManagedAgents))
	}
	if definition.ManagedAgents[0].Name != RoleLoreWorker {
		t.Fatalf("ManagedAgents[0].Name = %q, want %q", definition.ManagedAgents[0].Name, RoleLoreWorker)
	}
	if definition.ManagedAgents[1].Name != "sdd-init" || definition.ManagedAgents[len(definition.ManagedAgents)-1].Name != "sdd-archive" {
		t.Fatalf("managed overlay names = first=%q last=%q, want sdd-init..sdd-archive after lore-worker", definition.ManagedAgents[1].Name, definition.ManagedAgents[len(definition.ManagedAgents)-1].Name)
	}
	if got := definition.ManagedAgents[0].Tools; len(got) != 4 || got[0] != "read" {
		t.Fatalf("lore-worker tools = %v, want canonical worker tool list", got)
	}
	if got := definition.ManagedAgents[2].SkillPolicy.Mode; got != "explicit" {
		t.Fatalf("managed sdd phase skill policy = %q, want explicit", got)
	}
	if got := definition.ManagedAgents[2].Phase; got != PhaseExplore {
		t.Fatalf("managed phase agent = %q, want %q", got, PhaseExplore)
	}
	body := definition.ManagedAgents[0].Body
	if !contains(body, "You are the canonical Lore repository worker.") {
		t.Fatalf("lore-worker body = %q, want canonical worker body", body)
	}
	for _, want := range []string{
		"Return ONLY one JSON object with exactly these keys: `status`, `summary`, `artifacts`, `next`, `question`, `options`, `risks`, `skill_resolution`.",
		"`summary`: one compact operational line, <= 280 chars",
		"`artifacts`: string array with <= 8 artifact references, each <= 160 chars",
		"`next`: string <= 160 chars or null",
		"`question`: string <= 220 chars or null",
		"`options`: string array with <= 5 compact choices",
		"`risks`: string array with <= 5 compact items, each <= 180 chars",
		"`skill_resolution`: `injected` | `fallback-registry` | `fallback-path` | `none` and <= 80 chars",
		"Persist or reference long details in artifacts; do not embed long logs, diffs, or narratives in the envelope itself.",
	} {
		if !contains(body, want) {
			t.Fatalf("lore-worker body = %q, want envelope contract snippet %q", body, want)
		}
	}
	for _, forbidden := range []string{"`kind`", "`specialization`", "`memory_saved`"} {
		if contains(body, forbidden) {
			t.Fatalf("lore-worker body = %q, want legacy worker envelope field %q omitted", body, forbidden)
		}
	}
	if exception := "`judgment-day` remains explicitly out of scope"; contains(body, "judgment-day") && !contains(body, exception) {
		t.Fatalf("lore-worker body = %q, want strict child-envelope judgment-day exclusion %q when judgment-day is mentioned", body, exception)
	}
}

func TestDefinitionValidateRejectsInvalidShape(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Definition)
		want   string
	}{
		{
			name: "schema version",
			mutate: func(definition *Definition) {
				definition.SchemaVersion = 99
			},
			want: "schema_version",
		},
		{
			name: "pack id",
			mutate: func(definition *Definition) {
				definition.PackID = "  "
			},
			want: "pack_id",
		},
		{
			name: "missing phase",
			mutate: func(definition *Definition) {
				definition.Workflow.Phases = definition.Workflow.Phases[:len(definition.Workflow.Phases)-1]
			},
			want: "workflow phases",
		},
		{
			name: "wrong phase order",
			mutate: func(definition *Definition) {
				definition.Workflow.Phases[0].ID = PhaseArchive
			},
			want: "workflow phase 0",
		},
		{
			name: "missing roles",
			mutate: func(definition *Definition) {
				definition.Roles = nil
			},
			want: "roles are required",
		},
		{
			name: "blank role",
			mutate: func(definition *Definition) {
				definition.Roles[0].Name = "\t"
			},
			want: "role name",
		},
		{
			name: "missing orchestrator",
			mutate: func(definition *Definition) {
				definition.Roles = []Role{{Name: RoleLoreWorker}}
			},
			want: RoleOrchestrator,
		},
		{
			name: "missing lore worker",
			mutate: func(definition *Definition) {
				definition.Roles = []Role{{Name: RoleOrchestrator}}
			},
			want: RoleLoreWorker,
		},
		{
			name: "missing profiles",
			mutate: func(definition *Definition) {
				definition.Profiles = nil
			},
			want: "profiles are required",
		},
		{
			name: "blank profile id",
			mutate: func(definition *Definition) {
				definition.Profiles[0].ID = " "
			},
			want: "profile id",
		},
		{
			name: "blank profile model",
			mutate: func(definition *Definition) {
				definition.Profiles[0].DefaultModel = " "
			},
			want: "default model",
		},
		{
			name: "duplicate profile",
			mutate: func(definition *Definition) {
				definition.Profiles = append(definition.Profiles, definition.Profiles[0])
			},
			want: "duplicate profile",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			definition := DefaultDefinition()
			tt.mutate(&definition)
			err := definition.Validate()
			if err == nil || !contains(err.Error(), tt.want) {
				t.Fatalf("Validate error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestDefinitionSelectProfileAndRoleModel(t *testing.T) {
	definition := DefaultDefinition()

	profile, err := definition.Profile("balanced")
	if err != nil {
		t.Fatalf("Profile(balanced) error = %v, want nil", err)
	}
	if got := profile.ModelForRole(RoleOrchestrator); got == "" {
		t.Fatal("ModelForRole(orchestrator) = empty, want configured model")
	}
	if got := profile.ModelForRole("unknown-role"); got != profile.DefaultModel {
		t.Fatalf("ModelForRole(unknown-role) = %q, want default %q", got, profile.DefaultModel)
	}

	if _, err := definition.Profile("missing"); err == nil {
		t.Fatal("Profile(missing) error = nil, want not found error")
	}
}

func TestPhaseAgentNameMapsProposalToSddPropose(t *testing.T) {
	if got := PhaseAgentName(PhaseProposal); got != "sdd-propose" {
		t.Fatalf("PhaseAgentName(proposal) = %q, want sdd-propose", got)
	}
	if got := PhaseAgentName(PhaseApply); got != "sdd-apply" {
		t.Fatalf("PhaseAgentName(apply) = %q, want sdd-apply", got)
	}
}

func contains(value, want string) bool {
	return strings.Contains(value, want)
}
