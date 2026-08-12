package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/agentpack"
)

type boundedReviewVerifyMatrix struct {
	Version                    int    `json:"version"`
	CandidateStatus            string `json:"candidate_status"`
	RuntimeActivation          string `json:"runtime_activation"`
	RuntimeDynamicPromptCost   string `json:"runtime_dynamic_prompt_cost"`
	ExistingInstallationChange string `json:"existing_installation_change"`
	Cases                      []struct {
		Name     string `json:"name"`
		Expected string `json:"expected"`
	} `json:"cases"`
}

func TestBoundedReviewFinalAcceptanceMatrix(t *testing.T) {
	matrix := loadBoundedReviewVerifyMatrix(t)
	wantCases := []string{
		"exact_runtime_handshake_and_digest_vectors",
		"complete_judge_fix_pair_and_manifest_coherence",
		"authority_shaped_child_result_vectors",
		"semantic_invariant_parity",
		"self_contained_fallback_requires_fresh_blind_rejudgment",
		"independent_schema_inclusive_static_asset_reduction",
		"staged_candidate_is_not_runtime_active",
		"pi_only_paths_metadata_and_non_pi_isolation",
		"no_opencode_codex_or_antigravity_native_claims",
		"install_reinstall_upgrade_rollback_and_failure_preserve_fallback",
		"missing_mismatched_or_partial_pair_falls_back",
		"static_assets_exclude_machine_paths_secrets_and_dynamic_user_content",
		"runtime_discovery_is_absent_and_requires_explicit_follow_up",
	}
	gotCases := make([]string, len(matrix.Cases))
	for i, tc := range matrix.Cases {
		gotCases[i] = tc.Name
		if tc.Expected != "pass" {
			t.Fatalf("matrix case %q expected=%q, want pass", tc.Name, tc.Expected)
		}
	}
	if matrix.Version != 1 || matrix.CandidateStatus != boundedReviewReleaseStatusCompatibleCandidateStaged || matrix.RuntimeActivation != "disabled" || matrix.RuntimeDynamicPromptCost != "unavailable" || matrix.ExistingInstallationChange != "reinstall-or-regeneration-required" || !reflect.DeepEqual(gotCases, wantCases) {
		t.Fatalf("invalid final acceptance matrix: %+v cases=%v", matrix, gotCases)
	}

	pair := agentpack.BoundedReviewPair()
	if err := ValidateBoundedReviewPair(pair); err != nil {
		t.Fatalf("exact current runtime-compatible pair rejected: %v", err)
	}
	if got, want := agentpack.BoundedReviewStaticContractDigest(), "cc888a92b03cc2f4bed8abd0a5efe1bd010d94d1ff6a56cfbeda82c65ed57283"; got != want {
		t.Fatalf("static digest=%q, want pinned runtime digest %q", got, want)
	}
	for _, asset := range []agentpack.BoundedReviewAsset{pair.Judge, pair.Fix} {
		if asset.Handshake.Family != "judgment-day" || asset.Handshake.ContractVersion != "1.0.0" || asset.Handshake.ProjectionID != "lore.judgment-day.bounded-review" || asset.Handshake.Harness != "pi" || asset.Handshake.ResultSchemaVersion != "judgment-review-result/v1" || asset.Handshake.StaticContractVersion != "judgment-day-static/v1" || asset.Handshake.StaticContractDigest != agentpack.BoundedReviewStaticContractDigest() || asset.Handshake.ManifestRevision != pair.Revision || asset.Handshake.Fallback {
			t.Fatalf("runtime handshake drift for %s: %+v", asset.Role, asset.Handshake)
		}
	}
	if got := []string{pair.Manifest.Roles[0].Role, pair.Manifest.Roles[1].Role}; !reflect.DeepEqual(got, []string{agentpack.BoundedReviewJudgeRole, agentpack.BoundedReviewFixRole}) {
		t.Fatalf("manifest role order=%v, want coherent Judge/Fix pair", got)
	}

	for _, mutate := range []func(*agentpack.BoundedReviewContractPair){
		func(p *agentpack.BoundedReviewContractPair) {
			p.Fix.Role = agentpack.BoundedReviewJudgeRole
			p.Fix.Handshake.Role = agentpack.BoundedReviewJudgeRole
		},
		func(p *agentpack.BoundedReviewContractPair) { p.Fix.Handshake.ManifestRevision++ },
		func(p *agentpack.BoundedReviewContractPair) { p.Manifest.Roles = p.Manifest.Roles[:1] },
		func(p *agentpack.BoundedReviewContractPair) { p.Fix.Body = "" },
	} {
		broken := agentpack.BoundedReviewPair()
		mutate(&broken)
		if err := ValidateBoundedReviewPair(broken); err == nil {
			t.Fatal("missing, mismatched, or partial pair was accepted instead of leaving fallback selectable")
		}
	}

	fallback := agentpack.JudgmentDayPortable().Body
	for _, required := range []string{
		"Every code-modifying Fix Agent action, including later rounds, MUST be followed by a fresh pair of two blind Judges before APPROVED.",
		"After every code-modifying Fix Agent action, immediately launch two fresh blind judges in parallel before any terminal judgment.",
	} {
		if !strings.Contains(fallback, required) {
			t.Fatalf("portable fallback lacks required rejudgment invariant %q", required)
		}
	}
	for _, forbidden := range []string{"native lifecycle", "native activation", "~/.pi/", "lore_worker"} {
		if strings.Contains(fallback, forbidden) {
			t.Fatalf("portable fallback makes native claim %q", forbidden)
		}
	}

	adoption := loadBoundedReviewAdoptionFixture(t)
	if adoption.FinalDecision != boundedReviewReleaseStatusCompatibleCandidateStaged || adoption.RuntimeActive || len(adoption.Roles) != 2 {
		t.Fatalf("candidate adoption is not explicitly staged and inactive: %+v", adoption)
	}
	for _, role := range adoption.Roles {
		if role.BaselineBytes-role.CandidateBytes != role.NetReductionBytes || role.BaselineChars-role.CandidateChars != role.NetReductionChars || role.ReplacementSchemaBytes <= 0 || role.ReplacementSchemaChars <= 0 || role.NetReductionBytes <= 0 || role.NetReductionChars <= 0 || !role.Semantic || !role.Invariant || !role.Handshake {
			t.Fatalf("%s lacks independent, schema-inclusive parity and reduction evidence: %+v", role.Role, role)
		}
	}

	layout := ResolvePiLayout(t.TempDir())
	if err := ApplyBoundedReviewRelease(layout); err != nil {
		t.Fatalf("Pi candidate install: %v", err)
	}
	_, releaseDir, currentPath, err := boundedReviewPaths(layout, agentpack.BoundedReviewContractVersion, agentpack.BoundedReviewManifestRevision)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateBoundedReviewReleaseDir(releaseDir); err != nil {
		t.Fatalf("installed Judge/Fix pair and manifest are incoherent: %v", err)
	}
	if _, err := os.Stat(currentPath); err != nil {
		t.Fatalf("coherent Pi release has no current pointer: %v", err)
	}
	if _, err := os.Stat(filepath.Join(layout.AgentDir, "judgment-review.json")); !os.IsNotExist(err) {
		t.Fatalf("runtime discovery/config was activated or written: %v", err)
	}
	if err := ApplyBoundedReviewRelease(layout); err != nil {
		t.Fatalf("reinstall did not preserve coherent staged pair: %v", err)
	}

	if !containsComponent(DefaultComponentSelection(TargetPi), ComponentBoundedReviewProjection) {
		t.Fatal("Pi does not select its staged-only projection")
	}
	for _, target := range []TargetID{TargetOpenCode, TargetCodex, TargetAntigravity} {
		if containsComponent(DefaultComponentSelection(target), ComponentBoundedReviewProjection) {
			t.Fatalf("%s selected Pi-only projection", target)
		}
	}
	static := pair.Judge.Body + pair.Fix.Body + string(mustReadBoundedReviewFile(t, currentPath))
	for _, forbidden := range []string{"/Users/", "C:\\\\", "{{", "}}", "test-token", "secret", "password"} {
		if strings.Contains(strings.ToLower(static), strings.ToLower(forbidden)) {
			t.Fatalf("static projection contains sensitive, machine-local, or dynamic material %q", forbidden)
		}
	}
}

func loadBoundedReviewVerifyMatrix(t *testing.T) boundedReviewVerifyMatrix {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "bounded_review_verify_matrix.json"))
	if err != nil {
		t.Fatalf("read final acceptance matrix: %v", err)
	}
	var matrix boundedReviewVerifyMatrix
	if err := json.Unmarshal(data, &matrix); err != nil {
		t.Fatalf("parse final acceptance matrix: %v", err)
	}
	return matrix
}

func mustReadBoundedReviewFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
