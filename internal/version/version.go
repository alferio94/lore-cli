package version

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	defaultVersion   = "dev"
	defaultCommit    = "none"
	defaultBuildDate = "unknown"
)

var (
	Version   = defaultVersion
	Commit    = defaultCommit
	BuildDate = defaultBuildDate
)

// Info carries build metadata for the CLI.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`
}

// Current returns normalized build metadata from package variables.
func Current() Info {
	return Info{
		Version:   normalize(Version, defaultVersion),
		Commit:    normalize(Commit, defaultCommit),
		BuildDate: normalize(BuildDate, defaultBuildDate),
	}
}

// String renders the default human-readable version line.
func (i Info) String() string {
	info := i.Normalized()
	return fmt.Sprintf("lore version %s commit=%s buildDate=%s", info.Version, info.Commit, info.BuildDate)
}

// JSON renders normalized metadata for script consumption.
func (i Info) JSON() ([]byte, error) {
	return json.Marshal(i.Normalized())
}

// Normalized returns a copy with empty values replaced by defaults.
func (i Info) Normalized() Info {
	return Info{
		Version:   normalize(i.Version, defaultVersion),
		Commit:    normalize(i.Commit, defaultCommit),
		BuildDate: normalize(i.BuildDate, defaultBuildDate),
	}
}

func normalize(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
