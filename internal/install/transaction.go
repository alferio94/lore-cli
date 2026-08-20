package install

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/alferio94/lore-cli/internal/compiler"
	"github.com/alferio94/lore-cli/internal/manifest"
	"github.com/alferio94/lore-cli/internal/reconcile"
)

const provenanceV3Name = "lore-provenance-v3.json"

type TransactionCode string

const (
	CodeTransactionInvalidIR         TransactionCode = "invalid_ir"
	CodeTransactionInvalidTarget     TransactionCode = "invalid_target"
	CodeTransactionTargetMismatch    TransactionCode = "target_mismatch"
	CodeTransactionIdentityMismatch  TransactionCode = "identity_mismatch"
	CodeTransactionReconcileRejected TransactionCode = "reconcile_rejected"
	CodeTransactionPermitMismatch    TransactionCode = "finalization_permit_mismatch"
	CodeTransactionManifestMismatch  TransactionCode = "manifest_mismatch"
	CodeTransactionInvalidPath       TransactionCode = "invalid_provenance_path"
	CodeTransactionDrift             TransactionCode = "apply_time_drift"
	CodeTransactionServerBoundary    TransactionCode = "server_boundary"
)

func (c TransactionCode) Error() string { return string(c) }

type TransactionError struct {
	code TransactionCode
	path string
}

func (e *TransactionError) Error() string         { return "transaction plan rejected" }
func (e *TransactionError) Code() TransactionCode { return e.code }
func (e *TransactionError) Path() string          { return e.path }
func (e *TransactionError) Is(target error) bool {
	code, ok := target.(TransactionCode)
	return ok && code == e.code
}
func transactionError(code TransactionCode, path string) error {
	return &TransactionError{code: code, path: path}
}

type TransactionDriftFact struct {
	Resource string
	Present  bool
	Observed []byte
}

type TransactionServerIdentity struct {
	ProjectID, ProjectKey, RepositoryID, InferredFrom string
}

type TransactionInput struct {
	IR                compiler.ResolvedIR
	Semantic          SemanticPlan
	Reconcile         reconcile.Report
	Permit            reconcile.FinalizationPermit
	Targets           []TargetID
	Layout            HarnessLayout
	ProvenancePath    string
	Manifest          manifest.Manifest
	ProfileCompletion PersistenceFact
	Drift             []TransactionDriftFact
	ServerIdentity    TransactionServerIdentity
}

// TransactionEffects is the future apply seam. DryRun deliberately accepts it
// only so tests and callers can prove that planning invokes no effect.
type TransactionEffects interface {
	Read(string) ([]byte, error)
	Backup(string) error
	Write(string, []byte) error
	Resolve(string, string) ([]byte, error)
	CompleteProfile(PersistenceFact) error
	PublishManifest(string, []byte) error
	Network(string) error
}

type TransactionDecision struct {
	Resource            string
	Mode                reconcile.Mode
	Outcome             reconcile.Outcome
	PriorHash, NextHash string
}

type TransactionReport struct {
	Target            TargetID
	IRID              string
	ProvenancePath    string
	ManifestHash      string
	Profile           PersistenceFact
	Decisions         []TransactionDecision
	AllAdmitted       bool
	FinalizationCount int
	MutationCount     int
}

type TransactionPlan struct{ report TransactionReport }

func (p TransactionPlan) IsZero() bool { return p.report.Target == "" }

// DryRun returns only deterministic, non-secret facts and never invokes effects.
func (p TransactionPlan) DryRun(_ TransactionEffects) TransactionReport {
	return cloneTransactionReport(p.report)
}

func SealTransactionPlan(input TransactionInput) (TransactionPlan, error) {
	ir := input.IR
	if !ir.Sealed() || !ir.Admitted || ir.IRID() == "" {
		return TransactionPlan{}, transactionError(CodeTransactionInvalidIR, "ir")
	}
	if len(input.Targets) != 1 || !knownProjectorTarget(input.Targets[0]) {
		return TransactionPlan{}, transactionError(CodeTransactionInvalidTarget, "targets")
	}
	target := input.Targets[0]
	if compiler.TargetID(target) != ir.Target {
		return TransactionPlan{}, transactionError(CodeTransactionTargetMismatch, "targets[0]")
	}
	facts := input.Semantic.TargetFacts()
	if facts.Target != target || input.Layout.Target != target {
		return TransactionPlan{}, transactionError(CodeTransactionTargetMismatch, "semantic.target")
	}
	if facts.IRID != ir.IRID() {
		return TransactionPlan{}, transactionError(CodeTransactionIdentityMismatch, "semantic.ir_id")
	}
	if input.ServerIdentity != (TransactionServerIdentity{}) {
		return TransactionPlan{}, transactionError(CodeTransactionServerBoundary, "server_identity")
	}
	if !reflect.DeepEqual(input.ProfileCompletion, facts.Persistence) {
		return TransactionPlan{}, transactionError(CodeTransactionIdentityMismatch, "profile_completion.project_id")
	}

	expected, err := reconcile.Reconcile(ir, input.Semantic.Intents())
	if err != nil || !input.Reconcile.AllAdmitted() || !expected.AllAdmitted() {
		return TransactionPlan{}, transactionError(CodeTransactionReconcileRejected, "reconcile")
	}
	decisions := input.Reconcile.Decisions()
	if !reflect.DeepEqual(decisions, expected.Decisions()) {
		return TransactionPlan{}, transactionError(CodeTransactionReconcileRejected, "reconcile")
	}
	expectedPermit, hasPermit := expected.FinalizationPermit()
	if !permitEqual(input.Permit, expectedPermit, hasPermit) {
		return TransactionPlan{}, transactionError(CodeTransactionPermitMismatch, "finalization_permit")
	}
	if err := validateDrift(facts.Resources, input.Drift); err != nil {
		return TransactionPlan{}, err
	}
	if err := validateProvenancePath(input.Layout, input.ProvenancePath); err != nil {
		return TransactionPlan{}, err
	}
	canonical, err := manifest.Encode(input.Manifest)
	if err != nil {
		return TransactionPlan{}, transactionError(CodeTransactionManifestMismatch, manifestErrorPath(err))
	}
	normalized, err := manifest.Decode(canonical)
	if err != nil {
		return TransactionPlan{}, transactionError(CodeTransactionManifestMismatch, manifestErrorPath(err))
	}
	if err := validateManifestBinding(normalized, ir, facts, decisions, expectedPermit, hasPermit); err != nil {
		return TransactionPlan{}, err
	}

	report := TransactionReport{Target: target, IRID: ir.IRID(), ProvenancePath: input.ProvenancePath, Profile: facts.Persistence, AllAdmitted: true, MutationCount: 0}
	sum := sha256.Sum256(canonical)
	report.ManifestHash = fmt.Sprintf("%x", sum)
	if hasPermit {
		report.FinalizationCount = len(expectedPermit.Projections())
	}
	for _, decision := range decisions {
		report.Decisions = append(report.Decisions, TransactionDecision{Resource: decision.Resource, Mode: decision.Mode, Outcome: decision.Outcome, PriorHash: decision.PriorHash, NextHash: decision.NextHash})
	}
	return TransactionPlan{report: cloneTransactionReport(report)}, nil
}

func permitEqual(got, want reconcile.FinalizationPermit, required bool) bool {
	if !required {
		return got.IRID() == "" && len(got.Projections()) == 0
	}
	return got.IRID() == want.IRID() && reflect.DeepEqual(got.Projections(), want.Projections())
}

func validateDrift(resources []SemanticResourceFact, drift []TransactionDriftFact) error {
	got := append([]TransactionDriftFact(nil), drift...)
	for i := range got {
		got[i].Observed = append([]byte(nil), drift[i].Observed...)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].Resource < got[j].Resource })
	if len(got) != len(resources) {
		return transactionError(CodeTransactionDrift, "drift")
	}
	for i, fact := range resources {
		if got[i].Resource != fact.Resource || got[i].Present != fact.Present || !bytes.Equal(got[i].Observed, fact.Observed) {
			return transactionError(CodeTransactionDrift, "drift."+got[i].Resource)
		}
	}
	return nil
}

func validateProvenancePath(layout HarnessLayout, path string) error {
	if !filepath.IsAbs(layout.RootDir) || !filepath.IsAbs(layout.ManifestPath) || filepath.Clean(path) != filepath.Join(filepath.Dir(layout.ManifestPath), provenanceV3Name) || filepath.Clean(path) == filepath.Clean(layout.ManifestPath) {
		return transactionError(CodeTransactionInvalidPath, "provenance_path")
	}
	rel, err := filepath.Rel(filepath.Clean(layout.RootDir), filepath.Clean(path))
	if err != nil || rel == ".." || filepath.IsAbs(rel) || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator) {
		return transactionError(CodeTransactionInvalidPath, "provenance_path")
	}
	return nil
}

func validateManifestBinding(m manifest.Manifest, ir compiler.ResolvedIR, facts TargetFacts, decisions []reconcile.Decision, permit reconcile.FinalizationPermit, hasPermit bool) error {
	if m.Target != string(facts.Target) {
		return transactionError(CodeTransactionManifestMismatch, "manifest.target")
	}
	if m.SchemaVersion != manifest.SchemaV3 || m.CompilerVersion != ir.Compiler || m.InputID != ir.InputID() || m.ResolvedIRID != ir.IRID() || m.Profile.ID != facts.Profile.Winner.ProfileID || m.Profile.Scope != string(facts.Persistence.Scope) || m.Reconciliation.Owner != "compiler" || m.Reconciliation.Decision != "admitted" {
		return transactionError(CodeTransactionManifestMismatch, "manifest.identity")
	}
	projections := m.ProjectionsCopy()
	if len(projections) != len(decisions) {
		return transactionError(CodeTransactionManifestMismatch, "manifest.projections")
	}
	for i, decision := range decisions {
		if projections[i] != (manifest.Projection{Path: decision.Resource, Ownership: string(decision.Mode), Hash: decision.NextHash}) {
			return transactionError(CodeTransactionManifestMismatch, "manifest.projections")
		}
	}
	if len(m.RoleModelsCopy()) != len(facts.Roles) || len(m.CapabilitiesCopy()) != len(facts.Capabilities) {
		return transactionError(CodeTransactionManifestMismatch, "manifest.provenance")
	}
	for i, role := range facts.Roles {
		got := m.RoleModelsCopy()[i]
		if got.Role != role.Role || got.Model != role.Model || got.Source != role.Winner.SourceKey || got.Location != role.Winner.Location {
			return transactionError(CodeTransactionManifestMismatch, "manifest.role_models")
		}
	}
	for i, capability := range facts.Capabilities {
		got := m.CapabilitiesCopy()[i]
		if got.ID != string(capability.ID) || got.State != string(capability.State) || got.Reason != capability.Reason {
			return transactionError(CodeTransactionManifestMismatch, "manifest.capabilities")
		}
	}
	wantFinal := []manifest.FinalizationReference{}
	if hasPermit {
		for _, fact := range facts.Resources {
			for _, ref := range fact.SensitiveReferences {
				wantFinal = append(wantFinal, manifest.FinalizationReference{Path: fact.Resource, FinalizerID: ref.FinalizerID, Provider: ref.Provider, Slot: ref.Slot})
			}
		}
		if len(wantFinal) != len(permit.Projections()) {
			return transactionError(CodeTransactionManifestMismatch, "manifest.finalizations")
		}
	}
	sort.Slice(wantFinal, func(i, j int) bool { return wantFinal[i].Path < wantFinal[j].Path })
	if !reflect.DeepEqual(m.FinalizationsCopy(), wantFinal) {
		return transactionError(CodeTransactionManifestMismatch, "manifest.finalizations")
	}
	return nil
}

func manifestErrorPath(err error) string {
	if typed, ok := err.(*manifest.CodecError); ok {
		return "manifest." + typed.Path()
	}
	return "manifest"
}

func cloneTransactionReport(in TransactionReport) TransactionReport {
	out := in
	out.Decisions = append([]TransactionDecision(nil), in.Decisions...)
	return out
}
