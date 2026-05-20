package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type githubRelease struct {
	TagName string         `json:"tag_name"`
	Assets  []ReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

func (r *githubRelease) UnmarshalJSON(data []byte) error {
	var raw struct {
		TagName string               `json:"tag_name"`
		Assets  []githubReleaseAsset `json:"assets"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.TagName = strings.TrimSpace(raw.TagName)
	r.Assets = make([]ReleaseAsset, 0, len(raw.Assets))
	for _, asset := range raw.Assets {
		r.Assets = append(r.Assets, ReleaseAsset{Name: strings.TrimSpace(asset.Name), URL: strings.TrimSpace(asset.URL)})
	}
	return nil
}

func (r githubRelease) findAsset(name string) (ReleaseAsset, bool) {
	for _, asset := range r.Assets {
		if asset.Name == name {
			return asset, true
		}
	}
	return ReleaseAsset{}, false
}

func (s Service) fetchLatestRelease(ctx context.Context, currentVersion string, cached updateCache) (githubRelease, CacheSource, error) {
	url := s.githubBaseURL() + "/repos/alferio94/lore-cli/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return githubRelease{}, "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "lore-cli/"+currentVersion)
	if etag := strings.TrimSpace(cached.ETag); etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := s.httpClient().Do(req)
	if err != nil {
		return githubRelease{}, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return cached.release(), CacheSourceNotModified, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return githubRelease{}, "", fmt.Errorf("github latest release: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return githubRelease{}, "", fmt.Errorf("decode github release: %w", err)
	}
	if release.TagName == "" {
		return githubRelease{}, "", fmt.Errorf("github release missing tag_name")
	}

	cache := updateCache{
		ETag:        strings.TrimSpace(resp.Header.Get("ETag")),
		CheckedAt:   s.now()(),
		LatestTag:   release.TagName,
		ChecksumURL: mustAssetURL(release, "SHA256SUMS"),
	}
	assetName := expectedAssetName(release.TagName, s.goos(), s.goarch())
	if asset, ok := release.findAsset(assetName); ok {
		cache.AssetName = asset.Name
		cache.AssetURL = asset.URL
	}
	cachePath, err := s.cachePath()
	if err == nil {
		_ = writeUpdateCache(cachePath, cache)
	}
	return release, CacheSourceNetwork, nil
}

func mustAssetURL(release githubRelease, name string) string {
	asset, ok := release.findAsset(name)
	if !ok {
		return ""
	}
	return asset.URL
}
