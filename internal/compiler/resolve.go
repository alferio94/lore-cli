package compiler

import (
	"fmt"
	"sort"
	"strings"
)

func Compile(input Input) (ResolvedIR, error) {
	input = cloneInput(input)
	normalizeInput(&input)
	if input.Schema != SchemaV1 {
		return ResolvedIR{}, fmt.Errorf("schema %d unsupported; supported schema is %d", input.Schema, SchemaV1)
	}
	if strings.TrimSpace(input.Pack.ID) == "" || strings.TrimSpace(input.Pack.Version) == "" {
		return ResolvedIR{}, fmt.Errorf("pack.id and pack.version are required")
	}
	if input.Target == "" {
		return ResolvedIR{}, validationError(CodeTargetRequired, "target is required")
	}
	if !knownTarget(input.Target) {
		return ResolvedIR{}, validationError(CodeUnknownTarget, "target is not recognized; use a known target identifier")
	}
	if !validCompilerVersion(input.Compiler) {
		return ResolvedIR{}, validationError(CodeInvalidCompilerVersion, "compiler version is invalid; use an exact supported v1 version")
	}
	slots := sortedSlots(input.CredentialSlots)
	if len(slots) != len(input.CredentialSlots) {
		return ResolvedIR{}, validationError(CodeDuplicateCredentialRef, "credential provider and slot pair must be unique")
	}
	profiles := make(map[string]Profile, len(input.Profiles))
	for _, profile := range input.Profiles {
		if strings.TrimSpace(profile.ID) == "" || strings.TrimSpace(profile.DefaultModel) == "" {
			return ResolvedIR{}, fmt.Errorf("profile id and default model are required")
		}
		profiles[profile.ID] = profile
	}
	if err := validateCandidateUniverse(input.Candidates, profiles); err != nil {
		return ResolvedIR{}, err
	}
	candidates, _, err := scopedCandidates(input)
	if err != nil {
		return ResolvedIR{}, err
	}
	winner, shadowed, err := selectProfile(candidates, profiles)
	if err != nil {
		return ResolvedIR{}, err
	}
	winner = roleCandidate(winner, profiles[winner.ProfileID], "default")
	for i := range shadowed {
		shadowed[i] = roleCandidate(shadowed[i], profiles[shadowed[i].ProfileID], "default")
	}
	profile := profiles[winner.ProfileID]
	if err := validateRoleOverrides(profile, input.RoleOverrides); err != nil {
		return ResolvedIR{}, err
	}
	roles := resolveRoles(profile, profiles, winner, shadowed, input.RoleOverrides)
	if input.Target == TargetClaude {
		return ResolvedIR{}, validationError(CodeTargetRoadmapUnsupported, "target is known but roadmap-only")
	}
	if err := validatePersistence(input.Persistence, input.ProjectID, winner.ProfileID); err != nil {
		return ResolvedIR{}, err
	}
	capabilities := capabilityMatrix(input.Target, input.Requested)
	requested := make(map[CapabilityID]bool, len(input.Requested))
	for _, request := range input.Requested {
		requested[request.ID] = true
	}
	for _, decision := range capabilities {
		if !requested[decision.ID] {
			continue
		}
		switch decision.State {
		case StateUnsupported:
			return ResolvedIR{}, validationError(CodeCapabilityUnsupported, "requested capability is unsupported for target; select portable-content or a supported target")
		case StateUnknown:
			return ResolvedIR{}, validationError(CodeCapabilityUnknown, "requested capability is unknown; check spelling and compiler version")
		}
	}
	normalized := struct {
		Schema       uint16
		Compiler     string
		Pack         PackSnapshot
		Target       TargetID
		Profile      Candidate
		Roles        []ResolvedModel
		Capabilities []CapabilityDecision
		Extensions   map[string]string
		Slots        []CredentialRef
		ProjectID    ProjectID
		Persistence  RequestedProfilePersistence
	}{input.Schema, input.Compiler, input.Pack, input.Target, winner, roles, capabilities, input.Extensions, slots, input.ProjectID, input.Persistence}
	inputID, err := identity(inputDomain, normalized)
	if err != nil {
		return ResolvedIR{}, err
	}
	ir := ResolvedIR{Schema: SchemaV1, Compiler: input.Compiler, inputID: inputID, Target: input.Target, profile: ProfileResolution{Winner: winner, Shadowed: shadowed}, roles: roles, capabilities: capabilities, Admitted: true, credentialSlots: slots, sealed: true}
	if input.Persistence.ProfileID != "" {
		ir.persistence = &ProfilePersistenceIntent{scope: input.Persistence.Scope, projectID: input.Persistence.ProjectID, profileID: input.Persistence.ProfileID}
	}
	ir.irID, err = identity(irDomain, struct {
		InputID      string
		Target       TargetID
		Profile      ProfileResolution
		Roles        []ResolvedModel
		Capabilities []CapabilityDecision
		Persistence  *ProfilePersistenceIntent
	}{ir.inputID, ir.Target, ir.profile, ir.roles, ir.capabilities, ir.persistence})
	if err != nil {
		return ResolvedIR{}, err
	}
	return ir, nil
}

// Explain is a pure companion API to Compile. It preserves rejection evidence
// without ever returning an admitted IR on error.
func Explain(input Input) (CompileReport, error) {
	copy := cloneInput(input)
	normalizeInput(&copy)
	ir, err := Compile(input)
	if err == nil {
		_, mismatches, _ := scopedCandidates(copy)
		return admittedReport(ir, mismatches, copy.Persistence), nil
	}
	return partialRejectedReport(copy, err), err
}

func admittedReport(ir ResolvedIR, mismatches []ExplainEvent, requested RequestedProfilePersistence) CompileReport {
	report := CompileReport{Admitted: true, profile: ir.Profile(), roles: ir.Roles(), capabilities: ir.Capabilities(), events: append([]ExplainEvent(nil), mismatches...)}
	report.events = append(report.events, ExplainEvent{Code: ExplainProfileWinner, SourceKey: report.profile.Winner.SourceKey, ProfileID: report.profile.Winner.ProfileID, Scope: report.profile.Winner.Scope, ProjectID: report.profile.Winner.ProjectID})
	for _, candidate := range report.profile.Shadowed {
		report.events = append(report.events, ExplainEvent{Code: ExplainProfileShadow, SourceKey: candidate.SourceKey, ProfileID: candidate.ProfileID, Scope: candidate.Scope, ProjectID: candidate.ProjectID})
	}
	for _, role := range report.roles {
		report.events = append(report.events, ExplainEvent{Code: ExplainRoleWinner, Role: role.Role, SourceKey: role.Winner.SourceKey, ProfileID: role.Winner.ProfileID})
		for _, candidate := range role.Shadowed {
			report.events = append(report.events, ExplainEvent{Code: ExplainRoleShadow, Role: role.Role, SourceKey: candidate.SourceKey, ProfileID: candidate.ProfileID})
		}
	}
	for _, capability := range report.capabilities {
		report.events = append(report.events, ExplainEvent{Code: ExplainCapability, Capability: capability.ID, Remediation: capability.Reason})
	}
	if requested.ProfileID != "" {
		report.persistence = &ProfilePersistenceIntent{scope: requested.Scope, projectID: requested.ProjectID, profileID: requested.ProfileID}
	}
	report.events = append(report.events, ExplainEvent{Code: ExplainAdmission, Remediation: "admitted"})
	return report
}

func partialRejectedReport(input Input, err error) CompileReport {
	code, remediation := explainFailure(err)
	report := CompileReport{}
	// Earlier validation failures have no safe profile or capability context.
	if input.Schema != SchemaV1 || strings.TrimSpace(input.Pack.ID) == "" || strings.TrimSpace(input.Pack.Version) == "" || input.Target == "" || !knownTarget(input.Target) || !validCompilerVersion(input.Compiler) || len(sortedSlots(input.CredentialSlots)) != len(input.CredentialSlots) {
		report.events = []ExplainEvent{{Code: ExplainAdmission, ErrorCode: code, Remediation: remediation}}
		return report
	}
	profiles := make(map[string]Profile, len(input.Profiles))
	for _, profile := range input.Profiles {
		if strings.TrimSpace(profile.ID) == "" || strings.TrimSpace(profile.DefaultModel) == "" {
			report.events = []ExplainEvent{{Code: ExplainAdmission, ErrorCode: code, Remediation: remediation}}
			return report
		}
		profiles[profile.ID] = profile
	}
	if validateCandidateUniverse(input.Candidates, profiles) != nil {
		report.events = []ExplainEvent{{Code: ExplainAdmission, ErrorCode: code, Remediation: remediation}}
		return report
	}
	candidates, mismatches, scopeErr := scopedCandidates(input)
	if input.Target != TargetClaude {
		report.capabilities = capabilityMatrix(input.Target, input.Requested)
	}
	if scopeErr != nil {
		report.events = append(mismatches, capabilityEvents(report.capabilities)...)
		report.events = append(report.events, ExplainEvent{Code: ExplainAdmission, ErrorCode: code, Remediation: remediation})
		return report
	}
	winner, shadows, selectErr := selectProfile(candidates, profiles)
	if selectErr != nil {
		report.events = append(mismatches, capabilityEvents(report.capabilities)...)
		report.events = append(report.events, ExplainEvent{Code: ExplainAdmission, ErrorCode: code, Remediation: remediation})
		return report
	}
	winner = roleCandidate(winner, profiles[winner.ProfileID], "default")
	for i := range shadows {
		shadows[i] = roleCandidate(shadows[i], profiles[shadows[i].ProfileID], "default")
	}
	report.profile = ProfileResolution{Winner: winner, Shadowed: shadows}
	report.events = append(mismatches, profileEvents(report.profile)...)
	if validateRoleOverrides(profiles[winner.ProfileID], input.RoleOverrides) == nil {
		report.roles = resolveRoles(profiles[winner.ProfileID], profiles, winner, shadows, input.RoleOverrides)
		report.events = append(report.events, roleEvents(report.roles)...)
	}
	report.events = append(report.events, capabilityEvents(report.capabilities)...)
	if validatePersistence(input.Persistence, input.ProjectID, winner.ProfileID) == nil && input.Persistence.ProfileID != "" {
		report.persistence = &ProfilePersistenceIntent{scope: input.Persistence.Scope, projectID: input.Persistence.ProjectID, profileID: input.Persistence.ProfileID}
	}
	report.events = append(report.events, ExplainEvent{Code: ExplainAdmission, ErrorCode: code, Remediation: remediation})
	return report
}

func capabilityEvents(capabilities []CapabilityDecision) []ExplainEvent {
	events := make([]ExplainEvent, 0, len(capabilities))
	for _, capability := range capabilities {
		events = append(events, ExplainEvent{Code: ExplainCapability, Capability: capability.ID, Remediation: capability.Reason})
	}
	return events
}

func explainFailure(err error) (ErrorCode, string) {
	if validation, ok := err.(*ValidationError); ok {
		return validation.Code(), validation.Error()
	}
	return "", err.Error()
}

func profileEvents(profile ProfileResolution) []ExplainEvent {
	events := []ExplainEvent{{Code: ExplainProfileWinner, SourceKey: profile.Winner.SourceKey, ProfileID: profile.Winner.ProfileID, Scope: profile.Winner.Scope, ProjectID: profile.Winner.ProjectID}}
	for _, candidate := range profile.Shadowed {
		events = append(events, ExplainEvent{Code: ExplainProfileShadow, SourceKey: candidate.SourceKey, ProfileID: candidate.ProfileID, Scope: candidate.Scope, ProjectID: candidate.ProjectID})
	}
	return events
}

func roleEvents(roles []ResolvedModel) []ExplainEvent {
	events := make([]ExplainEvent, 0)
	for _, role := range roles {
		events = append(events, ExplainEvent{Code: ExplainRoleWinner, Role: role.Role, SourceKey: role.Winner.SourceKey, ProfileID: role.Winner.ProfileID})
		for _, candidate := range role.Shadowed {
			events = append(events, ExplainEvent{Code: ExplainRoleShadow, Role: role.Role, SourceKey: candidate.SourceKey, ProfileID: candidate.ProfileID})
		}
	}
	return events
}

func scopedCandidates(input Input) ([]Candidate, []ExplainEvent, error) {
	if input.ProjectID != "" && !validProjectID(input.ProjectID) {
		return nil, nil, validationError(CodeInvalidProjectID, "project id has invalid format; use project:<lowercase-ascii-token>")
	}
	matching := make([]Candidate, 0, len(input.Candidates))
	mismatches := make([]ExplainEvent, 0)
	for _, candidate := range input.Candidates {
		switch candidate.Scope {
		case "", ProfileScopeGlobal:
			if candidate.ProjectID != "" {
				return nil, nil, validationError(CodeInvalidProjectID, "global candidate must not include a project id")
			}
			matching = append(matching, candidate)
		case ProfileScopeProject:
			if !validProjectID(candidate.ProjectID) {
				return nil, nil, validationError(CodeInvalidProjectID, "project candidate requires a valid project id")
			}
			if candidate.ProjectID == input.ProjectID && input.ProjectID != "" {
				matching = append(matching, candidate)
			} else {
				mismatches = append(mismatches, ExplainEvent{Code: ExplainScopeMismatch, SourceKey: candidate.SourceKey, ProfileID: candidate.ProfileID, Scope: candidate.Scope, ProjectID: candidate.ProjectID, Remediation: "select a candidate for the active project"})
			}
		default:
			return nil, nil, validationError(CodeInvalidProfileScope, "profile scope must be global or project")
		}
	}
	if len(matching) == 0 && len(mismatches) > 0 {
		return nil, mismatches, validationError(CodeNoMatchingProfile, "no profile candidate matches the active project; select a global candidate or matching project")
	}
	return matching, mismatches, nil
}

func validProjectID(id ProjectID) bool {
	s := string(id)
	if !strings.HasPrefix(s, "project:") || len(s) <= len("project:") || len(s) > 71 {
		return false
	}
	for i, r := range s[len("project:"):] {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') || (i == 0 && r == '-') {
			return false
		}
	}
	return true
}

func validatePersistence(intent RequestedProfilePersistence, active ProjectID, winner string) error {
	if intent.ProfileID == "" && intent.Scope == "" && intent.ProjectID == "" {
		return nil
	}
	if intent.ProfileID == "" || intent.ProfileID != winner {
		return validationError(CodeInvalidPersistenceIntent, "persistence intent must select the resolved profile")
	}
	switch intent.Scope {
	case ProfileScopeGlobal:
		if intent.ProjectID != "" {
			return validationError(CodeInvalidPersistenceIntent, "global persistence intent must not include a project id")
		}
	case ProfileScopeProject:
		if !validProjectID(active) || intent.ProjectID != active {
			return validationError(CodeInvalidPersistenceIntent, "project persistence intent must match the active project id")
		}
	default:
		return validationError(CodeInvalidPersistenceIntent, "persistence intent scope must be global or project")
	}
	return nil
}

func normalizeInput(input *Input) {
	sort.Slice(input.Profiles, func(i, j int) bool { return input.Profiles[i].ID < input.Profiles[j].ID })
	sort.Slice(input.Candidates, func(i, j int) bool {
		if input.Candidates[i].Tier == input.Candidates[j].Tier {
			return input.Candidates[i].SourceKey < input.Candidates[j].SourceKey
		}
		return input.Candidates[i].Tier > input.Candidates[j].Tier
	})
	sort.Slice(input.RoleOverrides, func(i, j int) bool {
		if input.RoleOverrides[i].Tier == input.RoleOverrides[j].Tier {
			if input.RoleOverrides[i].Role == input.RoleOverrides[j].Role {
				return input.RoleOverrides[i].SourceKey < input.RoleOverrides[j].SourceKey
			}
			return input.RoleOverrides[i].Role < input.RoleOverrides[j].Role
		}
		return input.RoleOverrides[i].Tier > input.RoleOverrides[j].Tier
	})
	sort.Slice(input.Requested, func(i, j int) bool {
		if input.Requested[i].ID == input.Requested[j].ID {
			return !input.Requested[i].Required && input.Requested[j].Required
		}
		return input.Requested[i].ID < input.Requested[j].ID
	})
}

func knownTarget(target TargetID) bool {
	for _, known := range knownTargetIDs() {
		if target == known {
			return true
		}
	}
	return false
}

func knownTargetIDs() []TargetID {
	return []TargetID{TargetPi, TargetOpenCode, TargetCodex, TargetAntigravity, TargetClaude}
}

type normalizedView struct {
	candidates  []Candidate
	overrides   []RoleOverride
	requests    []CapabilityRequest
	credentials []CredentialRef
}

func normalizedSnapshot(input Input) normalizedView {
	input = cloneInput(input)
	normalizeInput(&input)
	return normalizedView{input.Candidates, input.RoleOverrides, input.Requested, sortedSlots(input.CredentialSlots)}
}
func sortedSlots(slots []CredentialRef) []CredentialRef {
	result := append([]CredentialRef(nil), slots...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Slot == result[j].Slot {
			return result[i].Provider < result[j].Provider
		}
		return result[i].Slot < result[j].Slot
	})
	for i := 1; i < len(result); i++ {
		if result[i] == result[i-1] {
			return nil
		}
	}
	return result
}

func validateRoleOverrides(profile Profile, overrides []RoleOverride) error {
	keys := map[string]bool{}
	for _, override := range overrides {
		if override.SourceKey == "" {
			return fmt.Errorf("override source key is required")
		}
		if keys[override.SourceKey] {
			return validationError(CodeDuplicateOverrideSource, "override source key must be globally unique")
		}
		keys[override.SourceKey] = true
		if strings.TrimSpace(override.Role) == "" || strings.TrimSpace(override.Model) == "" {
			return fmt.Errorf("role override role and model are required")
		}
		if override.Role != "default" {
			if _, ok := profile.Roles[override.Role]; !ok {
				return fmt.Errorf("unknown role %q in selected profile", override.Role)
			}
		}
	}
	return nil
}

func selectProfile(candidates []Candidate, profiles map[string]Profile) (Candidate, []Candidate, error) {
	if len(candidates) == 0 {
		return Candidate{}, nil, fmt.Errorf("profile candidate is required")
	}
	keys := map[string]bool{}
	for _, candidate := range candidates {
		if candidate.SourceKey == "" {
			return Candidate{}, nil, fmt.Errorf("candidate source key is required")
		}
		if keys[candidate.SourceKey] {
			return Candidate{}, nil, validationError(CodeDuplicateCandidateSource, "candidate source key must be globally unique")
		}
		keys[candidate.SourceKey] = true
		if _, ok := profiles[candidate.ProfileID]; !ok {
			return Candidate{}, nil, fmt.Errorf("profile %q is not defined", candidate.ProfileID)
		}
	}
	return candidates[0], candidates[1:], nil
}

func validateCandidateUniverse(candidates []Candidate, profiles map[string]Profile) error {
	keys := map[string]bool{}
	for _, candidate := range candidates {
		if candidate.SourceKey == "" {
			return fmt.Errorf("candidate source key is required")
		}
		if keys[candidate.SourceKey] {
			return validationError(CodeDuplicateCandidateSource, "candidate source key must be globally unique")
		}
		keys[candidate.SourceKey] = true
		if _, ok := profiles[candidate.ProfileID]; !ok {
			return fmt.Errorf("profile %q is not defined", candidate.ProfileID)
		}
	}
	return nil
}

func resolveRoles(profile Profile, profiles map[string]Profile, winner Candidate, shadowed []Candidate, overrides []RoleOverride) []ResolvedModel {
	roles := map[string]bool{"default": true}
	for role := range profile.Roles {
		roles[role] = true
	}
	candidates := append([]Candidate{winner}, shadowed...)
	result := make([]ResolvedModel, 0, len(roles))
	for role := range roles {
		profileValues := make([]Candidate, len(candidates))
		for i, candidate := range candidates {
			profileValues[i] = roleCandidate(candidate, profiles[candidate.ProfileID], role)
		}
		values := make([]roleValue, 0, len(profileValues)+len(overrides))
		for _, candidate := range profileValues {
			values = append(values, roleValue{candidate: candidate})
		}
		for _, candidate := range roleOverrideCandidates(overridesForRole(overrides, role)) {
			values = append(values, roleValue{candidate: candidate, override: true})
		}
		sort.Slice(values, func(i, j int) bool {
			if values[i].candidate.Tier == values[j].candidate.Tier {
				if values[i].candidate.SourceKey == values[j].candidate.SourceKey {
					return values[i].override && !values[j].override
				}
				return values[i].candidate.SourceKey < values[j].candidate.SourceKey
			}
			return values[i].candidate.Tier > values[j].candidate.Tier
		})
		resolved := ResolvedModel{Role: role, Model: values[0].candidate.Model, Winner: values[0].candidate, Shadowed: make([]Candidate, len(values)-1)}
		for i := range resolved.Shadowed {
			resolved.Shadowed[i] = values[i+1].candidate
		}
		result = append(result, resolved)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Role < result[j].Role })
	return result
}

type roleValue struct {
	candidate Candidate
	override  bool
}

func roleCandidate(candidate Candidate, profile Profile, role string) Candidate {
	if role == "default" && candidate.Model != "" {
		return candidate
	}
	candidate.Model = profile.DefaultModel
	if model, ok := profile.Roles[role]; ok {
		candidate.Model = model
	}
	return candidate
}

func overridesForRole(overrides []RoleOverride, role string) []RoleOverride {
	result := make([]RoleOverride, 0)
	for _, override := range overrides {
		if override.Role == role {
			result = append(result, override)
		}
	}
	return result
}

func roleOverrideCandidates(overrides []RoleOverride) []Candidate {
	result := make([]Candidate, len(overrides))
	for i, override := range overrides {
		result[i] = Candidate{Tier: override.Tier, Model: override.Model, Location: override.Location, SourceKey: override.SourceKey}
	}
	return result
}
