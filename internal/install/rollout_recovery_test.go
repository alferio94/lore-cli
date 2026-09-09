package install

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/alferio94/lore-cli/internal/releaseprofile"
)

func TestTask52ReleaseProfileDemotionBlocksFutureDAndAWithoutUndo(t *testing.T) {
	root := prepareTransactionTestRoot(t)
	mustWriteTransactionTestFile(t, filepath.Join(root, "owned.txt"), "prior")
	mustWriteTransactionTestFile(t, filepath.Join(root, "foreign.txt"), "keep")
	journal, err := applyTransactionFS(root, []transactionFSWrite{{Path: "owned.txt", Data: []byte("applied")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Commit(); err != nil {
		t.Fatal(err)
	}

	active := task52Policy(t, "a-release", "v0.4.0-rc.1", releaseprofile.GateA)
	if decision, err := active.Decide(Request{Target: TargetOpenCode, Mode: ModeApply}); err != nil || !decision.Admitted {
		t.Fatalf("A profile did not admit apply: %#v, %v", decision, err)
	}
	demoted := task52Policy(t, "demoted-release", "v0.4.0-rc.2", releaseprofile.GateE)
	for _, mode := range []Mode{ModeDryRun, ModeApply} {
		decision, err := demoted.Decide(Request{Target: TargetOpenCode, Mode: mode})
		if !errors.Is(err, CodeCanonicalRouteDisabled) || decision.Admitted || decision.Route != RouteCanonical {
			t.Fatalf("demoted %s decision = %#v, %v", mode, decision, err)
		}
	}
	assertTransactionTestFile(t, filepath.Join(root, "owned.txt"), "applied")
	assertTransactionTestFile(t, filepath.Join(root, "foreign.txt"), "keep")
	assertNoTransactionResidue(t, root)
}

func TestTask52ARecoveryRestorationAndInterruptionRetryRehearsal(t *testing.T) {
	var evidence [][]byte
	for attempt := 0; attempt < 2; attempt++ {
		root := prepareTransactionTestRoot(t)
		if err := os.Mkdir(filepath.Join(root, "managed"), 0o700); err != nil {
			t.Fatal(err)
		}
		secureTransactionTestDirectory(t, filepath.Join(root, "managed"))
		mustWriteTransactionTestFile(t, filepath.Join(root, "managed", "config.json"), "prior-owned")
		mustWriteTransactionTestFile(t, filepath.Join(root, provenanceV3Name), "prior-manifest")
		mustWriteTransactionTestFile(t, filepath.Join(root, "foreign.txt"), "foreign-user-content")
		receiptRoot := t.TempDir()
		receipt := task52AcceptedBackup(t, root, receiptRoot, []string{"managed/config.json", provenanceV3Name})
		writes := []transactionFSWrite{
			{Path: provenanceV3Name, Data: []byte("next-manifest")},
			{Path: "managed/config.json", Data: []byte("next-owned")},
		}
		ownedMutated := false
		_, err := applyTransactionFS(root, writes, func(stage, path string) error {
			if stage == "write" && path == "managed/config.json" {
				ownedMutated = true
			}
			if stage == "write" && path == provenanceV3Name {
				return errors.New("injected A-stage failure")
			}
			return nil
		})
		if err == nil || !ownedMutated {
			t.Fatalf("failure rehearsal = mutated %t, error %v", ownedMutated, err)
		}
		assertTask52State(t, root, "prior-owned", "prior-manifest")
		assertNoTransactionResidue(t, root)

		journal, err := applyTransactionFS(root, writes, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := journal.Commit(); err != nil {
			t.Fatal(err)
		}
		assertTask52State(t, root, "next-owned", "next-manifest")
		task52RestoreAcceptedBackup(t, root, receiptRoot, receipt)
		task52RestoreAcceptedBackup(t, root, receiptRoot, receipt)
		assertTask52State(t, root, "prior-owned", "prior-manifest")
		assertNoTransactionResidue(t, root)
		evidence = append(evidence, []byte("transaction=restored;user=restored;foreign=preserved;retry=deterministic;publication=blocked"))
	}
	if !reflect.DeepEqual(evidence[0], evidence[1]) {
		t.Fatalf("rehearsal evidence drift = %q / %q", evidence[0], evidence[1])
	}

	root := prepareTransactionRecoveryProcessRoot(t)
	mustWriteTransactionTestFile(t, filepath.Join(root, "foreign.txt"), "foreign")
	runTransactionRecoveryCrashHelper(t, root, "after-write-a")
	recoverTransactionBeforeAdmission(t, root, false)
	assertTransactionTestFile(t, filepath.Join(root, "foreign.txt"), "foreign")
	journal, err := applyTransactionFS(root, transactionRecoveryProcessWrites(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Commit(); err != nil {
		t.Fatal(err)
	}
	assertTransactionRecoveryState(t, root, true)
	assertTransactionTestFile(t, filepath.Join(root, "foreign.txt"), "foreign")
	assertNoTransactionResidue(t, root)
}

func task52Policy(t *testing.T, id, version string, gate releaseprofile.Gate) RoutePolicy {
	t.Helper()
	profile := releaseprofile.Profile{
		Schema: releaseprofile.Schema, ID: id, Version: 1,
		Release:  releaseprofile.ReleaseIdentity{Version: version, Channel: "prerelease", ArtifactSHA256: fmt.Sprintf("%064x", 1)},
		Rollback: releaseprofile.RollbackIdentity{Version: "v0.3.0", ProfileID: "default-off"},
		Gates:    releaseprofile.TargetGates{Pi: releaseprofile.GateOff, OpenCode: gate, Codex: releaseprofile.GateOff, Antigravity: releaseprofile.GateOff},
	}
	embedded, err := releaseprofile.Encode(profile)
	if err != nil {
		t.Fatal(err)
	}
	return NewRoutePolicyFromReleaseProfile(releaseprofile.Resolve(embedded, profile.Release, profile.Rollback))
}

func task52AcceptedBackup(t *testing.T, root, backupRoot string, paths []string) []byte {
	t.Helper()
	identity, err := canonicalTargetRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTargetTestIdentity(t, identity)
	states := make([]transactionFSState, len(paths))
	if err := os.Mkdir(filepath.Join(backupRoot, "backups"), 0o700); err != nil {
		t.Fatal(err)
	}
	for i, path := range paths {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		backup := fmt.Sprintf("backups/%06d", i)
		if err := os.WriteFile(filepath.Join(backupRoot, backup), data, 0o600); err != nil {
			t.Fatal(err)
		}
		states[i] = transactionFSState{path: path, backup: backup, exists: true, mode: 0o600}
	}
	receipt, err := encodeTransactionFSDurableJournal(identity.digest, states, nil)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

func task52RestoreAcceptedBackup(t *testing.T, root, backupRoot string, receipt []byte) {
	t.Helper()
	identity, err := canonicalTargetRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTargetTestIdentity(t, identity)
	record, err := decodeTransactionFSDurableJournal(receipt, identity.digest)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(record.Entries) - 1; i >= 0; i-- {
		entry := record.Entries[i]
		data, err := os.ReadFile(filepath.Join(backupRoot, entry.Backup))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, entry.Path), data, os.FileMode(entry.Mode)); err != nil {
			t.Fatal(err)
		}
	}
}

func assertTask52State(t *testing.T, root, owned, manifest string) {
	t.Helper()
	assertTransactionTestFile(t, filepath.Join(root, "managed", "config.json"), owned)
	assertTransactionTestFile(t, filepath.Join(root, provenanceV3Name), manifest)
	got, err := os.ReadFile(filepath.Join(root, "foreign.txt"))
	if err != nil || !bytes.Equal(got, []byte("foreign-user-content")) {
		t.Fatalf("foreign content = %q, %v", got, err)
	}
}
