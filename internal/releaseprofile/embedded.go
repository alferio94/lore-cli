package releaseprofile

import (
	"encoding/base64"
	"encoding/json"
)

var (
	embeddedPayloadBase64  string
	embeddedPayloadSHA256  string
	embeddedIdentityBase64 string
	processSnapshot        = resolveProcessSnapshot()
)

type embeddedIdentity struct {
	Release  ReleaseIdentity  `json:"release"`
	Rollback RollbackIdentity `json:"rollback"`
}

// Current returns the release profile resolved once while the package starts.
func Current() Snapshot { return processSnapshot }

func resolveProcessSnapshot() Snapshot {
	if embeddedPayloadBase64 == "" && embeddedPayloadSHA256 == "" && embeddedIdentityBase64 == "" {
		return Resolve(Embedded{}, ReleaseIdentity{}, RollbackIdentity{})
	}

	identityBytes, err := base64.StdEncoding.Strict().DecodeString(embeddedIdentityBase64)
	if err != nil {
		return invalidSnapshot()
	}
	var identity embeddedIdentity
	if err := json.Unmarshal(identityBytes, &identity); err != nil {
		return invalidSnapshot()
	}
	canonical, err := json.Marshal(identity)
	if err != nil || string(canonical) != string(identityBytes) {
		return invalidSnapshot()
	}
	return Resolve(
		Embedded{PayloadBase64: embeddedPayloadBase64, PayloadSHA256: embeddedPayloadSHA256},
		identity.Release,
		identity.Rollback,
	)
}
