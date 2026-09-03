package install

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alferio94/lore-cli/internal/manifest"
)

// W3.3-E traceability: R3/R8/R10/R12/R14; C4-C5, C11-C20,
// C21-C30, and C31-C46. Focused A-D tests retain local edge coverage;
// these tests prove the accepted seams compose as one transaction.
func TestW33ETransactionIntegrationSuccessOrdersAcceptedPipelineAndHasNoUnexpectedCalls(t *testing.T) {
	for _, target := range []TargetID{TargetPi, TargetOpenCode} {
		t.Run(string(target), func(t *testing.T) {
			root := prepareTransactionTestRoot(t)
			projectRoot := filepath.Join(t.TempDir(), "project")
			if err := os.Mkdir(projectRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			input := w33DCompletionInput(t, target, root, projectRoot)
			plan, err := SealTransactionPlan(input)
			if err != nil {
				t.Fatal(err)
			}
			trace := []string{"seal/admit"}
			journal, err := applyTransactionFS(root, w33ETransactionWrites(input), func(stage, path string) error {
				trace = append(trace, stage+":"+path)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			resolver := &hostedMCPResolverSpy{trace: &trace, value: []byte(hostedMCPTestSecret)}
			renderer := &hostedMCPRendererSpy{trace: &trace, delegate: hostedMCPNativeRenderer{}}
			handoff, err := finalizeHostedMCP(plan, input, journal, resolver, renderer)
			if err != nil {
				t.Fatal(err)
			}
			store, prepared, profile := w33EProfileStore(t, projectRoot, &trace, false)
			if err := completeHostedMCPCompletion(handoff, plan, input, store, prepared); err != nil {
				t.Fatal(err)
			}

			want := []string{
				"seal/admit",
				"resolve", "render", "write:mcp/lore.json",
				"profile-acquire", "profile-snapshot", "profile-write",
				"manifest-temp:" + provenanceV3Name, "manifest-write:" + provenanceV3Name,
				"manifest-sync:" + provenanceV3Name, "manifest-replace:" + provenanceV3Name,
				"manifest-dirsync:" + provenanceV3Name, "manifest-cleanup:" + provenanceV3Name,
				"final-commit:journal", "cleanup:journal", "profile-release",
			}
			assertW33EOrderedSubsequence(t, trace, want)
			assertW33EBackupsBeforeWrites(t, trace, len(input.Semantic.TargetFacts().Resources))
			if resolver.calls != 1 || renderer.calls != 1 || resolver.unexpectedCalls != 0 || renderer.unexpectedCalls != 0 {
				t.Fatalf("credential/prohibited calls = resolver=%#v renderer=%#v", resolver, renderer)
			}
			if profile.commits != 1 || profile.restores != 0 || profile.authority.held || journal.active {
				t.Fatalf("completion state = profile=%#v journal=%#v", profile, journal)
			}
			canonical, err := manifest.Encode(input.Manifest)
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(input.ProvenancePath)
			if err != nil || !bytes.Equal(got, canonical) {
				t.Fatalf("v3 publication = %q, %v", got, err)
			}
			assertNoTransactionResidue(t, root)
		})
	}
}

// W3.3-E traceability: R12/R14; C21-C30, C35-C40, C43-C45.
// The matrix distinguishes pre-handoff C ownership, post-handoff D ownership,
// pre-commit rollback, post-commit cleanup, and residual-risk precedence.
func TestW33ETransactionIntegrationFullFailureMatrix(t *testing.T) {
	t.Run("A rejection is pre-apply and effect-free", func(t *testing.T) {
		input := w33DCompletionInput(t, TargetPi, prepareTransactionTestRoot(t), filepath.Join(t.TempDir(), "project"))
		input.ServerIdentity.ProjectID = "server-project-sensitive"
		plan, err := SealTransactionPlan(input)
		assertTransactionError(t, err, CodeTransactionServerBoundary, "server_identity")
		if !plan.IsZero() {
			t.Fatal("rejected A input admitted a plan")
		}
	})

	for _, stage := range []string{"backup", "write", "resolve", "render", "protected-write"} {
		t.Run(stage, func(t *testing.T) {
			root := prepareTransactionTestRoot(t)
			projectRoot := filepath.Join(t.TempDir(), "project")
			if err := os.Mkdir(projectRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			input := w33DCompletionInput(t, TargetPi, root, projectRoot)
			plan := sealHostedMCPFixture(t, input)
			writes := w33ETransactionWrites(input)
			primary := errors.New("sensitive primary endpoint token project repository path config journal")
			mcpWrites := 0
			fail := func(got, path string) error {
				if got == "write" && path == "mcp/lore.json" {
					mcpWrites++
				}
				if stage == "backup" && got == "backup" || stage == "write" && got == "write" || stage == "protected-write" && got == "write" && path == "mcp/lore.json" && mcpWrites == 2 {
					return primary
				}
				return nil
			}
			journal, err := applyTransactionFS(root, writes, fail)
			if stage == "backup" || stage == "write" {
				if !errors.Is(err, primary) || journal != nil {
					t.Fatalf("B failure = %#v, journal=%#v", err, journal)
				}
				assertNoTransactionResidue(t, root)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			resolver := &hostedMCPResolverSpy{value: []byte(hostedMCPTestSecret)}
			renderer := &hostedMCPRendererSpy{delegate: hostedMCPNativeRenderer{}}
			if stage == "resolve" {
				resolver.err = primary
			}
			if stage == "render" {
				renderer.err, renderer.delegate = primary, nil
			}
			handoff, gotErr := finalizeHostedMCP(plan, input, journal, resolver, renderer)
			if handoff != nil || gotErr == nil || journal.active {
				t.Fatalf("C failure = handoff=%#v err=%#v journal=%#v", handoff, gotErr, journal)
			}
			if stage == "resolve" && !errors.Is(gotErr, CodeHostedMCPResolveFailed) || stage == "render" && !errors.Is(gotErr, CodeHostedMCPRenderFailed) || stage == "protected-write" && !errors.Is(gotErr, CodeHostedMCPWriteFailed) {
				t.Fatalf("C primary = %#v", gotErr)
			}
			assertW33ERedacted(t, gotErr)
			assertNoTransactionResidue(t, root)
		})
	}

	stages := []string{
		"profile-snapshot", "profile-write", "manifest-backup", "manifest-temp", "manifest-write",
		"manifest-sync", "manifest-replace", "manifest-dirsync", "manifest-cleanup", "final-commit", "precommit-cleanup",
	}
	for _, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			fixture := newW33DCompletionFixture(t, false)
			primary := errors.New("sensitive D endpoint token project repository path config journal")
			switch stage {
			case "profile-snapshot", "profile-write":
				fixture.profile.fail[stage] = primary
			case "manifest-backup", "manifest-temp", "manifest-write", "manifest-sync", "manifest-replace", "manifest-dirsync", "manifest-cleanup":
				fixture.platform.fail[stage] = primary
			case "final-commit":
				fixture.journal.fail = func(got, _ string) error {
					if got == "final-commit" {
						return primary
					}
					return nil
				}
			case "precommit-cleanup":
				fixture.journal.fail = func(got, _ string) error {
					if got == "cleanup" {
						return primary
					}
					return nil
				}
			}
			err := completeHostedMCPCompletion(fixture.handoff, fixture.plan, fixture.input, fixture.store, fixture.prepared)
			if errors.Is(err, CodeTransactionResidualRisk) {
				t.Fatalf("D primary = %#v", err)
			}
			switch stage {
			case "profile-snapshot", "profile-write":
				var typed *ProfileStoreError
				if !errors.As(err, &typed) || typed.Code() != CodeProfileIO || typed.Path() != "profile_store.commit" {
					t.Fatalf("D primary = %#v, want %s@%s", err, CodeProfileIO, "profile_store.commit")
				}
			default:
				var typed *transactionFSOutcomeError
				if !errors.As(err, &typed) || typed.Code() != CodeTransactionIO || typed.Path() != "transaction.journal" {
					t.Fatalf("D primary = %#v, want %s@%s", err, CodeTransactionIO, "transaction.journal")
				}
			}
			if fixture.profile.present || fixture.profile.authority.held || fixture.journal.active || fixture.target.commitCalls != 0 {
				t.Fatalf("precommit failure was not coherently restored: profile=%#v target=%#v", fixture.profile, fixture.target)
			}
			assertW33ERedacted(t, err)
		})
	}

	t.Run("rollback restoration overrides primary", func(t *testing.T) {
		fixture := newW33DCompletionFixture(t, false)
		fixture.platform.fail["manifest-write"] = errors.New("primary token")
		fixture.profile.fail["profile-restore"] = errors.New("rollback project path")
		err := completeHostedMCPCompletion(fixture.handoff, fixture.plan, fixture.input, fixture.store, fixture.prepared)
		if !errors.Is(err, CodeTransactionResidualRisk) {
			t.Fatalf("residual outcome = %#v", err)
		}
		assertW33ERedacted(t, err)
	})

	t.Run("post-commit cleanup is distinct", func(t *testing.T) {
		fixture := newW33DCompletionFixture(t, false)
		fixture.target.fail["cleanup"] = errors.New("post-commit token")
		err := completeHostedMCPCompletion(fixture.handoff, fixture.plan, fixture.input, fixture.store, fixture.prepared)
		if !errors.Is(err, CodeTransactionIO) || !errors.Is(err, errTransactionPostCommitCleanup) || errors.Is(err, CodeTransactionResidualRisk) {
			t.Fatalf("post-commit outcome = %#v", err)
		}
		if fixture.profile.commits != 1 || fixture.profile.restores != 0 || fixture.target.commitCalls != 1 {
			t.Fatalf("post-commit state = profile=%#v target=%#v", fixture.profile, fixture.target)
		}
		assertW33ERedacted(t, err)
	})
}

// W3.3-E traceability: R10/R12/R13/R14; C38-C43.
func TestW33ETransactionIntegrationRestoresAbsentOrPresentV3AndProfileAndPreservesV2(t *testing.T) {
	for _, priorV3 := range []bool{false, true} {
		t.Run(fmt.Sprintf("prior-v3-%t", priorV3), func(t *testing.T) {
			root := prepareTransactionTestRoot(t)
			projectRoot := filepath.Join(t.TempDir(), "project")
			if err := os.Mkdir(projectRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			profilePath := filepath.Join(t.TempDir(), "state", "profiles.json")
			store := NewProfileStore(profilePath)
			prepared, err := store.PrepareProject(projectRoot)
			if err != nil {
				t.Fatal(err)
			}
			legacy := []byte("legacy-v2-token-free-exact\n")
			v2Path := filepath.Join(root, "lore-install.json")
			if err := os.WriteFile(v2Path, legacy, 0o600); err != nil {
				t.Fatal(err)
			}
			secureTransactionTestPath(t, v2Path)
			priorManifest := []byte("prior-v3-exact\n")
			v3Path := filepath.Join(root, provenanceV3Name)
			if priorV3 {
				if err := os.WriteFile(v3Path, priorManifest, 0o600); err != nil {
					t.Fatal(err)
				}
				secureTransactionTestPath(t, v3Path)
			}
			input := w33DCompletionInput(t, TargetPi, root, projectRoot)
			plan := sealHostedMCPFixture(t, input)
			journal, err := applyTransactionFS(root, w33ETransactionWrites(input), func(stage, _ string) error {
				if stage == "manifest-dirsync" {
					return errors.New("injected durability")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			handoff, err := finalizeHostedMCP(plan, input, journal, &hostedMCPResolverSpy{value: []byte(hostedMCPTestSecret)}, &hostedMCPRendererSpy{delegate: hostedMCPNativeRenderer{}})
			if err != nil {
				t.Fatal(err)
			}
			err = completeHostedMCPCompletion(handoff, plan, input, store, prepared)
			if !errors.Is(err, CodeTransactionIO) || errors.Is(err, CodeTransactionResidualRisk) {
				t.Fatalf("failure = %#v", err)
			}
			if got, readErr := os.ReadFile(v2Path); readErr != nil || !bytes.Equal(got, legacy) {
				t.Fatalf("v2 = %q, %v", got, readErr)
			}
			gotV3, readErr := os.ReadFile(v3Path)
			if priorV3 && (readErr != nil || !bytes.Equal(gotV3, priorManifest)) {
				t.Fatalf("present v3 = %q, %v", gotV3, readErr)
			}
			if !priorV3 && !errors.Is(readErr, os.ErrNotExist) {
				t.Fatalf("absent v3 restored as %q, %v", gotV3, readErr)
			}
			if _, readErr := os.Lstat(profilePath); !errors.Is(readErr, os.ErrNotExist) {
				t.Fatalf("profile persisted after failure: %v", readErr)
			}
			assertNoTransactionResidue(t, root)
		})
	}
}

// W3.3-E traceability: R12; C19/C46.
func TestW33ETransactionIntegrationLeavesUnrelatedTargetIndependent(t *testing.T) {
	firstRoot, secondRoot := prepareTransactionTestRoot(t), prepareTransactionTestRoot(t)
	first, err := applyTransactionFSWithWait(firstRoot, []transactionFSWrite{{Path: "a", Data: []byte("one")}}, nil, 0, newTargetAuthorityTestWaiter(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	second, err := applyTransactionFSWithWait(secondRoot, []transactionFSWrite{{Path: "b", Data: []byte("two")}}, nil, 0, newTargetAuthorityTestWaiter(time.Now()))
	if err != nil {
		_ = first.Rollback()
		t.Fatalf("unrelated target blocked: %v", err)
	}
	if err := second.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := first.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertNoTransactionResidue(t, firstRoot)
	assertNoTransactionResidue(t, secondRoot)
}

func w33ETransactionWrites(input TransactionInput) []transactionFSWrite {
	facts := input.Semantic.TargetFacts().Resources
	writes := make([]transactionFSWrite, len(facts))
	for i, fact := range facts {
		writes[i] = transactionFSWrite{Path: fact.Resource, Data: append([]byte(nil), fact.Desired...)}
	}
	return writes
}

func w33EProfileStore(t *testing.T, projectRoot string, trace *[]string, prior bool) (ProfileStore, PreparedProject, *w33DProfilePlatform) {
	t.Helper()
	canonical, err := canonicalStorePath("w33e-profile", nil)
	if err != nil {
		t.Fatal(err)
	}
	authority := &w33DProfileAuthority{trace: trace}
	platform := &w33DProfilePlatform{canonical: canonical, authority: authority, trace: trace, fail: map[string]error{}, present: prior}
	path := filepath.Join(t.TempDir(), "state", "profiles.json")
	store := ProfileStore{path: path, platform: platform, waiter: &fakeMonotonicWaiter{}}
	prepared := PreparedProject{path: path, root: projectRoot, id: stableProjectID(projectRoot), state: profileState{Version: profileStateVersion, Projects: []profileProject{}}, existed: prior}
	return store, prepared, platform
}

func assertW33EBackupsBeforeWrites(t *testing.T, trace []string, resources int) {
	t.Helper()
	lastBackup, firstWrite, backups := -1, len(trace), 0
	for i, event := range trace {
		if strings.HasPrefix(event, "backup:") {
			lastBackup, backups = i, backups+1
		}
		if strings.HasPrefix(event, "write:") && firstWrite == len(trace) {
			firstWrite = i
		}
	}
	if backups != resources || lastBackup < 0 || firstWrite <= lastBackup {
		t.Fatalf("backup/write order = %v", trace)
	}
}

func assertW33EOrderedSubsequence(t *testing.T, got, want []string) {
	t.Helper()
	at := 0
	for _, event := range got {
		if at < len(want) && event == want[at] {
			at++
		}
	}
	if at != len(want) {
		t.Fatalf("trace = %v; missing ordered suffix %v", got, want[at:])
	}
}

func assertW33ERedacted(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("missing error")
	}
	lower := strings.ToLower(err.Error())
	for _, forbidden := range []string{"lore.example.test", hostedMCPTestSecret, "server-project-sensitive", "private-repository", "mcp/lore.json", "journal.json", string(filepath.Separator) + "tmp"} {
		if strings.Contains(lower, strings.ToLower(forbidden)) {
			t.Fatalf("error disclosed %q: %q", forbidden, err)
		}
	}
}
