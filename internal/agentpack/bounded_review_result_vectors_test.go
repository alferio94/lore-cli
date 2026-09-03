package agentpack

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"unicode/utf16"
)

const boundedReviewResultVectorsPath = "testdata/bounded_review_result_vectors.json"

type boundedReviewResultVectorFixture struct {
	Version          int                   `json:"version"`
	RuntimeSource    string                `json:"runtime_source"`
	ExpectedIdentity boundedReviewIdentity `json:"expected_identity"`
	Cases            []boundedReviewVector `json:"cases"`
}

type boundedReviewIdentity struct {
	ReviewID    string `json:"review_id"`
	Round       int    `json:"round"`
	ScopeDigest string `json:"scope_digest"`
}

type boundedReviewVector struct {
	Name        string                 `json:"name"`
	Role        string                 `json:"role"`
	Payload     map[string]any         `json:"payload"`
	Accepted    bool                   `json:"accepted"`
	RepeatArray *boundedReviewRepeated `json:"repeat_array,omitempty"`
	Authorized  *bool                  `json:"write_authorized,omitempty"`
	Policy      string                 `json:"policy,omitempty"`
}

type boundedReviewRepeated struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
}

// TestBoundedReviewResultVectors mirrors validateJudgeResult/validateFixResult
// from the pinned runtime source. This is deliberately test-only: it is an
// actionable cross-language drift guard, not a lore-cli runtime validator.
func TestBoundedReviewResultVectors(t *testing.T) {
	fixture := loadBoundedReviewResultVectors(t)
	if fixture.Version != 1 || fixture.RuntimeSource != "lore-pi-runtime/src/runtime/judgment-review-envelopes.ts@c48d306f66f125903aca93f5969747ff6e6e5605" {
		t.Fatalf("result-vector authority metadata drifted: version=%d source=%q", fixture.Version, fixture.RuntimeSource)
	}
	if len(fixture.Cases) < 50 {
		t.Fatalf("result vectors=%d, want broad validator branch coverage", len(fixture.Cases))
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			payload := cloneBoundedReviewPayload(t, tc.Payload)
			if tc.RepeatArray != nil {
				values, ok := payload[tc.RepeatArray.Path].([]any)
				if !ok || len(values) != 1 || tc.RepeatArray.Count <= 1 {
					t.Fatalf("invalid repeat vector: %+v", tc.RepeatArray)
				}
				repeated := make([]any, tc.RepeatArray.Count)
				for i := range repeated {
					repeated[i] = values[0]
				}
				payload[tc.RepeatArray.Path] = repeated
			}
			var got bool
			switch tc.Role {
			case "judge":
				got = validatePinnedJudgeResult(payload, fixture.ExpectedIdentity)
			case "fix":
				got = validatePinnedFixResult(payload, fixture.ExpectedIdentity)
			default:
				t.Fatalf("unknown role %q", tc.Role)
			}
			if got != tc.Accepted {
				t.Fatalf("pinned runtime-equivalent acceptance=%t, want %t", got, tc.Accepted)
			}
			if tc.Authorized != nil {
				if tc.Policy != "lore-cli canonical policy" {
					t.Fatal("write authorization must be labeled as lore-cli canonical policy, not runtime acceptance")
				}
				if *tc.Authorized {
					t.Fatal("only non-write authorization vectors belong in this fixture")
				}
			}
		})
	}
}

func TestBoundedReviewBodiesDeclareAuthorityAcceptedResults(t *testing.T) {
	fixture := loadBoundedReviewResultVectors(t)
	pair := BoundedReviewPair()
	for _, asset := range []BoundedReviewAsset{pair.Judge, pair.Fix} {
		payload := extractBoundedReviewSchemaPayload(t, asset.Body)
		if asset.Role == BoundedReviewJudgeRole && !validatePinnedJudgeResult(payload, fixture.ExpectedIdentity) {
			t.Fatal("Judge static result declaration is not accepted by pinned runtime validator")
		}
		if asset.Role == BoundedReviewFixRole && !validatePinnedFixResult(payload, fixture.ExpectedIdentity) {
			t.Fatal("Fix static result declaration is not accepted by pinned runtime validator")
		}
	}
	for _, want := range []string{
		"SUGGESTION, theoretical WARNING, suspects, and contradictions never authorize writes",
		"Every code-changing Fix needs a fresh blind Judge pair.",
	} {
		if !strings.Contains(pair.Fix.Body, want) {
			t.Fatalf("Fix lost non-write/fresh-pair policy %q", want)
		}
	}
}

func loadBoundedReviewResultVectors(t *testing.T) boundedReviewResultVectorFixture {
	t.Helper()
	data, err := os.ReadFile(boundedReviewResultVectorsPath)
	if err != nil {
		t.Fatalf("read result vectors: %v", err)
	}
	var fixture boundedReviewResultVectorFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse result vectors: %v", err)
	}
	return fixture
}

func cloneBoundedReviewPayload(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var cloned map[string]any
	if err := json.Unmarshal(data, &cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}

func extractBoundedReviewSchemaPayload(t *testing.T, body string) map[string]any {
	t.Helper()
	start := strings.Index(body, "{\"")
	end := strings.Index(body[start:], "\n")
	if start < 0 || end < 0 {
		t.Fatal("missing one-line result JSON declaration")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(body[start:start+end]), &payload); err != nil {
		t.Fatalf("parse declared result JSON: %v", err)
	}
	return payload
}

var boundedReviewFindingKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,127}$`)
var boundedReviewDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func validatePinnedJudgeResult(value map[string]any, expected boundedReviewIdentity) bool {
	if !hasPinnedKeys(value, []string{"schema_version", "capability", "role", "review_id", "round", "scope_digest", "findings"}) || !validPinnedIdentity(value, "judge", expected) {
		return false
	}
	findings, ok := value["findings"].([]any)
	if !ok || len(findings) > 200 {
		return false
	}
	seen := map[string]bool{}
	for _, raw := range findings {
		finding, ok := raw.(map[string]any)
		if !ok || !validatePinnedFinding(finding) {
			return false
		}
		key := finding["finding_key"].(string)
		if seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

func validatePinnedFixResult(value map[string]any, expected boundedReviewIdentity) bool {
	if !hasPinnedKeys(value, []string{"schema_version", "capability", "role", "review_id", "round", "scope_digest", "applied", "unapplied"}) || !validPinnedIdentity(value, "fix", expected) {
		return false
	}
	seen := map[string]bool{}
	for _, name := range []string{"applied", "unapplied"} {
		changes, ok := value[name].([]any)
		if !ok || len(changes) > 200 {
			return false
		}
		for _, raw := range changes {
			change, ok := raw.(map[string]any)
			if !ok {
				return false
			}
			valid := validatePinnedApplied(change)
			if name == "unapplied" {
				valid = validatePinnedUnapplied(change)
			}
			if !valid {
				return false
			}
			key := change["finding_key"].(string)
			if seen[key] {
				return false
			}
			seen[key] = true
		}
	}
	return true
}

func validPinnedIdentity(v map[string]any, role string, expected boundedReviewIdentity) bool {
	round, ok := v["round"].(float64)
	return ok && v["schema_version"] == "judgment-review-result/v1" && v["capability"] == "native-session-v1" && v["role"] == role && v["review_id"] == expected.ReviewID && round == float64(expected.Round) && round > 0 && round == float64(int(round)) && v["scope_digest"] == expected.ScopeDigest && validPinnedText(expected.ReviewID, 128) && boundedReviewDigestPattern.MatchString(expected.ScopeDigest)
}
func validatePinnedFinding(v map[string]any) bool {
	if !hasPinnedAllowedAndRequired(v, []string{"finding_key", "severity", "warning_kind", "location", "evidence", "description", "fix_intent"}, []string{"finding_key", "severity", "location", "evidence", "description", "fix_intent"}) {
		return false
	}
	key, keyOK := v["finding_key"].(string)
	severity, severityOK := v["severity"].(string)
	location, locationOK := v["location"].(map[string]any)
	evidence, evidenceOK := v["evidence"].(map[string]any)
	if !keyOK || !severityOK || !boundedReviewFindingKeyPattern.MatchString(key) || !locationOK || !evidenceOK || !validatePinnedLocation(location) || !validatePinnedEvidence(evidence) || !validPinnedValueText(v["description"], 2000) || !validPinnedValueText(v["fix_intent"], 2000) {
		return false
	}
	if severity == "WARNING" {
		kind, ok := v["warning_kind"].(string)
		return ok && (kind == "real" || kind == "theoretical")
	}
	_, hasKind := v["warning_kind"]
	return (severity == "CRITICAL" || severity == "SUGGESTION") && !hasKind
}
func validatePinnedApplied(v map[string]any) bool {
	if !hasPinnedKeys(v, []string{"finding_key", "files", "locations", "summary"}) {
		return false
	}
	key, ok := v["finding_key"].(string)
	if !ok || !boundedReviewFindingKeyPattern.MatchString(key) || !validPinnedValueText(v["summary"], 2000) {
		return false
	}
	files, ok := v["files"].([]any)
	if !ok || len(files) == 0 || len(files) > 200 {
		return false
	}
	seen := map[string]bool{}
	for _, raw := range files {
		file, ok := raw.(string)
		if !ok || !validPinnedPath(file) || seen[file] {
			return false
		}
		seen[file] = true
	}
	locations, ok := v["locations"].([]any)
	if !ok || len(locations) == 0 || len(locations) > 200 {
		return false
	}
	for _, raw := range locations {
		location, ok := raw.(map[string]any)
		if !ok || !validatePinnedLocation(location) {
			return false
		}
	}
	return true
}
func validatePinnedUnapplied(v map[string]any) bool {
	if !hasPinnedKeys(v, []string{"finding_key", "reason"}) {
		return false
	}
	key, ok := v["finding_key"].(string)
	return ok && boundedReviewFindingKeyPattern.MatchString(key) && validPinnedValueText(v["reason"], 2000)
}
func validatePinnedLocation(v map[string]any) bool {
	return hasPinnedKeys(v, []string{"path", "anchor"}) && validPinnedValuePath(v["path"]) && validPinnedValueText(v["anchor"], 2000)
}
func validatePinnedEvidence(v map[string]any) bool {
	return hasPinnedKeys(v, []string{"source", "detail"}) && validPinnedValueText(v["source"], 128) && validPinnedValueText(v["detail"], 2000)
}
func hasPinnedKeys(v map[string]any, keys []string) bool {
	return hasPinnedAllowedAndRequired(v, keys, keys)
}
func hasPinnedAllowedAndRequired(v map[string]any, allowed, required []string) bool {
	for key := range v {
		if !containsPinnedKey(allowed, key) {
			return false
		}
	}
	for _, key := range required {
		if _, ok := v[key]; !ok {
			return false
		}
	}
	return true
}
func containsPinnedKey(keys []string, want string) bool {
	for _, key := range keys {
		if key == want {
			return true
		}
	}
	return false
}
func validPinnedValueText(v any, max int) bool {
	text, ok := v.(string)
	return ok && validPinnedText(text, max)
}
func validPinnedText(text string, max int) bool {
	if text == "" || len(utf16.Encode([]rune(text))) > max || strings.TrimSpace(text) != text {
		return false
	}
	for _, r := range text {
		if r <= 0x1f || r == 0x7f {
			return false
		}
	}
	return true
}
func validPinnedValuePath(v any) bool { path, ok := v.(string); return ok && validPinnedPath(path) }
func validPinnedPath(path string) bool {
	if !validPinnedText(path, 512) || strings.HasPrefix(path, "/") || strings.Contains(path, "\\") || strings.ContainsRune(path, 0) {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}
