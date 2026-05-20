package update

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type updateCache struct {
	ETag        string    `json:"etag,omitempty"`
	CheckedAt   time.Time `json:"checked_at,omitempty"`
	LatestTag   string    `json:"latest_tag,omitempty"`
	AssetName   string    `json:"asset_name,omitempty"`
	AssetURL    string    `json:"asset_url,omitempty"`
	ChecksumURL string    `json:"checksum_url,omitempty"`
}

func (c updateCache) release() githubRelease {
	assets := make([]ReleaseAsset, 0, 2)
	if c.AssetName != "" && c.AssetURL != "" {
		assets = append(assets, ReleaseAsset{Name: c.AssetName, URL: c.AssetURL})
	}
	if c.ChecksumURL != "" {
		assets = append(assets, ReleaseAsset{Name: "SHA256SUMS", URL: c.ChecksumURL})
	}
	return githubRelease{TagName: c.LatestTag, Assets: assets}
}

func readUpdateCache(path string) (updateCache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return updateCache{}, err
	}
	var cache updateCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return updateCache{}, fmt.Errorf("decode update cache: %w", err)
	}
	return cache, nil
}

func writeUpdateCache(path string, cache updateCache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create update cache dir: %w", err)
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("encode update cache: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write update cache: %w", err)
	}
	return nil
}
