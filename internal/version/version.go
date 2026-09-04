package version

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/alferio94/lore-cli/internal/releaseprofile"
)

const (
	defaultVersion   = "dev"
	defaultCommit    = "none"
	defaultBuildDate = "unknown"
	unknownValue     = "unknown"
)

var (
	Version   = defaultVersion
	Commit    = defaultCommit
	BuildDate = defaultBuildDate

	safeIdentity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)
	safeDigest   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// ReleaseProfile is the safe, ordered diagnostic projection of the immutable
// process release profile. It deliberately cannot carry the embedded payload.
type ReleaseProfile struct {
	Schema           string       `json:"schema"`
	ID               string       `json:"id"`
	Version          uint64       `json:"version"`
	Release          string       `json:"release"`
	Channel          string       `json:"channel"`
	ArtifactSHA256   string       `json:"artifact_sha256"`
	ProvenanceStatus string       `json:"provenance_status"`
	Gates            ProfileGates `json:"gates"`
}

type ProfileGates struct {
	Pi          string `json:"pi"`
	OpenCode    string `json:"opencode"`
	Codex       string `json:"codex"`
	Antigravity string `json:"antigravity"`
}

// Info carries build metadata for the CLI.
type Info struct {
	Version        string         `json:"version"`
	Commit         string         `json:"commit"`
	BuildDate      string         `json:"buildDate"`
	ReleaseProfile ReleaseProfile `json:"release_profile"`
}

// Current returns normalized build metadata and observes the one immutable
// release profile snapshot resolved by releaseprofile at process startup.
func Current() Info {
	return Info{
		Version:        normalize(Version, defaultVersion),
		Commit:         normalize(Commit, defaultCommit),
		BuildDate:      normalize(BuildDate, defaultBuildDate),
		ReleaseProfile: ProfileFromSnapshot(releaseprofile.Current()),
	}
}

// ProfileFromSnapshot removes everything except approved identity, provenance,
// digest, and gate fields from an immutable snapshot.
func ProfileFromSnapshot(snapshot releaseprofile.Snapshot) ReleaseProfile {
	profile := snapshot.Profile()
	return ReleaseProfile{
		Schema: profile.Schema, ID: profile.ID, Version: profile.Version,
		Release: profile.Release.Version, Channel: profile.Release.Channel,
		ArtifactSHA256: profile.Release.ArtifactSHA256, ProvenanceStatus: snapshot.Status(),
		Gates: ProfileGates{Pi: string(profile.Gates.Pi), OpenCode: string(profile.Gates.OpenCode), Codex: string(profile.Gates.Codex), Antigravity: string(profile.Gates.Antigravity)},
	}.Normalized()
}

// String renders the default human-readable version diagnostics.
func (i Info) String() string {
	info := i.Normalized()
	return fmt.Sprintf("lore version %s commit=%s buildDate=%s\n%s", info.Version, info.Commit, info.BuildDate, info.ReleaseProfile.Summary())
}

// JSON renders normalized metadata for script consumption.
func (i Info) JSON() ([]byte, error) { return json.Marshal(i.Normalized()) }

// Normalized returns a copy with empty values replaced by safe defaults.
func (i Info) Normalized() Info {
	return Info{
		Version: normalize(i.Version, defaultVersion), Commit: normalize(i.Commit, defaultCommit),
		BuildDate: normalize(i.BuildDate, defaultBuildDate), ReleaseProfile: i.ReleaseProfile.Normalized(),
	}
}

// Normalized returns a projection that cannot print paths, credentials, user
// content, invalid gates, or an ambiguous partial digest.
func (p ReleaseProfile) Normalized() ReleaseProfile {
	if p == (ReleaseProfile{}) {
		p.ID, p.ProvenanceStatus = "default-off", releaseprofile.StatusDefaultOff
	}
	if p.Schema != releaseprofile.Schema {
		p.Schema = releaseprofile.Schema
	}
	p.ID = safeValue(p.ID, "default-off")
	p.Release = safeValue(p.Release, unknownValue)
	p.Channel = safeValue(p.Channel, unknownValue)
	if !safeDigest.MatchString(p.ArtifactSHA256) {
		p.ArtifactSHA256 = unknownValue
	}
	p.ProvenanceStatus = safeProvenance(p.ProvenanceStatus)
	p.Gates = ProfileGates{Pi: safeGate(p.Gates.Pi), OpenCode: safeGate(p.Gates.OpenCode), Codex: safeGate(p.Gates.Codex), Antigravity: safeGate(p.Gates.Antigravity)}
	return p
}

// Summary is shared by human CLI and TUI presenters.
func (p ReleaseProfile) Summary() string {
	p = p.Normalized()
	return fmt.Sprintf("release_profile schema=%s id=%s version=%d release=%s channel=%s\nartifact_sha256:\n%s\nprovenance=%s gates=pi:%s,opencode:%s,codex:%s,antigravity:%s", p.Schema, p.ID, p.Version, p.Release, p.Channel, p.ArtifactSHA256, p.ProvenanceStatus, p.Gates.Pi, p.Gates.OpenCode, p.Gates.Codex, p.Gates.Antigravity)
}

func normalize(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
func safeValue(value, fallback string) string {
	lower := strings.ToLower(value)
	if !safeIdentity.MatchString(value) || strings.Contains(lower, "authorization") || strings.Contains(lower, "bearer") || strings.Contains(lower, "credential") || strings.Contains(lower, "password") || strings.Contains(lower, "token") {
		return fallback
	}
	return value
}
func safeGate(gate string) string {
	switch gate {
	case string(releaseprofile.GateE), string(releaseprofile.GateD), string(releaseprofile.GateA):
		return gate
	default:
		return string(releaseprofile.GateOff)
	}
}
func safeProvenance(status string) string {
	switch status {
	case releaseprofile.StatusDefaultOff, releaseprofile.StatusValid, releaseprofile.StatusInvalid:
		return status
	default:
		return releaseprofile.StatusInvalid
	}
}
