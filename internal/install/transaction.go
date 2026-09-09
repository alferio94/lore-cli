package install

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"sync/atomic"

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

const (
	hostedMCPHandoffFresh uint32 = iota
	hostedMCPHandoffClaimed
	hostedMCPHandoffDisposed
)

// hostedMCPCompletionHandoff is the factory-only transfer of one active sealed
// selected-target journal from C to D. Its fields and lifecycle remain internal
// so callers cannot substitute paths, manifest bytes, credentials, or permits.
type hostedMCPCompletionHandoff struct {
	journal        *transactionFSJournal
	report         TransactionReport
	targetRoot     string
	resource       string
	planIRID       string
	inputIRID      string
	protectedWrite bool
	statePaths     []string
	stateCount     int
	mutatedCount   int
	provenancePath string
	provenanceRel  string
	manifestHash   string
	profile        PersistenceFact
	state          atomic.Uint32
}

// hostedMCPCompletionOwner is the post-claim D-only journal seam. Unit 1
// intentionally exposes rollback disposal only; Unit 2 extends this owner with
// reversible profile and manifest-last completion before the sole commit.
type hostedMCPCompletionOwner struct {
	handoff *hostedMCPCompletionHandoff
	journal *transactionFSJournal
}

type completionJournalPlatform interface {
	appendCompletionBackup(string, int, []transactionFSState) (transactionFSState, error)
	publishCompletionManifest(string, []byte, transactionFSFailpoint) error
}

type hostedMCPManifestEncoder func(manifest.Manifest) ([]byte, error)

var errTransactionPostCommitCleanup = errors.New("transaction post-commit cleanup failed")

func newHostedMCPCompletionHandoff(plan TransactionPlan, input TransactionInput, resealed TransactionPlan, journal *transactionFSJournal, resource string) (*hostedMCPCompletionHandoff, error) {
	if plan.IsZero() || resealed.IsZero() || !reflect.DeepEqual(plan.DryRun(nil), resealed.DryRun(nil)) || !validHostedMCPJournalState(journal, resource) {
		return nil, hostedMCPError(CodeHostedMCPInvalidIntent)
	}
	report := resealed.DryRun(nil)
	root := filepath.Clean(input.Layout.RootDir)
	if !filepath.IsAbs(root) || input.Layout.Target != report.Target || filepath.Clean(report.ProvenancePath) != filepath.Clean(input.ProvenancePath) {
		return nil, hostedMCPError(CodeHostedMCPInvalidIntent)
	}
	paths := hostedMCPJournalStatePaths(journal)
	nativeProvenanceRel, err := filepath.Rel(root, filepath.Clean(report.ProvenancePath))
	if err != nil {
		return nil, hostedMCPError(CodeHostedMCPInvalidIntent)
	}
	provenanceRel, err := transactionLogicalPathFromNative(nativeProvenanceRel)
	if err != nil || transactionPathBase(provenanceRel) != provenanceV3Name {
		return nil, hostedMCPError(CodeHostedMCPInvalidIntent)
	}
	return &hostedMCPCompletionHandoff{
		journal:        journal,
		report:         cloneTransactionReport(report),
		targetRoot:     root,
		resource:       resource,
		planIRID:       plan.DryRun(nil).IRID,
		inputIRID:      input.IR.IRID(),
		protectedWrite: true,
		statePaths:     paths,
		stateCount:     len(journal.states),
		mutatedCount:   journal.mutated,
		provenancePath: report.ProvenancePath,
		provenanceRel:  provenanceRel,
		manifestHash:   report.ManifestHash,
		profile:        report.Profile,
	}, nil
}

func claimHostedMCPCompletionHandoff(handoff *hostedMCPCompletionHandoff, plan TransactionPlan, input TransactionInput) (*hostedMCPCompletionOwner, error) {
	if handoff == nil || handoff.state.Load() != hostedMCPHandoffFresh {
		return nil, hostedMCPError(CodeHostedMCPInvalidIntent)
	}
	resealed, err := SealTransactionPlan(input)
	if err != nil {
		return nil, hostedMCPError(CodeHostedMCPInvalidIntent, err)
	}
	report := resealed.DryRun(nil)
	journal := handoff.journal
	if plan.IsZero() || !reflect.DeepEqual(plan.DryRun(nil), report) || !reflect.DeepEqual(handoff.report, report) ||
		handoff.targetRoot != filepath.Clean(input.Layout.RootDir) || handoff.provenancePath != report.ProvenancePath ||
		handoff.provenanceRel != completionProvenanceRelativePath(input.Layout.RootDir, report.ProvenancePath) ||
		handoff.planIRID != plan.DryRun(nil).IRID || handoff.inputIRID != input.IR.IRID() || !handoff.protectedWrite ||
		handoff.manifestHash != report.ManifestHash || !reflect.DeepEqual(handoff.profile, report.Profile) ||
		journal == nil || journal.platform == nil || handoff.stateCount != len(journal.states) || handoff.mutatedCount != journal.mutated ||
		!reflect.DeepEqual(handoff.statePaths, hostedMCPJournalStatePaths(journal)) || !validHostedMCPJournalState(journal, handoff.resource) {
		return nil, hostedMCPError(CodeHostedMCPInvalidIntent)
	}
	if !handoff.state.CompareAndSwap(hostedMCPHandoffFresh, hostedMCPHandoffClaimed) {
		return nil, hostedMCPError(CodeHostedMCPInvalidIntent)
	}
	return &hostedMCPCompletionOwner{handoff: handoff, journal: journal}, nil
}

func rollbackHostedMCPCompletion(owner *hostedMCPCompletionOwner, primary error) error {
	if owner == nil || owner.handoff == nil || owner.journal == nil || !owner.handoff.state.CompareAndSwap(hostedMCPHandoffClaimed, hostedMCPHandoffDisposed) {
		return hostedMCPError(CodeHostedMCPInvalidIntent)
	}
	if err := owner.journal.Rollback(); err != nil {
		return transactionFSResidualRiskError(primary, err)
	}
	return primary
}

func completionProvenanceRelativePath(root, nativePath string) string {
	root, nativePath = filepath.Clean(root), filepath.Clean(nativePath)
	nativeRel, err := filepath.Rel(root, nativePath)
	if err != nil {
		return ""
	}
	logical, err := transactionLogicalPathFromNative(nativeRel)
	if err != nil || transactionPathBase(logical) != provenanceV3Name {
		return ""
	}
	return logical
}

// completeHostedMCPCompletion is D's sole completion entry. It claims exactly
// one Unit-1 handoff, keeps target authority outermost, holds profile authority
// through canonical v3 publication, and performs one final commit/release.
func completeHostedMCPCompletion(handoff *hostedMCPCompletionHandoff, plan TransactionPlan, input TransactionInput, store ProfileStore, prepared PreparedProject) error {
	return completeHostedMCPCompletionWithEncoder(handoff, plan, input, store, prepared, manifest.Encode)
}

func completeHostedMCPCompletionWithEncoder(handoff *hostedMCPCompletionHandoff, plan TransactionPlan, input TransactionInput, store ProfileStore, prepared PreparedProject, encode hostedMCPManifestEncoder) error {
	owner, err := claimHostedMCPCompletionHandoff(handoff, plan, input)
	if err != nil {
		return err
	}
	if encode == nil || owner.handoff.provenanceRel == "" || owner.handoff.provenanceRel != completionProvenanceRelativePath(input.Layout.RootDir, input.ProvenancePath) {
		return rollbackHostedMCPCompletion(owner, hostedMCPError(CodeHostedMCPInvalidIntent))
	}

	lease, err := store.beginHeldProfileCompletion(prepared, owner.handoff.profile, CommitOptions{WaitBudget: defaultProfileStoreWait})
	if err != nil {
		primary := err
		var profileErr *ProfileStoreError
		profileResidual := errors.As(err, &profileErr) && profileErr.ResidualRisk()
		rolled := rollbackHostedMCPCompletion(owner, primary)
		if profileResidual || errors.Is(rolled, CodeTransactionResidualRisk) {
			return transactionFSResidualRiskError(primary, rolled)
		}
		return rolled
	}

	canonical, err := encode(input.Manifest)
	if err != nil {
		return rollbackHostedMCPCompletionD(owner, lease, len(owner.journal.states), err)
	}
	sum := sha256.Sum256(canonical)
	if fmt.Sprintf("%x", sum) != owner.handoff.manifestHash {
		return rollbackHostedMCPCompletionD(owner, lease, len(owner.journal.states), hostedMCPError(CodeHostedMCPInvalidIntent))
	}
	platform, ok := owner.journal.platform.(completionJournalPlatform)
	if !ok || platform == nil {
		return rollbackHostedMCPCompletionD(owner, lease, len(owner.journal.states), transactionFSIOError("transaction.journal"))
	}
	manifestIndex := len(owner.journal.states)
	state, err := platform.appendCompletionBackup(owner.handoff.provenanceRel, manifestIndex, owner.journal.states)
	if err != nil {
		return rollbackHostedMCPCompletionD(owner, lease, manifestIndex, transactionFSIOError("transaction.journal", err))
	}
	owner.journal.states = append(owner.journal.states, state)
	if err := platform.publishCompletionManifest(owner.handoff.provenanceRel, canonical, owner.journal.fail); err != nil {
		return rollbackHostedMCPCompletionD(owner, lease, manifestIndex, transactionFSIOError("transaction.journal", err))
	}
	owner.journal.mutated = len(owner.journal.states)
	return commitHostedMCPCompletionD(owner, lease, manifestIndex)
}

func rollbackHostedMCPCompletionD(owner *hostedMCPCompletionOwner, lease *profileCompletionLease, manifestIndex int, primary error) error {
	if owner == nil || owner.handoff == nil || owner.journal == nil || owner.handoff.state.Load() != hostedMCPHandoffClaimed {
		return hostedMCPError(CodeHostedMCPInvalidIntent)
	}
	journal := owner.journal
	var causes []error
	residual := false
	if err := journal.platform.removeTemps(journal.states); err != nil {
		causes = append(causes, err)
		residual = true
	}
	if manifestIndex < len(journal.states) {
		if err := journal.platform.restore(journal.states[manifestIndex]); err != nil {
			causes = append(causes, err)
			residual = true
		}
	}
	if lease != nil {
		if err := lease.restore(); err != nil {
			causes = append(causes, err)
			residual = true
		}
	}
	last := manifestIndex
	if last > len(journal.states) {
		last = len(journal.states)
	}
	for i := last - 1; i >= 0; i-- {
		state := journal.states[i]
		if err := injectTransactionFS(journal.fail, "rollback", state.path); err != nil {
			causes = append(causes, err)
		}
		if err := journal.platform.restore(state); err != nil {
			causes = append(causes, err)
			residual = true
		}
	}
	if err := journal.platform.removeCreatedDirs(); err != nil {
		causes = append(causes, err)
		residual = true
	}
	if !residual {
		if err := journal.platform.markRestored(); err != nil {
			causes = append(causes, err)
			residual = true
		}
	}
	if err := injectTransactionFS(journal.fail, "cleanup", "journal"); err != nil {
		causes = append(causes, err)
	}
	if !residual {
		if err := journal.platform.cleanup(); err != nil {
			causes = append(causes, err)
			residual = true
		}
	}
	if err := journal.guard.Release(); err != nil {
		causes = append(causes, err)
		residual = true
	}
	if lease != nil {
		if err := lease.releaseRollback(); err != nil {
			causes = append(causes, err)
			residual = true
		}
	}
	journal.active = false
	owner.handoff.state.Store(hostedMCPHandoffDisposed)
	if residual {
		return transactionFSResidualRiskError(append([]error{primary}, causes...)...)
	}
	return primary
}

func commitHostedMCPCompletionD(owner *hostedMCPCompletionOwner, lease *profileCompletionLease, manifestIndex int) error {
	journal := owner.journal
	primary := transactionFSIOError("transaction.journal")
	if err := injectTransactionFS(journal.fail, "final-commit", "journal"); err != nil {
		return rollbackHostedMCPCompletionD(owner, lease, manifestIndex, transactionFSIOError("transaction.journal", err))
	}
	if err := injectTransactionFS(journal.fail, "cleanup", "journal"); err != nil {
		return rollbackHostedMCPCompletionD(owner, lease, manifestIndex, transactionFSIOError("transaction.journal", err))
	}
	if err := journal.platform.markCommitted(); err != nil {
		return rollbackHostedMCPCompletionD(owner, lease, manifestIndex, transactionFSIOError("transaction.journal", err))
	}
	if err := journal.platform.cleanup(); err != nil {
		// The durable committed marker makes this a post-commit cleanup outcome,
		// not a pre-commit apply failure. Recovery may only finish cleanup.
		primary = transactionFSIOError("transaction.journal", errTransactionPostCommitCleanup)
	}
	if err := journal.guard.Release(); err != nil {
		journal.active = false
		owner.handoff.state.Store(hostedMCPHandoffDisposed)
		profileErr := lease.commit()
		return transactionFSResidualRiskError(primary, err, profileErr)
	}
	journal.active = false
	owner.handoff.state.Store(hostedMCPHandoffDisposed)
	if err := lease.commit(); err != nil {
		return transactionFSResidualRiskError(primary, err)
	}
	if errors.Is(primary, errTransactionPostCommitCleanup) {
		return primary
	}
	return nil
}

func validHostedMCPJournalState(journal *transactionFSJournal, resource string) bool {
	if journal == nil || journal.platform == nil || !journal.active || !journal.sealed || journal.mutated != len(journal.states) || !validTransactionRelativePath(resource) {
		return false
	}
	matches := 0
	for _, state := range journal.states {
		if state.path == resource {
			matches++
		}
	}
	return matches == 1
}

func hostedMCPJournalStatePaths(journal *transactionFSJournal) []string {
	if journal == nil {
		return nil
	}
	paths := make([]string, len(journal.states))
	for i, state := range journal.states {
		paths[i] = state.path
	}
	return paths
}

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
