package install

import (
	"regexp"
	"sort"
	"strings"

	"github.com/alferio94/lore-cli/internal/compiler"
	"github.com/alferio94/lore-cli/internal/reconcile"
)

// ProjectorCode identifies pure semantic projection contract failures.
type ProjectorCode string

const (
	CodeProjectorInvalidIR         ProjectorCode = "invalid_ir"
	CodeProjectorInvalidTarget     ProjectorCode = "invalid_target"
	CodeProjectorUnsupportedTarget ProjectorCode = "unsupported_target"
	CodeProjectorTargetMismatch    ProjectorCode = "target_mismatch"
	CodeProjectorInvalidFact       ProjectorCode = "invalid_target_fact"
	CodeProjectorDuplicateResource ProjectorCode = "duplicate_resource"
	CodeProjectorInvalidSensitive  ProjectorCode = "invalid_sensitive_reference"
	CodeProjectorSecret            ProjectorCode = "secret_value"
)

func (c ProjectorCode) Error() string { return string(c) }

type ProjectorError struct {
	code ProjectorCode
	path string
}

func (e *ProjectorError) Error() string       { return "semantic projection rejected" }
func (e *ProjectorError) Code() ProjectorCode { return e.code }
func (e *ProjectorError) Path() string        { return e.path }
func (e *ProjectorError) Is(target error) bool {
	code, ok := target.(ProjectorCode)
	return ok && code == e.code
}
func projectorError(code ProjectorCode, path string) error {
	return &ProjectorError{code: code, path: path}
}

// SensitiveReference deliberately carries a non-secret provider/slot pair.
type SensitiveReference struct {
	FinalizerID string
	Provider    string
	Slot        string
}

type SemanticResource struct {
	Resource            string
	Component           ComponentID
	Mode                MergeMode
	Present             bool
	Observed            []byte
	Desired             []byte
	Evidence            reconcile.Evidence
	Marker              reconcile.MarkerState
	AdditiveClaims      []reconcile.AdditiveClaim
	OwnershipMarkers    map[string]string
	SensitiveReferences []SensitiveReference
}

type ProjectorInput struct {
	Target    TargetID
	Resources []SemanticResource
}

type OwnershipMarker struct {
	Name  string
	Value string
}

type SourceFact struct {
	Tier                        compiler.Tier
	SourceKey, ProfileID, Model string
	Location                    string
	Scope                       compiler.ProfileScope
	ProjectID                   compiler.ProjectID
}

type ProfileFact struct {
	Winner   SourceFact
	Shadowed []SourceFact
}

type RoleFact struct {
	Role, Model string
	Winner      SourceFact
	Shadowed    []SourceFact
}

type PersistenceFact struct {
	Requested bool
	Scope     compiler.ProfileScope
	ProjectID compiler.ProjectID
	ProfileID string
}

type SemanticResourceFact struct {
	Resource            string
	Component           ComponentID
	Mode                reconcile.Mode
	Present             bool
	Observed            []byte
	Desired             []byte
	Evidence            reconcile.Evidence
	Marker              reconcile.MarkerState
	AdditiveClaims      []reconcile.AdditiveClaim
	OwnershipMarkers    []OwnershipMarker
	SensitiveReferences []SensitiveReference
}

type TargetFacts struct {
	Target       TargetID
	IRID         string
	Profile      ProfileFact
	Roles        []RoleFact
	Capabilities []compiler.CapabilityDecision
	Persistence  PersistenceFact
	Resources    []SemanticResourceFact
}

type SemanticPlan struct {
	facts   TargetFacts
	intents []reconcile.Intent
}

func (p SemanticPlan) IsZero() bool { return p.facts.Target == "" && len(p.intents) == 0 }
func (p SemanticPlan) TargetFacts() TargetFacts {
	return cloneTargetFacts(p.facts)
}
func (p SemanticPlan) Intents() []reconcile.Intent { return cloneIntents(p.intents) }

var (
	secretValuePattern      = regexp.MustCompile(`(?i)\b(?:bearer|basic)[ \t]+[^\r\n]+|["']?(?:secret|token|password|authorization|api[_-]?key)["']?[ \t]*[:=][ \t]*["'][^"']+["']`)
	secretAssignmentPattern = regexp.MustCompile(`(?i)\b(?:secret|token|password|bearer|credential|authorization|api[_-]?key|apikey)\b[ \t]*[:=][ \t]*[^ \t\r\n][^\r\n]*`)
)

// ProjectSemanticPlan maps caller-supplied, already-sanitized target facts to
// the accepted W2B reconciliation contract. It performs no IO and preserves
// conflicts as intents for deterministic reconciliation/explanation.
func ProjectSemanticPlan(ir compiler.ResolvedIR, input ProjectorInput) (SemanticPlan, error) {
	if !ir.Sealed() || !ir.Admitted || ir.IRID() == "" {
		return SemanticPlan{}, projectorError(CodeProjectorInvalidIR, "ir")
	}
	if !knownProjectorTarget(input.Target) {
		if input.Target == TargetCodex || input.Target == TargetAntigravity || input.Target == TargetClaudeCode {
			return SemanticPlan{}, projectorError(CodeProjectorUnsupportedTarget, "target")
		}
		return SemanticPlan{}, projectorError(CodeProjectorInvalidTarget, "target")
	}
	if string(ir.Target) != string(input.Target) {
		return SemanticPlan{}, projectorError(CodeProjectorTargetMismatch, "target")
	}
	resources := cloneSemanticResources(input.Resources)
	sort.Slice(resources, func(i, j int) bool { return resources[i].Resource < resources[j].Resource })
	if len(resources) == 0 {
		return SemanticPlan{}, projectorError(CodeProjectorInvalidFact, "resources")
	}

	facts := targetFactsFromIR(ir, input.Target)
	facts.Resources = make([]SemanticResourceFact, 0, len(resources))
	intents := make([]reconcile.Intent, 0, len(resources))
	for i := range resources {
		resource := resources[i]
		path := "resources." + resource.Resource
		if !safeSemanticResource(resource.Resource) || resource.Component == "" {
			return SemanticPlan{}, projectorError(CodeProjectorInvalidFact, "resources.resource")
		}
		if !projectorTargetSupports(input.Target, resource.Component) {
			return SemanticPlan{}, projectorError(CodeProjectorInvalidFact, path+".component")
		}
		if i > 0 && resource.Resource == resources[i-1].Resource {
			return SemanticPlan{}, projectorError(CodeProjectorDuplicateResource, path)
		}
		mode, ok := reconciliationMode(resource.Mode)
		if !ok {
			return SemanticPlan{}, projectorError(CodeProjectorInvalidFact, path+".mode")
		}
		if secretValuePattern.Match(resource.Observed) {
			return SemanticPlan{}, projectorError(CodeProjectorSecret, path+".observed")
		}
		if secretValuePattern.Match(resource.Desired) {
			return SemanticPlan{}, projectorError(CodeProjectorSecret, path+".desired")
		}
		if err := normalizeResource(&resource, mode, path, ir); err != nil {
			return SemanticPlan{}, err
		}
		markers := sortedOwnershipMarkers(resource.OwnershipMarkers)
		fact := SemanticResourceFact{
			Resource: resource.Resource, Component: resource.Component, Mode: mode,
			Present: resource.Present, Observed: append([]byte(nil), resource.Observed...), Desired: append([]byte(nil), resource.Desired...),
			Evidence: resource.Evidence, Marker: resource.Marker, AdditiveClaims: append([]reconcile.AdditiveClaim(nil), resource.AdditiveClaims...),
			OwnershipMarkers: markers, SensitiveReferences: append([]SensitiveReference(nil), resource.SensitiveReferences...),
		}
		facts.Resources = append(facts.Resources, fact)
		intents = append(intents, reconcile.Intent{
			Resource: resource.Resource, Mode: mode,
			Observed: reconcile.Observed{Present: resource.Present, Content: append([]byte(nil), resource.Observed...), Evidence: resource.Evidence, Marker: reconcile.MarkerMetadata{State: resource.Marker}, AdditiveClaims: append([]reconcile.AdditiveClaim(nil), resource.AdditiveClaims...)},
			Desired:  reconcile.Desired{Content: append([]byte(nil), resource.Desired...), SensitiveLinks: sensitiveLinks(resource.SensitiveReferences)},
		})
	}
	return SemanticPlan{facts: cloneTargetFacts(facts), intents: cloneIntents(intents)}, nil
}

func knownProjectorTarget(target TargetID) bool {
	return target == TargetPi || target == TargetOpenCode
}

func projectorTargetSupports(target TargetID, component ComponentID) bool {
	switch target {
	case TargetPi:
		return defaultPiAdapter().Supports(component)
	case TargetOpenCode:
		return defaultOpenCodeAdapter().Supports(component)
	default:
		return false
	}
}

func reconciliationMode(mode MergeMode) (reconcile.Mode, bool) {
	switch mode {
	case MergeModeReplace:
		return reconcile.ModeReplace, true
	case MergeModeMarkerMerge:
		return reconcile.ModeMarkerMerge, true
	case MergeModeAdditiveJSON:
		return reconcile.ModeAdditive, true
	default:
		return "", false
	}
}

func normalizeResource(resource *SemanticResource, mode reconcile.Mode, path string, ir compiler.ResolvedIR) error {
	if mode == reconcile.ModeMarkerMerge && (resource.Marker == "" || len(resource.OwnershipMarkers) == 0) {
		return projectorError(CodeProjectorInvalidFact, path+".marker")
	}
	if mode != reconcile.ModeAdditive && len(resource.AdditiveClaims) > 0 {
		return projectorError(CodeProjectorInvalidFact, path+".claims")
	}
	sort.Slice(resource.AdditiveClaims, func(i, j int) bool { return resource.AdditiveClaims[i].Subject < resource.AdditiveClaims[j].Subject })
	for i, claim := range resource.AdditiveClaims {
		if claim.Subject == "" || (i > 0 && claim.Subject == resource.AdditiveClaims[i-1].Subject) {
			return projectorError(CodeProjectorInvalidFact, path+".claims")
		}
	}
	for name, value := range resource.OwnershipMarkers {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(value) == "" {
			return projectorError(CodeProjectorInvalidFact, path+".ownership_markers")
		}
		if secretOwnershipMarker(name, value) {
			return projectorError(CodeProjectorSecret, path+".ownership_markers")
		}
	}
	sort.Slice(resource.SensitiveReferences, func(i, j int) bool {
		a, b := resource.SensitiveReferences[i], resource.SensitiveReferences[j]
		if a.FinalizerID != b.FinalizerID {
			return a.FinalizerID < b.FinalizerID
		}
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		return a.Slot < b.Slot
	})
	for i, ref := range resource.SensitiveReferences {
		if ref.FinalizerID == "" || ref.Provider == "" || ref.Slot == "" || (i > 0 && ref == resource.SensitiveReferences[i-1]) || !declaredCredential(ir, ref) {
			return projectorError(CodeProjectorInvalidSensitive, path+".sensitive_references")
		}
	}
	return nil
}

func declaredCredential(ir compiler.ResolvedIR, ref SensitiveReference) bool {
	for _, slot := range ir.CredentialSlots() {
		if slot.Provider == ref.Provider && slot.Slot == ref.Slot {
			return true
		}
	}
	return false
}

func targetFactsFromIR(ir compiler.ResolvedIR, target TargetID) TargetFacts {
	profile := ir.Profile()
	facts := TargetFacts{Target: target, IRID: ir.IRID(), Profile: ProfileFact{Winner: sourceFact(profile.Winner), Shadowed: sourceFacts(profile.Shadowed)}, Capabilities: ir.Capabilities()}
	for _, role := range ir.Roles() {
		facts.Roles = append(facts.Roles, RoleFact{Role: role.Role, Model: role.Model, Winner: sourceFact(role.Winner), Shadowed: sourceFacts(role.Shadowed)})
	}
	if intent, ok := ir.ProfilePersistenceIntent(); ok {
		facts.Persistence = PersistenceFact{Requested: true, Scope: intent.Scope(), ProjectID: intent.ProjectID(), ProfileID: intent.ProfileID()}
	}
	return facts
}

func sourceFact(candidate compiler.Candidate) SourceFact {
	return SourceFact{Tier: candidate.Tier, SourceKey: candidate.SourceKey, ProfileID: candidate.ProfileID, Model: candidate.Model, Location: candidate.Location, Scope: candidate.Scope, ProjectID: candidate.ProjectID}
}
func sourceFacts(candidates []compiler.Candidate) []SourceFact {
	facts := make([]SourceFact, len(candidates))
	for i := range candidates {
		facts[i] = sourceFact(candidates[i])
	}
	return facts
}
func sensitiveLinks(refs []SensitiveReference) []reconcile.SensitiveLink {
	links := make([]reconcile.SensitiveLink, len(refs))
	for i, ref := range refs {
		links[i] = reconcile.SensitiveLink{FinalizerID: ref.FinalizerID, Credential: compiler.CredentialRef{Provider: ref.Provider, Slot: ref.Slot}}
	}
	return links
}
func secretOwnershipMarker(name, value string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, term := range []string{"secret", "token", "password", "bearer", "credential", "authorization", "api_key", "api-key", "apikey"} {
		if strings.Contains(name, term) {
			return true
		}
	}
	value = strings.TrimSpace(value)
	return secretValuePattern.MatchString(value) || secretAssignmentPattern.MatchString(value)
}

func sortedOwnershipMarkers(markers map[string]string) []OwnershipMarker {
	result := make([]OwnershipMarker, 0, len(markers))
	for name, value := range markers {
		result = append(result, OwnershipMarker{Name: name, Value: value})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
func safeSemanticResource(resource string) bool {
	return resource != "" && !strings.Contains(resource, "..") && !strings.ContainsAny(resource, "\x00\r\n")
}

func cloneSemanticResources(resources []SemanticResource) []SemanticResource {
	out := append([]SemanticResource(nil), resources...)
	for i := range out {
		out[i].Observed = append([]byte(nil), resources[i].Observed...)
		out[i].Desired = append([]byte(nil), resources[i].Desired...)
		out[i].AdditiveClaims = append([]reconcile.AdditiveClaim(nil), resources[i].AdditiveClaims...)
		out[i].SensitiveReferences = append([]SensitiveReference(nil), resources[i].SensitiveReferences...)
		out[i].OwnershipMarkers = make(map[string]string, len(resources[i].OwnershipMarkers))
		for key, value := range resources[i].OwnershipMarkers {
			out[i].OwnershipMarkers[key] = value
		}
	}
	return out
}
func cloneTargetFacts(facts TargetFacts) TargetFacts {
	out := facts
	out.Profile.Shadowed = append([]SourceFact(nil), facts.Profile.Shadowed...)
	out.Roles = append([]RoleFact(nil), facts.Roles...)
	for i := range out.Roles {
		out.Roles[i].Shadowed = append([]SourceFact(nil), facts.Roles[i].Shadowed...)
	}
	out.Capabilities = append([]compiler.CapabilityDecision(nil), facts.Capabilities...)
	out.Resources = append([]SemanticResourceFact(nil), facts.Resources...)
	for i := range out.Resources {
		out.Resources[i].Observed = append([]byte(nil), facts.Resources[i].Observed...)
		out.Resources[i].Desired = append([]byte(nil), facts.Resources[i].Desired...)
		out.Resources[i].AdditiveClaims = append([]reconcile.AdditiveClaim(nil), facts.Resources[i].AdditiveClaims...)
		out.Resources[i].OwnershipMarkers = append([]OwnershipMarker(nil), facts.Resources[i].OwnershipMarkers...)
		out.Resources[i].SensitiveReferences = append([]SensitiveReference(nil), facts.Resources[i].SensitiveReferences...)
	}
	return out
}
func cloneIntents(intents []reconcile.Intent) []reconcile.Intent {
	out := append([]reconcile.Intent(nil), intents...)
	for i := range out {
		out[i].Observed.Content = append([]byte(nil), intents[i].Observed.Content...)
		out[i].Observed.AdditiveClaims = append([]reconcile.AdditiveClaim(nil), intents[i].Observed.AdditiveClaims...)
		out[i].Desired.Content = append([]byte(nil), intents[i].Desired.Content...)
		out[i].Desired.SensitiveLinks = append([]reconcile.SensitiveLink(nil), intents[i].Desired.SensitiveLinks...)
	}
	return out
}
