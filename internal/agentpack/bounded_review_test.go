package agentpack

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestBoundedReviewPairIsCompleteRuntimeCompatibleAndDeterministic(t *testing.T) {
	first, second := BoundedReviewPair(), BoundedReviewPair()
	if err := ValidateBoundedReviewPair(first); err != nil {
		t.Fatalf("validate canonical pair: %v", err)
	}
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first pair: %v", err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second pair: %v", err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatal("bounded review pair is not deterministic")
	}

	assertBoundedReviewRuntimePair(t, boundedReviewRuntimePair{
		Judge: boundedReviewProductionRuntimeAsset(first.Judge),
		Fix:   boundedReviewProductionRuntimeAsset(first.Fix),
	}, "compatible")
	if got := BoundedReviewStaticContractDigest(); got != "cc888a92b03cc2f4bed8abd0a5efe1bd010d94d1ff6a56cfbeda82c65ed57283" {
		t.Fatalf("static contract digest = %q", got)
	}
}

func TestBoundedReviewManifestBindsCompleteOrderedPair(t *testing.T) {
	pair := BoundedReviewPair()
	manifest := pair.Manifest
	if got, want := len(manifest.Roles), 2; got != want {
		t.Fatalf("manifest roles = %d, want %d", got, want)
	}
	for index, role := range manifest.Roles {
		if role.Role != []string{BoundedReviewJudgeRole, BoundedReviewFixRole}[index] || role.BodySHA256 == "" || role.Marker == "" {
			t.Fatalf("manifest role[%d] malformed: %+v", index, role)
		}
		if role.Handshake.ManifestRevision != pair.Revision || role.Handshake.StaticContractDigest != BoundedReviewStaticContractDigest() {
			t.Fatalf("manifest role[%d] runtime metadata drifted: %+v", index, role.Handshake)
		}
	}
}

func TestBoundedReviewRoleIsolationAndResultSchemas(t *testing.T) {
	pair := BoundedReviewPair()
	judge, fix := pair.Judge.Body, pair.Fix.Body
	for _, required := range []string{"Blind and read-only", "evidence", "location", "fix_intent", "CRITICAL", "WARNING with warning_kind real|theoretical", "SUGGESTION", "schema_version", "capability", "scope_digest", "No extra fields"} {
		if !strings.Contains(judge, required) {
			t.Errorf("judge body missing %q", required)
		}
	}
	for _, forbidden := range []string{"Judge A", "Judge B", "lifecycle", "terminal", "applied_changes"} {
		if strings.Contains(judge, forbidden) {
			t.Errorf("judge body leaks forbidden authority/context %q", forbidden)
		}
	}
	for _, required := range []string{"Approved allowlist", "real WARNING", "SUGGESTION", "suspects", "theoretical", "contradictions", "No unrelated refactors", "\"applied\"", "\"unapplied\"", "fresh blind Judge pair"} {
		if !strings.Contains(fix, required) {
			t.Errorf("fix body missing %q", required)
		}
	}
	for _, forbidden := range []string{"Judge A", "Judge B", "terminal"} {
		if strings.Contains(fix, forbidden) {
			t.Errorf("fix body leaks forbidden authority/context %q", forbidden)
		}
	}
}

func TestValidateBoundedReviewPairRejectsMalformedDuplicateAndSkewPairs(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*BoundedReviewContractPair)
	}{
		{"incomplete_body", func(pair *BoundedReviewContractPair) { pair.Fix.Body = "" }},
		{"duplicate_role", func(pair *BoundedReviewContractPair) {
			pair.Fix.Role = BoundedReviewJudgeRole
			pair.Fix.Handshake.Role = BoundedReviewJudgeRole
		}},
		{"duplicate_capability", func(pair *BoundedReviewContractPair) {
			pair.Fix.Handshake.Capabilities = append(pair.Fix.Handshake.Capabilities, "judge-v1")
		}},
		{"severity_order", func(pair *BoundedReviewContractPair) { pair.Fix.Handshake.SeverityVocabulary[0] = "WARNING (real)" }},
		{"revision_skew", func(pair *BoundedReviewContractPair) { pair.Fix.Handshake.ManifestRevision++ }},
		{"manifest_missing_role", func(pair *BoundedReviewContractPair) { pair.Manifest.Roles = pair.Manifest.Roles[:1] }},
		{"manifest_hash_tamper", func(pair *BoundedReviewContractPair) { pair.Manifest.Roles[1].BodySHA256 = strings.Repeat("0", 64) }},
		{"dynamic_placeholder", func(pair *BoundedReviewContractPair) { pair.Judge.Body += "{{target}}" }},
		{"absolute_path", func(pair *BoundedReviewContractPair) { pair.Fix.Body += "/Users/example/private" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			pair := BoundedReviewPair()
			test.mutate(&pair)
			if err := ValidateBoundedReviewPair(pair); err == nil {
				t.Fatal("invalid pair was accepted")
			}
		})
	}
}

func TestBoundedReviewStaticMaterialHasNoDynamicSensitivePlaceholders(t *testing.T) {
	pair := BoundedReviewPair()
	for _, asset := range []BoundedReviewAsset{pair.Judge, pair.Fix} {
		if strings.Contains(asset.Body, "\r\n") || !strings.HasSuffix(asset.Body, "\n") {
			t.Fatalf("%s body newline form is not stable", asset.Role)
		}
		for _, forbidden := range []string{"{{", "}}", "/Users/", "C:\\", "<target>", "<diff>", "<findings>", "<standards>"} {
			if strings.Contains(strings.ToLower(asset.Body), strings.ToLower(forbidden)) {
				t.Errorf("%s static body contains dynamic/sensitive marker %q", asset.Role, forbidden)
			}
		}
	}
}

const (
	boundedReviewInvariantsPath = "testdata/bounded_review_invariants.json"
	boundedReviewSemanticsPath  = "testdata/bounded_review_semantics.json"
)

type boundedReviewInvariantFixture struct {
	Version               int    `json:"version"`
	ResultValidatorSource string `json:"result_validator_source"`
	Canonical             struct {
		Family                string   `json:"family"`
		ProjectionID          string   `json:"projection_id"`
		ContractVersion       string   `json:"contract_version"`
		Harness               string   `json:"harness"`
		ResultSchemaVersion   string   `json:"result_schema_version"`
		StaticContractVersion string   `json:"static_contract_version"`
		StaticContractDigest  string   `json:"static_contract_digest"`
		ManifestRevision      int      `json:"manifest_revision"`
		Roles                 []string `json:"roles"`
		Capabilities          []string `json:"capabilities"`
		SeverityVocabulary    []string `json:"severity_vocabulary"`
		Fallback              bool     `json:"fallback"`
	} `json:"canonical"`
	CompatibilityCases []struct {
		Name     string `json:"name"`
		Class    string `json:"class"`
		Expected string `json:"expected"`
	} `json:"compatibility_cases"`
}

type boundedReviewSemanticRoleFixture struct {
	Role            string   `json:"role"`
	RequiredMarkers []string `json:"required_markers"`
	ForbiddenText   []string `json:"forbidden_text"`
	SemanticRules   []string `json:"semantic_rules"`
}

type boundedReviewSemanticsFixture struct {
	Version int `json:"version"`
	Privacy struct {
		StaticOnly    bool     `json:"static_only"`
		ForbiddenText []string `json:"forbidden_text"`
	} `json:"privacy"`
	Judge             boundedReviewSemanticRoleFixture `json:"judge"`
	Fix               boundedReviewSemanticRoleFixture `json:"fix"`
	RejectedMutations []struct {
		Name         string `json:"name"`
		Role         string `json:"role"`
		RemoveMarker string `json:"remove_marker"`
		AppendText   string `json:"append_text"`
		Expected     string `json:"expected"`
	} `json:"rejected_mutations"`
	DeterministicOrder struct {
		Roles              []string `json:"roles"`
		SeverityVocabulary []string `json:"severity_vocabulary"`
		ManifestRoles      []string `json:"manifest_roles"`
	} `json:"deterministic_order"`
}

func TestBoundedReviewInvariantFixtureMatchesCanonicalEmissionAndRuntimeMirror(t *testing.T) {
	fixture := loadBoundedReviewInvariantFixture(t)
	if fixture.ResultValidatorSource != "lore-pi-runtime/src/runtime/judgment-review-envelopes.ts@c48d306f66f125903aca93f5969747ff6e6e5605" {
		t.Fatalf("invariant result-validator authority drifted: %q", fixture.ResultValidatorSource)
	}
	pair := BoundedReviewPair()
	vectors := loadBoundedReviewResultVectors(t)
	if !validatePinnedJudgeResult(extractBoundedReviewSchemaPayload(t, pair.Judge.Body), vectors.ExpectedIdentity) || !validatePinnedFixResult(extractBoundedReviewSchemaPayload(t, pair.Fix.Body), vectors.ExpectedIdentity) {
		t.Fatal("invariant fixture requires authority-shaped result acceptance, not handshake/prose markers alone")
	}
	canonical := fixture.Canonical
	if canonical.Family != BoundedReviewFamily || canonical.ProjectionID != BoundedReviewProjectionID || canonical.ContractVersion != BoundedReviewContractVersion || canonical.Harness != BoundedReviewTarget || canonical.ResultSchemaVersion != BoundedReviewResultSchemaVersion || canonical.StaticContractVersion != BoundedReviewStaticContractVersion || canonical.StaticContractDigest != BoundedReviewStaticContractDigest() || canonical.ManifestRevision != BoundedReviewManifestRevision || canonical.Fallback {
		t.Fatalf("canonical invariant fixture drifted from lore-cli emission: %+v", canonical)
	}
	if !reflect.DeepEqual(canonical.Roles, []string{pair.Judge.Role, pair.Fix.Role}) || !reflect.DeepEqual(canonical.Capabilities, BoundedReviewCapabilities) || !reflect.DeepEqual(canonical.SeverityVocabulary, BoundedReviewSeverities) {
		t.Fatalf("canonical invariant ordering drifted: %+v", canonical)
	}

	want := map[string]string{
		"canonical_pair": "compatible", "projection_id_content_relaxed": "compatible", "missing_judge": "partial_asset", "missing_fix": "partial_asset", "duplicated_role": "malformed", "version_skew": "version_skew", "unsupported_version": "unsupported_version", "capability_superset_relaxed": "compatible", "missing_capability": "missing_capability", "duplicated_capability": "malformed", "missing_schema": "malformed", "wrong_schema": "malformed", "duplicated_severity": "malformed", "severity_order_skew": "malformed", "severity_vocabulary_skew": "malformed", "missing_digest": "malformed", "malformed_digest": "malformed", "stale_digest": "stale_digest", "missing_target": "target_mismatch", "wrong_target": "target_mismatch", "fallback_true": "fallback_only", "manifest_revision_skew": "stale_manifest", "malformed_metadata_unknown_field": "malformed", "malformed_metadata_missing_field": "malformed",
	}
	for _, tc := range fixture.CompatibilityCases {
		if tc.Name == "projection_id_non_string" {
			if tc.Class != "rejected" || tc.Expected != "malformed" {
				t.Fatalf("projection_id type boundary fixture = %+v", tc)
			}
			continue // The Go mirror is typed; the pinned TypeScript runtime owns this decode-time check.
		}
		got, ok := want[tc.Name]
		if !ok || got != tc.Expected {
			t.Fatalf("fixture case %q expected=%q, mirror contract=%q", tc.Name, tc.Expected, got)
		}
		assertBoundedReviewRuntimePair(t, boundedReviewFixtureRuntimeMutation(tc.Name), tc.Expected)
	}
}

func TestBoundedReviewSemanticFixtureEnforcesParityWithoutGeneralProseMatching(t *testing.T) {
	fixture := loadBoundedReviewSemanticsFixture(t)
	pair := BoundedReviewPair()
	roles := map[string]struct {
		body string
		rule boundedReviewSemanticRoleFixture
	}{
		"judge": {pair.Judge.Body, fixture.Judge},
		"fix":   {pair.Fix.Body, fixture.Fix},
	}
	for name, role := range roles {
		if !validBoundedReviewSemanticBody(role.body, role.rule, fixture.Privacy.ForbiddenText) {
			t.Fatalf("%s body does not satisfy the reviewed semantic fixture", name)
		}
		if len(role.rule.SemanticRules) == 0 {
			t.Fatalf("%s fixture must state semantic parity rules", name)
		}
	}
	vectors := loadBoundedReviewResultVectors(t)
	if !validatePinnedJudgeResult(extractBoundedReviewSchemaPayload(t, pair.Judge.Body), vectors.ExpectedIdentity) || !validatePinnedFixResult(extractBoundedReviewSchemaPayload(t, pair.Fix.Body), vectors.ExpectedIdentity) {
		t.Fatal("semantic acceptance requires authority-shaped child-result acceptance, not prose markers alone")
	}
	if !fixture.Privacy.StaticOnly || !reflect.DeepEqual(fixture.DeterministicOrder.Roles, []string{pair.Judge.Role, pair.Fix.Role}) || !reflect.DeepEqual(fixture.DeterministicOrder.ManifestRoles, []string{pair.Manifest.Roles[0].Role, pair.Manifest.Roles[1].Role}) || !reflect.DeepEqual(fixture.DeterministicOrder.SeverityVocabulary, BoundedReviewSeverities) {
		t.Fatal("semantic fixture lost static-only or deterministic ordering invariants")
	}
	for _, mutation := range fixture.RejectedMutations {
		role, ok := roles[mutation.Role]
		if !ok || mutation.Expected != "rejected" {
			t.Fatalf("invalid semantic mutation fixture: %+v", mutation)
		}
		body := strings.ReplaceAll(role.body, mutation.RemoveMarker, "") + mutation.AppendText
		if validBoundedReviewSemanticBody(body, role.rule, fixture.Privacy.ForbiddenText) {
			t.Fatalf("semantic mutation %q was accepted", mutation.Name)
		}
	}
}

func loadBoundedReviewInvariantFixture(t *testing.T) boundedReviewInvariantFixture {
	t.Helper()
	data, err := os.ReadFile(boundedReviewInvariantsPath)
	if err != nil {
		t.Fatalf("read invariant fixture: %v", err)
	}
	var fixture boundedReviewInvariantFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse invariant fixture: %v", err)
	}
	return fixture
}

func loadBoundedReviewSemanticsFixture(t *testing.T) boundedReviewSemanticsFixture {
	t.Helper()
	data, err := os.ReadFile(boundedReviewSemanticsPath)
	if err != nil {
		t.Fatalf("read semantics fixture: %v", err)
	}
	var fixture boundedReviewSemanticsFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse semantics fixture: %v", err)
	}
	return fixture
}

func validBoundedReviewSemanticBody(body string, role boundedReviewSemanticRoleFixture, privacyForbidden []string) bool {
	for _, marker := range role.RequiredMarkers {
		if !strings.Contains(body, marker) {
			return false
		}
	}
	for _, forbidden := range append(append([]string(nil), privacyForbidden...), role.ForbiddenText...) {
		if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) {
			return false
		}
	}
	return true
}

func boundedReviewFixtureRuntimeMutation(name string) boundedReviewRuntimePair {
	pair := boundedReviewRuntimeGoldenPair()
	switch name {
	case "canonical_pair":
	case "projection_id_content_relaxed":
		pair.Fix.ProjectionID = "runtime-accepted-id"
	case "missing_judge":
		pair.Judge = nil
	case "missing_fix":
		pair.Fix = nil
	case "duplicated_role":
		pair.Fix.Role = "judge"
	case "version_skew":
		pair.Fix.ContractVersion = "1.1.0"
	case "unsupported_version":
		pair.Judge.ContractVersion, pair.Fix.ContractVersion = "2.0.0", "2.0.0"
	case "capability_superset_relaxed":
		pair.Fix.Capabilities = append(pair.Fix.Capabilities, "future-capability-v1")
	case "missing_capability":
		pair.Fix.Capabilities = withoutBoundedReviewString(pair.Fix.Capabilities, "judge-v1")
	case "duplicated_capability":
		pair.Fix.Capabilities = append(pair.Fix.Capabilities, "native-session-v1")
	case "missing_schema":
		pair.Fix.ResultSchemaVersion = ""
	case "wrong_schema":
		pair.Fix.ResultSchemaVersion = "wrong-schema"
	case "duplicated_severity":
		pair.Fix.SeverityVocabulary[3] = "CRITICAL"
	case "severity_order_skew":
		pair.Fix.SeverityVocabulary[0], pair.Fix.SeverityVocabulary[1] = pair.Fix.SeverityVocabulary[1], pair.Fix.SeverityVocabulary[0]
	case "severity_vocabulary_skew":
		pair.Fix.SeverityVocabulary[3] = "INFO"
	case "missing_digest":
		pair.Fix.StaticContractDigest = ""
	case "malformed_digest":
		pair.Fix.StaticContractDigest = "not-a-digest"
	case "stale_digest":
		pair.Fix.StaticContractDigest = strings.Repeat("b", 64)
	case "missing_target":
		pair.Fix.Harness = ""
	case "wrong_target":
		pair.Fix.Harness = "codex"
	case "fallback_true":
		pair.Fix.Fallback = true
	case "manifest_revision_skew":
		pair.Fix.ManifestRevision = 2
	case "malformed_metadata_unknown_field":
		pair.Judge.Keys = append(pair.Judge.Keys, "unexpected")
	case "malformed_metadata_missing_field":
		pair.Judge.Keys = pair.Judge.Keys[:len(pair.Judge.Keys)-1]
	default:
		panic("unknown bounded-review fixture mutation: " + name)
	}
	return pair
}

func boundedReviewProductionRuntimeAsset(asset BoundedReviewAsset) *boundedReviewRuntimeAsset {
	handshake := asset.Handshake
	return &boundedReviewRuntimeAsset{
		Family: handshake.Family, Role: handshake.Role, ContractVersion: handshake.ContractVersion,
		ProjectionID: handshake.ProjectionID, Harness: handshake.Harness,
		Capabilities: append([]string(nil), handshake.Capabilities...), ResultSchemaVersion: handshake.ResultSchemaVersion,
		SeverityVocabulary: append([]string(nil), handshake.SeverityVocabulary...), Fallback: handshake.Fallback,
		StaticContractDigest: handshake.StaticContractDigest, StaticContractVersion: handshake.StaticContractVersion,
		ManifestRevision: handshake.ManifestRevision, Keys: append([]string(nil), boundedReviewRuntimeKeys...),
	}
}
