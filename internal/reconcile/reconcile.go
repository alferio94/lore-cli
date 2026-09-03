// Package reconcile compares already-normalized projection facts without IO.
package reconcile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/alferio94/lore-cli/internal/compiler"
)

type Mode string

const (
	ModeReplace     Mode = "replace"
	ModeMarkerMerge Mode = "marker-merge"
	ModeAdditive    Mode = "additive"
)

type Outcome string

const (
	OutcomeNoop     Outcome = "noop"
	OutcomeUpdate   Outcome = "update"
	OutcomeConflict Outcome = "conflict"
)

type MarkerState string

const (
	MarkerAbsent    MarkerState = "absent"
	MarkerValid     MarkerState = "valid"
	MarkerMissing   MarkerState = "missing"
	MarkerReversed  MarkerState = "reversed"
	MarkerDuplicate MarkerState = "duplicate"
	MarkerNested    MarkerState = "nested"
)

type AdditiveState string

const (
	AdditiveAbsent     AdditiveState = "absent"
	AdditiveEquivalent AdditiveState = "equivalent"
	AdditiveOccupied   AdditiveState = "occupied"
)

type ContractCode string

const (
	CodeInvalidIR            ContractCode = "invalid_ir"
	CodeInvalidIntent        ContractCode = "invalid_intent"
	CodeDuplicateResource    ContractCode = "duplicate_resource"
	CodeInvalidEvidence      ContractCode = "invalid_evidence"
	CodeInvalidMarker        ContractCode = "invalid_marker"
	CodeInvalidAdditive      ContractCode = "invalid_additive"
	CodeInvalidSensitiveLink ContractCode = "invalid_sensitive_link"
)

type ContractError struct {
	code          ContractCode
	path, message string
}

func (e *ContractError) Error() string      { return e.message }
func (e *ContractError) Code() ContractCode { return e.code }
func (e *ContractError) Path() string       { return e.path }
func (e *ContractError) Is(target error) bool {
	c, ok := target.(ContractCode)
	return ok && c == e.code
}
func contract(code ContractCode, path, message string) error {
	return &ContractError{code, path, message}
}
func (c ContractCode) Error() string { return string(c) }

type Evidence struct{ Managed, Foreign, Contradictory bool }
type MarkerMetadata struct{ State MarkerState }
type AdditiveClaim struct {
	Subject    string
	Equivalent bool
	Evidence   Evidence
}
type SensitiveLink struct {
	FinalizerID string
	Credential  compiler.CredentialRef
}
type Observed struct {
	Present        bool
	Content        []byte
	Evidence       Evidence
	Marker         MarkerMetadata
	AdditiveClaims []AdditiveClaim
}
type Desired struct {
	Content        []byte
	SensitiveLinks []SensitiveLink
}
type Intent struct {
	Resource string
	Mode     Mode
	Observed Observed
	Desired  Desired
}
type Explanation struct{ Subject, Code, Remediation string }
type Decision struct {
	Resource            string
	Mode                Mode
	Outcome             Outcome
	PriorHash, NextHash string
	next                []byte
	explanation         Explanation
}

func (d Decision) NextSanitized() []byte    { return append([]byte(nil), d.next...) }
func (d Decision) Explanation() Explanation { return d.explanation }

type FinalizationPermit struct {
	irID        string
	projections []compiler.SensitiveProjection
}

func (p FinalizationPermit) IRID() string { return p.irID }
func (p FinalizationPermit) Projections() []compiler.SensitiveProjection {
	return append([]compiler.SensitiveProjection(nil), p.projections...)
}

type Report struct {
	decisions   []Decision
	allAdmitted bool
	permit      *FinalizationPermit
}

func (r Report) Decisions() []Decision {
	out := append([]Decision(nil), r.decisions...)
	for i := range out {
		out[i].next = append([]byte(nil), out[i].next...)
	}
	return out
}
func (r Report) AllAdmitted() bool { return r.allAdmitted }
func (r Report) FinalizationPermit() (FinalizationPermit, bool) {
	if r.permit == nil {
		return FinalizationPermit{}, false
	}
	return FinalizationPermit{irID: r.permit.irID, projections: pcopy(r.permit.projections)}, true
}
func pcopy(in []compiler.SensitiveProjection) []compiler.SensitiveProjection {
	return append([]compiler.SensitiveProjection(nil), in...)
}

func Reconcile(ir compiler.ResolvedIR, intents []Intent) (Report, error) {
	if !ir.Sealed() || !ir.Admitted || ir.IRID() == "" {
		return Report{}, contract(CodeInvalidIR, "ir", "reconcile requires an admitted sealed compiler IR")
	}
	if len(intents) == 0 {
		return Report{}, contract(CodeInvalidIntent, "intents", "at least one intent is required")
	}
	items := append([]Intent(nil), intents...)
	sort.Slice(items, func(i, j int) bool { return items[i].Resource < items[j].Resource })
	for i := range items {
		if err := validate(items[i], i > 0 && items[i-1].Resource == items[i].Resource); err != nil {
			return Report{}, err
		}
		for _, link := range items[i].Desired.SensitiveLinks {
			if _, err := compiler.NewSensitiveProjection(ir, link.FinalizerID, link.Credential); err != nil {
				return Report{}, contract(CodeInvalidSensitiveLink, "intents."+items[i].Resource+".links", "invalid sensitive link")
			}
		}
	}
	report := Report{decisions: make([]Decision, 0, len(items)), allAdmitted: true}
	var projections []compiler.SensitiveProjection
	for _, in := range items {
		d := decide(ir, in)
		report.decisions = append(report.decisions, d)
		if d.Outcome == OutcomeConflict {
			report.allAdmitted = false
			continue
		}
		for _, link := range sortedLinks(in.Desired.SensitiveLinks) {
			p, _ := compiler.NewSensitiveProjection(ir, link.FinalizerID, link.Credential)
			projections = append(projections, p)
		}
	}
	if report.allAdmitted && len(projections) > 0 {
		report.permit = &FinalizationPermit{irID: ir.IRID(), projections: pcopy(projections)}
	}
	return report, nil
}
func validate(in Intent, duplicate bool) error {
	path := "intents." + in.Resource
	if !safeResource(in.Resource) {
		return contract(CodeInvalidIntent, "intents.resource", "resource is required")
	}
	if duplicate {
		return contract(CodeDuplicateResource, path, "resource appears more than once")
	}
	if in.Mode != ModeReplace && in.Mode != ModeMarkerMerge && in.Mode != ModeAdditive {
		return contract(CodeInvalidIntent, path+".mode", "mode is invalid")
	}
	if in.Observed.Evidence.Contradictory {
		return contract(CodeInvalidEvidence, path+".evidence", "ownership evidence is contradictory")
	}
	if in.Mode == ModeMarkerMerge && (in.Observed.Marker.State == "" || !markerKnown(in.Observed.Marker.State)) {
		return contract(CodeInvalidMarker, path+".marker", "marker metadata is required and invalid")
	}
	if in.Mode != ModeAdditive && len(in.Observed.AdditiveClaims) > 0 {
		return contract(CodeInvalidAdditive, path+".claims", "additive claims require additive mode")
	}
	for _, c := range in.Observed.AdditiveClaims {
		if c.Subject == "" || c.Evidence.Contradictory {
			return contract(CodeInvalidAdditive, path+".claims", "additive claims are invalid")
		}
	}
	claims := append([]AdditiveClaim(nil), in.Observed.AdditiveClaims...)
	sort.Slice(claims, func(i, j int) bool { return claims[i].Subject < claims[j].Subject })
	for i := 1; i < len(claims); i++ {
		if claims[i].Subject == claims[i-1].Subject {
			return contract(CodeInvalidAdditive, path+".claims", "additive claim subjects must be unique")
		}
	}
	links := sortedLinks(in.Desired.SensitiveLinks)
	for i, l := range links {
		if l.FinalizerID == "" || l.Credential.Provider == "" || l.Credential.Slot == "" {
			return contract(CodeInvalidSensitiveLink, path+".links", "sensitive link requires finalizer provider and slot")
		}
		if i > 0 && l == links[i-1] {
			return contract(CodeInvalidSensitiveLink, path+".links", "sensitive links must be unique")
		}
	}
	return nil
}
func safeResource(resource string) bool {
	return resource != "" && !strings.Contains(resource, "..") && !strings.ContainsAny(resource, "\x00\r\n")
}
func markerKnown(s MarkerState) bool {
	return s == MarkerAbsent || s == MarkerValid || s == MarkerMissing || s == MarkerReversed || s == MarkerDuplicate || s == MarkerNested
}
func decide(ir compiler.ResolvedIR, in Intent) Decision {
	equal := in.Observed.Present && string(in.Observed.Content) == string(in.Desired.Content)
	sensitive := len(in.Desired.SensitiveLinks) > 0
	links := sortedLinks(in.Desired.SensitiveLinks)
	d := Decision{Resource: in.Resource, Mode: in.Mode, PriorHash: hash("lore.reconcile.prior.v1\x00", ir, in, links), NextHash: hash("lore.reconcile.next.v1\x00", ir, in, links), next: append([]byte(nil), in.Desired.Content...)}
	outcome, code := OutcomeUpdate, ""
	switch in.Mode {
	case ModeReplace:
		if !in.Observed.Present {
			code = "replace_absent"
		} else if equal && !sensitive {
			outcome, code = OutcomeNoop, "already_desired"
		} else if in.Observed.Evidence.Foreign || in.Observed.Evidence.Contradictory {
			outcome, code = OutcomeConflict, "replace_foreign"
		} else if equal && sensitive {
			if in.Observed.Evidence.Managed {
				code = "replace_owned"
			} else {
				outcome, code = OutcomeConflict, "replace_unowned"
			}
		} else if in.Observed.Evidence.Managed {
			code = "replace_owned"
		} else {
			outcome, code = OutcomeConflict, "replace_unowned"
		}
	case ModeMarkerMerge:
		s := in.Observed.Marker.State
		if s == MarkerAbsent && in.Observed.Present && (!in.Observed.Evidence.Managed || in.Observed.Evidence.Foreign) {
			outcome, code = OutcomeConflict, "marker_foreign"
		} else if s == MarkerAbsent {
			code = "marker_absent"
		} else if s != MarkerValid {
			outcome, code = OutcomeConflict, "marker_malformed"
		} else if equal && !sensitive {
			outcome, code = OutcomeNoop, "already_desired"
		} else if in.Observed.Evidence.Foreign || in.Observed.Evidence.Contradictory {
			outcome, code = OutcomeConflict, "marker_foreign"
		} else if equal && sensitive {
			code = "marker_valid"
		} else {
			code = "marker_valid"
		}
	case ModeAdditive:
		unsafe, foreign, equivalent := additiveUnsafe(in.Observed.AdditiveClaims)
		if in.Observed.Present && (len(in.Observed.AdditiveClaims) == 0 || (sensitive && !additiveManaged(in.Observed.AdditiveClaims))) {
			outcome, code = OutcomeConflict, "additive_unowned"
			if additiveForeignSubject(in.Observed.AdditiveClaims) != "" {
				code = "additive_foreign"
			}
		} else if sensitive && additiveForeignSubject(in.Observed.AdditiveClaims) != "" && unsafe == "" {
			outcome, code = OutcomeConflict, "additive_foreign"
		} else if (equal || equivalent) && !sensitive {
			outcome, code = OutcomeNoop, "already_desired"
		} else if unsafe != "" {
			outcome = OutcomeConflict
			if foreign {
				code = "additive_foreign"
			} else {
				code = "additive_unowned"
			}
		} else {
			code = "additive_update"
		}
	}
	d.Outcome = outcome
	subject := in.Resource
	if in.Mode == ModeAdditive && outcome == OutcomeConflict {
		subject = additiveSubject(in.Observed.AdditiveClaims)
		if subject == "" {
			subject = additiveForeignSubject(in.Observed.AdditiveClaims)
		}
	}
	d.explanation = Explanation{Subject: subject, Code: code, Remediation: "review ownership evidence before applying"}
	return d
}
func additiveSubject(cs []AdditiveClaim) string { subject, _, _ := additiveUnsafe(cs); return subject }
func additiveForeignSubject(cs []AdditiveClaim) string {
	cs = append([]AdditiveClaim(nil), cs...)
	sort.Slice(cs, func(i, j int) bool { return cs[i].Subject < cs[j].Subject })
	for _, c := range cs {
		if c.Evidence.Foreign || c.Evidence.Contradictory {
			return c.Subject
		}
	}
	return ""
}
func additiveManaged(cs []AdditiveClaim) bool {
	if len(cs) == 0 {
		return false
	}
	for _, c := range cs {
		if !c.Evidence.Managed || c.Evidence.Foreign || c.Evidence.Contradictory {
			return false
		}
	}
	return true
}
func additiveUnsafe(cs []AdditiveClaim) (string, bool, bool) {
	cs = append([]AdditiveClaim(nil), cs...)
	sort.Slice(cs, func(i, j int) bool { return cs[i].Subject < cs[j].Subject })
	foreign, unsafe, equivalent := false, "", len(cs) > 0
	for _, c := range cs {
		if c.Equivalent {
			continue
		}
		equivalent = false
		if c.Evidence.Foreign || c.Evidence.Contradictory {
			foreign = true
		}
		if unsafe == "" && (c.Evidence.Foreign || !c.Evidence.Managed) {
			unsafe = c.Subject
		}
	}
	return unsafe, foreign, equivalent
}
func sortedLinks(in []SensitiveLink) []SensitiveLink {
	out := append([]SensitiveLink(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].FinalizerID != out[j].FinalizerID {
			return out[i].FinalizerID < out[j].FinalizerID
		}
		if out[i].Credential.Provider != out[j].Credential.Provider {
			return out[i].Credential.Provider < out[j].Credential.Provider
		}
		return out[i].Credential.Slot < out[j].Credential.Slot
	})
	return out
}
func hash(domain string, ir compiler.ResolvedIR, in Intent, links []SensitiveLink) string {
	claims := append([]AdditiveClaim(nil), in.Observed.AdditiveClaims...)
	sort.Slice(claims, func(i, j int) bool { return claims[i].Subject < claims[j].Subject })
	v := struct {
		IRID, Resource    string
		Mode              Mode
		Present           bool
		Observed, Desired []byte
		Evidence          Evidence
		Marker            MarkerMetadata
		Claims            []AdditiveClaim
		Links             []SensitiveLink
	}{ir.IRID(), in.Resource, in.Mode, in.Observed.Present, nil, nil, in.Observed.Evidence, in.Observed.Marker, claims, links}
	v.Observed, v.Desired = append([]byte(nil), in.Observed.Content...), append([]byte(nil), in.Desired.Content...)
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(append([]byte(domain), b...))
	return hex.EncodeToString(sum[:])
}
