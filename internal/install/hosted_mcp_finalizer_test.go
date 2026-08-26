package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/manifest"
	"github.com/alferio94/lore-cli/internal/reconcile"
)

const (
	hostedMCPTestEndpoint = "https://lore.example.test/v1/mcp"
	hostedMCPTestSecret   = "raw-hosted-mcp-secret"
)

func TestW33CHostedMCPFinalizerRequiresCompleteResealedReportEquivalence(t *testing.T) {
	input := hostedMCPFinalizerFixture(t, TargetPi, hostedMCPTestEndpoint, nil)
	plan := sealHostedMCPFixture(t, input)
	mutations := []struct {
		name   string
		mutate func(*TransactionReport)
	}{
		{"target", func(report *TransactionReport) { report.Target = TargetOpenCode }},
		{"ir id", func(report *TransactionReport) { report.IRID = "different" }},
		{"provenance path", func(report *TransactionReport) { report.ProvenancePath += ".different" }},
		{"manifest hash", func(report *TransactionReport) { report.ManifestHash = strings.Repeat("0", 64) }},
		{"profile", func(report *TransactionReport) { report.Profile.ProfileID = "different" }},
		{"decisions", func(report *TransactionReport) { report.Decisions[0].NextHash = strings.Repeat("1", 64) }},
		{"admission", func(report *TransactionReport) { report.AllAdmitted = false }},
		{"finalization count", func(report *TransactionReport) { report.FinalizationCount = 2 }},
		{"mutation count", func(report *TransactionReport) { report.MutationCount = 1 }},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			report := plan.DryRun(nil)
			tc.mutate(&report)
			journal, platform := hostedMCPJournal(input, nil)
			resolver := &hostedMCPResolverSpy{trace: &platform.trace, value: []byte(hostedMCPTestSecret)}
			renderer := &hostedMCPRendererSpy{trace: &platform.trace, output: []byte("sensitive-config")}

			handoff, err := finalizeHostedMCP(TransactionPlan{report: report}, input, journal, resolver, renderer)
			if handoff != nil {
				t.Fatal("mismatched report returned a handoff")
			}
			assertHostedMCPError(t, err, CodeHostedMCPInvalidIntent, "hosted_mcp.intent")
			if resolver.calls != 0 || renderer.calls != 0 || platform.writeCalls != 0 || platform.commitCalls != 0 {
				t.Fatalf("mismatch reached effects: resolver=%d renderer=%d writes=%d commits=%d", resolver.calls, renderer.calls, platform.writeCalls, platform.commitCalls)
			}
			assertHostedMCPRollbackTrace(t, platform.trace, nil)
		})
	}
}

func TestW33CHostedMCPFinalizerRejectsExternalOrUnboundIntentBeforeResolution(t *testing.T) {
	valid := hostedMCPFinalizerFixture(t, TargetPi, hostedMCPTestEndpoint, nil)
	cases := []struct {
		name    string
		input   TransactionInput
		plan    func(*testing.T, TransactionInput) TransactionPlan
		journal string
	}{
		{"invalid endpoint", hostedMCPFinalizerFixture(t, TargetPi, "https://lore.example.test/v1/other", nil), sealHostedMCPFixture, "mcp/lore.json"},
		{"unknown intent field", hostedMCPFinalizerFixture(t, TargetPi, hostedMCPTestEndpoint, map[string]any{"external_endpoint": "https://external.invalid/v1/mcp"}), sealHostedMCPFixture, "mcp/lore.json"},
		{"multiple sensitive intents", hostedMCPFinalizerFixtureWithExtraSensitive(t, TargetPi), sealHostedMCPFixture, "mcp/lore.json"},
		{"resource outside journal", valid, sealHostedMCPFixture, "other/resource.json"},
		{"externally replaced permit", cloneTransactionInput(valid), func(t *testing.T, _ TransactionInput) TransactionPlan { return sealHostedMCPFixture(t, valid) }, "mcp/lore.json"},
	}
	cases[len(cases)-1].input.Permit = reconcile.FinalizationPermit{}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := tc.plan(t, tc.input)
			journal, platform := hostedMCPJournalForPath(tc.journal, nil)
			resolver := &hostedMCPResolverSpy{trace: &platform.trace, value: []byte(hostedMCPTestSecret)}
			renderer := &hostedMCPRendererSpy{trace: &platform.trace, output: []byte("sensitive-config")}

			handoff, err := finalizeHostedMCP(plan, tc.input, journal, resolver, renderer)
			if handoff != nil {
				t.Fatal("invalid intent returned a handoff")
			}
			assertHostedMCPError(t, err, CodeHostedMCPInvalidIntent, "hosted_mcp.intent")
			if resolver.calls != 0 || renderer.calls != 0 || platform.writeCalls != 0 || platform.commitCalls != 0 || resolver.unexpectedCalls != 0 || renderer.unexpectedCalls != 0 {
				t.Fatalf("invalid intent reached effects: resolver=%#v renderer=%#v platform=%#v", resolver, renderer, platform)
			}
			assertHostedMCPRollbackTrace(t, platform.trace, nil)
		})
	}
}

func TestW33CHostedMCPFinalizerReturnsProvisionalHandoffWithoutCommitOrRelease(t *testing.T) {
	for _, target := range []TargetID{TargetPi, TargetOpenCode} {
		t.Run(string(target), func(t *testing.T) {
			input := hostedMCPFinalizerFixture(t, target, hostedMCPTestEndpoint, nil)
			beforeInput := cloneTransactionInput(input)
			plan := sealHostedMCPFixture(t, input)
			beforeReport := plan.DryRun(nil)
			journal, platform := hostedMCPJournal(input, nil)
			secret := []byte(hostedMCPTestSecret)
			resolver := &hostedMCPResolverSpy{trace: &platform.trace, value: secret}
			renderer := &hostedMCPRendererSpy{trace: &platform.trace, delegate: hostedMCPNativeRenderer{}}

			handoff, err := finalizeHostedMCP(plan, input, journal, resolver, renderer)
			if err != nil {
				t.Fatalf("finalizeHostedMCP() error = %v", err)
			}
			if handoff == nil || handoff.journal != journal || handoff.state.Load() != hostedMCPHandoffFresh {
				t.Fatalf("provisional handoff = %#v", handoff)
			}
			wantTrace := []string{"resolve", "render", "write:mcp/lore.json"}
			if !reflect.DeepEqual(platform.trace, wantTrace) {
				t.Fatalf("trace = %v, want %v", platform.trace, wantTrace)
			}
			if resolver.calls != 1 || renderer.calls != 1 || platform.writeCalls != 1 || platform.commitCalls != 0 || resolver.unexpectedCalls != 0 || renderer.unexpectedCalls != 0 {
				t.Fatalf("call counts: resolver=%#v renderer=%#v platform=%#v", resolver, renderer, platform)
			}
			if resolver.provider != "vault" || resolver.slot != "lore" || renderer.target != target || renderer.endpoint != hostedMCPTestEndpoint || platform.writtenPath != "mcp/lore.json" {
				t.Fatalf("sealed facts were not preserved: resolver=%#v renderer=%#v path=%q", resolver, renderer, platform.writtenPath)
			}
			if !bytes.Contains(platform.written, []byte("Bearer "+hostedMCPTestSecret)) || !bytes.Contains(platform.written, []byte(hostedMCPTestEndpoint)) {
				t.Fatalf("protected target lacks sealed endpoint/credential: %q", platform.written)
			}
			if !allZero(secret) || !allZero(renderer.returned) {
				t.Fatal("credential or rendered buffer was not discarded")
			}
			if !journal.active || !journal.sealed || platform.cleaned {
				t.Fatalf("C prematurely completed authority: journal=%#v platform=%#v", journal, platform)
			}
			if !reflect.DeepEqual(input, beforeInput) || !reflect.DeepEqual(plan.DryRun(nil), beforeReport) {
				t.Fatal("finalization mutated the original input or admitted report")
			}
		})
	}
}

func TestW33CHostedMCPFinalizerFailsFastAndRollsBackInOrder(t *testing.T) {
	rawCause := errors.New("sensitive cause: " + hostedMCPTestSecret + " https://private.invalid/v1/mcp")
	cases := []struct {
		name     string
		resolver *hostedMCPResolverSpy
		renderer *hostedMCPRendererSpy
		fail     map[string]error
		code     HostedMCPFinalizerCode
		path     string
		prefix   []string
	}{
		{
			name: "resolve", resolver: &hostedMCPResolverSpy{err: rawCause}, renderer: &hostedMCPRendererSpy{output: []byte("must-not-render")},
			code: CodeHostedMCPResolveFailed, path: "hosted_mcp.credential", prefix: []string{"resolve"},
		},
		{
			name: "render", resolver: &hostedMCPResolverSpy{value: []byte(hostedMCPTestSecret)}, renderer: &hostedMCPRendererSpy{err: rawCause},
			code: CodeHostedMCPRenderFailed, path: "hosted_mcp.config", prefix: []string{"resolve", "render"},
		},
		{
			name: "write", resolver: &hostedMCPResolverSpy{value: []byte(hostedMCPTestSecret)}, renderer: &hostedMCPRendererSpy{output: []byte("sensitive-config")}, fail: map[string]error{"write:mcp/lore.json": rawCause},
			code: CodeHostedMCPWriteFailed, path: "hosted_mcp.write", prefix: []string{"resolve", "render", "write:mcp/lore.json"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := hostedMCPFinalizerFixture(t, TargetPi, hostedMCPTestEndpoint, nil)
			journal, platform := hostedMCPJournal(input, tc.fail)
			tc.resolver.trace = &platform.trace
			tc.renderer.trace = &platform.trace

			handoff, err := finalizeHostedMCP(sealHostedMCPFixture(t, input), input, journal, tc.resolver, tc.renderer)
			if handoff != nil {
				t.Fatal("failed finalization returned a handoff")
			}
			assertHostedMCPError(t, err, tc.code, tc.path)
			assertHostedMCPRedacted(t, err, hostedMCPTestSecret, "private.invalid", "mcp/lore.json")
			assertHostedMCPRollbackTrace(t, platform.trace, tc.prefix)
			if tc.resolver.calls != 1 || tc.resolver.unexpectedCalls != 0 || tc.renderer.unexpectedCalls != 0 || platform.commitCalls != 0 {
				t.Fatalf("failure call counts: resolver=%#v renderer=%#v platform=%#v", tc.resolver, tc.renderer, platform)
			}
			switch tc.name {
			case "resolve":
				if tc.renderer.calls != 0 || platform.writeCalls != 0 {
					t.Fatal("resolve failure reached a later stage")
				}
			case "render":
				if tc.renderer.calls != 1 || platform.writeCalls != 0 {
					t.Fatal("render failure reached protected write")
				}
			case "write":
				if tc.renderer.calls != 1 || platform.writeCalls != 1 {
					t.Fatal("write failure call count differs")
				}
			}
		})
	}
}

func TestW33CHostedMCPFinalizerPreservesResidualRiskOutcome(t *testing.T) {
	input := hostedMCPFinalizerFixture(t, TargetPi, hostedMCPTestEndpoint, nil)
	rollbackCause := errors.New("rollback leaked " + hostedMCPTestSecret)
	journal, platform := hostedMCPJournal(input, map[string]error{"restore:mcp/lore.json": rollbackCause})
	resolver := &hostedMCPResolverSpy{trace: &platform.trace, err: errors.New("resolve leaked " + hostedMCPTestSecret)}
	renderer := &hostedMCPRendererSpy{trace: &platform.trace, output: []byte("must-not-render")}

	handoff, err := finalizeHostedMCP(sealHostedMCPFixture(t, input), input, journal, resolver, renderer)
	if handoff != nil {
		t.Fatal("residual-risk finalization returned a handoff")
	}
	if !errors.Is(err, CodeTransactionResidualRisk) {
		t.Fatalf("error = %v, want %s", err, CodeTransactionResidualRisk)
	}
	var typed interface {
		Code() TransactionCode
		Path() string
	}
	if !errors.As(err, &typed) || typed.Code() != CodeTransactionResidualRisk || typed.Path() != "transaction.rollback" || err.Error() != "transaction restoration left residual risk" {
		t.Fatalf("residual risk error = %#v", err)
	}
	assertHostedMCPRedacted(t, err, hostedMCPTestSecret, "mcp/lore.json")
	wantTrace := []string{"resolve", "remove-temps", "restore:mcp/lore.json", "remove-created-dirs", "release"}
	if !reflect.DeepEqual(platform.trace, wantTrace) {
		t.Fatalf("residual trace = %v, want %v", platform.trace, wantTrace)
	}
	if platform.commitCalls != 0 || renderer.calls != 0 || platform.cleaned {
		t.Fatalf("residual-risk path discarded evidence or reached later work: %#v", platform)
	}
}

func TestW33CHostedMCPNativeRendererUsesOnlySealedEndpointAndCredential(t *testing.T) {
	renderer := hostedMCPNativeRenderer{}
	for _, target := range []TargetID{TargetPi, TargetOpenCode} {
		t.Run(string(target), func(t *testing.T) {
			got, err := renderer.Render(target, hostedMCPTestEndpoint, []byte(hostedMCPTestSecret))
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]any
			if err := json.Unmarshal(got, &document); err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(got, []byte(hostedMCPTestEndpoint)) || !bytes.Contains(got, []byte("Bearer "+hostedMCPTestSecret)) {
				t.Fatalf("rendered config = %s", got)
			}
			rootKey := "mcpServers"
			if target == TargetOpenCode {
				rootKey = "mcp"
			}
			if len(document) != 1 || document[rootKey] == nil {
				t.Fatalf("target-native root = %#v", document)
			}
		})
	}
	for _, tc := range []struct {
		target   TargetID
		endpoint string
		secret   []byte
	}{
		{TargetCodex, hostedMCPTestEndpoint, []byte(hostedMCPTestSecret)},
		{TargetPi, "https://external.invalid/not-mcp", []byte(hostedMCPTestSecret)},
		{TargetPi, hostedMCPTestEndpoint, nil},
	} {
		if got, err := renderer.Render(tc.target, tc.endpoint, tc.secret); err == nil || len(got) != 0 || strings.Contains(err.Error(), hostedMCPTestSecret) || strings.Contains(err.Error(), "external.invalid") {
			t.Fatalf("invalid render = %q, %v", got, err)
		}
	}
}

func TestW33CHostedMCPFinalizerInterfaceHasNoExternalEndpointPermitOrEffects(t *testing.T) {
	typeOf := reflect.TypeOf(finalizeHostedMCP)
	if typeOf.NumIn() != 5 || typeOf.NumOut() != 2 || typeOf.In(0) != reflect.TypeOf(TransactionPlan{}) || typeOf.In(1) != reflect.TypeOf(TransactionInput{}) || typeOf.In(2) != reflect.TypeOf((*transactionFSJournal)(nil)) || typeOf.Out(0) != reflect.TypeOf((*hostedMCPCompletionHandoff)(nil)) || typeOf.Out(1) != reflect.TypeOf((*error)(nil)).Elem() {
		t.Fatalf("finalizer signature = %v", typeOf)
	}
	for i := 0; i < typeOf.NumIn(); i++ {
		if typeOf.In(i).Kind() == reflect.String || typeOf.In(i) == reflect.TypeOf(reconcile.FinalizationPermit{}) || typeOf.In(i) == reflect.TypeOf((*TransactionEffects)(nil)).Elem() {
			t.Fatalf("external endpoint, permit, or effects input at argument %d: %v", i, typeOf.In(i))
		}
	}
}

func hostedMCPFinalizerFixture(t *testing.T, target TargetID, endpoint string, extra map[string]any) TransactionInput {
	t.Helper()
	projected := projectorInput(target, false)
	desired, err := json.Marshal(hostedMCPIntentDocument{Endpoint: endpoint, Credential: hostedMCPIntentCredential{Provider: "vault", Slot: "lore"}})
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range extra {
		encodedKey, keyErr := json.Marshal(key)
		encodedValue, valueErr := json.Marshal(value)
		if keyErr != nil || valueErr != nil {
			t.Fatal("encode extra intent field")
		}
		desired = append(desired[:len(desired)-1], ',')
		desired = append(desired, encodedKey...)
		desired = append(desired, ':')
		desired = append(desired, encodedValue...)
		desired = append(desired, '}')
	}
	projected.Resources[2].Desired = desired
	return hostedMCPRebuildFixture(t, target, projected)
}

func hostedMCPFinalizerFixtureWithExtraSensitive(t *testing.T, target TargetID) TransactionInput {
	t.Helper()
	projected := projectorInput(target, false)
	first, _ := json.Marshal(hostedMCPIntentDocument{Endpoint: hostedMCPTestEndpoint, Credential: hostedMCPIntentCredential{Provider: "vault", Slot: "lore"}})
	second, _ := json.Marshal(hostedMCPIntentDocument{Endpoint: "https://second.example.test/v1/mcp", Credential: hostedMCPIntentCredential{Provider: "vault", Slot: "lore"}})
	projected.Resources[2].Desired = first
	projected.Resources = append(projected.Resources, SemanticResource{Resource: "mcp/second.json", Component: ComponentLoreServerMCP, Mode: MergeModeReplace, Desired: second, SensitiveReferences: []SensitiveReference{{FinalizerID: hostedMCPFinalizerID, Provider: "vault", Slot: "lore"}}})
	return hostedMCPRebuildFixture(t, target, projected)
}

func hostedMCPRebuildFixture(t *testing.T, target TargetID, projected ProjectorInput) TransactionInput {
	t.Helper()
	root := hostedMCPTestRoot()
	input := transactionFixture(t, target, root, false)
	input.Semantic = projectMust(t, input.IR, projected)
	var err error
	input.Reconcile, err = reconcile.Reconcile(input.IR, input.Semantic.Intents())
	if err != nil {
		t.Fatal(err)
	}
	var ok bool
	input.Permit, ok = input.Reconcile.FinalizationPermit()
	if !ok {
		t.Fatal("fixture missing finalization permit")
	}

	facts, decisions := input.Semantic.TargetFacts(), input.Reconcile.Decisions()
	input.ProfileCompletion = facts.Persistence
	input.Drift = make([]TransactionDriftFact, len(facts.Resources))
	projections := make([]manifest.Projection, len(decisions))
	finalizations := make([]manifest.FinalizationReference, 0, len(facts.Resources))
	for i, decision := range decisions {
		projections[i] = manifest.Projection{Path: decision.Resource, Ownership: string(decision.Mode), Hash: decision.NextHash}
	}
	for i, fact := range facts.Resources {
		input.Drift[i] = TransactionDriftFact{Resource: fact.Resource, Present: fact.Present, Observed: append([]byte(nil), fact.Observed...)}
		for _, ref := range fact.SensitiveReferences {
			finalizations = append(finalizations, manifest.FinalizationReference{Path: fact.Resource, FinalizerID: ref.FinalizerID, Provider: ref.Provider, Slot: ref.Slot})
		}
	}
	input.Manifest.Projections = projections
	input.Manifest.Finalizations = finalizations
	input.Manifest.RollbackBoundary = manifest.RollbackBoundary{ID: "rb-v1", Kind: "selected-target-backup-v1", Target: string(target), Resources: append([]manifest.Projection(nil), projections...), Finalizations: append([]manifest.FinalizationReference(nil), finalizations...)}
	return input
}

func sealHostedMCPFixture(t *testing.T, input TransactionInput) TransactionPlan {
	t.Helper()
	plan, err := SealTransactionPlan(input)
	if err != nil {
		t.Fatalf("SealTransactionPlan() error = %#v", err)
	}
	return plan
}

func hostedMCPTestRoot() string {
	volume := filepath.VolumeName(os.TempDir())
	return filepath.Join(volume+string(filepath.Separator), "lore-w33c-sealed-target")
}

func hostedMCPJournal(input TransactionInput, fail map[string]error) (*transactionFSJournal, *hostedMCPPlatformSpy) {
	facts := input.Semantic.TargetFacts()
	for _, fact := range facts.Resources {
		if len(fact.SensitiveReferences) == 1 {
			return hostedMCPJournalForPath(fact.Resource, fail)
		}
	}
	return hostedMCPJournalForPath("missing", fail)
}

func hostedMCPJournalForPath(path string, fail map[string]error) (*transactionFSJournal, *hostedMCPPlatformSpy) {
	platform := &hostedMCPPlatformSpy{fail: fail}
	guard := &hostedMCPGuardSpy{trace: &platform.trace}
	journal := &transactionFSJournal{platform: platform, guard: guard, states: []transactionFSState{{path: path}}, mutated: 1, sealed: true, active: true}
	return journal, platform
}

func assertHostedMCPError(t *testing.T, err error, code HostedMCPFinalizerCode, path string) {
	t.Helper()
	if err == nil || !errors.Is(err, code) {
		t.Fatalf("error = %v, want %s", err, code)
	}
	var typed *hostedMCPFinalizerError
	if !errors.As(err, &typed) || typed.Code() != code || typed.Path() != path {
		t.Fatalf("typed error = %#v, want %s@%s", err, code, path)
	}
	want := map[HostedMCPFinalizerCode]string{
		CodeHostedMCPInvalidIntent: "hosted MCP intent is invalid",
		CodeHostedMCPResolveFailed: "hosted MCP credential resolution failed",
		CodeHostedMCPRenderFailed:  "hosted MCP configuration render failed",
		CodeHostedMCPWriteFailed:   "hosted MCP protected write failed",
	}[code]
	if err.Error() != want {
		t.Fatalf("message = %q, want %q", err.Error(), want)
	}
}

func assertHostedMCPRedacted(t *testing.T, err error, forbidden ...string) {
	t.Helper()
	for _, value := range forbidden {
		if value != "" && strings.Contains(strings.ToLower(err.Error()), strings.ToLower(value)) {
			t.Fatalf("error disclosed %q: %q", value, err)
		}
	}
}

func assertHostedMCPRollbackTrace(t *testing.T, got, prefix []string) {
	t.Helper()
	want := append([]string(nil), prefix...)
	want = append(want, "remove-temps", "restore:mcp/lore.json", "remove-created-dirs", "mark-restored", "cleanup", "release")
	if len(prefix) == 0 && len(got) > 1 && strings.HasPrefix(got[1], "restore:") {
		want[1] = got[1]
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rollback trace = %v, want %v", got, want)
	}
}

func allZero(data []byte) bool {
	for _, value := range data {
		if value != 0 {
			return false
		}
	}
	return true
}

type hostedMCPResolverSpy struct {
	trace                  *[]string
	value                  []byte
	err                    error
	provider, slot         string
	calls, unexpectedCalls int
}

func (s *hostedMCPResolverSpy) Resolve(provider, slot string) ([]byte, error) {
	s.calls++
	s.provider, s.slot = provider, slot
	if s.trace != nil {
		*s.trace = append(*s.trace, "resolve")
	}
	return s.value, s.err
}
func (s *hostedMCPResolverSpy) LoreServer(string) error {
	s.unexpectedCalls++
	return errors.New("unexpected server call")
}
func (s *hostedMCPResolverSpy) Repository(string) error {
	s.unexpectedCalls++
	return errors.New("unexpected repository call")
}
func (s *hostedMCPResolverSpy) Storage(string) error {
	s.unexpectedCalls++
	return errors.New("unexpected storage call")
}
func (s *hostedMCPResolverSpy) Memory(string) error {
	s.unexpectedCalls++
	return errors.New("unexpected memory call")
}
func (s *hostedMCPResolverSpy) Profile(string) error {
	s.unexpectedCalls++
	return errors.New("unexpected profile call")
}

type hostedMCPRendererSpy struct {
	trace                  *[]string
	delegate               hostedMCPRenderer
	output, returned       []byte
	err                    error
	target                 TargetID
	endpoint               string
	credential             []byte
	calls, unexpectedCalls int
}

func (s *hostedMCPRendererSpy) Render(target TargetID, endpoint string, credential []byte) ([]byte, error) {
	s.calls++
	s.target, s.endpoint, s.credential = target, endpoint, append([]byte(nil), credential...)
	if s.trace != nil {
		*s.trace = append(*s.trace, "render")
	}
	if s.delegate != nil {
		s.returned, s.err = s.delegate.Render(target, endpoint, credential)
		return s.returned, s.err
	}
	s.returned = s.output
	return s.returned, s.err
}
func (s *hostedMCPRendererSpy) API(string) error {
	s.unexpectedCalls++
	return errors.New("unexpected API call")
}
func (s *hostedMCPRendererSpy) Filesystem(string) error {
	s.unexpectedCalls++
	return errors.New("unexpected filesystem call")
}
func (s *hostedMCPRendererSpy) Subprocess(string) error {
	s.unexpectedCalls++
	return errors.New("unexpected subprocess call")
}

type hostedMCPPlatformSpy struct {
	trace                   []string
	fail                    map[string]error
	writtenPath             string
	written                 []byte
	writeCalls, commitCalls int
	cleaned                 bool
}

func (s *hostedMCPPlatformSpy) event(name string) error {
	s.trace = append(s.trace, name)
	return s.fail[name]
}
func (s *hostedMCPPlatformSpy) recover() error { return s.event("recover") }
func (s *hostedMCPPlatformSpy) begin() error   { return s.event("begin") }
func (s *hostedMCPPlatformSpy) backup(path string, _ int) (transactionFSState, error) {
	return transactionFSState{path: path}, s.event("backup:" + path)
}
func (s *hostedMCPPlatformSpy) seal([]transactionFSState) error { return s.event("seal") }
func (s *hostedMCPPlatformSpy) write(path string, data []byte) error {
	s.writeCalls++
	s.writtenPath, s.written = path, append([]byte(nil), data...)
	return s.event("write:" + path)
}
func (s *hostedMCPPlatformSpy) removeTemps([]transactionFSState) error {
	return s.event("remove-temps")
}
func (s *hostedMCPPlatformSpy) restore(state transactionFSState) error {
	return s.event("restore:" + state.path)
}
func (s *hostedMCPPlatformSpy) removeCreatedDirs() error { return s.event("remove-created-dirs") }
func (s *hostedMCPPlatformSpy) markCommitted() error {
	s.commitCalls++
	return s.event("mark-committed")
}
func (s *hostedMCPPlatformSpy) markRestored() error { return s.event("mark-restored") }
func (s *hostedMCPPlatformSpy) cleanup() error {
	s.cleaned = true
	return s.event("cleanup")
}
func (*hostedMCPPlatformSpy) journalRoot() string { return "sealed-journal" }

type hostedMCPGuardSpy struct{ trace *[]string }

func (*hostedMCPGuardSpy) Recover() error { return nil }
func (s *hostedMCPGuardSpy) Release() error {
	*s.trace = append(*s.trace, "release")
	return nil
}
