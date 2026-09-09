package install

import "github.com/alferio94/lore-cli/internal/releaseprofile"

// NewRoutePolicyFromReleaseProfile is the sole mapping from the immutable
// process release profile into install routing. Profile validation and fallback
// remain owned by releaseprofile; NewRoutePolicy remains the routing authority.
func NewRoutePolicyFromReleaseProfile(snapshot releaseprofile.Snapshot) RoutePolicy {
	gates := make(map[TargetID]Gate, len(SupportedTargets()))
	for _, target := range SupportedTargets() {
		gates[target] = Gate(snapshot.Gate(string(target)))
	}
	policy, err := NewRoutePolicy(gates)
	if err != nil {
		panic("release profile produced an invalid install route policy")
	}
	return policy
}
