package releaseprofile

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func require(t *testing.T, ok bool, message string) {
	t.Helper()
	if !ok {
		t.Fatal(message)
	}
}
func mustProfile(t *testing.T, name string) Profile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require(t, err == nil, "read fixture")
	profile, err := ParseCanonical(data)
	require(t, err == nil, "parse fixture")
	return profile
}

func TestProfileCore(t *testing.T) {
	profile := mustProfile(t, "valid.json")
	embedded, err := Encode(profile)
	require(t, err == nil && embedded.PayloadBase64 != "" && len(embedded.PayloadSHA256) == 64, "invalid embedding")
	snapshot := Resolve(embedded, profile.Release, profile.Rollback)
	require(t, snapshot.Status() == StatusValid && snapshot.Gate(TargetOpenCode) == GateE, "valid profile rejected")
	copy := snapshot.Profile()
	copy.Gates.OpenCode = GateA
	require(t, snapshot.Gate(TargetOpenCode) == GateE, "snapshot aliases returned profile")

	rollback := mustProfile(t, "invalid-rollback.json")
	rollbackEmbedded, _ := Encode(rollback)
	mismatch := profile.Release
	mismatch.Version = "v0.3.0-rc.2"
	invalid := []Snapshot{
		Resolve(Embedded{}, profile.Release, profile.Rollback),
		Resolve(Embedded{"%%%", strings.Repeat("0", 64)}, profile.Release, profile.Rollback),
		Resolve(Embedded{embedded.PayloadBase64, strings.Repeat("0", 64)}, profile.Release, profile.Rollback),
		Resolve(embedded, mismatch, profile.Rollback),
		Resolve(rollbackEmbedded, profile.Release, profile.Rollback),
	}
	for _, got := range invalid {
		require(t, got.Status() == StatusInvalid && got.Gate(TargetOpenCode) == GateOff, "profile did not fail closed")
	}
	unknown, err := os.ReadFile(filepath.Join("testdata", "invalid-unknown.json"))
	require(t, err == nil, "read invalid fixture")
	_, err = ParseCanonical(unknown)
	require(t, err != nil, "unknown target accepted")
	profile.Gates.Pi = "future"
	_, err = Canonical(profile)
	require(t, err != nil, "unknown gate accepted")
}

func TestCanonicalDefaultOff(t *testing.T) {
	snapshot := Resolve(Embedded{}, ReleaseIdentity{}, RollbackIdentity{})
	require(t, snapshot.Status() == StatusDefaultOff, "wrong default status")
	for _, target := range []string{TargetPi, TargetOpenCode, TargetCodex, TargetAntigravity, "unknown"} {
		require(t, snapshot.Gate(target) == GateOff, "default gate enabled")
	}
	_, err := ParseCanonical([]byte(`{"schema":"lore.release-profile/v1","schema":"other"}`))
	require(t, err != nil, "duplicate accepted")
}

func TestPrereleaseProfileSnapshot(t *testing.T) {
	const (
		wantDigest = "bdd0198c303dc9ccede88bb2e285399e41890e8eed925354e74fa7e2b5082e35"
		wantBase64 = "eyJzY2hlbWEiOiJsb3JlLnJlbGVhc2UtcHJvZmlsZS92MSIsImlkIjoicHJlcmVsZWFzZS1vcGVuY29kZS1lIiwidmVyc2lvbiI6MSwicmVsZWFzZSI6eyJ2ZXJzaW9uIjoidjAuMy4wLXJjLjEiLCJjaGFubmVsIjoicHJlcmVsZWFzZSIsImFydGlmYWN0X3NoYTI1NiI6ImFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWEifSwicm9sbGJhY2siOnsidmVyc2lvbiI6InYwLjIuMCIsInByb2ZpbGVfaWQiOiJkZWZhdWx0LW9mZiJ9LCJnYXRlcyI6eyJwaSI6Im9mZiIsIm9wZW5jb2RlIjoiRSIsImNvZGV4Ijoib2ZmIiwiYW50aWdyYXZpdHkiOiJvZmYifX0="
	)
	source, err := os.ReadFile(filepath.Join("..", "..", ".github", "release-profiles", "prerelease-opencode-e.json"))
	require(t, err == nil, "read prerelease profile")
	golden, err := os.ReadFile(filepath.Join("testdata", "valid.json"))
	require(t, err == nil && bytes.Equal(source, golden), "release profile snapshot drift; run make release-profile-snapshots")
	profile, err := ParseCanonical(source)
	require(t, err == nil, "prerelease profile is not canonical")
	require(t, profile.ID == "prerelease-opencode-e" && profile.Release.Channel == "prerelease", "wrong prerelease identity")
	require(t, profile.Gates == (TargetGates{GateOff, GateE, GateOff, GateOff}), "wrong prerelease gate matrix")
	embedded, err := Encode(profile)
	require(t, err == nil && embedded.PayloadSHA256 == wantDigest && embedded.PayloadBase64 == wantBase64, "prerelease embedding identity drift")
	for _, forbidden := range []string{"authorization", "bearer", "password", "secret", "token", "header", "audience"} {
		require(t, !strings.Contains(strings.ToLower(string(source)), forbidden), "prerelease profile contains forbidden data")
	}
}
