package install

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestW33BTransactionFSBacksUpAllResourcesBeforeOrderedWrites(t *testing.T) {
	for _, failure := range []string{"backup:a.txt", "backup:b.txt", "write:a.txt", "write:b.txt"} {
		t.Run(failure, func(t *testing.T) {
			root := prepareTransactionTestRoot(t)
			mustWriteTransactionTestFile(t, filepath.Join(root, "a.txt"), "prior-a")
			mustWriteTransactionTestFile(t, filepath.Join(root, "b.txt"), "prior-b")
			var events []string
			injected := errors.New("injected primary")
			_, err := applyTransactionFS(root, []transactionFSWrite{{Path: "b.txt", Data: []byte("next-b")}, {Path: "a.txt", Data: []byte("next-a")}}, func(stage, path string) error {
				events = append(events, stage+":"+path)
				if stage+":"+path == failure {
					return injected
				}
				return nil
			})
			if !errors.Is(err, injected) {
				t.Fatalf("applyTransactionFS() error = %v", err)
			}
			assertTransactionTestFile(t, filepath.Join(root, "a.txt"), "prior-a")
			assertTransactionTestFile(t, filepath.Join(root, "b.txt"), "prior-b")
			if strings.HasPrefix(failure, "write:") {
				want := []string{"backup:a.txt", "backup:b.txt"}
				if len(events) < 2 || !reflect.DeepEqual(events[:2], want) {
					t.Fatalf("events = %v, backups were not complete and ordered", events)
				}
			}
			assertNoTransactionResidue(t, root)
		})
	}
}

func TestW33BTransactionFSRollbackRestoresFilesModesStatesAndDirectories(t *testing.T) {
	root := prepareTransactionTestRoot(t)
	existingDir := filepath.Join(root, "existing")
	if err := os.Mkdir(existingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	secureTransactionTestDirectory(t, existingDir)
	mustWriteTransactionTestFile(t, filepath.Join(existingDir, "owned.txt"), "prior")
	journal, err := applyTransactionFS(root, []transactionFSWrite{
		{Path: "new/deep/absent.txt", Data: []byte("created")},
		{Path: "existing/owned.txt", Data: []byte("next")},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertTransactionTestFile(t, filepath.Join(existingDir, "owned.txt"), "next")
	assertTransactionTestFile(t, filepath.Join(root, "new/deep/absent.txt"), "created")
	if err := journal.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertTransactionTestFile(t, filepath.Join(existingDir, "owned.txt"), "prior")
	if _, err := os.Lstat(filepath.Join(root, "new")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created directory survived rollback: %v", err)
	}
	assertNoTransactionResidue(t, root)
}

func TestW33BTransactionFSRollbackErrorsRetainPrimaryInStableOrder(t *testing.T) {
	root := prepareTransactionTestRoot(t)
	mustWriteTransactionTestFile(t, filepath.Join(root, "a.txt"), "prior-a")
	mustWriteTransactionTestFile(t, filepath.Join(root, "b.txt"), "prior-b")
	primary, rollback := errors.New("primary"), errors.New("rollback")
	_, err := applyTransactionFS(root, []transactionFSWrite{{Path: "a.txt", Data: []byte("next-a")}, {Path: "b.txt", Data: []byte("next-b")}}, func(stage, path string) error {
		switch stage + ":" + path {
		case "write:b.txt":
			return primary
		case "rollback:a.txt":
			return rollback
		}
		return nil
	})
	if !errors.Is(err, primary) || !errors.Is(err, rollback) || err.Error() != "transaction filesystem apply failed" {
		t.Fatalf("error = %#v", err)
	}
	joined := err.(interface{ Unwrap() []error }).Unwrap()
	if len(joined) < 2 || joined[0] != primary || joined[1] != rollback {
		t.Fatalf("ordered causes = %#v", joined)
	}
	assertNoTransactionResidue(t, root)
}

func TestW33BTransactionFSCommitKeepsWritesAndCleansJournal(t *testing.T) {
	root := prepareTransactionTestRoot(t)
	journal, err := applyTransactionFS(root, []transactionFSWrite{{Path: "z.txt", Data: []byte("z")}, {Path: "a.txt", Data: []byte("a")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Commit(); err != nil {
		t.Fatal(err)
	}
	assertTransactionTestFile(t, filepath.Join(root, "a.txt"), "a")
	assertTransactionTestFile(t, filepath.Join(root, "z.txt"), "z")
	assertNoTransactionResidue(t, root)
}

func mustWriteTransactionTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	secureTransactionTestPath(t, path)
}

func assertTransactionTestFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("%s = %q, %v; want %q", filepath.Base(path), got, err, want)
	}
	assertTransactionTestProtection(t, path, false)
}

func assertNoTransactionResidue(t *testing.T, root string) {
	t.Helper()
	matches, err := filepath.Glob(transactionRecoveryExpectedJournal(t, root) + "*")
	if err != nil || len(matches) != 0 {
		t.Fatalf("transaction residue = %v, %v", matches, err)
	}
}

func TestW33C1ADurableJournalCodecRejectsNonCanonicalOrUnsafeEvidence(t *testing.T) {
	root := sha256.Sum256([]byte("selected-root"))
	states := []transactionFSState{
		{path: "a.txt", backup: filepath.Join("backups", "000000"), exists: true, mode: 0o600},
		{path: filepath.Join("new", "absent.txt")},
		{path: provenanceV3Name, backup: filepath.Join("backups", "000002"), exists: true, mode: 0o600},
	}
	created := []string{"new"}
	encoded, err := encodeTransactionFSDurableJournal(root, states, created)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeTransactionFSDurableJournal(encoded, root)
	if err != nil {
		t.Fatal(err)
	}
	if got := durableTransactionStates(decoded); !reflect.DeepEqual(got, states) {
		t.Fatalf("decoded states = %#v, want %#v", got, states)
	}
	if !reflect.DeepEqual(decoded.CreatedDirs, created) {
		t.Fatalf("created directories = %v, want %v", decoded.CreatedDirs, created)
	}
	if !bytes.Equal(bytes.TrimSpace(encoded), bytes.TrimSpace(mustEncodeTransactionTestEnvelope(t, decoded))) {
		t.Fatal("journal encoding was not canonical and repeatable")
	}

	wrongRoot := sha256.Sum256([]byte("other-root"))
	cases := map[string][]byte{
		"empty":           nil,
		"incomplete":      []byte(`{"payload":{}}`),
		"unknown field":   []byte(`{"payload":{},"sha256":"00","extra":true}`),
		"duplicate field": []byte(`{"payload":{},"payload":{},"sha256":"00"}`),
		"oversized":       bytes.Repeat([]byte("x"), transactionFSJournalMaxBytes+1),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeTransactionFSDurableJournal(data, root); err == nil {
				t.Fatal("unsafe journal was admitted")
			}
		})
	}
	if _, err := decodeTransactionFSDurableJournal(encoded, wrongRoot); err == nil {
		t.Fatal("wrong-root journal was admitted")
	}

	var envelope transactionFSDurableEnvelope
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope.Checksum = strings.Repeat("0", sha256.Size*2)
	badChecksum, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeTransactionFSDurableJournal(badChecksum, root); err == nil {
		t.Fatal("checksum mismatch was admitted")
	}

	invalidRecords := []transactionFSDurableJournal{
		{Version: 2, Root: hex.EncodeToString(root[:])},
		{Version: 1, Root: hex.EncodeToString(root[:]), Entries: []transactionFSDurableEntry{{Path: "../escape"}}},
		{Version: 1, Root: hex.EncodeToString(root[:]), Entries: []transactionFSDurableEntry{{Path: "b"}, {Path: "a"}}},
		{Version: 1, Root: hex.EncodeToString(root[:]), Entries: []transactionFSDurableEntry{{Path: "a", Exists: true, Backup: "backups/999999", Mode: 0o600}}},
		{Version: 1, Root: hex.EncodeToString(root[:]), Entries: []transactionFSDurableEntry{{Path: "a", Backup: "backups/000000"}}},
		{Version: 1, Root: hex.EncodeToString(root[:]), CreatedDirs: []string{filepath.Join("deep", "child"), "deep"}},
	}
	for i, record := range invalidRecords {
		if _, err := decodeTransactionFSDurableJournal(mustEncodeTransactionTestEnvelope(t, record), root); err == nil {
			t.Fatalf("invalid record %d was admitted: %#v", i, record)
		}
	}
}

func TestW33C1ARollbackRestoresExactReverseOrderAndRetainsEvidenceOnRisk(t *testing.T) {
	states := []transactionFSState{{path: "a.txt"}, {path: "b.txt"}, {path: provenanceV3Name}}
	platform := &transactionFSPlatformRecorder{}
	guard := &transactionFSGuardRecorder{events: &platform.events}
	journal := &transactionFSJournal{platform: platform, guard: guard, states: states, sealed: true, active: true}
	if err := journal.Rollback(); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"remove-temps", "restore:" + provenanceV3Name, "restore:b.txt", "restore:a.txt",
		"remove-created-dirs", "mark-restored", "cleanup", "release",
	}
	if !reflect.DeepEqual(platform.events, want) {
		t.Fatalf("rollback events = %v, want %v", platform.events, want)
	}
	if err := journal.Rollback(); err != nil {
		t.Fatalf("repeated rollback = %v", err)
	}

	platform = &transactionFSPlatformRecorder{fail: map[string]error{"restore:b.txt": errors.New("sensitive-root owner=4321 bearer-secret")}}
	guard = &transactionFSGuardRecorder{events: &platform.events}
	journal = &transactionFSJournal{platform: platform, guard: guard, states: states, sealed: true, active: true}
	err := journal.Rollback()
	assertTransactionResidualRisk(t, err, "sensitive-root", "owner=4321", "bearer-secret")
	if containsTransactionTestEvent(platform.events, "mark-restored") || containsTransactionTestEvent(platform.events, "cleanup") {
		t.Fatalf("failed restoration deleted recovery evidence: %v", platform.events)
	}
	if len(platform.events) == 0 || platform.events[len(platform.events)-1] != "release" {
		t.Fatalf("authority was not released after retained evidence: %v", platform.events)
	}
}

func TestW33C1ACommitPublishesMarkerBeforeCleanupAndIsIdempotent(t *testing.T) {
	platform := &transactionFSPlatformRecorder{}
	guard := &transactionFSGuardRecorder{events: &platform.events}
	journal := &transactionFSJournal{platform: platform, guard: guard, active: true}
	if err := journal.Commit(); err != nil {
		t.Fatal(err)
	}
	want := []string{"mark-committed", "cleanup", "release"}
	if !reflect.DeepEqual(platform.events, want) {
		t.Fatalf("commit events = %v, want %v", platform.events, want)
	}
	if err := journal.Commit(); err != nil {
		t.Fatalf("repeated commit = %v", err)
	}

	platform = &transactionFSPlatformRecorder{fail: map[string]error{"mark-committed": errors.New("marker failed")}}
	guard = &transactionFSGuardRecorder{events: &platform.events}
	journal = &transactionFSJournal{platform: platform, guard: guard, active: true}
	if err := journal.Commit(); err == nil {
		t.Fatal("commit marker failure was hidden")
	}
	if containsTransactionTestEvent(platform.events, "cleanup") {
		t.Fatalf("journal evidence cleaned without durable commit marker: %v", platform.events)
	}
	if got := platform.events[len(platform.events)-1]; got != "release" {
		t.Fatalf("authority release event = %q", got)
	}
}

func TestW33C1ADurableJournalSchemaHasNoStalenessMetadata(t *testing.T) {
	assertTransactionTestStructFields(t, reflect.TypeOf(transactionFSDurableJournal{}), []string{"Version", "Root", "Entries", "CreatedDirs"})
	assertTransactionTestStructFields(t, reflect.TypeOf(transactionFSDurableEntry{}), []string{"Path", "Backup", "Exists", "Mode", "WindowsACL"})

	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate recovery test source")
	}
	dir := filepath.Dir(testFile)
	allowedImports := map[string]map[string]bool{
		"transaction_fs.go": {
			"bytes": true, "crypto/sha256": true, "encoding/hex": true, "encoding/json": true,
			"errors": true, "fmt": true, "io": true, "path/filepath": true, "sort": true,
			"strings": true, "time": true,
		},
		"transaction_fs_unix.go": {
			"crypto/sha256": true, "encoding/hex": true, "errors": true, "fmt": true,
			"os": true, "path/filepath": true, "sort": true, "golang.org/x/sys/unix": true,
		},
		"transaction_fs_windows.go": {
			"crypto/sha256": true, "encoding/hex": true, "errors": true, "fmt": true,
			"os": true, "path/filepath": true, "sort": true, "golang.org/x/sys/windows": true,
		},
	}
	for name, allowed := range allowedImports {
		path := filepath.Join(dir, name)
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, spec := range parsed.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil || !allowed[importPath] {
				t.Fatalf("%s has prohibited import %q", name, spec.Path.Value)
			}
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			lower := strings.ToLower(identifier.Name)
			switch lower {
			case "timestamp", "hostname", "pid", "profilestore", "profilestoreauthority", "loreserver", "memoryclient", "repositoryclient":
				t.Errorf("%s couples recovery or infers staleness through %q", name, identifier.Name)
			}
			return true
		})
		text := strings.ToLower(string(source))
		for _, prohibited := range []string{"os.getpid(", "os.hostname(", "time.since(", "profile_store", "lore_memory", "/v1/"} {
			if strings.Contains(text, prohibited) {
				t.Errorf("%s contains prohibited recovery coupling %q", name, prohibited)
			}
		}
	}
}

func mustEncodeTransactionTestEnvelope(t *testing.T, record transactionFSDurableJournal) []byte {
	t.Helper()
	payload, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	data, err := json.Marshal(transactionFSDurableEnvelope{Payload: payload, Checksum: hex.EncodeToString(sum[:])})
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func assertTransactionResidualRisk(t *testing.T, err error, forbidden ...string) {
	t.Helper()
	if !errors.Is(err, CodeTransactionResidualRisk) {
		t.Fatalf("error = %v, want %s", err, CodeTransactionResidualRisk)
	}
	var typed interface {
		Code() TransactionCode
		Path() string
	}
	if !errors.As(err, &typed) || typed.Code() != CodeTransactionResidualRisk || typed.Path() != "transaction.rollback" {
		t.Fatalf("typed residual-risk error = %#v", err)
	}
	if typedErr, ok := typed.(error); !ok || typedErr.Error() != "transaction restoration left residual risk" {
		t.Fatalf("residual-risk message = %v", typed)
	}
	for _, value := range forbidden {
		if value != "" && strings.Contains(strings.ToLower(err.Error()), strings.ToLower(value)) {
			t.Fatalf("error disclosed %q: %q", value, err)
		}
	}
}

func assertTransactionTestStructFields(t *testing.T, typ reflect.Type, want []string) {
	t.Helper()
	got := make([]string, typ.NumField())
	for i := range got {
		got[i] = typ.Field(i).Name
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s fields = %v, want %v", typ.Name(), got, want)
	}
}

type transactionFSPlatformRecorder struct {
	events []string
	fail   map[string]error
}

func (p *transactionFSPlatformRecorder) event(name string) error {
	p.events = append(p.events, name)
	return p.fail[name]
}
func (p *transactionFSPlatformRecorder) recover() error { return p.event("recover") }
func (p *transactionFSPlatformRecorder) begin() error   { return p.event("begin") }
func (p *transactionFSPlatformRecorder) backup(path string, _ int) (transactionFSState, error) {
	err := p.event("backup:" + path)
	return transactionFSState{path: path}, err
}
func (p *transactionFSPlatformRecorder) seal([]transactionFSState) error { return p.event("seal") }
func (p *transactionFSPlatformRecorder) write(path string, _ []byte) error {
	return p.event("write:" + path)
}
func (p *transactionFSPlatformRecorder) removeTemps([]transactionFSState) error {
	return p.event("remove-temps")
}
func (p *transactionFSPlatformRecorder) restore(state transactionFSState) error {
	return p.event("restore:" + state.path)
}
func (p *transactionFSPlatformRecorder) removeCreatedDirs() error {
	return p.event("remove-created-dirs")
}
func (p *transactionFSPlatformRecorder) markCommitted() error { return p.event("mark-committed") }
func (p *transactionFSPlatformRecorder) markRestored() error  { return p.event("mark-restored") }
func (p *transactionFSPlatformRecorder) cleanup() error       { return p.event("cleanup") }
func (*transactionFSPlatformRecorder) journalRoot() string    { return "fixed-journal" }

type transactionFSGuardRecorder struct{ events *[]string }

func (*transactionFSGuardRecorder) Recover() error { return nil }
func (g *transactionFSGuardRecorder) Release() error {
	*g.events = append(*g.events, "release")
	return nil
}

func containsTransactionTestEvent(events []string, want string) bool {
	for _, event := range events {
		if event == want {
			return true
		}
	}
	return false
}
