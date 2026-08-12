package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/alferio94/lore-cli/internal/agentpack"
)

type boundedReviewAdoptionFixture struct {
	Version       int    `json:"version"`
	FinalDecision string `json:"final_decision"`
	RuntimeActive bool   `json:"runtime_active"`
	Roles         []struct {
		Role                   string  `json:"role"`
		BaselineBytes          int     `json:"baseline_bytes"`
		CandidateBytes         int     `json:"candidate_bytes"`
		ReplacementSchemaBytes int     `json:"replacement_schema_bytes"`
		NetReductionBytes      int     `json:"net_reduction_bytes"`
		BaselineChars          int     `json:"baseline_chars"`
		CandidateChars         int     `json:"candidate_chars"`
		ReplacementSchemaChars int     `json:"replacement_schema_chars"`
		NetReductionChars      int     `json:"net_reduction_chars"`
		ReductionPercent       float64 `json:"reduction_percent"`
		Semantic               bool    `json:"semantic"`
		Invariant              bool    `json:"invariant"`
		Handshake              bool    `json:"handshake"`
	} `json:"roles"`
	Cases []boundedReviewAdoptionCase `json:"cases"`
}

type boundedReviewAdoptionCase struct {
	Name                  string `json:"name"`
	JudgeNetReduction     int    `json:"judge_net_reduction"`
	FixNetReduction       int    `json:"fix_net_reduction"`
	Semantic              bool   `json:"semantic"`
	Invariant             bool   `json:"invariant"`
	Handshake             bool   `json:"handshake"`
	PairInstallRollback   bool   `json:"pair_install_rollback"`
	Fallback              bool   `json:"fallback"`
	CrossHarnessIsolation bool   `json:"cross_harness_isolation"`
	ExpectedAdopted       bool   `json:"expected_adopted"`
}

func TestBoundedReviewAdoptionDecisionIsIndependentAndFailClosed(t *testing.T) {
	fixture := loadBoundedReviewAdoptionFixture(t)
	if fixture.Version != 1 || fixture.FinalDecision != boundedReviewReleaseStatusCompatibleCandidateStaged || fixture.RuntimeActive {
		t.Fatalf("adoption fixture decision=%q runtime_active=%t, want explicit staged compatible candidate and inactive runtime", fixture.FinalDecision, fixture.RuntimeActive)
	}
	if got, want := len(fixture.Roles), 2; got != want {
		t.Fatalf("adoption roles=%d, want %d", got, want)
	}
	for index, wantRole := range []string{agentpack.BoundedReviewJudgeRole, agentpack.BoundedReviewFixRole} {
		role := fixture.Roles[index]
		if role.Role != wantRole || role.NetReductionBytes != role.BaselineBytes-role.CandidateBytes || role.NetReductionChars != role.BaselineChars-role.CandidateChars || role.ReplacementSchemaBytes <= 0 || role.ReplacementSchemaChars <= 0 || role.ReplacementSchemaBytes > role.CandidateBytes || role.ReplacementSchemaChars > role.CandidateChars || role.ReductionPercent <= 0 || !role.Semantic || !role.Invariant || !role.Handshake {
			t.Fatalf("%s role evidence is not an exact positive, schema-inclusive, compatible gate: %+v", wantRole, role)
		}
	}
	if got, want := len(fixture.Cases), 9; got != want {
		t.Fatalf("adoption cases=%d, want %d", got, want)
	}
	for _, tc := range fixture.Cases {
		if got := boundedReviewCandidateAdopted(tc); got != tc.ExpectedAdopted {
			t.Errorf("%s adopted=%t, want %t", tc.Name, got, tc.ExpectedAdopted)
		}
	}
}

func TestBoundedReviewAdoptionRetainsOnlyExplicitlyStagedInactiveManifestState(t *testing.T) {
	pair := agentpack.BoundedReviewPair()
	if err := ValidateBoundedReviewPair(pair); err != nil {
		t.Fatalf("candidate pair compatibility: %v", err)
	}
	layout := ResolvePiLayout(t.TempDir())
	if err := ApplyBoundedReviewRelease(layout); err != nil {
		t.Fatalf("candidate release install: %v", err)
	}
	if _, err := os.Stat(filepath.Join(layout.AgentDir, "judgment-review.json")); !os.IsNotExist(err) {
		t.Fatalf("candidate assets inferred or enabled runtime configuration: %v", err)
	}

	manifest := validManifestForTest(layout)
	manifest.BoundedReviewRelease = &BoundedReviewReleaseRecord{
		Version: agentpack.BoundedReviewContractVersion, Revision: agentpack.BoundedReviewManifestRevision,
		CurrentPath:  filepath.ToSlash(filepath.Join(boundedReviewReleaseRootRelativePath, "current.json")),
		ManifestPath: filepath.ToSlash(filepath.Join("releases", "1.0.0-r1", "manifest.json")),
	}
	if err := manifest.Validate(layout); err == nil {
		t.Fatal("asset presence without explicit staged adoption status was accepted")
	}
	manifest.BoundedReviewRelease.Status = boundedReviewReleaseStatusCompatibleCandidateStaged
	manifest.BoundedReviewRelease.RuntimeActive = true
	if err := manifest.Validate(layout); err == nil {
		t.Fatal("runtime-active bounded review manifest state was accepted")
	}
	manifest.BoundedReviewRelease.RuntimeActive = false
	if err := manifest.Validate(layout); err != nil {
		t.Fatalf("explicit staged inactive candidate rejected: %v", err)
	}
}

func boundedReviewCandidateAdopted(tc boundedReviewAdoptionCase) bool {
	return tc.JudgeNetReduction > 0 && tc.FixNetReduction > 0 && tc.Semantic && tc.Invariant && tc.Handshake && tc.PairInstallRollback && tc.Fallback && tc.CrossHarnessIsolation
}

func loadBoundedReviewAdoptionFixture(t *testing.T) boundedReviewAdoptionFixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "bounded_review_adoption_cases.json"))
	if err != nil {
		t.Fatalf("read adoption fixture: %v", err)
	}
	var fixture boundedReviewAdoptionFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse adoption fixture: %v", err)
	}
	return fixture
}
