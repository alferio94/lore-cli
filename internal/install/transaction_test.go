package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestW33DUnit1HandoffRejectsMissingForeignAlteredAndStaleWithoutConsumption(t *testing.T) {
	if owner, err := claimHostedMCPCompletionHandoff(nil, TransactionPlan{}, TransactionInput{}); owner != nil || !errors.Is(err, CodeHostedMCPInvalidIntent) {
		t.Fatalf("nil handoff claim = %#v, %v", owner, err)
	}

	t.Run("foreign target", func(t *testing.T) {
		input, plan, handoff, journal, platform := w33DUnit1Handoff(t, TargetPi)
		foreignInput := hostedMCPFinalizerFixture(t, TargetOpenCode, hostedMCPTestEndpoint, nil)
		foreignPlan := sealHostedMCPFixture(t, foreignInput)
		before := append([]string(nil), platform.trace...)
		if owner, err := claimHostedMCPCompletionHandoff(handoff, foreignPlan, foreignInput); owner != nil || !errors.Is(err, CodeHostedMCPInvalidIntent) {
			t.Fatalf("foreign claim = %#v, %v", owner, err)
		}
		assertW33DUnit1Unconsumed(t, handoff, journal, platform, before)
		owner := claimW33DUnit1(t, handoff, plan, input)
		if err := rollbackHostedMCPCompletion(owner, nil); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("altered plan", func(t *testing.T) {
		input, plan, handoff, journal, platform := w33DUnit1Handoff(t, TargetPi)
		altered := plan.DryRun(nil)
		altered.ManifestHash = strings.Repeat("0", 64)
		before := append([]string(nil), platform.trace...)
		if owner, err := claimHostedMCPCompletionHandoff(handoff, TransactionPlan{report: altered}, input); owner != nil || !errors.Is(err, CodeHostedMCPInvalidIntent) {
			t.Fatalf("altered claim = %#v, %v", owner, err)
		}
		assertW33DUnit1Unconsumed(t, handoff, journal, platform, before)
		owner := claimW33DUnit1(t, handoff, plan, input)
		if err := rollbackHostedMCPCompletion(owner, nil); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("altered handoff", func(t *testing.T) {
		input, plan, handoff, journal, platform := w33DUnit1Handoff(t, TargetPi)
		root := handoff.targetRoot
		handoff.targetRoot = filepath.Join(root, "foreign")
		before := append([]string(nil), platform.trace...)
		if owner, err := claimHostedMCPCompletionHandoff(handoff, plan, input); owner != nil || !errors.Is(err, CodeHostedMCPInvalidIntent) {
			t.Fatalf("altered handoff claim = %#v, %v", owner, err)
		}
		assertW33DUnit1Unconsumed(t, handoff, journal, platform, before)
		handoff.targetRoot = root
		owner := claimW33DUnit1(t, handoff, plan, input)
		if err := rollbackHostedMCPCompletion(owner, nil); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("stale journal", func(t *testing.T) {
		input, plan, handoff, journal, platform := w33DUnit1Handoff(t, TargetPi)
		journal.active = false
		before := append([]string(nil), platform.trace...)
		if owner, err := claimHostedMCPCompletionHandoff(handoff, plan, input); owner != nil || !errors.Is(err, CodeHostedMCPInvalidIntent) {
			t.Fatalf("stale claim = %#v, %v", owner, err)
		}
		if handoff.state.Load() != hostedMCPHandoffFresh || !reflect.DeepEqual(platform.trace, before) {
			t.Fatalf("stale claim consumed or mutated: state=%d trace=%v", handoff.state.Load(), platform.trace)
		}
		journal.active = true
		owner := claimW33DUnit1(t, handoff, plan, input)
		if err := rollbackHostedMCPCompletion(owner, nil); err != nil {
			t.Fatal(err)
		}
	})
}

func TestW33DUnit1HandoffUsesOneCASAndOneExistingTransaction(t *testing.T) {
	input, plan, handoff, journal, platform := w33DUnit1Handoff(t, TargetPi)
	const contenders = 16
	owners := make(chan *hostedMCPCompletionOwner, contenders)
	errs := make(chan error, contenders)
	var wait sync.WaitGroup
	for i := 0; i < contenders; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			owner, err := claimHostedMCPCompletionHandoff(handoff, plan, input)
			if owner != nil {
				owners <- owner
			}
			if err != nil {
				errs <- err
			}
		}()
	}
	wait.Wait()
	close(owners)
	close(errs)

	var winner *hostedMCPCompletionOwner
	winnerCount := 0
	for owner := range owners {
		winner, winnerCount = owner, winnerCount+1
	}
	loserCount := 0
	for err := range errs {
		if !errors.Is(err, CodeHostedMCPInvalidIntent) {
			t.Fatalf("loser error = %v", err)
		}
		loserCount++
	}
	if winnerCount != 1 || loserCount != contenders-1 || winner == nil || winner.journal != journal || winner.handoff != handoff {
		t.Fatalf("claims: winners=%d losers=%d owner=%#v", winnerCount, loserCount, winner)
	}
	if platform.commitCalls != 0 || !journal.active || len(platform.trace) != 3 {
		t.Fatalf("claim started a second transaction or completed C: journal=%#v platform=%#v", journal, platform)
	}
	if err := rollbackHostedMCPCompletion(winner, nil); err != nil {
		t.Fatal(err)
	}
}

func TestW33DUnit1ClaimedOwnerAloneRollsBackAndDisposesHandoff(t *testing.T) {
	input, plan, handoff, journal, platform := w33DUnit1Handoff(t, TargetPi)
	owner := claimW33DUnit1(t, handoff, plan, input)
	primary := errors.New("D completion failed")
	if got := rollbackHostedMCPCompletion(owner, primary); got != primary {
		t.Fatalf("rollback result = %v, want primary", got)
	}
	want := []string{"resolve", "render", "write:mcp/lore.json", "remove-temps", "restore:mcp/lore.json", "remove-created-dirs", "mark-restored", "cleanup", "release"}
	if !reflect.DeepEqual(platform.trace, want) || journal.active || handoff.state.Load() != hostedMCPHandoffDisposed || platform.commitCalls != 0 {
		t.Fatalf("D rollback state: trace=%v journal=%#v handoff=%d platform=%#v", platform.trace, journal, handoff.state.Load(), platform)
	}
	before := append([]string(nil), platform.trace...)
	if err := rollbackHostedMCPCompletion(owner, primary); !errors.Is(err, CodeHostedMCPInvalidIntent) {
		t.Fatalf("reused owner rollback = %v", err)
	}
	if !reflect.DeepEqual(platform.trace, before) {
		t.Fatalf("reused owner mutated state: %v", platform.trace)
	}
	if reused, err := claimHostedMCPCompletionHandoff(handoff, plan, input); reused != nil || !errors.Is(err, CodeHostedMCPInvalidIntent) {
		t.Fatalf("reused handoff claim = %#v, %v", reused, err)
	}
}

func w33DUnit1Handoff(t *testing.T, target TargetID) (TransactionInput, TransactionPlan, *hostedMCPCompletionHandoff, *transactionFSJournal, *hostedMCPPlatformSpy) {
	t.Helper()
	input := hostedMCPFinalizerFixture(t, target, hostedMCPTestEndpoint, nil)
	plan := sealHostedMCPFixture(t, input)
	journal, platform := hostedMCPJournal(input, nil)
	resolver := &hostedMCPResolverSpy{trace: &platform.trace, value: []byte(hostedMCPTestSecret)}
	renderer := &hostedMCPRendererSpy{trace: &platform.trace, output: []byte("sensitive-config")}
	handoff, err := finalizeHostedMCP(plan, input, journal, resolver, renderer)
	if err != nil || handoff == nil {
		t.Fatalf("finalizeHostedMCP() = %#v, %v", handoff, err)
	}
	if resolver.calls != 1 || renderer.calls != 1 || platform.writeCalls != 1 || platform.commitCalls != 0 || !journal.active || platform.cleaned {
		t.Fatalf("provisional C state: resolver=%#v renderer=%#v journal=%#v platform=%#v", resolver, renderer, journal, platform)
	}
	return input, plan, handoff, journal, platform
}

func claimW33DUnit1(t *testing.T, handoff *hostedMCPCompletionHandoff, plan TransactionPlan, input TransactionInput) *hostedMCPCompletionOwner {
	t.Helper()
	owner, err := claimHostedMCPCompletionHandoff(handoff, plan, input)
	if err != nil || owner == nil {
		t.Fatalf("claimHostedMCPCompletionHandoff() = %#v, %v", owner, err)
	}
	return owner
}

func assertW33DUnit1Unconsumed(t *testing.T, handoff *hostedMCPCompletionHandoff, journal *transactionFSJournal, platform *hostedMCPPlatformSpy, before []string) {
	t.Helper()
	if handoff.state.Load() != hostedMCPHandoffFresh || !journal.active || platform.commitCalls != 0 || platform.cleaned || !reflect.DeepEqual(platform.trace, before) {
		t.Fatalf("invalid claim consumed or mutated: state=%d journal=%#v platform=%#v", handoff.state.Load(), journal, platform)
	}
}

func TestW33DUnit2CompletesProfileThenCanonicalManifestLastAndCommitsOnce(t *testing.T) {
	fixture := newW33DCompletionFixture(t, false)
	beforeV2 := append([]byte(nil), fixture.legacyV2...)
	wantManifest, err := manifest.Encode(fixture.input.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := completeHostedMCPCompletion(fixture.handoff, fixture.plan, fixture.input, fixture.store, fixture.prepared); err != nil {
		t.Fatalf("completeHostedMCPCompletion() error = %#v", err)
	}
	want := []string{
		"resolve", "render", "write:mcp/lore.json",
		"profile-acquire", "profile-snapshot", "profile-write",
		"manifest-backup", "manifest-temp", "manifest-write", "manifest-sync", "manifest-replace", "manifest-dirsync", "manifest-cleanup", "manifest-publish",
		"mark-committed", "cleanup", "release", "profile-release",
	}
	if !reflect.DeepEqual(fixture.target.trace, want) {
		t.Fatalf("completion trace = %v, want %v", fixture.target.trace, want)
	}
	if !bytes.Equal(fixture.platform.manifest, wantManifest) || fixture.platform.manifestPath != provenanceV3Name {
		t.Fatalf("manifest publication = %q at %q", fixture.platform.manifest, fixture.platform.manifestPath)
	}
	if fixture.target.commitCalls != 1 || fixture.profile.commits != 1 || fixture.profile.restores != 0 || fixture.profile.authority.held || fixture.journal.active || fixture.handoff.state.Load() != hostedMCPHandoffDisposed {
		t.Fatalf("completion state: target=%#v profile=%#v journal=%#v handoff=%d", fixture.target, fixture.profile, fixture.journal, fixture.handoff.state.Load())
	}
	if !bytes.Equal(beforeV2, fixture.legacyV2) {
		t.Fatal("legacy lore-install.json v2 bytes changed")
	}
	for _, event := range fixture.target.trace {
		if strings.Contains(event, "lore-install.json") || strings.Contains(event, "server") || strings.Contains(event, "memory") || strings.Contains(event, "storage") {
			t.Fatalf("completion crossed an excluded boundary: %q", event)
		}
	}
}

func TestW33DUnit2RejectsHandoffMisuseBeforeProfileOrManifestMutation(t *testing.T) {
	fixture := newW33DCompletionFixture(t, false)
	if err := completeHostedMCPCompletion(nil, fixture.plan, fixture.input, fixture.store, fixture.prepared); !errors.Is(err, CodeHostedMCPInvalidIntent) {
		t.Fatalf("missing handoff error = %v", err)
	}
	if fixture.profile.acquireCalls != 0 || fixture.platform.backups != 0 || len(fixture.platform.manifest) != 0 {
		t.Fatal("missing handoff reached Unit-2 mutation")
	}
	owner := claimW33DUnit1(t, fixture.handoff, fixture.plan, fixture.input)
	if err := rollbackHostedMCPCompletion(owner, nil); err != nil {
		t.Fatal(err)
	}
	before := append([]string(nil), fixture.target.trace...)
	if err := completeHostedMCPCompletion(fixture.handoff, fixture.plan, fixture.input, fixture.store, fixture.prepared); !errors.Is(err, CodeHostedMCPInvalidIntent) {
		t.Fatalf("reused handoff error = %v", err)
	}
	if fixture.profile.acquireCalls != 0 || fixture.platform.backups != 0 || !reflect.DeepEqual(before, fixture.target.trace) {
		t.Fatal("reused handoff reached Unit-2 mutation")
	}
}

func TestW33DUnit2EveryPreCommitFailureRestoresProfileThenCAndB(t *testing.T) {
	stages := []string{"manifest-backup", "manifest-temp", "manifest-write", "manifest-sync", "manifest-replace", "manifest-dirsync", "manifest-cleanup", "final-commit", "cleanup"}
	for _, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			fixture := newW33DCompletionFixture(t, false)
			if strings.HasPrefix(stage, "manifest-") {
				fixture.platform.fail[stage] = errors.New("injected manifest failure")
			} else {
				fixture.journal.fail = func(got, _ string) error {
					if got == stage {
						return errors.New("injected final failure")
					}
					return nil
				}
			}
			err := completeHostedMCPCompletion(fixture.handoff, fixture.plan, fixture.input, fixture.store, fixture.prepared)
			if !errors.Is(err, CodeTransactionIO) {
				t.Fatalf("%s error = %#v, want transaction_io", stage, err)
			}
			assertW33DProfileBeforeTargetRollback(t, fixture.target.trace)
			if fixture.profile.restores != 1 || fixture.profile.present || fixture.target.commitCalls != 0 || fixture.journal.active || fixture.handoff.state.Load() != hostedMCPHandoffDisposed {
				t.Fatalf("%s rollback state: target=%#v profile=%#v", stage, fixture.target, fixture.profile)
			}
		})
	}
}

func TestW33DUnit2ProfileAndManifestEncodingFailuresRemainPrimary(t *testing.T) {
	t.Run("profile", func(t *testing.T) {
		fixture := newW33DCompletionFixture(t, false)
		fixture.profile.fail["profile-write"] = errors.New("injected profile failure")
		err := completeHostedMCPCompletion(fixture.handoff, fixture.plan, fixture.input, fixture.store, fixture.prepared)
		if !errors.Is(err, CodeProfileIO) || errors.Is(err, CodeTransactionResidualRisk) || fixture.platform.backups != 0 {
			t.Fatalf("profile primary = %#v backups=%d", err, fixture.platform.backups)
		}
	})
	t.Run("manifest encode", func(t *testing.T) {
		fixture := newW33DCompletionFixture(t, false)
		primary := &manifest.CodecError{}
		err := completeHostedMCPCompletionWithEncoder(fixture.handoff, fixture.plan, fixture.input, fixture.store, fixture.prepared, func(manifest.Manifest) ([]byte, error) {
			return nil, primary
		})
		if err != primary || fixture.platform.backups != 0 || fixture.profile.restores != 1 {
			t.Fatalf("manifest primary = %#v backups=%d restores=%d", err, fixture.platform.backups, fixture.profile.restores)
		}
		assertW33DProfileBeforeTargetRollback(t, fixture.target.trace)
	})
}

func TestW33DUnit2RollbackFailureOverridesPrimaryAndPostCommitCleanupIsDistinct(t *testing.T) {
	t.Run("rollback residual risk", func(t *testing.T) {
		fixture := newW33DCompletionFixture(t, false)
		fixture.platform.fail["manifest-write"] = errors.New("primary")
		fixture.profile.fail["profile-restore"] = errors.New("restore failure")
		err := completeHostedMCPCompletion(fixture.handoff, fixture.plan, fixture.input, fixture.store, fixture.prepared)
		if !errors.Is(err, CodeTransactionResidualRisk) {
			t.Fatalf("rollback error = %#v", err)
		}
		if fixture.profile.authority.held || fixture.journal.active {
			t.Fatal("residual-risk path retained live authority")
		}
	})
	t.Run("post-commit cleanup", func(t *testing.T) {
		fixture := newW33DCompletionFixture(t, false)
		fixture.target.fail["cleanup"] = errors.New("post-commit cleanup")
		err := completeHostedMCPCompletion(fixture.handoff, fixture.plan, fixture.input, fixture.store, fixture.prepared)
		if !errors.Is(err, CodeTransactionIO) || !errors.Is(err, errTransactionPostCommitCleanup) || errors.Is(err, CodeTransactionResidualRisk) {
			t.Fatalf("post-commit cleanup error = %#v", err)
		}
		if fixture.profile.restores != 0 || fixture.profile.commits != 1 || fixture.profile.authority.held || fixture.target.commitCalls != 1 {
			t.Fatalf("post-commit state: target=%#v profile=%#v", fixture.target, fixture.profile)
		}
	})
}

func TestW33DUnit2NativeHeldProfileRestorePreservesAbsenceAndPresentBytes(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		store := NewProfileStore(filepath.Join(t.TempDir(), "state", "profiles.json"))
		root := filepath.Join(t.TempDir(), "project")
		prepared, err := store.PrepareProject(root)
		if err != nil {
			t.Fatal(err)
		}
		lease, err := store.beginHeldProfileCompletion(prepared, PersistenceFact{}, CommitOptions{WaitBudget: defaultProfileStoreWait})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(store.Path()); err != nil {
			t.Fatalf("provisional profile missing: %v", err)
		}
		if err := lease.rollback(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(store.Path()); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("absent prior profile was not restored: %v", err)
		}
		if residue, _ := filepath.Glob(filepath.Join(filepath.Dir(store.Path()), ".profiles-*")); len(residue) != 0 {
			t.Fatalf("profile residue = %v", residue)
		}
	})
	t.Run("present", func(t *testing.T) {
		store := NewProfileStore(filepath.Join(t.TempDir(), "state", "profiles.json"))
		root := filepath.Join(t.TempDir(), "project")
		prepared, _ := store.PrepareProject(root)
		if err := store.Complete(prepared, PersistenceFact{Requested: true, Scope: compiler.ProfileScopeGlobal, ProfileID: "prior"}, ApplyBoundarySuccess); err != nil {
			t.Fatal(err)
		}
		prior, _ := os.ReadFile(store.Path())
		priorMode, _ := os.Stat(store.Path())
		update, _ := store.PrepareProject(root)
		lease, err := store.beginHeldProfileCompletion(update, PersistenceFact{Requested: true, Scope: compiler.ProfileScopeGlobal, ProfileID: "next"}, CommitOptions{WaitBudget: defaultProfileStoreWait})
		if err != nil {
			t.Fatal(err)
		}
		if err := lease.rollback(); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(store.Path())
		gotMode, _ := os.Stat(store.Path())
		if !bytes.Equal(got, prior) || gotMode.Mode() != priorMode.Mode() {
			t.Fatalf("present profile restore differs: bytes=%t mode=%v/%v", bytes.Equal(got, prior), gotMode.Mode(), priorMode.Mode())
		}
	})
}

func assertW33DProfileBeforeTargetRollback(t *testing.T, trace []string) {
	t.Helper()
	profileRestore, targetRestore, targetRelease, profileRelease := -1, -1, -1, -1
	for i, event := range trace {
		switch event {
		case "profile-restore":
			profileRestore = i
		case "restore:mcp/lore.json":
			targetRestore = i
		case "release":
			targetRelease = i
		case "profile-release":
			profileRelease = i
		}
	}
	if profileRestore < 0 || targetRestore < 0 || targetRelease < 0 || profileRelease < 0 || profileRestore > targetRestore || targetRestore > targetRelease || targetRelease > profileRelease {
		t.Fatalf("rollback order = %v", trace)
	}
}

type w33DCompletionFixture struct {
	input    TransactionInput
	plan     TransactionPlan
	handoff  *hostedMCPCompletionHandoff
	journal  *transactionFSJournal
	target   *hostedMCPPlatformSpy
	platform *w33DCompletionPlatform
	profile  *w33DProfilePlatform
	store    ProfileStore
	prepared PreparedProject
	legacyV2 []byte
}

func newW33DCompletionFixture(t *testing.T, priorProfile bool) w33DCompletionFixture {
	t.Helper()
	projectRoot := filepath.Join(t.TempDir(), "project")
	targetRoot := filepath.Join(t.TempDir(), "target")
	input := w33DCompletionInput(t, TargetPi, targetRoot, projectRoot)
	plan := sealHostedMCPFixture(t, input)
	journal, target := hostedMCPJournal(input, map[string]error{})
	resolver := &hostedMCPResolverSpy{trace: &target.trace, value: []byte(hostedMCPTestSecret)}
	renderer := &hostedMCPRendererSpy{trace: &target.trace, output: []byte("sensitive-config")}
	handoff, err := finalizeHostedMCP(plan, input, journal, resolver, renderer)
	if err != nil {
		t.Fatal(err)
	}
	completion := &w33DCompletionPlatform{hostedMCPPlatformSpy: target, fail: map[string]error{}}
	journal.platform = completion
	canonical, _ := canonicalStorePath("w33d-profile", nil)
	profile := &w33DProfilePlatform{canonical: canonical, authority: &w33DProfileAuthority{trace: &target.trace}, trace: &target.trace, fail: map[string]error{}}
	if priorProfile {
		profile.present = true
		profile.raw = []byte("prior")
	}
	storePath := filepath.Join(t.TempDir(), "state", "profiles.json")
	store := ProfileStore{path: storePath, platform: profile, waiter: &fakeMonotonicWaiter{}}
	prepared := PreparedProject{path: storePath, root: projectRoot, id: stableProjectID(projectRoot), state: profileState{Version: profileStateVersion, Projects: []profileProject{}}, baseline: nil, existed: false}
	return w33DCompletionFixture{input: input, plan: plan, handoff: handoff, journal: journal, target: target, platform: completion, profile: profile, store: store, prepared: prepared, legacyV2: []byte(`{"schema_version":2,"preserved":true}`)}
}

func w33DCompletionInput(t *testing.T, target TargetID, targetRoot, projectRoot string) TransactionInput {
	t.Helper()
	projectID := stableProjectID(projectRoot)
	ir, err := compiler.Compile(compiler.Input{
		Schema: compiler.SchemaV1, Compiler: compiler.CompilerV1,
		Pack: compiler.PackSnapshot{ID: "portable-agent-pack", Version: "1.0.0"}, Target: compiler.TargetID(target), ProjectID: projectID,
		Profiles:   []compiler.Profile{{ID: "global", DefaultModel: "global-model", Roles: map[string]string{"worker": "global-worker"}}, {ID: "project", DefaultModel: "project-model", Roles: map[string]string{"worker": "project-worker"}}},
		Candidates: []compiler.Candidate{{Tier: compiler.TierGlobal, ProfileID: "global", SourceKey: "global-source", Scope: compiler.ProfileScopeGlobal, Location: "global/config"}, {Tier: compiler.TierProject, ProfileID: "project", SourceKey: "project-source", Scope: compiler.ProfileScopeProject, ProjectID: projectID, Location: "project/config"}},
		Requested:  []compiler.CapabilityRequest{{ID: compiler.CapabilityPortable, Required: true}}, CredentialSlots: []compiler.CredentialRef{{Provider: "vault", Slot: "lore"}},
		Persistence: compiler.RequestedProfilePersistence{Scope: compiler.ProfileScopeProject, ProjectID: projectID, ProfileID: "project"},
	})
	if err != nil {
		t.Fatal(err)
	}
	projected := projectorInput(target, false)
	desired, _ := json.Marshal(hostedMCPIntentDocument{Endpoint: hostedMCPTestEndpoint, Credential: hostedMCPIntentCredential{Provider: "vault", Slot: "lore"}})
	projected.Resources[2].Desired = desired
	semantic := projectMust(t, ir, projected)
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
	finalizations := []manifest.FinalizationReference{{Path: "mcp/lore.json", FinalizerID: hostedMCPFinalizerID, Provider: "vault", Slot: "lore"}}
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
		RoleModels: roles, Capabilities: capabilities, Reconciliation: manifest.ReconciliationProvenance{Owner: "compiler", Decision: "admitted"}, Finalizations: finalizations,
	}
	return TransactionInput{IR: ir, Semantic: semantic, Reconcile: report, Permit: permit, Targets: []TargetID{target}, Layout: HarnessLayout{Target: target, RootDir: targetRoot, ManifestPath: filepath.Join(targetRoot, "lore-install.json")}, ProvenancePath: filepath.Join(targetRoot, provenanceV3Name), Manifest: m, ProfileCompletion: facts.Persistence, Drift: drift}
}

type w33DCompletionPlatform struct {
	*hostedMCPPlatformSpy
	fail         map[string]error
	backups      int
	manifestPath string
	manifest     []byte
}

func (p *w33DCompletionPlatform) appendCompletionBackup(path string, index int, states []transactionFSState) (transactionFSState, error) {
	p.backups++
	p.trace = append(p.trace, "manifest-backup")
	if err := p.fail["manifest-backup"]; err != nil {
		return transactionFSState{}, err
	}
	if index != len(states) || path != provenanceV3Name {
		return transactionFSState{}, errTransactionFSUnsafePath
	}
	return transactionFSState{path: path}, nil
}

func (p *w33DCompletionPlatform) publishCompletionManifest(path string, data []byte, _ transactionFSFailpoint) error {
	for _, stage := range []string{"manifest-temp", "manifest-write", "manifest-sync", "manifest-replace", "manifest-dirsync", "manifest-cleanup"} {
		p.trace = append(p.trace, stage)
		if err := p.fail[stage]; err != nil {
			return err
		}
	}
	p.trace = append(p.trace, "manifest-publish")
	p.manifestPath, p.manifest = path, append([]byte(nil), data...)
	return nil
}

type w33DProfileState struct {
	owner   *w33DProfileAuthority
	present bool
	raw     []byte
}

func (*w33DProfileState) heldProfileState() {}

type w33DProfilePlatform struct {
	canonical    storePath
	authority    *w33DProfileAuthority
	trace        *[]string
	fail         map[string]error
	raw          []byte
	present      bool
	acquireCalls int
	commits      int
	restores     int
}

func (p *w33DProfilePlatform) Canonical(string) (storePath, error) { return p.canonical, nil }
func (p *w33DProfilePlatform) Acquire(storePath, time.Duration, monotonicWaiter) (authority, error) {
	p.acquireCalls++
	p.authority.held = true
	*p.trace = append(*p.trace, "profile-acquire")
	return p.authority, nil
}
func (p *w33DProfilePlatform) Read(authority) ([]byte, bool, error) {
	return append([]byte(nil), p.raw...), p.present, nil
}
func (p *w33DProfilePlatform) Commit(owned authority, _, next []byte) error {
	if owned != p.authority || !p.authority.held {
		return errors.New("profile commit without authority")
	}
	*p.trace = append(*p.trace, "profile-write")
	if err := p.fail["profile-write"]; err != nil {
		return err
	}
	p.commits++
	p.raw, p.present = append([]byte(nil), next...), true
	return nil
}
func (p *w33DProfilePlatform) snapshotHeld(owned authority) ([]byte, bool, heldProfileState, error) {
	if owned != p.authority || !p.authority.held {
		return nil, false, nil, errStoreAuthorityInvalidPath
	}
	*p.trace = append(*p.trace, "profile-snapshot")
	if err := p.fail["profile-snapshot"]; err != nil {
		return nil, false, nil, err
	}
	return append([]byte(nil), p.raw...), p.present, &w33DProfileState{owner: p.authority, present: p.present, raw: append([]byte(nil), p.raw...)}, nil
}
func (p *w33DProfilePlatform) restoreHeld(owned authority, prior heldProfileState) error {
	state, ok := prior.(*w33DProfileState)
	if !ok || state.owner != p.authority || owned != p.authority || !p.authority.held {
		return errStoreAuthorityInvalidPath
	}
	*p.trace = append(*p.trace, "profile-restore")
	if err := p.fail["profile-restore"]; err != nil {
		return err
	}
	p.restores++
	p.present, p.raw = state.present, append([]byte(nil), state.raw...)
	return nil
}

type w33DProfileAuthority struct {
	trace *[]string
	held  bool
}

func (a *w33DProfileAuthority) Release() error {
	*a.trace = append(*a.trace, "profile-release")
	a.held = false
	return nil
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
