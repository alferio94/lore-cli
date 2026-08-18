package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func validManifest() Manifest {
	return Manifest{
		SchemaVersion:   SchemaV3,
		CompilerVersion: "canonical-capability-profile-compiler/v1.0.0",
		Pack:            PackIdentity{ID: "base", Version: "1.0.0"},
		InputID:         "input-1",
		ResolvedIRID:    "ir-1",
		Profile:         ProfileIdentity{ID: "default", Version: "1.0.0", Scope: "global"},
		Target:          "pi",
		Projections: []Projection{
			{Path: "skills/a.md", Ownership: "replace", Hash: "aaa"},
			{Path: "skills/b.md", Ownership: "marker-merge", Hash: "bbb"},
		},
		RollbackBoundary: RollbackBoundary{
			ID: "rb-v1", Kind: "selected-target-backup-v1", Target: "pi",
			Resources:     []Projection{{Path: "skills/a.md", Ownership: "replace", Hash: "aaa"}, {Path: "skills/b.md", Ownership: "marker-merge", Hash: "bbb"}},
			Finalizations: []FinalizationReference{},
		},
		RoleModels:     []RoleModelProvenance{{Role: "planner", Model: "gpt-4", Source: "project", Location: "project:demo"}},
		Capabilities:   []CapabilityProvenance{{ID: "skills", State: "supported", Reason: "target matrix"}},
		Reconciliation: ReconciliationProvenance{Owner: "compiler", Decision: "update"},
		Finalizations:  []FinalizationReference{},
		Extensions:     map[string]json.RawMessage{},
	}
}

func TestEncodeV3CanonicalBytesAndCopies(t *testing.T) {
	manifest := validManifest()
	got, err := Encode(manifest)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	const want = `{"schema_version":3,"compiler_version":"canonical-capability-profile-compiler/v1.0.0","pack":{"id":"base","version":"1.0.0"},"input_id":"input-1","resolved_ir_id":"ir-1","profile":{"id":"default","version":"1.0.0","scope":"global"},"target":"pi","projections":[{"path":"skills/a.md","ownership":"replace","hash":"aaa"},{"path":"skills/b.md","ownership":"marker-merge","hash":"bbb"}],"rollback_boundary":{"id":"rb-v1","kind":"selected-target-backup-v1","target":"pi","resources":[{"path":"skills/a.md","ownership":"replace","hash":"aaa"},{"path":"skills/b.md","ownership":"marker-merge","hash":"bbb"}],"finalizations":[]},"role_models":[{"role":"planner","model":"gpt-4","source":"project","location":"project:demo"}],"capabilities":[{"id":"skills","state":"supported","reason":"target matrix"}],"reconciliation":{"owner":"compiler","decision":"update"},"finalizations":[]}`
	if string(got) != want {
		t.Fatalf("Encode() = %s, want %s", got, want)
	}

	manifest.Projections[0].Path = "mutated.md"
	decoded, err := Decode(got)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	projections := decoded.ProjectionsCopy()
	projections[0].Path = "mutated-again.md"
	if decoded.ProjectionsCopy()[0].Path != "skills/a.md" {
		t.Fatalf("ProjectionsCopy() did not return a defensive copy")
	}
}

func TestDecodeRejectsContractFailuresWithZeroOutput(t *testing.T) {
	validBytes, err := Encode(validManifest())
	if err != nil {
		t.Fatal(err)
	}
	valid := string(validBytes)
	tests := []struct {
		name, data, path string
		code             ErrorCode
	}{
		{"malformed JSON", `{`, "$", CodeMalformedJSON},
		{"missing required field", strings.Replace(valid, `,"target":"pi"`, "", 1), "target", CodeRequiredField},
		{"duplicate semantic projection", strings.Replace(valid, `}],"rollback_boundary"`, `},{"path":"skills/a.md","ownership":"replace","hash":"bbb"}],"rollback_boundary"`, 1), "projections[1].path", CodeDuplicateRecord},
		{"unknown schema", strings.Replace(valid, `"schema_version":3`, `"schema_version":99`, 1), "schema_version", CodeUnsupportedSchema},
		{"unknown normative field", strings.Replace(valid, `,"target":"pi"`, `,"forbidden":"secret-token","target":"pi"`, 1), "forbidden", CodeUnknownField},
		{"secret-shaped forbidden data", strings.Replace(valid, `,"target":"pi"`, `,"token":"secret-token","target":"pi"`, 1), "token", CodeUnknownField},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decode([]byte(tt.data))
			if !reflect.DeepEqual(got, Manifest{}) {
				t.Fatalf("Decode() output = %+v, want zero Manifest", got)
			}
			if !errors.Is(err, tt.code) {
				t.Fatalf("Decode() error = %v, want errors.Is(%v), data=%s", err, tt.code, tt.data)
			}
			var typed *CodecError
			if !errors.As(err, &typed) || typed.Path() != tt.path {
				t.Fatalf("Decode() typed error = %#v, want path %q", typed, tt.path)
			}
		})
	}
}

func TestV3ProvenanceAndSafeExtensions(t *testing.T) {
	manifest := validManifest()
	manifest.RoleModels = []RoleModelProvenance{{Role: "planner", Model: "gpt-4", Source: "project", Location: "project:demo"}}
	manifest.Capabilities = []CapabilityProvenance{{ID: "skills", State: "supported", Reason: "target matrix"}}
	manifest.Reconciliation = ReconciliationProvenance{Owner: "compiler", Decision: "update"}
	manifest.Finalizations = []FinalizationReference{{Path: "skills/a.md", FinalizerID: "adapter", Provider: "vault", Slot: "primary"}}
	manifest.RollbackBoundary.Finalizations = append([]FinalizationReference(nil), manifest.Finalizations...)
	manifest.Extensions = map[string]json.RawMessage{"example.com/note": json.RawMessage(` { "retained" : true } `)}

	got, err := Encode(manifest)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	const want = `{"schema_version":3,"compiler_version":"canonical-capability-profile-compiler/v1.0.0","pack":{"id":"base","version":"1.0.0"},"input_id":"input-1","resolved_ir_id":"ir-1","profile":{"id":"default","version":"1.0.0","scope":"global"},"target":"pi","projections":[{"path":"skills/a.md","ownership":"replace","hash":"aaa"},{"path":"skills/b.md","ownership":"marker-merge","hash":"bbb"}],"rollback_boundary":{"id":"rb-v1","kind":"selected-target-backup-v1","target":"pi","resources":[{"path":"skills/a.md","ownership":"replace","hash":"aaa"},{"path":"skills/b.md","ownership":"marker-merge","hash":"bbb"}],"finalizations":[{"path":"skills/a.md","finalizer_id":"adapter","provider":"vault","slot":"primary"}]},"role_models":[{"role":"planner","model":"gpt-4","source":"project","location":"project:demo"}],"capabilities":[{"id":"skills","state":"supported","reason":"target matrix"}],"reconciliation":{"owner":"compiler","decision":"update"},"finalizations":[{"path":"skills/a.md","finalizer_id":"adapter","provider":"vault","slot":"primary"}],"extensions":{"example.com/note":{"retained":true}}}`
	if string(got) != want {
		t.Fatalf("Encode() = %s, want independently calculated %s", got, want)
	}
	decoded, err := Decode(got)
	if err != nil || string(decoded.Extensions["example.com/note"]) != `{"retained":true}` {
		t.Fatalf("Decode() provenance/extension = %#v, %v", decoded, err)
	}
	extensions := decoded.ExtensionsCopy()
	extensions["example.com/note"][0] = '['
	if string(decoded.ExtensionsCopy()["example.com/note"]) != `{"retained":true}` {
		t.Fatal("extension accessor leaked a mutable value")
	}
}

func TestV3RejectsUnsafeExtensionAndProvenanceCollisions(t *testing.T) {
	valid := validManifest()
	valid.RoleModels = []RoleModelProvenance{{Role: "planner", Model: "gpt-4", Source: "project", Location: "project:demo"}}
	valid.Capabilities = []CapabilityProvenance{{ID: "skills", State: "supported", Reason: "matrix"}}
	valid.Reconciliation = ReconciliationProvenance{Owner: "compiler", Decision: "noop"}
	valid.Finalizations = []FinalizationReference{{Path: "skills/a.md", FinalizerID: "adapter", Provider: "vault", Slot: "primary"}}
	valid.RollbackBoundary.Finalizations = append([]FinalizationReference(nil), valid.Finalizations...)
	for name, extensions := range map[string]map[string]json.RawMessage{
		"unnamespaced": {"note": json.RawMessage(`true`)},
		"reserved":     {"lore.dev/note": json.RawMessage(`true`)},
		"secret":       {"example.com/note": json.RawMessage(`{"token":"raw-secret"}`)},
	} {
		t.Run(name, func(t *testing.T) {
			valid.Extensions = extensions
			if _, err := Encode(valid); !errors.Is(err, CodeInvalidField) {
				t.Fatalf("Encode() error = %v, want invalid field", err)
			}
		})
	}
	first, second := valid, valid
	first.Extensions = map[string]json.RawMessage{"example.com/a": json.RawMessage(`1`), "example.com/b": json.RawMessage(`2`)}
	second.Extensions = map[string]json.RawMessage{"example.com/b": json.RawMessage(`2`), "example.com/a": json.RawMessage(`1`)}
	firstBytes, firstErr := Encode(first)
	secondBytes, secondErr := Encode(second)
	if firstErr != nil || secondErr != nil || string(firstBytes) != string(secondBytes) {
		t.Fatalf("extension map permutations were not stable: %v %v", firstErr, secondErr)
	}
	valid.Extensions = map[string]json.RawMessage{"example.com/note": json.RawMessage(`true`)}
	encoded, err := Encode(valid)
	if err != nil {
		t.Fatal(err)
	}
	duplicateExtension := strings.Replace(string(encoded), `}}`, `,"example.com/note":false}}`, 1)
	if got, err := Decode([]byte(duplicateExtension)); !reflect.DeepEqual(got, Manifest{}) || !errors.Is(err, CodeDuplicateField) {
		t.Fatalf("duplicate extension = %#v, %v", got, err)
	}
	valid.Extensions = nil
	valid.RoleModels = append(valid.RoleModels, valid.RoleModels[0])
	if _, err := Encode(valid); !errors.Is(err, CodeDuplicateRecord) {
		t.Fatalf("duplicate role provenance error = %v", err)
	}
}

const legacyV1Fixture = `{"schema_version":1,"compiler_version":"canonical-capability-profile-compiler/v1.0.0","pack":{"id":"base","version":"1.0.0"},"input_id":"input-1","resolved_ir_id":"ir-1","profile":{"id":"default","version":"1.0.0","scope":"global"},"target":"pi","projections":[{"path":"skills/a.md","ownership":"replace","hash":"aaa"},{"path":"skills/b.md","ownership":"marker-merge","hash":"bbb"}],"role_models":[{"role":"planner","model":"gpt-4","source":"project","location":"project:demo"}],"capabilities":[{"id":"skills","state":"supported","reason":"target matrix"}],"reconciliation":{"owner":"compiler","decision":"update"},"finalizations":[]}`
const legacyV2Fixture = `{"schema_version":2,"compiler_version":"canonical-capability-profile-compiler/v1.0.0","pack":{"id":"base","version":"1.0.0"},"input_id":"input-1","resolved_ir_id":"ir-1","profile":{"id":"default","version":"1.0.0","scope":"global"},"target":"pi","projections":[{"path":"skills/a.md","ownership":"replace","hash":"aaa"},{"path":"skills/b.md","ownership":"marker-merge","hash":"bbb"}],"role_models":[{"role":"planner","model":"gpt-4","source":"project","location":"project:demo"}],"capabilities":[{"id":"skills","state":"supported","reason":"target matrix"}],"reconciliation":{"owner":"compiler","decision":"update"},"finalizations":[]}`

func TestMigrateRejectsUnreconstructableLegacyBoundary(t *testing.T) {
	for _, fixture := range []string{legacyV1Fixture, legacyV2Fixture} {
		got, err := Migrate([]byte(fixture))
		var typed *CodecError
		if !reflect.DeepEqual(got, Manifest{}) || !errors.Is(err, CodeInvalidField) ||
			!errors.As(err, &typed) || typed.Path() != "rollback_boundary" {
			t.Fatalf("Migrate() = %#v, %#v; want zero invalid rollback boundary", got, err)
		}
		decision, reconcileErr := Reconcile([]byte(fixture), mustEncode(t, validManifest()))
		if decision != (Decision{}) || !errors.Is(reconcileErr, CodeInvalidField) {
			t.Fatalf("Reconcile(legacy) = %#v, %v", decision, reconcileErr)
		}
	}
}

func TestMigrateRejectsUnknownLossyAndAmbiguousLegacy(t *testing.T) {
	tests := map[string]struct {
		data string
		code ErrorCode
	}{
		"unknown schema":                 {strings.Replace(legacyV1Fixture, `"schema_version":1`, `"schema_version":9`, 1), CodeUnsupportedSchema},
		"unknown field":                  {strings.Replace(legacyV1Fixture, `,"target":"pi"`, `,"unknown":"value","target":"pi"`, 1), CodeUnknownField},
		"lossy missing provenance":       {strings.Replace(legacyV1Fixture, `,"role_models":[{"role":"planner","model":"gpt-4","source":"project","location":"project:demo"}]`, "", 1), CodeRequiredField},
		"ambiguous duplicate provenance": {strings.Replace(legacyV1Fixture, `}],"capabilities"`, `},{"role":"planner","model":"gpt-4","source":"project","location":"project:demo"}],"capabilities"`, 1), CodeDuplicateRecord},
	}
	for _, fixture := range []string{legacyV1Fixture, legacyV2Fixture} {
		for name, test := range tests {
			t.Run(fmt.Sprintf("v%d/%s", fixture[18]-'0', name), func(t *testing.T) {
				data := strings.Replace(test.data, `"schema_version":1`, `"schema_version":`+string(fixture[18]), 1)
				got, err := Migrate([]byte(data))
				if !reflect.DeepEqual(got, Manifest{}) || !errors.Is(err, test.code) {
					t.Fatalf("Migrate() = %#v, %v; want zero and %v", got, err, test.code)
				}
			})
		}
	}
}

func TestReconcileDecidesV3BoundaryStatesWithoutSecrets(t *testing.T) {
	desired, err := Encode(validManifest())
	if err != nil {
		t.Fatal(err)
	}
	conflict := validManifest()
	conflict.Projections[0].Ownership = "conflict"
	conflict.RollbackBoundary.Resources[0].Ownership = "conflict"
	conflictBytes, _ := Encode(conflict)
	updated := validManifest()
	updated.RollbackBoundary.ID = "rb-v2"
	updatedBytes, _ := Encode(updated)
	for _, test := range []struct {
		name, current, wantCode string
		outcome                 Outcome
	}{
		{"v3 noop", string(desired), "unchanged", OutcomeNoop},
		{"boundary update", string(desired), "desired_changed", OutcomeUpdate},
		{"conflict", string(conflictBytes), "ambiguous_ownership", OutcomeConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			next := desired
			if test.outcome == OutcomeUpdate {
				next = updatedBytes
			}
			got, err := Reconcile([]byte(test.current), next)
			if err != nil || got.Outcome != test.outcome || got.Explanation().Code != test.wantCode {
				t.Fatalf("Reconcile() = %#v, %v", got, err)
			}
			if !strings.HasPrefix(got.CurrentIdentity, "manifest-v3:") ||
				(test.outcome != OutcomeNoop && got.CurrentHash == got.DesiredHash) {
				t.Fatalf("identity/hash binding missing: %#v", got)
			}
		})
	}
	for _, bad := range [][]byte{[]byte(`{"schema_version":9}`), []byte(strings.Replace(string(desired), `,"target":"pi"`, `,"token":"secret","target":"pi"`, 1))} {
		got, err := Reconcile(bad, desired)
		if got != (Decision{}) || err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("Reconcile() invalid = %#v, %v", got, err)
		}
	}
}

func TestReconcileCanonicalizesPermutationsAndRetainsNoInput(t *testing.T) {
	first := validManifest()
	second := validManifest()
	second.Projections[0], second.Projections[1] = second.Projections[1], second.Projections[0]
	current, err := Encode(first)
	if err != nil {
		t.Fatal(err)
	}
	desired, err := Encode(second)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Reconcile(current, desired)
	if err != nil || got.Outcome != OutcomeNoop || got.CurrentIdentity != got.DesiredIdentity {
		t.Fatalf("Reconcile() = %#v, %v", got, err)
	}
	current[0] = '!'
	again, err := Reconcile(desired, desired)
	if err != nil || !reflect.DeepEqual(got, again) {
		t.Fatalf("Reconcile() retained mutable input: %#v, %v", again, err)
	}
}

func TestCodecCanonicalizesObjectAndSlicePermutations(t *testing.T) {
	first := validManifest()
	second := validManifest()
	second.Projections[0], second.Projections[1] = second.Projections[1], second.Projections[0]
	firstBytes, err := Encode(first)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := Encode(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstBytes) != string(secondBytes) {
		t.Fatalf("Encode() was not deterministic across slice permutations:\n%s\n%s", firstBytes, secondBytes)
	}

	permutedJSON := `{"target":"pi","profile":{"scope":"global","version":"1.0.0","id":"default"},"resolved_ir_id":"ir-1","input_id":"input-1","pack":{"version":"1.0.0","id":"base"},"compiler_version":"canonical-capability-profile-compiler/v1.0.0","schema_version":3,"projections":[{"hash":"bbb","ownership":"marker-merge","path":"skills/b.md"},{"hash":"aaa","ownership":"replace","path":"skills/a.md"}],"rollback_boundary":{"finalizations":[],"resources":[{"hash":"aaa","ownership":"replace","path":"skills/a.md"},{"hash":"bbb","ownership":"marker-merge","path":"skills/b.md"}],"target":"pi","kind":"selected-target-backup-v1","id":"rb-v1"},"role_models":[{"location":"project:demo","source":"project","model":"gpt-4","role":"planner"}],"capabilities":[{"reason":"target matrix","state":"supported","id":"skills"}],"reconciliation":{"decision":"update","owner":"compiler"},"finalizations":[]}`
	decoded, err := Decode([]byte(permutedJSON))
	if err != nil {
		t.Fatalf("Decode(permuted JSON) error = %v", err)
	}
	got, err := Encode(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(firstBytes) {
		t.Fatalf("Decode/Encode() = %s, want %s", got, firstBytes)
	}
}

func TestRollbackBoundaryValidationPathsAndZeroOutputs(t *testing.T) {
	base := validManifest()
	final := FinalizationReference{Path: "skills/a.md", FinalizerID: "adapter", Provider: "vault", Slot: "primary"}
	base.Finalizations = []FinalizationReference{final}
	base.RollbackBoundary.Finalizations = []FinalizationReference{final}
	cases := []struct {
		name, path string
		code       ErrorCode
		mutate     func(*Manifest)
	}{
		{"invalid id", "rollback_boundary.id", CodeInvalidField, func(m *Manifest) { m.RollbackBoundary.ID = "Bad ID" }},
		{"kind", "rollback_boundary.kind", CodeInvalidField, func(m *Manifest) { m.RollbackBoundary.Kind = "other" }},
		{"target", "rollback_boundary.target", CodeInvalidField, func(m *Manifest) { m.RollbackBoundary.Target = "codex" }},
		{"resource order", "rollback_boundary.resources[1].path", CodeInvalidField, func(m *Manifest) {
			m.RollbackBoundary.Resources[0], m.RollbackBoundary.Resources[1] = m.RollbackBoundary.Resources[1], m.RollbackBoundary.Resources[0]
		}},
		{"resource duplicate", "rollback_boundary.resources[1].path", CodeInvalidField, func(m *Manifest) { m.RollbackBoundary.Resources[1] = m.RollbackBoundary.Resources[0] }},
		{"resource ownership", "rollback_boundary.resources[0].ownership", CodeInvalidField, func(m *Manifest) { m.RollbackBoundary.Resources[0].Ownership = "additive" }},
		{"resource hash", "rollback_boundary.resources[0].hash", CodeInvalidField, func(m *Manifest) { m.RollbackBoundary.Resources[0].Hash = "changed" }},
		{"finalizer", "rollback_boundary.finalizations[0].provider", CodeInvalidField, func(m *Manifest) { m.RollbackBoundary.Finalizations[0].Provider = "other" }},
		{"unprojected finalizer", "rollback_boundary.finalizations[0].path", CodeInvalidField, func(m *Manifest) {
			m.Finalizations[0].Path, m.RollbackBoundary.Finalizations[0].Path = "other", "other"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := base
			m.Projections = append([]Projection(nil), base.Projections...)
			m.Finalizations = append([]FinalizationReference(nil), base.Finalizations...)
			m.RollbackBoundary.Resources = append([]Projection(nil), base.RollbackBoundary.Resources...)
			m.RollbackBoundary.Finalizations = append([]FinalizationReference(nil), base.RollbackBoundary.Finalizations...)
			tc.mutate(&m)
			got, err := Encode(m)
			if got != nil {
				t.Fatalf("Encode() = %s, want nil", got)
			}
			assertCodec(t, err, tc.code, tc.path)
		})
	}

	valid := string(mustEncode(t, base))
	for _, tc := range []struct {
		name, data, path string
		code             ErrorCode
	}{
		{"missing boundary", strings.Replace(valid, `,"rollback_boundary":`+boundaryJSON(base.RollbackBoundary), "", 1), "rollback_boundary", CodeRequiredField},
		{"null boundary", strings.Replace(valid, `"rollback_boundary":`+boundaryJSON(base.RollbackBoundary), `"rollback_boundary":null`, 1), "rollback_boundary", CodeInvalidField},
		{"missing id", strings.Replace(valid, `"id":"rb-v1",`, "", 1), "rollback_boundary.id", CodeRequiredField},
		{"wrong id type", strings.Replace(valid, `"id":"rb-v1"`, `"id":7`, 1), "rollback_boundary.id", CodeInvalidField},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Decode([]byte(tc.data))
			if !reflect.DeepEqual(got, Manifest{}) {
				t.Fatalf("Decode() = %#v, want zero", got)
			}
			assertCodec(t, err, tc.code, tc.path)
		})
	}
}

func TestDecodeAndReconcileRejectRollbackBoundaryNulls(t *testing.T) {
	base := validManifest()
	final := FinalizationReference{Path: "skills/a.md", FinalizerID: "adapter", Provider: "vault", Slot: "primary"}
	base.Finalizations, base.RollbackBoundary.Finalizations = []FinalizationReference{final}, []FinalizationReference{final}
	valid := string(mustEncode(t, base))
	prefix, boundary, _ := strings.Cut(valid, `"rollback_boundary":`)
	check := func(name, body string, code ErrorCode, path string) {
		t.Run(name, func(t *testing.T) {
			data := []byte(prefix + `"rollback_boundary":` + body)
			got, err := Decode(data)
			if !reflect.DeepEqual(got, Manifest{}) {
				t.Fatalf("Decode() = %#v, want zero", got)
			}
			assertCodec(t, err, code, path)
			decision, err := Reconcile(data, []byte(valid))
			if decision != (Decision{}) {
				t.Fatalf("Reconcile() = %#v, want zero", decision)
			}
			assertCodec(t, err, code, path)
		})
	}
	nulls := []struct{ name, old, new, path string }{
		{"id", `"id":"rb-v1"`, `"id":null`, "rollback_boundary.id"}, {"kind", `"kind":"selected-target-backup-v1"`, `"kind":null`, "rollback_boundary.kind"}, {"target", `"target":"pi"`, `"target":null`, "rollback_boundary.target"},
		{"resources list", `"resources":[{"path":"skills/a.md","ownership":"replace","hash":"aaa"},{"path":"skills/b.md","ownership":"marker-merge","hash":"bbb"}]`, `"resources":null`, "rollback_boundary.resources"}, {"finalizations list", `"finalizations":[{"path":"skills/a.md","finalizer_id":"adapter","provider":"vault","slot":"primary"}]`, `"finalizations":null`, "rollback_boundary.finalizations"},
		{"resource record", `"resources":[{`, `"resources":[null,{`, "rollback_boundary.resources[0]"}, {"resource path", `"resources":[{"path":"skills/a.md"`, `"resources":[{"path":null`, "rollback_boundary.resources[0].path"}, {"resource ownership", `"ownership":"replace"`, `"ownership":null`, "rollback_boundary.resources[0].ownership"}, {"resource hash", `"hash":"aaa"`, `"hash":null`, "rollback_boundary.resources[0].hash"},
		{"finalization record", `"finalizations":[{`, `"finalizations":[null,{`, "rollback_boundary.finalizations[0]"}, {"finalization path", `"finalizations":[{"path":"skills/a.md"`, `"finalizations":[{"path":null`, "rollback_boundary.finalizations[0].path"}, {"finalizer id", `"finalizer_id":"adapter"`, `"finalizer_id":null`, "rollback_boundary.finalizations[0].finalizer_id"}, {"provider", `"provider":"vault"`, `"provider":null`, "rollback_boundary.finalizations[0].provider"}, {"slot", `"slot":"primary"`, `"slot":null`, "rollback_boundary.finalizations[0].slot"},
	}
	for _, tc := range nulls {
		check("null "+tc.name, strings.Replace(boundary, tc.old, tc.new, 1), CodeInvalidField, tc.path)
	}
	missing := []struct{ name, old, new, path string }{
		{"id", `"id":"rb-v1",`, ``, "rollback_boundary.id"}, {"kind", `"kind":"selected-target-backup-v1",`, ``, "rollback_boundary.kind"}, {"target", `"target":"pi",`, ``, "rollback_boundary.target"}, {"resources", `"resources":[{"path":"skills/a.md","ownership":"replace","hash":"aaa"},{"path":"skills/b.md","ownership":"marker-merge","hash":"bbb"}],`, ``, "rollback_boundary.resources"}, {"finalizations", `,"finalizations":[{"path":"skills/a.md","finalizer_id":"adapter","provider":"vault","slot":"primary"}]`, ``, "rollback_boundary.finalizations"},
		{"resource path", `"path":"skills/a.md",`, ``, "rollback_boundary.resources[0].path"}, {"resource ownership", `,"ownership":"replace"`, ``, "rollback_boundary.resources[0].ownership"}, {"resource hash", `,"hash":"aaa"`, ``, "rollback_boundary.resources[0].hash"}, {"finalization path", `"finalizations":[{"path":"skills/a.md",`, `"finalizations":[{`, "rollback_boundary.finalizations[0].path"}, {"finalizer id", `,"finalizer_id":"adapter"`, ``, "rollback_boundary.finalizations[0].finalizer_id"}, {"provider", `,"provider":"vault"`, ``, "rollback_boundary.finalizations[0].provider"}, {"slot", `,"slot":"primary"`, ``, "rollback_boundary.finalizations[0].slot"},
	}
	for _, tc := range missing {
		check("missing "+tc.name, strings.Replace(boundary, tc.old, tc.new, 1), CodeRequiredField, tc.path)
	}
}

func TestEncodePreservesDeepCallerGraphAndDefensiveCopies(t *testing.T) {
	m := validManifest()
	m.Projections[0], m.Projections[1] = m.Projections[1], m.Projections[0]
	m.RoleModels = []RoleModelProvenance{{Role: "planner", Model: "p", Source: "project", Location: "p"}, {Role: "analyst", Model: "a", Source: "global", Location: "g"}}
	m.Capabilities = []CapabilityProvenance{{ID: "z", State: "supported", Reason: "z"}, {ID: "a", State: "unknown", Reason: "a"}}
	m.Finalizations = []FinalizationReference{{Path: "skills/b.md", FinalizerID: "b", Provider: "p", Slot: "b"}, {Path: "skills/a.md", FinalizerID: "a", Provider: "p", Slot: "a"}}
	m.RollbackBoundary.Finalizations = []FinalizationReference{m.Finalizations[1], m.Finalizations[0]}
	m.Migrations = []Migration{{FromSchema: 2, ToSchema: 3, ID: "manifest-v2-to-v3"}, {FromSchema: 1, ToSchema: 3, ID: "manifest-v1-to-v3"}}
	m.Extensions = map[string]json.RawMessage{"example.com/note": json.RawMessage(` {"nested":[{"ok":true}]} `)}
	before, _ := json.Marshal(m)
	first, err := Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Encode(m)
	if err != nil || string(first) != string(second) {
		t.Fatalf("repeated Encode = %v, %s / %s", err, first, second)
	}
	after, _ := json.Marshal(m)
	if string(before) != string(after) {
		t.Fatalf("caller graph mutated\nbefore=%s\nafter=%s", before, after)
	}

	decoded, err := Decode(first)
	if err != nil {
		t.Fatal(err)
	}
	roles, capabilities := decoded.RoleModelsCopy(), decoded.CapabilitiesCopy()
	finalizations, migrations := decoded.FinalizationsCopy(), decoded.MigrationsCopy()
	boundary := decoded.RollbackBoundaryCopy()
	roles[0].Role, capabilities[0].ID, finalizations[0].Path = "x", "x", "x"
	migrations[0].ID, boundary.Resources[0].Path = "x", "x"
	if decoded.RoleModels[0].Role == "x" || decoded.Capabilities[0].ID == "x" || decoded.Finalizations[0].Path == "x" || decoded.Migrations[0].ID == "x" || decoded.RollbackBoundary.Resources[0].Path == "x" {
		t.Fatal("defensive copy accessor leaked mutable storage")
	}
	m.Extensions["example.com/note"] = json.RawMessage(`{"safe":["Bearer rejected"]}`)
	beforeFailure, _ := json.Marshal(m)
	for range 2 {
		if got, err := Encode(m); got != nil || !errors.Is(err, CodeInvalidField) {
			t.Fatalf("failing Encode() = %s, %v", got, err)
		}
	}
	afterFailure, _ := json.Marshal(m)
	if string(beforeFailure) != string(afterFailure) {
		t.Fatal("failing Encode mutated caller graph")
	}
}

func TestRecursiveSecretExtensionsRejectAllAPIsWithRedaction(t *testing.T) {
	const payload = "independently-supplied-value"
	for _, tc := range []struct{ name, raw, path string }{
		{"nested key", `{"safe":[{"credential":"` + payload + `"}]}`, `extensions["example.com/note"].safe[0].credential`},
		{"bearer scalar", `{"safe":["Bearer ` + payload + `"]}`, `extensions["example.com/note"].safe[0]`},
		{"basic scalar", `{"safe":["bAsIc\t` + payload + `"]}`, `extensions["example.com/note"].safe[0]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := validManifest()
			m.Extensions = map[string]json.RawMessage{"example.com/note": json.RawMessage(tc.raw)}
			encoded, err := Encode(m)
			if encoded != nil {
				t.Fatalf("Encode() = %s", encoded)
			}
			assertRedacted(t, err, tc.path, payload)
		})
	}
	good := validManifest()
	good.Extensions = map[string]json.RawMessage{"example.com/note": json.RawMessage(`{"auth":["bearerish value","Basic","ordinary"]}`)}
	goodBytes := mustEncode(t, good)
	badV3 := strings.Replace(string(goodBytes), `{"auth":["bearerish value","Basic","ordinary"]}`, `{"safe":["Bearer `+payload+`"]}`, 1)
	if got, err := Decode([]byte(badV3)); !reflect.DeepEqual(got, Manifest{}) {
		t.Fatalf("Decode() = %#v", got)
	} else {
		assertRedacted(t, err, `extensions["example.com/note"].safe[0]`, payload)
	}
	badLegacy := strings.TrimSuffix(legacyV1Fixture, "}") + `,"extensions":{"example.com/note":{"safe":["Bearer ` + payload + `"]}}}`
	if got, err := Migrate([]byte(badLegacy)); !reflect.DeepEqual(got, Manifest{}) {
		t.Fatalf("Migrate() = %#v", got)
	} else {
		assertRedacted(t, err, `extensions["example.com/note"].safe[0]`, payload)
	}
	if got, err := Reconcile([]byte(badV3), goodBytes); got != (Decision{}) {
		t.Fatalf("Reconcile() = %#v", got)
	} else {
		assertRedacted(t, err, `extensions["example.com/note"].safe[0]`, payload)
	}
}

func mustEncode(t *testing.T, m Manifest) []byte {
	t.Helper()
	got, err := Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func boundaryJSON(b RollbackBoundary) string { got, _ := json.Marshal(b); return string(got) }
func assertCodec(t *testing.T, err error, code ErrorCode, path string) {
	t.Helper()
	var typed *CodecError
	if !errors.Is(err, code) || !errors.As(err, &typed) || typed.Code() != code || typed.Path() != path {
		t.Fatalf("error = %#v, want %s at %s", err, code, path)
	}
}
func assertRedacted(t *testing.T, err error, path, payload string) {
	t.Helper()
	assertCodec(t, err, CodeInvalidField, path)
	if strings.Contains(fmt.Sprint(err), payload) {
		t.Fatalf("error disclosed rejected payload: %v", err)
	}
}
