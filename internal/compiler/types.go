// Package compiler resolves a sealed, non-secret target plan from pure input.
package compiler

const (
	SchemaV1   uint16 = 1
	CompilerV1        = "canonical-capability-profile-compiler/v1.0.0"
)

type ErrorCode string

func (code ErrorCode) Error() string { return string(code) }

const (
	CodeInvalidCompilerVersion   ErrorCode = "invalid_compiler_version"
	CodeTargetRequired           ErrorCode = "target_required"
	CodeUnknownTarget            ErrorCode = "unknown_target"
	CodeTargetRoadmapUnsupported ErrorCode = "target_roadmap_unsupported"
	CodeDuplicateCandidateSource ErrorCode = "duplicate_candidate_source"
	CodeDuplicateOverrideSource  ErrorCode = "duplicate_override_source"
	CodeDuplicateCredentialRef   ErrorCode = "duplicate_credential_ref"
	CodeCapabilityUnsupported    ErrorCode = "capability_unsupported"
	CodeCapabilityUnknown        ErrorCode = "capability_unknown"
	CodeInvalidProfileScope      ErrorCode = "invalid_profile_scope"
	CodeInvalidProjectID         ErrorCode = "invalid_project_id"
	CodeInvalidPersistenceIntent ErrorCode = "invalid_persistence_intent"
	CodeNoMatchingProfile        ErrorCode = "no_matching_profile"
)

type ValidationError struct {
	code    ErrorCode
	message string
}

func (e *ValidationError) Error() string   { return e.message }
func (e *ValidationError) Code() ErrorCode { return e.code }
func (e *ValidationError) Is(target error) bool {
	if code, ok := target.(ErrorCode); ok {
		return e.code == code
	}
	other, ok := target.(*ValidationError)
	return ok && e.code == other.code
}

func validationError(code ErrorCode, message string) error {
	return &ValidationError{code: code, message: message}
}

type TargetID string

const (
	TargetPi          TargetID = "pi"
	TargetOpenCode    TargetID = "opencode"
	TargetCodex       TargetID = "codex"
	TargetAntigravity TargetID = "antigravity"
	TargetClaude      TargetID = "claude-code"
)

type CapabilityID string

const (
	CapabilityPortable      CapabilityID = "portable-content"
	CapabilityBoundedReview CapabilityID = "bounded-review"
)

type CapabilityState string

const (
	StateSupported      CapabilityState = "supported"
	StateUnsupported    CapabilityState = "unsupported"
	StateStagedInactive CapabilityState = "staged/inactive"
	StateUnknown        CapabilityState = "unknown"
)

type Tier uint8

const (
	TierDefaults Tier = iota
	TierLocal
	TierGlobal
	TierProject
	TierCLI
)

// ProfileScope is deliberately closed: profile persistence is either global or
// bound to an explicit, already persisted project identity.
type ProfileScope string

const (
	ProfileScopeGlobal  ProfileScope = "global"
	ProfileScopeProject ProfileScope = "project"
)

// ProjectID is opaque to the compiler. W3 creates and persists it; this pure
// package only validates its stable, safe representation.
type ProjectID string

type RequestedProfilePersistence struct {
	Scope     ProfileScope
	ProjectID ProjectID
	ProfileID string
}

type PackSnapshot struct{ ID, Version string }
type Profile struct {
	ID, DefaultModel string
	Roles            map[string]string
}

// Candidate.Model optionally supplies the synthetic default-role value in
// canonical input/profile provenance. An empty value falls back to that
// candidate profile's DefaultModel. In ResolvedModel.Winner and Shadowed it
// contains the source's effective model for the resolved role; named roles use
// that candidate profile's Roles mapping or DefaultModel.
type Candidate struct {
	Tier                       Tier
	ProfileID, Model, Location string
	SourceKey                  string
	Scope                      ProfileScope
	ProjectID                  ProjectID
}
type RoleOverride struct {
	Role, Model, Location string
	Tier                  Tier
	SourceKey             string
}
type CapabilityRequest struct {
	ID       CapabilityID
	Required bool
}
type CredentialRef struct{ Slot, Provider string }

type Input struct {
	Schema          uint16
	Compiler        string
	Pack            PackSnapshot
	Target          TargetID
	Profiles        []Profile
	Candidates      []Candidate
	RoleOverrides   []RoleOverride
	Requested       []CapabilityRequest
	Extensions      map[string]string
	CredentialSlots []CredentialRef
	ProjectID       ProjectID
	Persistence     RequestedProfilePersistence
}

type ExplainCode string

const (
	ExplainProfileWinner ExplainCode = "profile_winner"
	ExplainProfileShadow ExplainCode = "profile_shadow"
	ExplainRoleWinner    ExplainCode = "role_winner"
	ExplainRoleShadow    ExplainCode = "role_shadow"
	ExplainCapability    ExplainCode = "capability"
	ExplainScopeMismatch ExplainCode = "scope_mismatch"
	ExplainAdmission     ExplainCode = "admission"
)

type ExplainEvent struct {
	Code                       ExplainCode
	SourceKey, ProfileID, Role string
	Scope                      ProfileScope
	ProjectID                  ProjectID
	Capability                 CapabilityID
	ErrorCode                  ErrorCode
	Remediation                string
}

type CompileReport struct {
	Admitted     bool
	profile      ProfileResolution
	roles        []ResolvedModel
	capabilities []CapabilityDecision
	events       []ExplainEvent
	persistence  *ProfilePersistenceIntent
}

// ProfilePersistenceIntent is a copy-only declarative handoff to W3; it has
// no storage, generation, or mutation behavior.
type ProfilePersistenceIntent struct {
	scope     ProfileScope
	projectID ProjectID
	profileID string
}

func (i ProfilePersistenceIntent) Scope() ProfileScope  { return i.scope }
func (i ProfilePersistenceIntent) ProjectID() ProjectID { return i.projectID }
func (i ProfilePersistenceIntent) ProfileID() string    { return i.profileID }
func (r CompileReport) Events() []ExplainEvent          { return append([]ExplainEvent(nil), r.events...) }
func (r CompileReport) Profile() ProfileResolution      { return copyProfile(r.profile) }
func (r CompileReport) Roles() []ResolvedModel          { return copyRoles(r.roles) }
func (r CompileReport) Capabilities() []CapabilityDecision {
	return append([]CapabilityDecision(nil), r.capabilities...)
}
func (r CompileReport) ProfilePersistenceIntent() (ProfilePersistenceIntent, bool) {
	if r.persistence == nil {
		return ProfilePersistenceIntent{}, false
	}
	return *r.persistence, true
}

type ProfileResolution struct {
	Winner   Candidate
	Shadowed []Candidate
}
type ResolvedModel struct {
	Role, Model string
	Winner      Candidate
	Shadowed    []Candidate
}
type CapabilityDecision struct {
	ID               CapabilityID
	Target           TargetID
	State            CapabilityState
	Reason, Evidence string
	Required         bool
	RuntimeActive    bool
}
type ResolvedIR struct {
	Schema          uint16
	Compiler        string
	Target          TargetID
	inputID         string
	irID            string
	profile         ProfileResolution
	roles           []ResolvedModel
	capabilities    []CapabilityDecision
	Admitted        bool
	credentialSlots []CredentialRef
	persistence     *ProfilePersistenceIntent
	sealed          bool
}

func (ir ResolvedIR) CredentialSlots() []CredentialRef {
	return append([]CredentialRef(nil), ir.credentialSlots...)
}
func (ir ResolvedIR) InputID() string            { return ir.inputID }
func (ir ResolvedIR) IRID() string               { return ir.irID }
func (ir ResolvedIR) Profile() ProfileResolution { return copyProfile(ir.profile) }
func (ir ResolvedIR) Roles() []ResolvedModel     { return copyRoles(ir.roles) }
func (ir ResolvedIR) Capabilities() []CapabilityDecision {
	return append([]CapabilityDecision(nil), ir.capabilities...)
}
func (ir ResolvedIR) ProfilePersistenceIntent() (ProfilePersistenceIntent, bool) {
	if ir.persistence == nil {
		return ProfilePersistenceIntent{}, false
	}
	return *ir.persistence, true
}
func (ir ResolvedIR) Sealed() bool                   { return ir.sealed }
func (ir ResolvedIR) PostSealFinalizationOnly() bool { return ir.sealed }

func copyProfile(p ProfileResolution) ProfileResolution {
	p.Shadowed = append([]Candidate(nil), p.Shadowed...)
	return p
}
func copyRoles(roles []ResolvedModel) []ResolvedModel {
	result := append([]ResolvedModel(nil), roles...)
	for i := range result {
		result[i].Shadowed = append([]Candidate(nil), result[i].Shadowed...)
	}
	return result
}
