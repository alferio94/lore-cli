package reconcile_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/alferio94/lore-cli/internal/compiler"
	"github.com/alferio94/lore-cli/internal/reconcile"
)

func TestW2BReplaceMatrixAndSensitiveEquality(t *testing.T) {
	ir := sealedIR(t, []compiler.CredentialRef{{Provider: "vault", Slot: "token"}})
	for _, tc := range []struct {
		name     string
		observed reconcile.Observed
		want     reconcile.Outcome
		code     string
	}{
		{"absent", reconcile.Observed{}, reconcile.OutcomeUpdate, "replace_absent"},
		{"owned", reconcile.Observed{Present: true, Content: []byte("old"), Evidence: reconcile.Evidence{Managed: true}}, reconcile.OutcomeUpdate, "replace_owned"},
		{"unknown", reconcile.Observed{Present: true, Content: []byte("old")}, reconcile.OutcomeConflict, "replace_unowned"},
		{"foreign", reconcile.Observed{Present: true, Content: []byte("old"), Evidence: reconcile.Evidence{Foreign: true}}, reconcile.OutcomeConflict, "replace_foreign"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := reconcile.Intent{Resource: "a", Mode: reconcile.ModeReplace, Observed: tc.observed, Desired: reconcile.Desired{Content: []byte("new")}}
			report, err := reconcile.Reconcile(ir, []reconcile.Intent{r})
			if err != nil || report.Decisions()[0].Outcome != tc.want || report.Decisions()[0].Explanation().Code != tc.code {
				t.Fatalf("%#v %v", report, err)
			}
		})
	}
	equal := reconcile.Intent{Resource: "a", Mode: reconcile.ModeReplace, Observed: reconcile.Observed{Present: true, Content: []byte("same"), Evidence: reconcile.Evidence{Foreign: true}}, Desired: reconcile.Desired{Content: []byte("same")}}
	report, err := reconcile.Reconcile(ir, []reconcile.Intent{equal})
	if err != nil || report.Decisions()[0].Outcome != reconcile.OutcomeNoop {
		t.Fatalf("foreign equal = %#v %v", report, err)
	}
	equal.Observed.Evidence = reconcile.Evidence{Managed: true}
	equal.Desired.SensitiveLinks = []reconcile.SensitiveLink{{FinalizerID: "final", Credential: compiler.CredentialRef{Provider: "vault", Slot: "token"}}}
	report, err = reconcile.Reconcile(ir, []reconcile.Intent{equal})
	if err != nil || report.Decisions()[0].Outcome != reconcile.OutcomeUpdate || !report.AllAdmitted() {
		t.Fatalf("sensitive equality = %#v %v", report, err)
	}
	permit, ok := report.FinalizationPermit()
	if !ok || permit.IRID() != ir.IRID() || len(permit.Projections()) != 1 || permit.Projections()[0].RedactionToken != "<redacted:vault/token>" {
		t.Fatalf("permit = %#v %v", permit, ok)
	}
}

func TestW2BMarkerAndAdditiveOwnership(t *testing.T) {
	ir := sealedIR(t, nil)
	for _, state := range []reconcile.MarkerState{reconcile.MarkerMissing, reconcile.MarkerReversed, reconcile.MarkerDuplicate, reconcile.MarkerNested} {
		r, err := reconcile.Reconcile(ir, []reconcile.Intent{{Resource: "marker", Mode: reconcile.ModeMarkerMerge, Observed: reconcile.Observed{Present: true, Content: []byte("same"), Marker: reconcile.MarkerMetadata{State: state}}, Desired: reconcile.Desired{Content: []byte("same")}}})
		if err != nil || r.Decisions()[0].Outcome != reconcile.OutcomeConflict || r.Decisions()[0].Explanation().Code != "marker_malformed" {
			t.Fatalf("%s: %#v %v", state, r, err)
		}
	}
	valid := reconcile.Intent{Resource: "marker", Mode: reconcile.ModeMarkerMerge, Observed: reconcile.Observed{Present: true, Content: []byte("old"), Marker: reconcile.MarkerMetadata{State: reconcile.MarkerValid}}, Desired: reconcile.Desired{Content: []byte("new")}}
	r, err := reconcile.Reconcile(ir, []reconcile.Intent{valid})
	if err != nil || r.Decisions()[0].Outcome != reconcile.OutcomeUpdate {
		t.Fatalf("valid marker = %#v %v", r, err)
	}
	valid.Observed.Evidence.Foreign = true
	r, err = reconcile.Reconcile(ir, []reconcile.Intent{valid})
	if err != nil || r.Decisions()[0].Explanation().Code != "marker_foreign" {
		t.Fatalf("foreign marker = %#v %v", r, err)
	}
	claims := []reconcile.AdditiveClaim{{Subject: "z", Equivalent: false}, {Subject: "a", Equivalent: false}}
	r, err = reconcile.Reconcile(ir, []reconcile.Intent{{Resource: "add", Mode: reconcile.ModeAdditive, Observed: reconcile.Observed{Present: true, AdditiveClaims: claims}, Desired: reconcile.Desired{Content: []byte("new")}}})
	if err != nil || r.Decisions()[0].Outcome != reconcile.OutcomeConflict || r.Decisions()[0].Explanation().Code != "additive_unowned" {
		t.Fatalf("additive = %#v %v", r, err)
	}
	claims[1].Evidence.Foreign = true
	r, err = reconcile.Reconcile(ir, []reconcile.Intent{{Resource: "add", Mode: reconcile.ModeAdditive, Observed: reconcile.Observed{Present: true, AdditiveClaims: claims}, Desired: reconcile.Desired{Content: []byte("new")}}})
	if err != nil || r.Decisions()[0].Explanation().Code != "additive_foreign" {
		t.Fatalf("foreign additive = %#v %v", r, err)
	}
}

func TestW2BContractErrorsHashesAndCopies(t *testing.T) {
	ir := sealedIR(t, nil)
	for _, tc := range []struct {
		name    string
		intents []reconcile.Intent
		code    reconcile.ContractCode
	}{
		{"duplicate", []reconcile.Intent{{Resource: "a", Mode: reconcile.ModeReplace}, {Resource: "a", Mode: reconcile.ModeReplace}}, reconcile.CodeDuplicateResource},
		{"mode", []reconcile.Intent{{Resource: "a", Mode: "bad"}}, reconcile.CodeInvalidIntent},
		{"marker", []reconcile.Intent{{Resource: "a", Mode: reconcile.ModeMarkerMerge}}, reconcile.CodeInvalidMarker},
		{"evidence", []reconcile.Intent{{Resource: "a", Mode: reconcile.ModeReplace, Observed: reconcile.Observed{Evidence: reconcile.Evidence{Contradictory: true}}}}, reconcile.CodeInvalidEvidence},
		{"link", []reconcile.Intent{{Resource: "a", Mode: reconcile.ModeReplace, Desired: reconcile.Desired{SensitiveLinks: []reconcile.SensitiveLink{{}}}}}, reconcile.CodeInvalidSensitiveLink},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := reconcile.Reconcile(ir, tc.intents)
			if !reflect.DeepEqual(r, reconcile.Report{}) || !errors.Is(err, tc.code) {
				t.Fatalf("%#v %v", r, err)
			}
			var typed *reconcile.ContractError
			if !errors.As(err, &typed) || typed.Code() != tc.code {
				t.Fatalf("%#v", typed)
			}
		})
	}
	base := reconcile.Intent{Resource: "a", Mode: reconcile.ModeReplace, Observed: reconcile.Observed{Present: true, Content: []byte{}}, Desired: reconcile.Desired{Content: []byte("next")}}
	r, _ := reconcile.Reconcile(ir, []reconcile.Intent{base})
	empty := r.Decisions()[0]
	base.Observed.Present = false
	r, _ = reconcile.Reconcile(ir, []reconcile.Intent{base})
	if empty.PriorHash == r.Decisions()[0].PriorHash || empty.PriorHash == empty.NextHash {
		t.Fatal("hash domains did not bind presence and direction")
	}
	base.Observed.Present = true
	base.Observed.Content = []byte("prior")
	r, _ = reconcile.Reconcile(ir, []reconcile.Intent{base})
	d := r.Decisions()[0]
	b := d.NextSanitized()
	b[0] = 'X'
	if r.Decisions()[0].NextSanitized()[0] == 'X' || d.Explanation().Subject != "a" {
		t.Fatal("decision accessor leaked")
	}
}

func TestW2BPrecedenceForeignAndLinks(t *testing.T) {
	ir := sealedIR(t, []compiler.CredentialRef{{Provider: "vault", Slot: "ok"}})
	link := []reconcile.SensitiveLink{{FinalizerID: "f", Credential: compiler.CredentialRef{Provider: "vault", Slot: "ok"}}}
	for _, tc := range []struct {
		name    string
		in      reconcile.Intent
		outcome reconcile.Outcome
		code    string
	}{
		{"replace_equal_foreign", reconcile.Intent{Resource: "r", Mode: reconcile.ModeReplace, Observed: reconcile.Observed{Present: true, Content: []byte("x"), Evidence: reconcile.Evidence{Foreign: true}}, Desired: reconcile.Desired{Content: []byte("x")}}, reconcile.OutcomeNoop, "already_desired"},
		{"replace_sensitive_foreign", reconcile.Intent{Resource: "r", Mode: reconcile.ModeReplace, Observed: reconcile.Observed{Present: true, Content: []byte("x"), Evidence: reconcile.Evidence{Foreign: true}}, Desired: reconcile.Desired{Content: []byte("x"), SensitiveLinks: link}}, reconcile.OutcomeConflict, "replace_foreign"},
		{"marker_malformed_sensitive", reconcile.Intent{Resource: "m", Mode: reconcile.ModeMarkerMerge, Observed: reconcile.Observed{Present: true, Content: []byte("x"), Marker: reconcile.MarkerMetadata{State: reconcile.MarkerNested}}, Desired: reconcile.Desired{Content: []byte("x"), SensitiveLinks: link}}, reconcile.OutcomeConflict, "marker_malformed"},
		{"marker_valid_foreign_sensitive", reconcile.Intent{Resource: "m", Mode: reconcile.ModeMarkerMerge, Observed: reconcile.Observed{Present: true, Content: []byte("x"), Evidence: reconcile.Evidence{Foreign: true}, Marker: reconcile.MarkerMetadata{State: reconcile.MarkerValid}}, Desired: reconcile.Desired{Content: []byte("x"), SensitiveLinks: link}}, reconcile.OutcomeConflict, "marker_foreign"},
		{"additive_equivalent_foreign", reconcile.Intent{Resource: "a", Mode: reconcile.ModeAdditive, Observed: reconcile.Observed{Present: true, Content: []byte("x"), AdditiveClaims: []reconcile.AdditiveClaim{{Subject: "s", Equivalent: true, Evidence: reconcile.Evidence{Foreign: true}}}}, Desired: reconcile.Desired{Content: []byte("x")}}, reconcile.OutcomeNoop, "already_desired"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, e := reconcile.Reconcile(ir, []reconcile.Intent{tc.in})
			if e != nil || r.Decisions()[0].Outcome != tc.outcome || r.Decisions()[0].Explanation().Code != tc.code {
				t.Fatalf("%#v %v", r, e)
			}
		})
	}
	bad := reconcile.Intent{Resource: "bad", Mode: reconcile.ModeReplace, Observed: reconcile.Observed{Present: true, Content: []byte("x"), Evidence: reconcile.Evidence{Foreign: true}}, Desired: reconcile.Desired{Content: []byte("x"), SensitiveLinks: []reconcile.SensitiveLink{{FinalizerID: "f", Credential: compiler.CredentialRef{Provider: "vault", Slot: "missing"}}}}}
	r, e := reconcile.Reconcile(ir, []reconcile.Intent{bad})
	if !errors.Is(e, reconcile.CodeInvalidSensitiveLink) || !reflect.DeepEqual(r, reconcile.Report{}) {
		t.Fatalf("%#v %v", r, e)
	}
}

func TestW2BAdditiveForeignDominanceAndPermitBatch(t *testing.T) {
	ir := sealedIR(t, []compiler.CredentialRef{{Provider: "v", Slot: "s"}})
	claims := []reconcile.AdditiveClaim{{Subject: "z", Equivalent: false, Evidence: reconcile.Evidence{Foreign: true, Managed: true}}, {Subject: "a", Equivalent: false}}
	in := reconcile.Intent{Resource: "a", Mode: reconcile.ModeAdditive, Observed: reconcile.Observed{Present: true, AdditiveClaims: claims}, Desired: reconcile.Desired{Content: []byte("n")}}
	r, e := reconcile.Reconcile(ir, []reconcile.Intent{in})
	d := r.Decisions()[0]
	if e != nil || d.Outcome != reconcile.OutcomeConflict || d.Explanation().Code != "additive_foreign" || d.Explanation().Subject != "a" {
		t.Fatalf("%#v %v", r, e)
	}
	claims[0], claims[1] = claims[1], claims[0]
	r, e = reconcile.Reconcile(ir, []reconcile.Intent{{Resource: "a", Mode: reconcile.ModeAdditive, Observed: reconcile.Observed{Present: true, AdditiveClaims: claims}, Desired: reconcile.Desired{Content: []byte("n")}}})
	if e != nil || r.Decisions()[0].Explanation().Subject != "a" {
		t.Fatalf("permutation %#v %v", r, e)
	}
	safe := reconcile.Intent{Resource: "safe", Mode: reconcile.ModeReplace, Observed: reconcile.Observed{Present: true, Content: []byte("x"), Evidence: reconcile.Evidence{Managed: true}}, Desired: reconcile.Desired{Content: []byte("x"), SensitiveLinks: []reconcile.SensitiveLink{{FinalizerID: "f", Credential: compiler.CredentialRef{Provider: "v", Slot: "s"}}}}}
	r, e = reconcile.Reconcile(ir, []reconcile.Intent{safe, in})
	if e != nil || r.AllAdmitted() {
		t.Fatalf("batch %#v %v", r, e)
	}
	if _, ok := r.FinalizationPermit(); ok {
		t.Fatal("conflict admitted a permit")
	}
}

func TestW2BHashesPermutationsAndDefensivePermit(t *testing.T) {
	ir := sealedIR(t, []compiler.CredentialRef{{Provider: "v", Slot: "a"}, {Provider: "v", Slot: "b"}})
	links := []reconcile.SensitiveLink{{FinalizerID: "z", Credential: compiler.CredentialRef{Provider: "v", Slot: "b"}}, {FinalizerID: "a", Credential: compiler.CredentialRef{Provider: "v", Slot: "a"}}}
	in := reconcile.Intent{Resource: "r", Mode: reconcile.ModeReplace, Observed: reconcile.Observed{Present: true, Content: []byte("old"), Evidence: reconcile.Evidence{Managed: true}}, Desired: reconcile.Desired{Content: []byte("new"), SensitiveLinks: links}}
	r, e := reconcile.Reconcile(ir, []reconcile.Intent{in})
	if e != nil {
		t.Fatal(e)
	}
	d := r.Decisions()[0]
	prior := ownershipHashFixture("lore.reconcile.prior.v1\x00", ir.IRID(), in, []reconcile.SensitiveLink{links[1], links[0]})
	next := ownershipHashFixture("lore.reconcile.next.v1\x00", ir.IRID(), in, []reconcile.SensitiveLink{links[1], links[0]})
	if d.PriorHash != prior || d.NextHash != next || prior == next {
		t.Fatalf("hashes %q %q", d.PriorHash, d.NextHash)
	}
	other := in
	other.Desired.SensitiveLinks = []reconcile.SensitiveLink{links[1], links[0]}
	rr, _ := reconcile.Reconcile(ir, []reconcile.Intent{other})
	if !reflect.DeepEqual(d, rr.Decisions()[0]) {
		t.Fatal("link permutation changed decision")
	}
	p, ok := r.FinalizationPermit()
	if !ok {
		t.Fatal("missing permit")
	}
	ps := p.Projections()
	ps[0].FinalizerID = "mutated"
	p2, ok := r.FinalizationPermit()
	if r.Decisions()[0].NextSanitized()[0] != 'n' || r.Decisions()[0].Explanation().Code != "replace_owned" || !ok || p2.Projections()[0].FinalizerID == "mutated" {
		t.Fatal("report or permit accessor leaked")
	}
}

func TestW2BGateProofGaps(t *testing.T) {
	ir := sealedIR(t, []compiler.CredentialRef{{Provider: "v", Slot: "s"}})
	link := []reconcile.SensitiveLink{{FinalizerID: "f", Credential: compiler.CredentialRef{Provider: "v", Slot: "s"}}}
	for _, tc := range []struct {
		name string
		in   reconcile.Intent
		code string
	}{
		{"marker_absent_foreign", reconcile.Intent{Resource: "m", Mode: reconcile.ModeMarkerMerge, Observed: reconcile.Observed{Present: true, Content: []byte("x"), Evidence: reconcile.Evidence{Foreign: true}, Marker: reconcile.MarkerMetadata{State: reconcile.MarkerAbsent}}, Desired: reconcile.Desired{Content: []byte("x"), SensitiveLinks: link}}, "marker_foreign"},
		{"additive_equivalent_foreign_sensitive", reconcile.Intent{Resource: "a", Mode: reconcile.ModeAdditive, Observed: reconcile.Observed{Present: true, Content: []byte("x"), AdditiveClaims: []reconcile.AdditiveClaim{{Subject: "foreign", Equivalent: true, Evidence: reconcile.Evidence{Foreign: true}}}}, Desired: reconcile.Desired{Content: []byte("x"), SensitiveLinks: link}}, "additive_foreign"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := reconcile.Reconcile(ir, []reconcile.Intent{tc.in})
			if err != nil || r.AllAdmitted() || r.Decisions()[0].Outcome != reconcile.OutcomeConflict || r.Decisions()[0].Explanation().Code != tc.code {
				t.Fatalf("report=%#v err=%v", r, err)
			}
			if _, ok := r.FinalizationPermit(); ok {
				t.Fatal("conflict permitted finalization")
			}
		})
	}
	for _, tc := range []struct {
		name string
		ir   compiler.ResolvedIR
		in   reconcile.Intent
		code reconcile.ContractCode
		path string
	}{
		{"invalid_ir", compiler.ResolvedIR{}, reconcile.Intent{}, reconcile.CodeInvalidIR, "ir"},
		{"invalid_additive", ir, reconcile.Intent{Resource: "a", Mode: reconcile.ModeReplace, Observed: reconcile.Observed{AdditiveClaims: []reconcile.AdditiveClaim{{Subject: "x"}}}}, reconcile.CodeInvalidAdditive, "intents.a.claims"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := reconcile.Reconcile(tc.ir, []reconcile.Intent{tc.in})
			var typed *reconcile.ContractError
			if !reflect.DeepEqual(r, reconcile.Report{}) || !errors.Is(err, tc.code) || !errors.As(err, &typed) || typed.Code() != tc.code || typed.Path() != tc.path {
				t.Fatalf("report=%#v err=%v typed=%#v", r, err, typed)
			}
		})
	}
	in := reconcile.Intent{Resource: "r", Mode: reconcile.ModeReplace, Observed: reconcile.Observed{Present: true, Content: []byte("old"), Evidence: reconcile.Evidence{Managed: true}}, Desired: reconcile.Desired{Content: []byte("new"), SensitiveLinks: link}}
	r, err := reconcile.Reconcile(ir, []reconcile.Intent{in})
	if err != nil || r.Decisions()[0].Outcome != reconcile.OutcomeUpdate || r.Decisions()[0].PriorHash == "" || r.Decisions()[0].NextHash == "" {
		t.Fatalf("report=%#v err=%v", r, err)
	}
	copy := r.Decisions()
	copy[0].Resource = "mutated"
	if r.Decisions()[0].Resource != "r" {
		t.Fatal("decisions accessor returned report-owned object")
	}
}

func TestW2BCorrectionD1D3Regression(t *testing.T) {
	ir := sealedIR(t, []compiler.CredentialRef{{Provider: "v", Slot: "s"}})
	link := []reconcile.SensitiveLink{{FinalizerID: "f", Credential: compiler.CredentialRef{Provider: "v", Slot: "s"}}}
	for _, tc := range []struct {
		name string
		in   reconcile.Intent
	}{
		{"marker_present_unproven", reconcile.Intent{Resource: "m", Mode: reconcile.ModeMarkerMerge, Observed: reconcile.Observed{Present: true, Content: []byte("old"), Marker: reconcile.MarkerMetadata{State: reconcile.MarkerAbsent}}, Desired: reconcile.Desired{Content: []byte("new"), SensitiveLinks: link}}},
		{"additive_present_unproven_sensitive", reconcile.Intent{Resource: "a", Mode: reconcile.ModeAdditive, Observed: reconcile.Observed{Present: true, Content: []byte("old")}, Desired: reconcile.Desired{Content: []byte("new"), SensitiveLinks: link}}},
		{"additive_equivalent_unproven_sensitive", reconcile.Intent{Resource: "a", Mode: reconcile.ModeAdditive, Observed: reconcile.Observed{Present: true, Content: []byte("old"), AdditiveClaims: []reconcile.AdditiveClaim{{Subject: "claim", Equivalent: true}}}, Desired: reconcile.Desired{Content: []byte("new"), SensitiveLinks: link}}},
		{"additive_present_unproven", reconcile.Intent{Resource: "a", Mode: reconcile.ModeAdditive, Observed: reconcile.Observed{Present: true, Content: []byte("old")}, Desired: reconcile.Desired{Content: []byte("new")}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := reconcile.Reconcile(ir, []reconcile.Intent{tc.in})
			if err != nil || r.AllAdmitted() || r.Decisions()[0].Outcome != reconcile.OutcomeConflict {
				t.Fatalf("report=%#v err=%v", r, err)
			}
			if _, ok := r.FinalizationPermit(); ok {
				t.Fatal("unproven ownership permitted finalization")
			}
		})
	}
	decision := func(in reconcile.Intent) reconcile.Decision {
		r, err := reconcile.Reconcile(ir, []reconcile.Intent{in})
		if err != nil {
			t.Fatal(err)
		}
		return r.Decisions()[0]
	}
	replace := reconcile.Intent{Resource: "r", Mode: reconcile.ModeReplace, Observed: reconcile.Observed{Present: true, Content: []byte("old"), Evidence: reconcile.Evidence{Managed: true}}, Desired: reconcile.Desired{Content: []byte("new")}}
	managed := decision(replace)
	if managed.PriorHash != ownershipHashFixture("lore.reconcile.prior.v1\x00", ir.IRID(), replace, nil) || managed.NextHash != ownershipHashFixture("lore.reconcile.next.v1\x00", ir.IRID(), replace, nil) {
		t.Fatal("hash omitted independently calculated ownership facts")
	}
	foreign := replace
	foreign.Observed.Evidence = reconcile.Evidence{Foreign: true}
	changed := decision(foreign)
	if managed.PriorHash == changed.PriorHash || managed.NextHash == changed.NextHash {
		t.Fatal("replace ownership did not bind both hashes")
	}
	marker := reconcile.Intent{Resource: "m", Mode: reconcile.ModeMarkerMerge, Observed: reconcile.Observed{Present: true, Content: []byte("old"), Marker: reconcile.MarkerMetadata{State: reconcile.MarkerValid}}, Desired: reconcile.Desired{Content: []byte("new")}}
	malformed := marker
	malformed.Observed.Marker.State = reconcile.MarkerMissing
	if a, b := decision(marker), decision(malformed); a.PriorHash == b.PriorHash || a.NextHash == b.NextHash {
		t.Fatal("marker ownership did not bind both hashes")
	}
	claims := []reconcile.AdditiveClaim{{Subject: "b", Equivalent: false, Evidence: reconcile.Evidence{Managed: true}}, {Subject: "a", Equivalent: false, Evidence: reconcile.Evidence{Managed: true}}}
	additive := reconcile.Intent{Resource: "a", Mode: reconcile.ModeAdditive, Observed: reconcile.Observed{Present: true, Content: []byte("old"), AdditiveClaims: claims}, Desired: reconcile.Desired{Content: []byte("new")}}
	permuted := additive
	permuted.Observed.AdditiveClaims = []reconcile.AdditiveClaim{claims[1], claims[0]}
	if a, b := decision(additive), decision(permuted); !reflect.DeepEqual(a, b) {
		t.Fatal("claim permutation changed decision")
	}
	unproven := additive
	unproven.Observed.AdditiveClaims = append([]reconcile.AdditiveClaim(nil), additive.Observed.AdditiveClaims...)
	unproven.Observed.AdditiveClaims[0].Evidence = reconcile.Evidence{}
	if a, b := decision(additive), decision(unproven); a.PriorHash == b.PriorHash || a.NextHash == b.NextHash {
		t.Fatal("claim ownership did not bind both hashes")
	}
}

func ownershipHashFixture(domain, id string, in reconcile.Intent, links []reconcile.SensitiveLink) string {
	claims := append([]reconcile.AdditiveClaim(nil), in.Observed.AdditiveClaims...)
	sort.Slice(claims, func(i, j int) bool { return claims[i].Subject < claims[j].Subject })
	v := struct {
		IRID, Resource    string
		Mode              reconcile.Mode
		Present           bool
		Observed, Desired []byte
		Evidence          reconcile.Evidence
		Marker            reconcile.MarkerMetadata
		Claims            []reconcile.AdditiveClaim
		Links             []reconcile.SensitiveLink
	}{id, in.Resource, in.Mode, in.Observed.Present, nil, nil, in.Observed.Evidence, in.Observed.Marker, claims, links}
	v.Observed, v.Desired = append([]byte(nil), in.Observed.Content...), append([]byte(nil), in.Desired.Content...)
	b, _ := json.Marshal(v)
	s := sha256.Sum256(append([]byte(domain), b...))
	return hex.EncodeToString(s[:])
}

func sealedIR(t *testing.T, slots []compiler.CredentialRef) compiler.ResolvedIR {
	t.Helper()
	in := compiler.Input{Schema: compiler.SchemaV1, Compiler: compiler.CompilerV1, Pack: compiler.PackSnapshot{ID: "pack", Version: "1"}, Target: compiler.TargetPi, Profiles: []compiler.Profile{{ID: "p", DefaultModel: "m"}}, Candidates: []compiler.Candidate{{ProfileID: "p", SourceKey: "s"}}, CredentialSlots: slots}
	ir, err := compiler.Compile(in)
	if err != nil {
		t.Fatal(err)
	}
	return ir
}

func TestW2BD5NextHashBindsObservedPresenceAndOwnership(t *testing.T) {
	ir := sealedIR(t, nil)
	cases := []struct {
		name     string
		observed reconcile.Observed
		want     reconcile.Outcome
	}{
		{"absent_managed", reconcile.Observed{Evidence: reconcile.Evidence{Managed: true}}, reconcile.OutcomeUpdate},
		{"present_managed_noop", reconcile.Observed{Present: true, Content: []byte("same"), Evidence: reconcile.Evidence{Managed: true}}, reconcile.OutcomeNoop},
		{"present_managed_update", reconcile.Observed{Present: true, Content: []byte("old"), Evidence: reconcile.Evidence{Managed: true}}, reconcile.OutcomeUpdate},
		{"present_foreign_conflict", reconcile.Observed{Present: true, Content: []byte("old"), Evidence: reconcile.Evidence{Foreign: true}}, reconcile.OutcomeConflict},
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		r, err := reconcile.Reconcile(ir, []reconcile.Intent{{Resource: "r", Mode: reconcile.ModeReplace, Observed: tc.observed, Desired: reconcile.Desired{Content: []byte("same")}}})
		d := r.Decisions()[0]
		if err != nil || d.Outcome != tc.want || seen[d.NextHash] {
			t.Fatalf("%s: %#v %v", tc.name, r, err)
		}
		seen[d.NextHash] = true
	}
}

func TestW2BCorrectionD4MixedMarkerOwnership(t *testing.T) {
	ir := sealedIR(t, []compiler.CredentialRef{{Provider: "v", Slot: "s"}})
	link := []reconcile.SensitiveLink{{FinalizerID: "f", Credential: compiler.CredentialRef{Provider: "v", Slot: "s"}}}
	mixed := func(e reconcile.Evidence, sensitive bool) reconcile.Intent {
		in := reconcile.Intent{Resource: "mixed", Mode: reconcile.ModeMarkerMerge, Observed: reconcile.Observed{Present: true, Content: []byte("old"), Evidence: e, Marker: reconcile.MarkerMetadata{State: reconcile.MarkerAbsent}}, Desired: reconcile.Desired{Content: []byte("new")}}
		if sensitive {
			in.Desired.SensitiveLinks = link
		}
		return in
	}
	for _, tc := range []struct {
		name string
		in   reconcile.Intent
	}{
		{"non_sensitive", mixed(reconcile.Evidence{Managed: true, Foreign: true}, false)},
		{"sensitive_permuted_evidence", mixed(reconcile.Evidence{Foreign: true, Managed: true}, true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := reconcile.Reconcile(ir, []reconcile.Intent{tc.in})
			if err != nil || r.AllAdmitted() || r.Decisions()[0].Outcome != reconcile.OutcomeConflict || r.Decisions()[0].Explanation().Code != "marker_foreign" {
				t.Fatalf("report=%#v err=%v", r, err)
			}
			if _, ok := r.FinalizationPermit(); ok {
				t.Fatal("mixed ownership permitted finalization")
			}
		})
	}
	safe := reconcile.Intent{Resource: "safe", Mode: reconcile.ModeReplace, Observed: reconcile.Observed{Present: true, Content: []byte("old"), Evidence: reconcile.Evidence{Managed: true}}, Desired: reconcile.Desired{Content: []byte("new"), SensitiveLinks: link}}
	r, err := reconcile.Reconcile(ir, []reconcile.Intent{safe, mixed(reconcile.Evidence{Managed: true, Foreign: true}, true)})
	if err != nil || r.AllAdmitted() {
		t.Fatalf("batch=%#v err=%v", r, err)
	}
	if _, ok := r.FinalizationPermit(); ok {
		t.Fatal("mixed batch permitted finalization")
	}
}
