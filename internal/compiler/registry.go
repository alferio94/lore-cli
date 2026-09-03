package compiler

import "sort"

func knownCapabilityIDs() []CapabilityID {
	return []CapabilityID{CapabilityBoundedReview, CapabilityPortable}
}

func capabilityMatrix(target TargetID, requests []CapabilityRequest) []CapabilityDecision {
	required := make(map[CapabilityID]bool, len(requests))
	ids := make(map[CapabilityID]bool, len(knownCapabilityIDs())+len(requests))
	for _, id := range knownCapabilityIDs() {
		ids[id] = true
	}
	for _, request := range requests {
		ids[request.ID] = true
		required[request.ID] = required[request.ID] || request.Required
	}
	ordered := make([]CapabilityID, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	decisions := make([]CapabilityDecision, 0, len(ordered))
	for _, id := range ordered {
		decisions = append(decisions, resolveCapability(target, CapabilityRequest{ID: id, Required: required[id]}))
	}
	return decisions
}

func resolveCapability(target TargetID, request CapabilityRequest) CapabilityDecision {
	decision := CapabilityDecision{ID: request.ID, Target: target, Required: request.Required, Evidence: "compiler-registry/v1"}
	if !knownCapability(request.ID) {
		decision.State, decision.Reason = StateUnknown, "unknown capability; check spelling and compiler version"
		return decision
	}
	if target == TargetClaude {
		decision.State, decision.Reason = StateUnsupported, "Claude Code is unsupported and roadmap-only"
		return decision
	}
	switch request.ID {
	case CapabilityPortable:
		decision.State, decision.Reason, decision.RuntimeActive = StateSupported, "portable compiler content is supported", true
	case CapabilityBoundedReview:
		if target == TargetPi {
			decision.State, decision.Reason = StateStagedInactive, "Pi bounded review is staged and inactive"
		} else {
			decision.State, decision.Reason = StateUnsupported, "bounded review is currently Pi-only"
		}
	}
	return decision
}

func knownCapability(id CapabilityID) bool {
	for _, known := range knownCapabilityIDs() {
		if id == known {
			return true
		}
	}
	return false
}
