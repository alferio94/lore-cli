package version

import (
	"encoding/json"
	"testing"
)

func TestCurrentUsesDefaultsForEmptyMetadata(t *testing.T) {
	originalVersion, originalCommit, originalBuildDate := Version, Commit, BuildDate
	t.Cleanup(func() {
		Version, Commit, BuildDate = originalVersion, originalCommit, originalBuildDate
	})

	Version, Commit, BuildDate = "", "", ""

	got := Current()
	want := Info{Version: "dev", Commit: "none", BuildDate: "unknown"}
	if got != want {
		t.Fatalf("Current() = %+v, want %+v", got, want)
	}
	if got.String() != "lore version dev commit=none buildDate=unknown" {
		t.Fatalf("String() = %q", got.String())
	}
}

func TestCurrentPreservesInjectedMetadataAndJSONFields(t *testing.T) {
	originalVersion, originalCommit, originalBuildDate := Version, Commit, BuildDate
	t.Cleanup(func() {
		Version, Commit, BuildDate = originalVersion, originalCommit, originalBuildDate
	})

	Version, Commit, BuildDate = "v1.2.3", "abc1234", "2026-05-17T12:34:56Z"

	got := Current()
	if got.String() != "lore version v1.2.3 commit=abc1234 buildDate=2026-05-17T12:34:56Z" {
		t.Fatalf("String() = %q", got.String())
	}

	payload, err := got.JSON()
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}

	var decoded map[string]string
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if decoded["version"] != "v1.2.3" || decoded["commit"] != "abc1234" || decoded["buildDate"] != "2026-05-17T12:34:56Z" {
		t.Fatalf("decoded JSON = %#v", decoded)
	}
	if _, ok := decoded["build_date"]; ok {
		t.Fatalf("unexpected snake_case field in JSON: %#v", decoded)
	}
}
