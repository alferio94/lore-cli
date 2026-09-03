package agentpack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

const boundedReviewBaselinePath = "testdata/bounded_review_baseline.json"
const boundedReviewMeasurementsPath = "testdata/bounded_review_measurements.json"
const boundedReviewBudgetsPath = "testdata/bounded_review_budgets.json"
const boundedReviewDigestVectorsPath = "testdata/bounded_review_digest_vectors.json"
const updateBoundedReviewFixturesEnv = "LORE_UPDATE_BOUNDED_REVIEW_FIXTURES"
const updateBoundedReviewFixturesReasonEnv = "LORE_UPDATE_BOUNDED_REVIEW_FIXTURES_REASON"

const boundedReviewStaticContractVersion = "judgment-day-static/v1"
const boundedReviewSourceIdentity = "internal/agentpack/extended_skills.go:JudgmentDayPortable"
const boundedReviewMeasurementBoundary = "Static role material delivered by the planned Pi runtime injection: the rendered judge.md or fix.md body only. The release manifest, current pointer, handshake metadata, target, standards, findings, and dynamic prompt are not role prompt context. The required replacement result schema is inline in each measured body."

type boundedReviewMetrics struct {
	Bytes           int `json:"bytes"`
	Chars           int `json:"chars"`
	Lines           int `json:"lines"`
	EstimatedTokens int `json:"estimated_tokens"`
}

type boundedReviewRoleBaseline struct {
	Role        string               `json:"role"`
	Segment     string               `json:"segment"`
	Source      string               `json:"source"`
	Measurement boundedReviewMetrics `json:"measurement"`
}

type boundedReviewBaseline struct {
	Version         int                         `json:"version"`
	MeasurementKind string                      `json:"measurement_kind"`
	Source          string                      `json:"source"`
	Asset           string                      `json:"asset"`
	Note            string                      `json:"note"`
	FullSkill       boundedReviewMetrics        `json:"full_skill"`
	Roles           []boundedReviewRoleBaseline `json:"roles"`
}

type boundedReviewRoleMeasurement struct {
	Role                    string               `json:"role"`
	Baseline                boundedReviewMetrics `json:"baseline"`
	Candidate               boundedReviewMetrics `json:"candidate"`
	RequiredResultSchema    boundedReviewMetrics `json:"required_result_schema"`
	CandidateIncludesSchema bool                 `json:"candidate_includes_required_result_schema"`
	Delta                   boundedReviewMetrics `json:"candidate_minus_baseline"`
	AbsoluteDelta           boundedReviewMetrics `json:"absolute_delta"`
	ReductionPercent        float64              `json:"reduction_percent"`
	StaticContractVersion   string               `json:"static_contract_version"`
	StaticContractDigest    string               `json:"static_contract_digest"`
	ProjectionID            string               `json:"projection_id"`
	InvariantFixtureVersion int                  `json:"invariant_fixture_version"`
	SemanticFixtureVersion  int                  `json:"semantic_fixture_version"`
	HandshakeCompatibility  string               `json:"handshake_compatibility"`
}

type boundedReviewMeasurements struct {
	Version                    int                            `json:"version"`
	MeasurementKind            string                         `json:"measurement_kind"`
	MeasurementBoundary        string                         `json:"measurement_boundary"`
	ObservedDynamicRuntimeCost string                         `json:"observed_dynamic_runtime_cost"`
	RuntimeSyntheticProxy      string                         `json:"runtime_synthetic_proxy"`
	Roles                      []boundedReviewRoleMeasurement `json:"roles"`
}

type boundedReviewRoleBudget struct {
	Role                    string `json:"role"`
	MaxCandidateBytes       int    `json:"max_candidate_bytes"`
	MaxCandidateChars       int    `json:"max_candidate_chars"`
	MaxCandidateLines       int    `json:"max_candidate_lines"`
	MaxCandidateTokens      int    `json:"max_candidate_tokens"`
	MinReductionBytes       int    `json:"min_reduction_bytes"`
	MinReductionChars       int    `json:"min_reduction_chars"`
	RequiredFixtureVersions string `json:"required_fixture_versions"`
}

type boundedReviewBudgets struct {
	Version string                    `json:"version"`
	Note    string                    `json:"note"`
	Roles   []boundedReviewRoleBudget `json:"roles"`
}

type boundedReviewDigestVector struct {
	Name             string `json:"name"`
	Input            string `json:"input"`
	InputUTF8Hex     string `json:"input_utf8_hex"`
	SHA256           string `json:"sha256"`
	MatchesCanonical bool   `json:"matches_canonical"`
}

type boundedReviewDigestVectors struct {
	Version       int                         `json:"version"`
	Algorithm     string                      `json:"algorithm"`
	Encoding      string                      `json:"encoding"`
	HexEncoding   string                      `json:"hex_encoding"`
	RuntimeSource string                      `json:"runtime_source"`
	Semantics     string                      `json:"semantics"`
	Vectors       []boundedReviewDigestVector `json:"vectors"`
}

func TestBoundedReviewBaselineFixture(t *testing.T) {
	first := renderBoundedReviewBaseline(t)
	second := renderBoundedReviewBaseline(t)
	if got, want := marshalBoundedReviewFixture(t, second), marshalBoundedReviewFixture(t, first); string(got) != string(want) {
		t.Fatal("bounded review baseline rendering is not deterministic")
	}

	want, baseline := loadBoundedReviewBaseline(t)
	if os.Getenv(updateBoundedReviewFixturesEnv) == "1" {
		reason := strings.TrimSpace(os.Getenv(updateBoundedReviewFixturesReasonEnv))
		if reason == "" {
			t.Fatalf("refusing to regenerate bounded review fixtures without %s; record the reviewed reason with the fixture diff", updateBoundedReviewFixturesReasonEnv)
		}
		if err := os.WriteFile(boundedReviewBaselinePath, marshalBoundedReviewFixture(t, first), 0o644); err != nil {
			t.Fatalf("write baseline fixture: %v", err)
		}
		return
	}

	if got := marshalBoundedReviewFixture(t, first); string(want) != string(got) {
		t.Fatalf("bounded review baseline drifted; regenerate deliberately with %s=1 and %s=<reviewed reason>, then review the static fallback role-template fixture\nwant:\n%s\ngot:\n%s", updateBoundedReviewFixturesEnv, updateBoundedReviewFixturesReasonEnv, want, got)
	}
	if baseline.Note == "" || strings.Contains(strings.ToLower(baseline.Note), "observed runtime") {
		t.Fatal("baseline fixture must identify static fallback asset cost without claiming observed runtime prompt cost")
	}
}

func TestBoundedReviewBaselineRoleSeparation(t *testing.T) {
	baseline := renderBoundedReviewBaseline(t)
	if baseline.Version != 2 || baseline.FullSkill.Bytes == 0 || baseline.FullSkill.Chars == 0 || baseline.FullSkill.Lines == 0 {
		t.Fatalf("corrected full-skill baseline = version=%d metrics=%+v, want version 2 with non-empty metrics", baseline.Version, baseline.FullSkill)
	}
	if baseline.FullSkill.EstimatedTokens != (baseline.FullSkill.Chars+3)/4 {
		t.Fatalf("corrected full-skill estimated_tokens=%d, want ceil(chars/4)=%d", baseline.FullSkill.EstimatedTokens, (baseline.FullSkill.Chars+3)/4)
	}
	if got, want := len(baseline.Roles), 2; got != want {
		t.Fatalf("role baselines = %d, want %d", got, want)
	}
	for index, want := range []struct{ role, segment string }{{"judge", "judge_prompt_template"}, {"fix", "fix_agent_prompt_template"}} {
		got := baseline.Roles[index]
		if got.Role != want.role || got.Segment != want.segment || got.Source != boundedReviewSourceIdentity {
			t.Fatalf("role baseline[%d] = %+v, want role=%q segment=%q source=%q", index, got, want.role, want.segment, boundedReviewSourceIdentity)
		}
		if got.Measurement.Bytes == 0 || got.Measurement.Chars == 0 || got.Measurement.Lines == 0 {
			t.Fatalf("role baseline[%d] has empty measurement: %+v", index, got.Measurement)
		}
		if got.Measurement.EstimatedTokens != (got.Measurement.Chars+3)/4 {
			t.Fatalf("role baseline[%d] estimated_tokens=%d, want ceil(chars/4)=%d", index, got.Measurement.EstimatedTokens, (got.Measurement.Chars+3)/4)
		}
	}
	if baseline.Roles[0].Measurement == baseline.Roles[1].Measurement {
		t.Fatal("judge and fix role-template measurements unexpectedly match")
	}
}

func TestBoundedReviewMeasurementFixturesAreExactAndDeterministic(t *testing.T) {
	first := renderBoundedReviewMeasurements(t)
	second := renderBoundedReviewMeasurements(t)
	if got, want := marshalBoundedReviewFixture(t, second), marshalBoundedReviewFixture(t, first); string(got) != string(want) {
		t.Fatal("bounded review candidate measurement rendering is not deterministic")
	}
	want, fixture := loadBoundedReviewMeasurements(t)
	if got := marshalBoundedReviewFixture(t, first); string(want) != string(got) {
		t.Fatalf("bounded review measurement fixture drifted; normal tests never rewrite reviewed fixtures\nwant:\n%s\ngot:\n%s", want, got)
	}
	if fixture.MeasurementBoundary != boundedReviewMeasurementBoundary || fixture.ObservedDynamicRuntimeCost != "unavailable" || fixture.RuntimeSyntheticProxy != "not counted; handoff-only synthetic proxy" {
		t.Fatalf("measurement boundary/runtime evidence drifted: %+v", fixture)
	}
}

func TestBoundedReviewBudgetFixtureIsExactAndCandidateDerived(t *testing.T) {
	first := renderBoundedReviewBudgets(t)
	second := renderBoundedReviewBudgets(t)
	if got, want := marshalBoundedReviewFixture(t, second), marshalBoundedReviewFixture(t, first); string(got) != string(want) {
		t.Fatal("bounded review budget rendering is not deterministic")
	}
	data, err := os.ReadFile(boundedReviewBudgetsPath)
	if err != nil {
		t.Fatalf("read budget fixture: %v", err)
	}
	if got := marshalBoundedReviewFixture(t, first); string(data) != string(got) {
		t.Fatalf("bounded review budget fixture drifted; normal tests never rewrite reviewed fixtures\nwant:\n%s\ngot:\n%s", data, got)
	}
}

func TestBoundedReviewMeasurementRolesIncludeSchemasAndHaveIndependentGates(t *testing.T) {
	fixture := renderBoundedReviewMeasurements(t)
	budgets := loadBoundedReviewBudgets(t)
	if got, want := len(fixture.Roles), 2; got != want {
		t.Fatalf("measurement roles=%d, want %d", got, want)
	}
	if got, want := len(budgets.Roles), 2; got != want {
		t.Fatalf("budget roles=%d, want %d", got, want)
	}
	for index, role := range fixture.Roles {
		budget := budgets.Roles[index]
		if role.Role != []string{BoundedReviewJudgeRole, BoundedReviewFixRole}[index] || budget.Role != role.Role {
			t.Fatalf("role/budget ordering=%q/%q, want independent judge then fix gates", role.Role, budget.Role)
		}
		if !role.CandidateIncludesSchema || role.RequiredResultSchema.Bytes == 0 || role.RequiredResultSchema.Chars == 0 || role.RequiredResultSchema.Lines != 1 {
			t.Fatalf("%s required result schema is not measured inline: %+v", role.Role, role)
		}
		if role.Candidate.EstimatedTokens != (role.Candidate.Chars+3)/4 || role.Baseline.EstimatedTokens != (role.Baseline.Chars+3)/4 || role.RequiredResultSchema.EstimatedTokens != (role.RequiredResultSchema.Chars+3)/4 {
			t.Fatalf("%s metrics do not use ceil(chars/4): %+v", role.Role, role)
		}
		if role.Candidate.Bytes > budget.MaxCandidateBytes || role.Candidate.Chars > budget.MaxCandidateChars || role.Candidate.Lines > budget.MaxCandidateLines || role.Candidate.EstimatedTokens > budget.MaxCandidateTokens {
			t.Fatalf("%s candidate exceeded reviewed budget: candidate=%+v budget=%+v", role.Role, role.Candidate, budget)
		}
		if role.Baseline.Bytes-role.Candidate.Bytes < budget.MinReductionBytes || role.Baseline.Chars-role.Candidate.Chars < budget.MinReductionChars {
			t.Fatalf("%s candidate lacks independently reviewed benefit: role=%+v budget=%+v", role.Role, role, budget)
		}
		if role.StaticContractVersion != BoundedReviewStaticContractVersion || role.StaticContractDigest != BoundedReviewStaticContractDigest() || role.ProjectionID != BoundedReviewProjectionID || role.InvariantFixtureVersion != 1 || role.SemanticFixtureVersion != 2 || role.HandshakeCompatibility != "compatible" {
			t.Fatalf("%s digest/version binding drifted: %+v", role.Role, role)
		}
	}
}

func TestBoundedReviewMeasurementUnicodeAndNewlineBehavior(t *testing.T) {
	metrics := measureBoundedReview([]byte("Aé\n"))
	if metrics != (boundedReviewMetrics{Bytes: 4, Chars: 3, Lines: 1, EstimatedTokens: 1}) {
		t.Fatalf("Unicode/newline metrics=%+v, want UTF-8 bytes, Unicode chars, trailing-newline line, ceil(chars/4)", metrics)
	}
	if got := measureBoundedReview([]byte("A\nB")); got.Lines != 2 {
		t.Fatalf("non-trailing newline lines=%d, want 2", got.Lines)
	}
}

func TestBoundedReviewMeasurementRejectsNoBenefitCandidateFixture(t *testing.T) {
	fixture := renderBoundedReviewMeasurements(t)
	fixture.Roles[1].Candidate = fixture.Roles[1].Baseline
	if boundedReviewRolePassesGate(fixture.Roles[1], loadBoundedReviewBudgets(t).Roles[1]) {
		t.Fatal("no-benefit fix candidate passed its independent gate")
	}
}

func TestBoundedReviewDigestVectors(t *testing.T) {
	vectors := loadBoundedReviewDigestVectors(t)
	if vectors.Algorithm != "sha-256" || vectors.Encoding != "utf-8" || vectors.HexEncoding != "lowercase" {
		t.Fatalf("digest fixture semantics = algorithm=%q encoding=%q hex_encoding=%q, want sha-256/utf-8/lowercase", vectors.Algorithm, vectors.Encoding, vectors.HexEncoding)
	}
	if len(vectors.Vectors) != 4 {
		t.Fatalf("digest vectors = %d, want canonical plus newline, Unicode, and order drift vectors", len(vectors.Vectors))
	}
	for _, vector := range vectors.Vectors {
		input := []byte(vector.Input)
		if got := hex.EncodeToString(input); got != vector.InputUTF8Hex {
			t.Errorf("%s input UTF-8 bytes = %s, want %s", vector.Name, got, vector.InputUTF8Hex)
		}
		digest := sha256.Sum256(input)
		if got := hex.EncodeToString(digest[:]); got != vector.SHA256 {
			t.Errorf("%s SHA-256 = %s, want %s", vector.Name, got, vector.SHA256)
		}
		if vector.MatchesCanonical != (vector.Input == boundedReviewStaticContractVersion) {
			t.Errorf("%s matches_canonical=%t does not match exact version-byte equality", vector.Name, vector.MatchesCanonical)
		}
	}
	canonical := vectors.Vectors[0]
	if canonical.Input != boundedReviewStaticContractVersion || canonical.SHA256 != "cc888a92b03cc2f4bed8abd0a5efe1bd010d94d1ff6a56cfbeda82c65ed57283" || !canonical.MatchesCanonical {
		t.Fatalf("canonical digest vector = %+v, want runtime-compatible judgment-day-static/v1 vector", canonical)
	}
}

func renderBoundedReviewBaseline(t *testing.T) boundedReviewBaseline {
	t.Helper()
	body := JudgmentDayPortable().Body
	judge := boundedReviewRoleSegment(t, body, "### Judge Prompt (use for BOTH Judge A and Judge B — identical)\n\n", "\n### Fix Agent Prompt")
	fix := boundedReviewRoleSegment(t, body, "### Fix Agent Prompt\n\n", "\n## Output Format")
	return boundedReviewBaseline{
		Version: 2, MeasurementKind: "utf-8 static portable/global fallback full-skill and role-template segments", Source: boundedReviewSourceIdentity, Asset: "judgment-day",
		Note:      "Captured corrected lore-cli rendered static fallback full-skill and role-template costs; not observed dynamic runtime prompt cost or compact-candidate savings.",
		FullSkill: measureBoundedReview([]byte(body)),
		Roles: []boundedReviewRoleBaseline{
			{Role: "judge", Segment: "judge_prompt_template", Source: boundedReviewSourceIdentity, Measurement: measureBoundedReview(judge)},
			{Role: "fix", Segment: "fix_agent_prompt_template", Source: boundedReviewSourceIdentity, Measurement: measureBoundedReview(fix)},
		},
	}
}

func renderBoundedReviewMeasurements(t *testing.T) boundedReviewMeasurements {
	t.Helper()
	baseline := renderBoundedReviewBaseline(t)
	pair := BoundedReviewPair()
	invariantVersion, semanticVersion := assertBoundedReviewMeasurementAcceptance(t, pair)
	assets := []BoundedReviewAsset{pair.Judge, pair.Fix}
	roles := make([]boundedReviewRoleMeasurement, 0, len(assets))
	for index, asset := range assets {
		schema := boundedReviewResultSchema(t, asset.Body)
		candidate := measureBoundedReview([]byte(asset.Body))
		base := baseline.Roles[index].Measurement
		roles = append(roles, boundedReviewRoleMeasurement{
			Role: asset.Role, Baseline: base, Candidate: candidate, RequiredResultSchema: measureBoundedReview(schema), CandidateIncludesSchema: strings.Contains(asset.Body, string(schema)),
			Delta:                 boundedReviewMetrics{Bytes: candidate.Bytes - base.Bytes, Chars: candidate.Chars - base.Chars, Lines: candidate.Lines - base.Lines, EstimatedTokens: candidate.EstimatedTokens - base.EstimatedTokens},
			AbsoluteDelta:         boundedReviewMetrics{Bytes: base.Bytes - candidate.Bytes, Chars: base.Chars - candidate.Chars, Lines: base.Lines - candidate.Lines, EstimatedTokens: base.EstimatedTokens - candidate.EstimatedTokens},
			ReductionPercent:      float64(base.Chars-candidate.Chars) * 100 / float64(base.Chars),
			StaticContractVersion: asset.Handshake.StaticContractVersion, StaticContractDigest: asset.Handshake.StaticContractDigest, ProjectionID: asset.Handshake.ProjectionID,
			InvariantFixtureVersion: invariantVersion, SemanticFixtureVersion: semanticVersion, HandshakeCompatibility: "compatible",
		})
	}
	return boundedReviewMeasurements{Version: 1, MeasurementKind: "actual lore-cli static rendered role asset cost", MeasurementBoundary: boundedReviewMeasurementBoundary, ObservedDynamicRuntimeCost: "unavailable", RuntimeSyntheticProxy: "not counted; handoff-only synthetic proxy", Roles: roles}
}

func renderBoundedReviewBudgets(t *testing.T) boundedReviewBudgets {
	t.Helper()
	measurements := renderBoundedReviewMeasurements(t)
	roles := make([]boundedReviewRoleBudget, 0, len(measurements.Roles))
	for _, role := range measurements.Roles {
		roles = append(roles, boundedReviewRoleBudget{
			Role: role.Role, MaxCandidateBytes: role.Candidate.Bytes, MaxCandidateChars: role.Candidate.Chars, MaxCandidateLines: role.Candidate.Lines, MaxCandidateTokens: role.Candidate.EstimatedTokens,
			MinReductionBytes: role.Baseline.Bytes - role.Candidate.Bytes, MinReductionChars: role.Baseline.Chars - role.Candidate.Chars, RequiredFixtureVersions: fmt.Sprintf("invariants/v%d, semantics/v%d", role.InvariantFixtureVersion, role.SemanticFixtureVersion),
		})
	}
	return boundedReviewBudgets{Version: "1", Note: "Reviewed ceilings and minimum benefits are exact values derived from the corrected fallback role baselines and the measured rendered candidate bodies; they are not percentage targets. A candidate regression requires an explicit fixture review.", Roles: roles}
}

func assertBoundedReviewMeasurementAcceptance(t *testing.T, pair BoundedReviewContractPair) (int, int) {
	t.Helper()
	if err := ValidateBoundedReviewPair(pair); err != nil {
		t.Fatalf("candidate pair must pass pair acceptance before measurement: %v", err)
	}
	invariants := loadBoundedReviewInvariantFixture(t)
	semantics := loadBoundedReviewSemanticsFixture(t)
	if invariants.Version != 1 || semantics.Version != 2 || invariants.Canonical.StaticContractDigest != BoundedReviewStaticContractDigest() || invariants.Canonical.ProjectionID != BoundedReviewProjectionID {
		t.Fatalf("measurement acceptance fixture versions or static binding drifted: invariants=%d semantics=%d", invariants.Version, semantics.Version)
	}
	if !validBoundedReviewSemanticBody(pair.Judge.Body, semantics.Judge, semantics.Privacy.ForbiddenText) || !validBoundedReviewSemanticBody(pair.Fix.Body, semantics.Fix, semantics.Privacy.ForbiddenText) {
		t.Fatal("candidate body failed semantic matrix acceptance before measurement")
	}
	if got := validateBoundedReviewRuntimeHandshake(boundedReviewRuntimePair{Judge: boundedReviewProductionRuntimeAsset(pair.Judge), Fix: boundedReviewProductionRuntimeAsset(pair.Fix)}); got != "compatible" {
		t.Fatalf("candidate pair failed pinned runtime handshake before measurement: %q", got)
	}
	return invariants.Version, semantics.Version
}

func boundedReviewResultSchema(t *testing.T, body string) []byte {
	t.Helper()
	start := strings.Index(body, "{\"")
	if start < 0 {
		t.Fatal("candidate body is missing its required replacement result schema")
	}
	end := strings.Index(body[start:], "\n")
	if end < 0 {
		t.Fatal("candidate result schema must end with a newline")
	}
	return []byte(body[start : start+end])
}

func boundedReviewRolePassesGate(role boundedReviewRoleMeasurement, budget boundedReviewRoleBudget) bool {
	return role.CandidateIncludesSchema && role.HandshakeCompatibility == "compatible" && role.InvariantFixtureVersion == 1 && role.SemanticFixtureVersion == 2 &&
		role.Candidate.Bytes <= budget.MaxCandidateBytes && role.Candidate.Chars <= budget.MaxCandidateChars && role.Candidate.Lines <= budget.MaxCandidateLines && role.Candidate.EstimatedTokens <= budget.MaxCandidateTokens &&
		role.Baseline.Bytes-role.Candidate.Bytes >= budget.MinReductionBytes && role.Baseline.Chars-role.Candidate.Chars >= budget.MinReductionChars
}

func boundedReviewRoleSegment(t *testing.T, body, start, end string) []byte {
	t.Helper()
	from := strings.Index(body, start)
	if from < 0 {
		t.Fatalf("portable Judgment Day role-template start marker missing: %q", start)
	}
	from += len(start)
	to := strings.Index(body[from:], end)
	if to < 0 {
		t.Fatalf("portable Judgment Day role-template end marker missing: %q", end)
	}
	return []byte(body[from : from+to])
}

func measureBoundedReview(content []byte) boundedReviewMetrics {
	lines := 0
	if len(content) > 0 {
		text := string(content)
		lines = strings.Count(text, "\n")
		if !strings.HasSuffix(text, "\n") {
			lines++
		}
	}
	chars := utf8.RuneCount(content)
	return boundedReviewMetrics{Bytes: len(content), Chars: chars, Lines: lines, EstimatedTokens: (chars + 3) / 4}
}

func loadBoundedReviewBaseline(t *testing.T) ([]byte, boundedReviewBaseline) {
	t.Helper()
	data, err := os.ReadFile(boundedReviewBaselinePath)
	if err != nil {
		t.Fatalf("read baseline fixture: %v", err)
	}
	var baseline boundedReviewBaseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		t.Fatalf("parse baseline fixture: %v", err)
	}
	return data, baseline
}
func loadBoundedReviewMeasurements(t *testing.T) ([]byte, boundedReviewMeasurements) {
	t.Helper()
	data, err := os.ReadFile(boundedReviewMeasurementsPath)
	if err != nil {
		t.Fatalf("read measurement fixture: %v", err)
	}
	var fixture boundedReviewMeasurements
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse measurement fixture: %v", err)
	}
	return data, fixture
}
func loadBoundedReviewBudgets(t *testing.T) boundedReviewBudgets {
	t.Helper()
	data, err := os.ReadFile(boundedReviewBudgetsPath)
	if err != nil {
		t.Fatalf("read budget fixture: %v", err)
	}
	var fixture boundedReviewBudgets
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse budget fixture: %v", err)
	}
	return fixture
}
func loadBoundedReviewDigestVectors(t *testing.T) boundedReviewDigestVectors {
	t.Helper()
	data, err := os.ReadFile(boundedReviewDigestVectorsPath)
	if err != nil {
		t.Fatalf("read digest vectors fixture: %v", err)
	}
	var vectors boundedReviewDigestVectors
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatalf("parse digest vectors fixture: %v", err)
	}
	return vectors
}
func marshalBoundedReviewFixture(t *testing.T, fixture any) []byte {
	t.Helper()
	data, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return append(data, '\n')
}
