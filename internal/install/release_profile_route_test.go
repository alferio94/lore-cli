package install

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/alferio94/lore-cli/internal/releaseprofile"
)

func TestReleaseProfileRouteMatrixAndExplicitLegacy(t *testing.T) {
	profile := releaseRouteProfile()
	embedded, err := releaseprofile.Encode(profile)
	if err != nil {
		t.Fatal(err)
	}
	policy := NewRoutePolicyFromReleaseProfile(releaseprofile.Resolve(embedded, profile.Release, profile.Rollback))
	for _, target := range SupportedTargets() {
		for _, mode := range []Mode{ModeExplain, ModeDryRun, ModeApply} {
			decision, routeErr := policy.Decide(Request{Target: target, Mode: mode})
			want := target == TargetOpenCode && mode == ModeExplain
			if decision.Admitted != want || decision.Route != RouteCanonical {
				t.Fatalf("%s/%s decision = %#v, want canonical admitted=%t", target, mode, decision, want)
			}
			if want && routeErr != nil || !want && !errors.Is(routeErr, CodeCanonicalRouteDisabled) {
				t.Fatalf("%s/%s error = %v", target, mode, routeErr)
			}
		}
		for _, mode := range []Mode{ModeLegacyDryRun, ModeLegacyApply} {
			decision, routeErr := policy.Decide(Request{Target: target, Mode: mode})
			if routeErr != nil || !decision.Admitted || decision.Route != RouteLegacy {
				t.Fatalf("explicit legacy %s/%s decision = %#v, error = %v", target, mode, decision, routeErr)
			}
		}
	}
}

func TestReleaseProfileRouteDefaultAndInvalidFailClosed(t *testing.T) {
	profile := releaseRouteProfile()
	embedded, err := releaseprofile.Encode(profile)
	if err != nil {
		t.Fatal(err)
	}
	badDigest := embedded
	badDigest.PayloadSHA256 = "0" + badDigest.PayloadSHA256[1:]
	unknownPayload := []byte(`{"schema":"lore.release-profile/v1","id":"unknown","version":1,"release":{"version":"v0.3.0-rc.1","channel":"prerelease","artifact_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"rollback":{"version":"v0.2.0","profile_id":"default-off"},"gates":{"pi":"off","opencode":"future","codex":"off","antigravity":"off"}}`)
	unknownDigest := sha256.Sum256(unknownPayload)
	rollbackMismatch := profile.Rollback
	rollbackMismatch.Version = "v0.1.0"

	cases := map[string]releaseprofile.Snapshot{
		"unprofiled":            releaseprofile.Resolve(releaseprofile.Embedded{}, releaseprofile.ReleaseIdentity{}, releaseprofile.RollbackIdentity{}),
		"missing":               releaseprofile.Resolve(releaseprofile.Embedded{}, profile.Release, profile.Rollback),
		"malformed":             releaseprofile.Resolve(releaseprofile.Embedded{PayloadBase64: "%%%", PayloadSHA256: embedded.PayloadSHA256}, profile.Release, profile.Rollback),
		"digest-invalid":        releaseprofile.Resolve(badDigest, profile.Release, profile.Rollback),
		"unknown-gate":          releaseprofile.Resolve(releaseprofile.Embedded{PayloadBase64: base64.StdEncoding.EncodeToString(unknownPayload), PayloadSHA256: hex.EncodeToString(unknownDigest[:])}, profile.Release, profile.Rollback),
		"rollback-incompatible": releaseprofile.Resolve(embedded, profile.Release, rollbackMismatch),
	}
	for name, snapshot := range cases {
		t.Run(name, func(t *testing.T) {
			policy := NewRoutePolicyFromReleaseProfile(snapshot)
			for _, target := range SupportedTargets() {
				for _, mode := range []Mode{ModeExplain, ModeDryRun, ModeApply} {
					decision, routeErr := policy.Decide(Request{Target: target, Mode: mode})
					if !errors.Is(routeErr, CodeCanonicalRouteDisabled) || decision.Admitted || decision.Route != RouteCanonical {
						t.Fatalf("%s/%s decision = %#v, error = %v", target, mode, decision, routeErr)
					}
				}
			}
		})
	}
}

func releaseRouteProfile() releaseprofile.Profile {
	return releaseprofile.Profile{
		Schema:  releaseprofile.Schema,
		ID:      "prerelease-opencode-e",
		Version: 1,
		Release: releaseprofile.ReleaseIdentity{Version: "v0.3.0-rc.1", Channel: "prerelease", ArtifactSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Rollback: releaseprofile.RollbackIdentity{
			Version:   "v0.2.0",
			ProfileID: "default-off",
		},
		Gates: releaseprofile.TargetGates{Pi: releaseprofile.GateOff, OpenCode: releaseprofile.GateE, Codex: releaseprofile.GateOff, Antigravity: releaseprofile.GateOff},
	}
}
