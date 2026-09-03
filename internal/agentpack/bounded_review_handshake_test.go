package agentpack

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// These are cross-language contract tests for lore-pi-runtime at
// c48d306f66f125903aca93f5969747ff6e6e5605. They deliberately mirror its
// current handshake validator in test code only; lore-cli has no production
// handshake implementation, asset, installation, fallback, or activation path
// in this slice.
const (
	boundedReviewRuntimeFamily          = "judgment-day"
	boundedReviewRuntimeContractVersion = "1.0.0"
	boundedReviewRuntimeResultSchema    = "judgment-review-result/v1"
	boundedReviewRuntimeTarget          = "pi"
)

var boundedReviewRuntimeKeys = []string{
	"family", "role", "contract_version", "projection_id", "harness",
	"capabilities", "result_schema_version", "severity_vocabulary", "fallback",
	"static_contract_digest", "static_contract_version", "manifest_revision",
}

var boundedReviewRuntimeCapabilities = []string{
	"native-session-v1", "judge-v1", "fix-v1", "severity-semantics-v1",
	"blind-pair-v1", "approval-allowlist-v1", "mandatory-rejudgment-v1",
	"privacy-safe-metrics-v1",
}

var boundedReviewRuntimeSeverities = []string{
	"CRITICAL", "WARNING (real)", "WARNING (theoretical)", "SUGGESTION",
}

type boundedReviewRuntimeAsset struct {
	Family                string
	Role                  string
	ContractVersion       string
	ProjectionID          string
	Harness               string
	Capabilities          []string
	ResultSchemaVersion   string
	SeverityVocabulary    []string
	Fallback              bool
	StaticContractDigest  string
	StaticContractVersion string
	ManifestRevision      int
	Keys                  []string
	Malformed             bool
}

type boundedReviewRuntimePair struct {
	Judge *boundedReviewRuntimeAsset
	Fix   *boundedReviewRuntimeAsset
}

func TestBoundedReviewRuntimeHandshakeGoldenPair(t *testing.T) {
	// Reuse task 1.1's pinned Node/Go digest vector rather than a hand-copied
	// value: the runtime hashes the exact UTF-8 static version string.
	vectors := loadBoundedReviewDigestVectors(t)
	canonical := vectors.Vectors[0]
	if canonical.Input != boundedReviewStaticContractVersion || canonical.SHA256 != boundedReviewStaticContractDigest() {
		t.Fatalf("task 1.1 canonical digest vector drifted: %+v", canonical)
	}

	pair := boundedReviewRuntimeGoldenPair()
	assertBoundedReviewRuntimePair(t, pair, "compatible")

	for _, asset := range []*boundedReviewRuntimeAsset{pair.Judge, pair.Fix} {
		if asset.Family != boundedReviewRuntimeFamily || asset.ContractVersion != boundedReviewRuntimeContractVersion || asset.Harness != boundedReviewRuntimeTarget {
			t.Fatalf("runtime invariant fields drifted: %+v", asset)
		}
		if asset.ResultSchemaVersion != boundedReviewRuntimeResultSchema || !sameBoundedReviewStrings(asset.SeverityVocabulary, boundedReviewRuntimeSeverities) {
			t.Fatalf("runtime result contract drifted: %+v", asset)
		}
		if asset.StaticContractVersion != boundedReviewStaticContractVersion || asset.StaticContractDigest != boundedReviewStaticContractDigest() || asset.ManifestRevision != 1 || asset.Fallback {
			t.Fatalf("runtime static/fallback contract drifted: %+v", asset)
		}
		if !sameBoundedReviewStrings(asset.Keys, boundedReviewRuntimeKeys) {
			t.Fatalf("runtime closed key set drifted: %v", asset.Keys)
		}
	}
	if pair.Judge.Role != "judge" || pair.Fix.Role != "fix" {
		t.Fatalf("runtime role pair = %q/%q, want judge/fix", pair.Judge.Role, pair.Fix.Role)
	}

	// The runtime only type-checks projection_id. This deliberately accepts both
	// the planned identifier and the current runtime test's legacy identifier;
	// do not treat either spelling as additional runtime validation authority.
	pair.Judge.ProjectionID = "lore.judgment-day.bounded-review"
	pair.Fix.ProjectionID = "lore-cli/judgment-day"
	assertBoundedReviewRuntimePair(t, pair, "compatible")
}

func TestBoundedReviewRuntimeHandshakeMutationRejections(t *testing.T) {
	missingCapabilityCases := make([]boundedReviewRuntimeMutation, 0, len(boundedReviewRuntimeCapabilities))
	for _, capability := range boundedReviewRuntimeCapabilities {
		missingCapabilityCases = append(missingCapabilityCases, boundedReviewRuntimeMutation{
			name: "missing_capability_" + capability,
			want: "missing_capability",
			mutate: func(pair boundedReviewRuntimePair) boundedReviewRuntimePair {
				pair.Fix.Capabilities = withoutBoundedReviewString(pair.Fix.Capabilities, capability)
				return pair
			},
		})
	}

	cases := append([]boundedReviewRuntimeMutation{
		{name: "missing_judge", want: "partial_asset", mutate: func(pair boundedReviewRuntimePair) boundedReviewRuntimePair { pair.Judge = nil; return pair }},
		{name: "missing_fix", want: "partial_asset", mutate: func(pair boundedReviewRuntimePair) boundedReviewRuntimePair { pair.Fix = nil; return pair }},
		{name: "fallback_only_judge", want: "fallback_only", mutate: func(pair boundedReviewRuntimePair) boundedReviewRuntimePair { pair.Judge.Fallback = true; return pair }},
		{name: "fallback_only_fix", want: "fallback_only", mutate: func(pair boundedReviewRuntimePair) boundedReviewRuntimePair { pair.Fix.Fallback = true; return pair }},
		{name: "duplicate_judge_role", want: "malformed", mutate: func(pair boundedReviewRuntimePair) boundedReviewRuntimePair { pair.Fix.Role = "judge"; return pair }},
		{name: "wrong_family", want: "malformed", mutate: func(pair boundedReviewRuntimePair) boundedReviewRuntimePair { pair.Fix.Family = "other"; return pair }},
		{name: "version_skew", want: "version_skew", mutate: func(pair boundedReviewRuntimePair) boundedReviewRuntimePair {
			pair.Fix.ContractVersion = "1.1.0"
			return pair
		}},
		{name: "unsupported_future_revision", want: "unsupported_version", mutate: func(pair boundedReviewRuntimePair) boundedReviewRuntimePair {
			pair.Judge.ContractVersion, pair.Fix.ContractVersion = "2.0.0", "2.0.0"
			return pair
		}},
		{name: "wrong_target", want: "target_mismatch", mutate: func(pair boundedReviewRuntimePair) boundedReviewRuntimePair { pair.Fix.Harness = "codex"; return pair }},
		{name: "wrong_schema", want: "malformed", mutate: func(pair boundedReviewRuntimePair) boundedReviewRuntimePair {
			pair.Fix.ResultSchemaVersion = "1.0.0"
			return pair
		}}, // Legacy handoff wording, explicitly not authoritative.
		{name: "wrong_severity_order", want: "malformed", mutate: func(pair boundedReviewRuntimePair) boundedReviewRuntimePair {
			pair.Fix.SeverityVocabulary[0], pair.Fix.SeverityVocabulary[1] = pair.Fix.SeverityVocabulary[1], pair.Fix.SeverityVocabulary[0]
			return pair
		}},
		{name: "wrong_severity_vocabulary", want: "malformed", mutate: func(pair boundedReviewRuntimePair) boundedReviewRuntimePair {
			pair.Fix.SeverityVocabulary[3] = "INFO"
			return pair
		}},
		{name: "duplicate_capability", want: "malformed", mutate: func(pair boundedReviewRuntimePair) boundedReviewRuntimePair {
			pair.Fix.Capabilities = append(pair.Fix.Capabilities, pair.Fix.Capabilities[0])
			return pair
		}},
		{name: "stale_digest", want: "stale_digest", mutate: func(pair boundedReviewRuntimePair) boundedReviewRuntimePair {
			pair.Fix.StaticContractDigest = repeatBoundedReviewString("b", 64)
			return pair
		}},
		{name: "wrong_static_version", want: "stale_digest", mutate: func(pair boundedReviewRuntimePair) boundedReviewRuntimePair {
			pair.Fix.StaticContractVersion = "judgment-day-static/v2"
			return pair
		}},
		{name: "stale_manifest_revision", want: "stale_manifest", mutate: func(pair boundedReviewRuntimePair) boundedReviewRuntimePair {
			pair.Fix.ManifestRevision = 0
			return pair
		}},
		{name: "manifest_revision_skew", want: "stale_manifest", mutate: func(pair boundedReviewRuntimePair) boundedReviewRuntimePair {
			pair.Fix.ManifestRevision = 2
			return pair
		}},
		{name: "unknown_field", want: "malformed", mutate: func(pair boundedReviewRuntimePair) boundedReviewRuntimePair {
			pair.Judge.Keys = append(pair.Judge.Keys, "unexpected")
			return pair
		}},
		{name: "missing_field", want: "malformed", mutate: func(pair boundedReviewRuntimePair) boundedReviewRuntimePair {
			pair.Judge.Keys = pair.Judge.Keys[:len(pair.Judge.Keys)-1]
			return pair
		}},
		{name: "malformed_digest", want: "malformed", mutate: func(pair boundedReviewRuntimePair) boundedReviewRuntimePair {
			pair.Fix.StaticContractDigest = "not-a-digest"
			return pair
		}},
	}, missingCapabilityCases...)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertBoundedReviewRuntimePair(t, tc.mutate(boundedReviewRuntimeGoldenPair()), tc.want)
		})
	}
}

type boundedReviewRuntimeMutation struct {
	name   string
	want   string
	mutate func(boundedReviewRuntimePair) boundedReviewRuntimePair
}

// validateBoundedReviewRuntimeHandshake mirrors validateJudgmentReviewHandshake
// exactly at the pinned runtime commit. Keep failures actionable: an update here
// means the TypeScript runtime algorithm or contract has drifted and must be
// re-baselined before lore-cli gains a production projection.
func validateBoundedReviewRuntimeHandshake(pair boundedReviewRuntimePair) string {
	if pair.Judge == nil && pair.Fix == nil {
		return "missing_asset"
	}
	if pair.Judge == nil || pair.Fix == nil {
		return "partial_asset"
	}
	judge, fix := pair.Judge, pair.Fix
	if !validBoundedReviewRuntimeAsset(judge) || !validBoundedReviewRuntimeAsset(fix) {
		return "malformed"
	}
	if judge.Role != "judge" || fix.Role != "fix" || judge.Family != boundedReviewRuntimeFamily || fix.Family != boundedReviewRuntimeFamily {
		return "malformed"
	}
	if judge.Harness != boundedReviewRuntimeTarget || fix.Harness != boundedReviewRuntimeTarget {
		return "target_mismatch"
	}
	if judge.ContractVersion != fix.ContractVersion {
		return "version_skew"
	}
	if judge.ContractVersion != boundedReviewRuntimeContractVersion {
		return "unsupported_version"
	}
	if judge.ResultSchemaVersion != boundedReviewRuntimeResultSchema || fix.ResultSchemaVersion != boundedReviewRuntimeResultSchema || !sameBoundedReviewStrings(judge.SeverityVocabulary, boundedReviewRuntimeSeverities) || !sameBoundedReviewStrings(fix.SeverityVocabulary, boundedReviewRuntimeSeverities) {
		return "malformed"
	}
	if judge.StaticContractVersion != boundedReviewStaticContractVersion || fix.StaticContractVersion != boundedReviewStaticContractVersion || judge.StaticContractDigest != fix.StaticContractDigest || judge.StaticContractDigest != boundedReviewStaticContractDigest() {
		return "stale_digest"
	}
	if judge.ManifestRevision != fix.ManifestRevision || judge.ManifestRevision < 1 {
		return "stale_manifest"
	}
	if judge.Fallback || fix.Fallback {
		return "fallback_only"
	}
	for _, capability := range boundedReviewRuntimeCapabilities {
		if !containsBoundedReviewString(judge.Capabilities, capability) || !containsBoundedReviewString(fix.Capabilities, capability) {
			return "missing_capability"
		}
	}
	return "compatible"
}

func validBoundedReviewRuntimeAsset(asset *boundedReviewRuntimeAsset) bool {
	return !asset.Malformed && sameBoundedReviewStringSet(asset.Keys, boundedReviewRuntimeKeys) &&
		(asset.Role == "judge" || asset.Role == "fix") && uniqueBoundedReviewStrings(asset.Capabilities) &&
		uniqueBoundedReviewStrings(asset.SeverityVocabulary) && len(asset.StaticContractDigest) == 64 &&
		isLowerBoundedReviewHex(asset.StaticContractDigest) && asset.ManifestRevision >= -1<<53 && asset.ManifestRevision <= 1<<53
}

func boundedReviewRuntimeGoldenPair() boundedReviewRuntimePair {
	return boundedReviewRuntimePair{
		Judge: boundedReviewRuntimeGoldenAsset("judge"),
		Fix:   boundedReviewRuntimeGoldenAsset("fix"),
	}
}

func boundedReviewRuntimeGoldenAsset(role string) *boundedReviewRuntimeAsset {
	return &boundedReviewRuntimeAsset{
		Family:                boundedReviewRuntimeFamily,
		Role:                  role,
		ContractVersion:       boundedReviewRuntimeContractVersion,
		ProjectionID:          "lore.judgment-day.bounded-review",
		Harness:               boundedReviewRuntimeTarget,
		Capabilities:          append([]string(nil), boundedReviewRuntimeCapabilities...),
		ResultSchemaVersion:   boundedReviewRuntimeResultSchema,
		SeverityVocabulary:    append([]string(nil), boundedReviewRuntimeSeverities...),
		Fallback:              false,
		StaticContractDigest:  boundedReviewStaticContractDigest(),
		StaticContractVersion: boundedReviewStaticContractVersion,
		ManifestRevision:      1,
		Keys:                  append([]string(nil), boundedReviewRuntimeKeys...),
	}
}

func assertBoundedReviewRuntimePair(t *testing.T, pair boundedReviewRuntimePair, want string) {
	t.Helper()
	if got := validateBoundedReviewRuntimeHandshake(pair); got != want {
		t.Fatalf("runtime handshake result = %q, want %q; runtime source: lore-pi-runtime/src/runtime/judgment-review-config.ts@c48d306f66f125903aca93f5969747ff6e6e5605", got, want)
	}
}

func boundedReviewStaticContractDigest() string {
	digest := sha256.Sum256([]byte(boundedReviewStaticContractVersion))
	return hex.EncodeToString(digest[:])
}

func sameBoundedReviewStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func sameBoundedReviewStringSet(got, want []string) bool {
	return len(got) == len(want) && uniqueBoundedReviewStrings(got) && uniqueBoundedReviewStrings(want) && containsAllBoundedReviewStrings(got, want)
}

func containsAllBoundedReviewStrings(got, want []string) bool {
	for _, value := range want {
		if !containsBoundedReviewString(got, value) {
			return false
		}
	}
	return true
}

func uniqueBoundedReviewStrings(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func containsBoundedReviewString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func withoutBoundedReviewString(values []string, unwanted string) []string {
	result := make([]string, 0, len(values)-1)
	for _, value := range values {
		if value != unwanted {
			result = append(result, value)
		}
	}
	return result
}

func isLowerBoundedReviewHex(value string) bool {
	for _, character := range value {
		if !(character >= '0' && character <= '9') && !(character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func repeatBoundedReviewString(value string, count int) string {
	result := ""
	for range count {
		result += value
	}
	return result
}
