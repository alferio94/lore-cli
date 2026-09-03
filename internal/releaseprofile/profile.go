package releaseprofile

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
)

const Schema = "lore.release-profile/v1"

type Gate string

const (
	GateOff Gate = "off"
	GateE   Gate = "E"
	GateD   Gate = "D"
	GateA   Gate = "A"
)

const (
	TargetPi          = "pi"
	TargetOpenCode    = "opencode"
	TargetCodex       = "codex"
	TargetAntigravity = "antigravity"
)

type ReleaseIdentity struct {
	Version        string `json:"version"`
	Channel        string `json:"channel"`
	ArtifactSHA256 string `json:"artifact_sha256"`
}
type RollbackIdentity struct {
	Version   string `json:"version"`
	ProfileID string `json:"profile_id"`
}
type TargetGates struct {
	Pi          Gate `json:"pi"`
	OpenCode    Gate `json:"opencode"`
	Codex       Gate `json:"codex"`
	Antigravity Gate `json:"antigravity"`
}
type Profile struct {
	Schema   string           `json:"schema"`
	ID       string           `json:"id"`
	Version  uint64           `json:"version"`
	Release  ReleaseIdentity  `json:"release"`
	Rollback RollbackIdentity `json:"rollback"`
	Gates    TargetGates      `json:"gates"`
}
type Embedded struct{ PayloadBase64, PayloadSHA256 string }

const (
	StatusDefaultOff = "default-off"
	StatusValid      = "valid"
	StatusInvalid    = "invalid"
)

type Snapshot struct {
	status  string
	profile Profile
}

func (s Snapshot) Status() string   { return s.status }
func (s Snapshot) Profile() Profile { return s.profile }
func (s Snapshot) Gate(target string) Gate {
	switch target {
	case TargetPi:
		return s.profile.Gates.Pi
	case TargetOpenCode:
		return s.profile.Gates.OpenCode
	case TargetCodex:
		return s.profile.Gates.Codex
	case TargetAntigravity:
		return s.profile.Gates.Antigravity
	default:
		return GateOff
	}
}

func Canonical(profile Profile) ([]byte, error) {
	if err := validate(profile); err != nil {
		return nil, err
	}
	return json.Marshal(profile)
}

func ParseCanonical(data []byte) (Profile, error) {
	var profile Profile
	if err := json.Unmarshal(data, &profile); err != nil {
		return Profile{}, err
	}
	canonical, err := Canonical(profile)
	if err != nil || !bytes.Equal(data, canonical) {
		return Profile{}, errors.New("invalid or non-canonical profile")
	}
	return profile, nil
}

func Encode(profile Profile) (Embedded, error) {
	canonical, err := Canonical(profile)
	if err != nil {
		return Embedded{}, err
	}
	digest := sha256.Sum256(canonical)
	return Embedded{base64.StdEncoding.EncodeToString(canonical), hex.EncodeToString(digest[:])}, nil
}

func Resolve(embedded Embedded, release ReleaseIdentity, rollback RollbackIdentity) Snapshot {
	if embedded == (Embedded{}) && release == (ReleaseIdentity{}) && rollback == (RollbackIdentity{}) {
		return Snapshot{StatusDefaultOff, allOffProfile()}
	}
	invalid := invalidSnapshot()
	payload, err := base64.StdEncoding.Strict().DecodeString(embedded.PayloadBase64)
	if err != nil {
		return invalid
	}
	digest := sha256.Sum256(payload)
	if embedded.PayloadSHA256 != hex.EncodeToString(digest[:]) {
		return invalid
	}
	profile, err := ParseCanonical(payload)
	if err != nil || profile.Release != release || profile.Rollback != rollback {
		return invalid
	}
	return Snapshot{StatusValid, profile}
}

func validate(profile Profile) error {
	if profile.Schema != Schema || profile.ID == "" || profile.Version == 0 {
		return errors.New("invalid profile identity")
	}
	if profile.Release.Version == "" || profile.Release.Channel == "" || !validSHA256(profile.Release.ArtifactSHA256) {
		return errors.New("invalid release identity")
	}
	if profile.Rollback.Version == "" || profile.Rollback.ProfileID == "" {
		return errors.New("invalid rollback identity")
	}
	for _, gate := range []Gate{profile.Gates.Pi, profile.Gates.OpenCode, profile.Gates.Codex, profile.Gates.Antigravity} {
		if gate != GateOff && gate != GateE && gate != GateD && gate != GateA {
			return errors.New("invalid gate")
		}
	}
	return nil
}
func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == hex.EncodeToString(decoded)
}
func invalidSnapshot() Snapshot { return Snapshot{StatusInvalid, allOffProfile()} }
func allOffProfile() Profile {
	return Profile{Schema: Schema, ID: "default-off", Gates: TargetGates{GateOff, GateOff, GateOff, GateOff}}
}
