package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestW48CanonicalApplyUsesOneSealedW33TransactionAndPublishesManifestLast(t *testing.T) {
	root, project := prepareTransactionTestRoot(t), filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	input := w33DCompletionInput(t, TargetPi, root, project)
	store := NewProfileStore(filepath.Join(t.TempDir(), "state", "profiles.json"))
	preparedProject, err := store.PrepareProject(project)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	workflow := NewCanonicalApplyWorkflow(routePolicyFixture(t, GateApply), input, CanonicalApplyOptions{Store: store, Project: preparedProject, ResolveCredential: func(_, _ string) ([]byte, error) {
		calls++
		return []byte(hostedMCPTestSecret), nil
	}})
	prepared, ready := workflow.Prepare(t.Context(), Request{Mode: ModeApply, Target: TargetPi})
	var events []Event
	result := workflow.Execute(t.Context(), prepared, ObserverFunc(func(event Event) { events = append(events, event) }))
	if !ready.Admitted || result.Status != StatusSucceeded || !result.ChangedState || result.Report.MutationCount != len(result.Operations) || calls != 1 || len(events) != 8 || events[len(events)-1].Phase != PhasePublish {
		t.Fatalf("ready/result/calls/events = %#v/%#v/%d/%#v", ready, result, calls, events)
	}
	if _, err := os.Stat(input.ProvenancePath); err != nil {
		t.Fatalf("manifest-last publication missing: %v", err)
	}
	assertNoTransactionResidue(t, root)
	if again := workflow.Execute(t.Context(), prepared, nil); again.Error == nil || calls != 1 {
		t.Fatalf("Prepared reused or operation repeated: %#v calls=%d", again, calls)
	}
}
func TestW48CanonicalApplyRejectsMismatchWithoutEffectsAndRollsBackFinalizerBoundary(t *testing.T) {
	root, project := prepareTransactionTestRoot(t), filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	input := w33DCompletionInput(t, TargetPi, root, project)
	store := NewProfileStore(filepath.Join(t.TempDir(), "state", "profiles.json"))
	preparedProject, _ := store.PrepareProject(project)
	workflow := NewCanonicalApplyWorkflow(routePolicyFixture(t, GateApply), input, CanonicalApplyOptions{Store: store, Project: preparedProject, ResolveCredential: func(_, _ string) ([]byte, error) { return []byte(hostedMCPTestSecret), nil }})
	prepared, _ := workflow.Prepare(t.Context(), Request{Mode: ModeApply, Target: TargetPi})
	prepared.request.Target = TargetOpenCode
	if result := workflow.Execute(t.Context(), prepared, nil); result.Error == nil || result.ChangedState {
		t.Fatalf("mismatch result = %#v", result)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatalf("rejection mutated root: %v", entries)
	}
	prepared, _ = workflow.Prepare(t.Context(), Request{Mode: ModeApply, Target: TargetPi})
	workflow.apply.fail = func(stage, _ string) error {
		if stage == "manifest-write" {
			return os.ErrPermission
		}
		return nil
	}
	result := workflow.Execute(t.Context(), prepared, nil)
	if result.Status != StatusRolledBack || !result.Rollback.Complete || result.ResidualRisk {
		t.Fatalf("finalizer boundary result = %#v", result)
	}
	assertNoTransactionResidue(t, root)
}
