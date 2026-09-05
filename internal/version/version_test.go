package version

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/releaseprofile"
)

const testDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestCurrentUsesDefaultsAndImmutableProfile(t *testing.T) {
	originalVersion, originalCommit, originalBuildDate := Version, Commit, BuildDate
	t.Cleanup(func() { Version, Commit, BuildDate = originalVersion, originalCommit, originalBuildDate })
	Version, Commit, BuildDate = "", "", ""

	got := Current()
	if got.Version != "dev" || got.Commit != "none" || got.BuildDate != "unknown" || got.ReleaseProfile.ProvenanceStatus != releaseprofile.StatusDefaultOff {
		t.Fatalf("Current() = %+v", got)
	}
	if !strings.Contains(got.String(), "id=default-off") || !strings.Contains(got.String(), "gates=pi:off,opencode:off,codex:off,antigravity:off") {
		t.Fatalf("String() = %q", got.String())
	}
}

func TestProfileSnapshotHumanJSONParityAndFullDigest(t *testing.T) {
	release := releaseprofile.ReleaseIdentity{Version: "v0.3.0-rc.1", Channel: "prerelease", ArtifactSHA256: testDigest}
	rollback := releaseprofile.RollbackIdentity{Version: "v0.2.0", ProfileID: "default-off"}
	profile := releaseprofile.Profile{Schema: releaseprofile.Schema, ID: "prerelease-opencode-e", Version: 1, Release: release, Rollback: rollback, Gates: releaseprofile.TargetGates{Pi: releaseprofile.GateOff, OpenCode: releaseprofile.GateE, Codex: releaseprofile.GateOff, Antigravity: releaseprofile.GateOff}}
	embedded, err := releaseprofile.Encode(profile)
	if err != nil {
		t.Fatal(err)
	}
	info := Info{Version: release.Version, Commit: "abc1234", BuildDate: "2026-05-17T12:34:56Z", ReleaseProfile: ProfileFromSnapshot(releaseprofile.Resolve(embedded, release, rollback))}.Normalized()
	for _, want := range []string{"id=prerelease-opencode-e", "version=1", "channel=prerelease", testDigest, "provenance=valid", "opencode:E"} {
		if !strings.Contains(info.String(), want) {
			t.Fatalf("human diagnostics missing %q: %s", want, info.String())
		}
	}
	payload, err := info.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var decoded Info
	if err := json.Unmarshal(payload, &decoded); err != nil || decoded != info {
		t.Fatalf("JSON parity error=%v decoded=%+v want=%+v", err, decoded, info)
	}
	if strings.Count(string(payload), testDigest) != 1 || !strings.Contains(string(payload), `"opencode":"E"`) {
		t.Fatalf("JSON digest/gate drift: %s", payload)
	}
}

func TestProfileDiagnosticsRedactUnsafeIdentityAndNeverCarryPayload(t *testing.T) {
	secret, opaque := "Bearer c2VjcmV0LXBheWxvYWQ=", "secret-token-value"
	profile := ReleaseProfile{Schema: "/Users/private/profile.json", ID: opaque, Release: "/Users/private/user-content", Channel: "Authorization:secret", ArtifactSHA256: "aaaa", ProvenanceStatus: secret, Gates: ProfileGates{Pi: "X-MCP-Header:" + secret}}
	first, second := profile.Summary(), profile.Summary()
	if first != second {
		t.Fatal("profile diagnostics are nondeterministic")
	}
	payload, err := (Info{ReleaseProfile: profile}).JSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{first, string(payload)} {
		for _, forbidden := range []string{secret, opaque, "/Users/private", "user-content", "c2VjcmV0LXBheWxvYWQ=", "Authorization:", "X-MCP-Header"} {
			if strings.Contains(output, forbidden) {
				t.Fatalf("profile diagnostics leaked %q: %s", forbidden, output)
			}
		}
	}
	if !strings.Contains(first, "artifact_sha256:\nunknown") || strings.Contains(first, "artifact_sha256:\naaaa") {
		t.Fatalf("digest was ambiguously truncated: %s", first)
	}
}
