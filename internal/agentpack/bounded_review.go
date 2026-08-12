package agentpack

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	BoundedReviewFamily                = "judgment-day"
	BoundedReviewProjectionID          = "lore.judgment-day.bounded-review"
	BoundedReviewContractVersion       = "1.0.0"
	BoundedReviewTarget                = "pi"
	BoundedReviewResultSchemaVersion   = "judgment-review-result/v1"
	BoundedReviewStaticContractVersion = "judgment-day-static/v1"
	BoundedReviewManifestRevision      = 1
	BoundedReviewJudgeRole             = "judge"
	BoundedReviewFixRole               = "fix"
)

var BoundedReviewCapabilities = []string{
	"native-session-v1",
	"judge-v1",
	"fix-v1",
	"severity-semantics-v1",
	"blind-pair-v1",
	"approval-allowlist-v1",
	"mandatory-rejudgment-v1",
	"privacy-safe-metrics-v1",
}

var BoundedReviewSeverities = []string{
	"CRITICAL",
	"WARNING (real)",
	"WARNING (theoretical)",
	"SUGGESTION",
}

// BoundedReviewHandshake is the closed runtime-compatible metadata member.
// JSON field names and omission rules intentionally match the pinned Pi runtime.
type BoundedReviewHandshake struct {
	Family                string   `json:"family"`
	Role                  string   `json:"role"`
	ContractVersion       string   `json:"contract_version"`
	ProjectionID          string   `json:"projection_id"`
	Harness               string   `json:"harness"`
	Capabilities          []string `json:"capabilities"`
	ResultSchemaVersion   string   `json:"result_schema_version"`
	SeverityVocabulary    []string `json:"severity_vocabulary"`
	Fallback              bool     `json:"fallback"`
	StaticContractDigest  string   `json:"static_contract_digest"`
	StaticContractVersion string   `json:"static_contract_version"`
	ManifestRevision      int      `json:"manifest_revision"`
}

// BoundedReviewAsset is static, Pi-native model material. Its body must never
// contain target, diff, standards, findings, user data, or absolute paths.
type BoundedReviewAsset struct {
	Role      string                 `json:"role"`
	Marker    string                 `json:"marker"`
	Body      string                 `json:"body"`
	Handshake BoundedReviewHandshake `json:"handshake"`
}

// BoundedReviewContractPair is a complete release candidate. It is data only: no
// installer, discovery, or activation behavior is implied by this model.
type BoundedReviewContractPair struct {
	Revision int                   `json:"revision"`
	Judge    BoundedReviewAsset    `json:"judge"`
	Fix      BoundedReviewAsset    `json:"fix"`
	Manifest BoundedReviewManifest `json:"manifest"`
}

// BoundedReviewStaticContractDigest reproduces the current runtime digest:
// SHA-256 over the exact UTF-8 static contract version bytes only.
func BoundedReviewStaticContractDigest() string {
	digest := sha256.Sum256([]byte(BoundedReviewStaticContractVersion))
	return hex.EncodeToString(digest[:])
}

// BoundedReviewPair returns a deterministic, complete static Judge/Fix pair.
func BoundedReviewPair() BoundedReviewContractPair {
	revision := BoundedReviewManifestRevision
	judge := BoundedReviewAsset{
		Role:      BoundedReviewJudgeRole,
		Marker:    "lore.judgment-day.bounded-review/judge/v1",
		Body:      boundedReviewJudgeBody,
		Handshake: boundedReviewHandshake(BoundedReviewJudgeRole, revision),
	}
	fix := BoundedReviewAsset{
		Role:      BoundedReviewFixRole,
		Marker:    "lore.judgment-day.bounded-review/fix/v1",
		Body:      boundedReviewFixBody,
		Handshake: boundedReviewHandshake(BoundedReviewFixRole, revision),
	}
	return BoundedReviewContractPair{
		Revision: revision,
		Judge:    judge,
		Fix:      fix,
		Manifest: NewBoundedReviewManifest(revision, judge, fix),
	}
}

func boundedReviewHandshake(role string, revision int) BoundedReviewHandshake {
	return BoundedReviewHandshake{
		Family:                BoundedReviewFamily,
		Role:                  role,
		ContractVersion:       BoundedReviewContractVersion,
		ProjectionID:          BoundedReviewProjectionID,
		Harness:               BoundedReviewTarget,
		Capabilities:          append([]string(nil), BoundedReviewCapabilities...),
		ResultSchemaVersion:   BoundedReviewResultSchemaVersion,
		SeverityVocabulary:    append([]string(nil), BoundedReviewSeverities...),
		Fallback:              false,
		StaticContractDigest:  BoundedReviewStaticContractDigest(),
		StaticContractVersion: BoundedReviewStaticContractVersion,
		ManifestRevision:      revision,
	}
}

// ValidateBoundedReviewPair rejects malformed, incomplete, duplicate, or
// skewed static pairs before a future installer can construct a plan.
func ValidateBoundedReviewPair(pair BoundedReviewContractPair) error {
	if pair.Revision < 1 {
		return fmt.Errorf("bounded review revision must be positive")
	}
	if err := validateBoundedReviewAsset(pair.Judge, BoundedReviewJudgeRole, pair.Revision); err != nil {
		return fmt.Errorf("judge asset: %w", err)
	}
	if err := validateBoundedReviewAsset(pair.Fix, BoundedReviewFixRole, pair.Revision); err != nil {
		return fmt.Errorf("fix asset: %w", err)
	}
	if pair.Judge.Role == pair.Fix.Role || pair.Judge.Marker == pair.Fix.Marker {
		return fmt.Errorf("bounded review pair has duplicate role or marker")
	}
	if err := pair.Manifest.Validate(pair); err != nil {
		return fmt.Errorf("manifest: %w", err)
	}
	return nil
}

func validateBoundedReviewAsset(asset BoundedReviewAsset, role string, revision int) error {
	if asset.Role != role || asset.Handshake.Role != role {
		return fmt.Errorf("role must be %q", role)
	}
	if strings.TrimSpace(asset.Marker) == "" || strings.TrimSpace(asset.Body) == "" {
		return fmt.Errorf("marker and body are required")
	}
	if err := validateBoundedReviewHandshake(asset.Handshake, revision); err != nil {
		return err
	}
	if strings.Contains(asset.Body, "{{") || strings.Contains(asset.Body, "}}") || strings.Contains(asset.Body, "\x00") {
		return fmt.Errorf("body contains dynamic or invalid placeholder material")
	}
	if strings.Contains(asset.Body, "/Users/") || strings.Contains(asset.Body, "C:\\") {
		return fmt.Errorf("body contains an absolute path")
	}
	return nil
}

func validateBoundedReviewHandshake(handshake BoundedReviewHandshake, revision int) error {
	if handshake.Family != BoundedReviewFamily || handshake.ContractVersion != BoundedReviewContractVersion || handshake.ProjectionID != BoundedReviewProjectionID || handshake.Harness != BoundedReviewTarget {
		return fmt.Errorf("static identity mismatch")
	}
	if handshake.ResultSchemaVersion != BoundedReviewResultSchemaVersion || handshake.Fallback || handshake.StaticContractVersion != BoundedReviewStaticContractVersion || handshake.StaticContractDigest != BoundedReviewStaticContractDigest() || handshake.ManifestRevision != revision {
		return fmt.Errorf("runtime compatibility mismatch")
	}
	if !sameBoundedReviewOrderedStrings(handshake.SeverityVocabulary, BoundedReviewSeverities) || !sameBoundedReviewOrderedStrings(handshake.Capabilities, BoundedReviewCapabilities) {
		return fmt.Errorf("capability or severity vocabulary mismatch")
	}
	return nil
}

func sameBoundedReviewOrderedStrings(got, want []string) bool {
	return len(got) == len(want) && strings.Join(got, "\x00") == strings.Join(want, "\x00")
}

const boundedReviewJudgeBody = `<!-- lore.judgment-day.bounded-review/judge/v1 -->
# Bounded Review Judge

Blind and read-only: return JSON findings only; never modify code or authorize writes. No extra fields.
Identity is exact: schema_version judgment-review-result/v1, capability native-session-v1, role judge, supplied review_id, positive round, and supplied 64-lowercase-hex scope_digest. Finding keys are unique. Use CRITICAL, WARNING with warning_kind real|theoretical, or SUGGESTION; warning_kind is forbidden for CRITICAL/SUGGESTION. Paths are repo-relative; location/evidence are structured.
{"schema_version":"judgment-review-result/v1","capability":"native-session-v1","role":"judge","review_id":"review-1","round":1,"scope_digest":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","findings":[{"finding_key":"finding-1","severity":"CRITICAL","location":{"path":"internal/file.go","anchor":"line 1"},"evidence":{"source":"diff","detail":"shown code"},"description":"A concrete defect.","fix_intent":"Correct the defect."}]}
`

const boundedReviewFixBody = `<!-- lore.judgment-day.bounded-review/fix/v1 -->
# Bounded Review Fix

Approved allowlist: confirmed CRITICAL/real WARNING only. SUGGESTION, theoretical WARNING, suspects, and contradictions never authorize writes. No unrelated refactors. Every code-changing Fix needs a fresh blind Judge pair.
Identity: schema_version, capability native-session-v1, role fix, review_id, positive round, 64-lowercase-hex scope_digest. applied/unapplied keys are unique/disjoint; applied files repo-relative with locations/summary; unapplied has reason.
{"schema_version":"judgment-review-result/v1","capability":"native-session-v1","role":"fix","review_id":"review-1","round":1,"scope_digest":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","applied":[{"finding_key":"finding-1","files":["internal/file.go"],"locations":[{"path":"internal/file.go","anchor":"line 1"}],"summary":"Corrected the defect."}],"unapplied":[{"finding_key":"finding-2","reason":"Needs approval."}]}
`
