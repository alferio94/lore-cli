package install

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/compiler"
	"github.com/alferio94/lore-cli/internal/manifest"
	"github.com/alferio94/lore-cli/internal/reconcile"
)

func TestW33ATransactionDryRunIsDeterministicDefensiveAndEffectFree(t *testing.T) {
	for _, target := range []TargetID{TargetPi, TargetOpenCode} {
		t.Run(string(target), func(t *testing.T) {
			root := t.TempDir()
			first := transactionFixture(t, target, root, false)
			second := transactionFixture(t, target, root, true)
			plan, err := SealTransactionPlan(first)
			if err != nil {
				t.Fatalf("SealTransactionPlan() error = %#v", err)
			}
			permuted, err := SealTransactionPlan(second)
			if err != nil {
				t.Fatalf("SealTransactionPlan(permuted) error = %#v", err)
			}
			spy := &transactionEffectsSpy{secret: "raw-W33A-secret"}
			got := plan.DryRun(spy)
			again := permuted.DryRun(spy)
			if !reflect.DeepEqual(got, again) {
				t.Fatalf("permuted report differs:\n%#v\n%#v", got, again)
			}
			if got.Target != target || got.IRID != first.IR.IRID() || got.ProvenancePath != first.ProvenancePath || !got.AllAdmitted || got.MutationCount != 0 || len(got.Decisions) != 3 || got.FinalizationCount != 1 {
				t.Fatalf("DryRun() = %#v", got)
			}
			if spy.calls != 0 || strings.Contains(fmt.Sprintf("%#v", got), spy.secret) {
				t.Fatalf("dry run invoked effects or leaked secret: calls=%d report=%#v", spy.calls, got)
			}
			entries, readErr := filepath.Glob(filepath.Join(root, "*"))
			if readErr != nil || len(entries) != 0 {
				t.Fatalf("dry run filesystem entries = %v, error = %v", entries, readErr)
			}

			got.Decisions[0].Resource = "mutated"
			got.Profile.ProfileID = "mutated"
			fresh := plan.DryRun(spy)
			if fresh.Decisions[0].Resource == "mutated" || fresh.Profile.ProfileID == "mutated" {
				t.Fatal("DryRun() return aliases plan state")
			}
		})
	}
}

func TestW33ATransactionRejectsEveryMismatchWithZeroOutputAndEffects(t *testing.T) {
	root := t.TempDir()
	base := transactionFixture(t, TargetPi, root, false)
	openCode := transactionFixture(t, TargetOpenCode, root, false)
	conflicted := base
	conflictedInput := projectorInput(TargetPi, false)
	conflictedInput.Resources[2].Evidence = reconcile.Evidence{Foreign: true}
	conflictedInput.Resources[2].Present = true
	conflictedInput.Resources[2].Observed = []byte("foreign")
	conflicted.Semantic = projectMust(t, conflicted.IR, conflictedInput)
	conflicted.Reconcile, _ = reconcile.Reconcile(conflicted.IR, conflicted.Semantic.Intents())

	cases := []struct {
		name, path string
		code       TransactionCode
		mutate     func(*TransactionInput)
	}{
		{"unadmitted IR", "ir", CodeTransactionInvalidIR, func(in *TransactionInput) { in.IR = compiler.ResolvedIR{} }},
		{"unsealed IR", "ir", CodeTransactionInvalidIR, func(in *TransactionInput) { in.IR = compiler.ResolvedIR{Target: compiler.TargetPi, Admitted: true} }},
		{"multiple targets", "targets", CodeTransactionInvalidTarget, func(in *TransactionInput) { in.Targets = append(in.Targets, TargetOpenCode) }},
		{"target mismatch", "targets[0]", CodeTransactionTargetMismatch, func(in *TransactionInput) { in.Targets[0] = TargetOpenCode }},
		{"fact mismatch", "semantic.target", CodeTransactionTargetMismatch, func(in *TransactionInput) { in.Semantic = openCode.Semantic }},
		{"identity mismatch", "profile_completion.project_id", CodeTransactionIdentityMismatch, func(in *TransactionInput) { in.ProfileCompletion.ProjectID = "project:other" }},
		{"conflicted reconcile", "reconcile", CodeTransactionReconcileRejected, func(in *TransactionInput) { in.Reconcile = conflicted.Reconcile }},
		{"invalid permit", "finalization_permit", CodeTransactionPermitMismatch, func(in *TransactionInput) { in.Permit = reconcile.FinalizationPermit{} }},
		{"mismatched permit", "finalization_permit", CodeTransactionPermitMismatch, func(in *TransactionInput) { in.Permit = openCode.Permit }},
		{"manifest schema", "manifest.schema_version", CodeTransactionManifestMismatch, func(in *TransactionInput) { in.Manifest.SchemaVersion = 2 }},
		{"manifest target", "manifest.target", CodeTransactionManifestMismatch, func(in *TransactionInput) {
			in.Manifest.Target = "opencode"
			in.Manifest.RollbackBoundary.Target = "opencode"
		}},
		{"provenance path", "provenance_path", CodeTransactionInvalidPath, func(in *TransactionInput) { in.ProvenancePath = filepath.Join(root, "lore-install.json") }},
		{"drift bytes", "drift.mcp/lore.json", CodeTransactionDrift, func(in *TransactionInput) { in.Drift[1].Observed = []byte("changed") }},
		{"server identity", "server_identity", CodeTransactionServerBoundary, func(in *TransactionInput) { in.ServerIdentity.ProjectID = "server-uuid" }},
		{"repository inference", "server_identity", CodeTransactionServerBoundary, func(in *TransactionInput) { in.ServerIdentity.RepositoryID = "inferred-from-git" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := cloneTransactionInput(base)
			tc.mutate(&in)
			spy := &transactionEffectsSpy{secret: "raw-rejected-secret"}
			got, err := SealTransactionPlan(in)
			assertTransactionError(t, err, tc.code, tc.path)
			if !got.IsZero() || spy.calls != 0 || strings.Contains(err.Error(), spy.secret) {
				t.Fatalf("rejection output/effects/error = %#v/%d/%q", got, spy.calls, err)
			}
		})
	}
}

func transactionFixture(t *testing.T, target TargetID, root string, reverse bool) TransactionInput {
	t.Helper()
	ir := projectorIR(t, target)
	semantic := projectMust(t, ir, projectorInput(target, reverse))
	report, err := reconcile.Reconcile(ir, semantic.Intents())
	if err != nil {
		t.Fatal(err)
	}
	permit, ok := report.FinalizationPermit()
	if !ok {
		t.Fatal("fixture missing finalization permit")
	}
	facts, decisions := semantic.TargetFacts(), report.Decisions()
	projections := make([]manifest.Projection, len(decisions))
	drift := make([]TransactionDriftFact, len(facts.Resources))
	for i, decision := range decisions {
		projections[i] = manifest.Projection{Path: decision.Resource, Ownership: string(decision.Mode), Hash: decision.NextHash}
	}
	for i, fact := range facts.Resources {
		drift[i] = TransactionDriftFact{Resource: fact.Resource, Present: fact.Present, Observed: append([]byte(nil), fact.Observed...)}
	}
	finalizations := []manifest.FinalizationReference{{Path: "mcp/lore.json", FinalizerID: "mcp-finalizer", Provider: "vault", Slot: "lore"}}
	roles := make([]manifest.RoleModelProvenance, len(facts.Roles))
	for i, role := range facts.Roles {
		roles[i] = manifest.RoleModelProvenance{Role: role.Role, Model: role.Model, Source: role.Winner.SourceKey, Location: role.Winner.Location}
	}
	capabilities := make([]manifest.CapabilityProvenance, len(facts.Capabilities))
	for i, capability := range facts.Capabilities {
		capabilities[i] = manifest.CapabilityProvenance{ID: string(capability.ID), State: string(capability.State), Reason: capability.Reason}
	}
	m := manifest.Manifest{
		SchemaVersion: manifest.SchemaV3, CompilerVersion: ir.Compiler,
		Pack: manifest.PackIdentity{ID: "portable-agent-pack", Version: "1.0.0"}, InputID: ir.InputID(), ResolvedIRID: ir.IRID(),
		Profile: manifest.ProfileIdentity{ID: facts.Profile.Winner.ProfileID, Version: "1.0.0", Scope: string(facts.Persistence.Scope)}, Target: string(target),
		Projections: projections, RollbackBoundary: manifest.RollbackBoundary{ID: "rb-v1", Kind: "selected-target-backup-v1", Target: string(target), Resources: append([]manifest.Projection(nil), projections...), Finalizations: append([]manifest.FinalizationReference(nil), finalizations...)},
		RoleModels: roles, Capabilities: capabilities,
		Reconciliation: manifest.ReconciliationProvenance{Owner: "compiler", Decision: "admitted"}, Finalizations: finalizations,
	}
	return TransactionInput{IR: ir, Semantic: semantic, Reconcile: report, Permit: permit, Targets: []TargetID{target}, Layout: HarnessLayout{Target: target, RootDir: root, ManifestPath: filepath.Join(root, "lore-install.json")}, ProvenancePath: filepath.Join(root, "lore-provenance-v3.json"), Manifest: m, ProfileCompletion: facts.Persistence, Drift: drift}
}

func cloneTransactionInput(in TransactionInput) TransactionInput {
	out := in
	out.Targets = append([]TargetID(nil), in.Targets...)
	out.Drift = append([]TransactionDriftFact(nil), in.Drift...)
	for i := range out.Drift {
		out.Drift[i].Observed = append([]byte(nil), in.Drift[i].Observed...)
	}
	out.Manifest.Projections = append([]manifest.Projection(nil), in.Manifest.Projections...)
	out.Manifest.RollbackBoundary = in.Manifest.RollbackBoundaryCopy()
	out.Manifest.Finalizations = append([]manifest.FinalizationReference(nil), in.Manifest.Finalizations...)
	return out
}

type transactionEffectsSpy struct {
	calls  int
	secret string
}

func (s *transactionEffectsSpy) Read(string) ([]byte, error) {
	s.calls++
	return nil, errors.New("read called")
}
func (s *transactionEffectsSpy) Backup(string) error { s.calls++; return errors.New("backup called") }
func (s *transactionEffectsSpy) Write(string, []byte) error {
	s.calls++
	return errors.New("write called")
}
func (s *transactionEffectsSpy) Resolve(string, string) ([]byte, error) {
	s.calls++
	return []byte(s.secret), errors.New("resolve called")
}
func (s *transactionEffectsSpy) CompleteProfile(PersistenceFact) error {
	s.calls++
	return errors.New("profile called")
}
func (s *transactionEffectsSpy) PublishManifest(string, []byte) error {
	s.calls++
	return errors.New("manifest called")
}
func (s *transactionEffectsSpy) Network(string) error { s.calls++; return errors.New("network called") }

func assertTransactionError(t *testing.T, err error, code TransactionCode, path string) {
	t.Helper()
	if err == nil || !errors.Is(err, code) {
		t.Fatalf("error = %v, want %q", err, code)
	}
	var typed *TransactionError
	if !errors.As(err, &typed) || typed.Code() != code || typed.Path() != path {
		t.Fatalf("error = %#v, want %q@%s", err, code, path)
	}
}
