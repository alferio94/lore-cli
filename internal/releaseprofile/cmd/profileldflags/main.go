package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/alferio94/lore-cli/internal/releaseprofile"
)

const packagePath = "github.com/alferio94/lore-cli/internal/releaseprofile"

type identity struct {
	Release  releaseprofile.ReleaseIdentity  `json:"release"`
	Rollback releaseprofile.RollbackIdentity `json:"rollback"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: profileldflags PROFILE.json")
		os.Exit(2)
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fail(err)
	}
	profile, err := releaseprofile.ParseCanonical(data)
	if err != nil {
		fail(err)
	}
	embedded, err := releaseprofile.Encode(profile)
	if err != nil {
		fail(err)
	}
	identityJSON, err := json.Marshal(identity{profile.Release, profile.Rollback})
	if err != nil {
		fail(err)
	}
	fmt.Printf("-X=%s.embeddedPayloadBase64=%s -X=%s.embeddedPayloadSHA256=%s -X=%s.embeddedIdentityBase64=%s\n",
		packagePath, embedded.PayloadBase64,
		packagePath, embedded.PayloadSHA256,
		packagePath, base64.StdEncoding.EncodeToString(identityJSON),
	)
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "release profile: %v\n", err)
	os.Exit(1)
}
