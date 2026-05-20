package update

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/alferio94/lore-cli/internal/version"
)

const defaultGitHubBaseURL = "https://api.github.com"

type UpdateStatus string

type CacheSource string

type ResultStatus string

const (
	StatusUpToDate    UpdateStatus = "up_to_date"
	StatusAvailable   UpdateStatus = "available"
	StatusUnsupported UpdateStatus = "unsupported"
	StatusDevBuild    UpdateStatus = "dev_build"

	CacheSourceNetwork     CacheSource = "network"
	CacheSourceCache       CacheSource = "cache"
	CacheSourceNotModified CacheSource = "304"

	ResultStatusUnsupported ResultStatus = "unsupported"
	ResultStatusApplied     ResultStatus = "applied"
)

type Service struct {
	HTTP             *http.Client
	Now              func() time.Time
	ExecPath         func() (string, error)
	LookPath         func(string) (string, error)
	ConfigDir        func() (string, error)
	CandidateVersion func(context.Context, string) (version.Info, error)
	GitHubBaseURL    string
	GOOS             string
	GOARCH           string
	BuildInfo        version.Info
}

type CheckOptions struct{}

type Plan struct {
	Current     version.Info
	LatestTag   string
	Status      UpdateStatus
	Target      BinaryTarget
	Asset       ReleaseAsset
	ChecksumURL string
	CacheSource CacheSource
}

type Result struct {
	Status         ResultStatus
	Installed      version.Info
	BackupPath     string
	ManualRecovery string
}

type ReleaseAsset struct {
	Name string
	URL  string
}

func (s Service) Check(ctx context.Context, opts CheckOptions) (Plan, error) {
	_ = opts
	build := s.buildInfo()
	target, err := s.resolveTarget()
	if err != nil {
		return Plan{}, err
	}

	cachePath, err := s.cachePath()
	if err != nil {
		return Plan{}, err
	}
	cached, _ := readUpdateCache(cachePath)

	release, source, err := s.fetchLatestRelease(ctx, build.Version, cached)
	if err != nil {
		return Plan{}, err
	}

	assetName := expectedAssetName(release.TagName, s.goos(), s.goarch())
	asset, ok := release.findAsset(assetName)
	if !ok {
		return Plan{}, fmt.Errorf("update asset %q not found", assetName)
	}
	checksum, ok := release.findAsset("SHA256SUMS")
	if !ok {
		return Plan{}, fmt.Errorf("release checksum asset not found")
	}

	plan := Plan{
		Current:     build,
		LatestTag:   release.TagName,
		Status:      compareVersions(build.Version, release.TagName),
		Target:      target,
		Asset:       asset,
		ChecksumURL: checksum.URL,
		CacheSource: source,
	}
	if target.Status != TargetStatusOK {
		plan.Status = StatusUnsupported
	}
	return plan, nil
}

func (s Service) Apply(ctx context.Context, plan Plan) (Result, error) {
	if plan.Target.GOOS == "windows" || s.goos() == "windows" {
		return applyWindows(plan), nil
	}
	return s.applyUnix(ctx, plan)
}

func (s Service) resolveTarget() (BinaryTarget, error) {
	execPath, err := s.execPath()()
	if err != nil {
		return BinaryTarget{}, err
	}
	lookPath, err := s.lookPath("lore")
	if err != nil {
		lookPath = execPath
	}
	return resolveBinaryTarget(resolveTargetOptions{
		ExecPath: execPath,
		LookPath: lookPath,
		GOOS:     s.goos(),
		GOARCH:   s.goarch(),
	})
}

func (s Service) cachePath() (string, error) {
	dir, err := s.configDir()()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "update-check.json"), nil
}

func (s Service) buildInfo() version.Info {
	return s.BuildInfo.Normalized()
}

func (s Service) httpClient() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 5 * time.Second}
}

func (s Service) now() func() time.Time {
	if s.Now != nil {
		return s.Now
	}
	return time.Now
}

func (s Service) execPath() func() (string, error) {
	if s.ExecPath != nil {
		return s.ExecPath
	}
	return func() (string, error) { return "", fmt.Errorf("executable path is not configured") }
}

func (s Service) lookPath(name string) (string, error) {
	if s.LookPath != nil {
		return s.LookPath(name)
	}
	return "", fmt.Errorf("look path is not configured")
}

func (s Service) configDir() func() (string, error) {
	if s.ConfigDir != nil {
		return s.ConfigDir
	}
	return func() (string, error) { return "", fmt.Errorf("config dir is not configured") }
}

func (s Service) githubBaseURL() string {
	if strings.TrimSpace(s.GitHubBaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(s.GitHubBaseURL), "/")
	}
	return defaultGitHubBaseURL
}

func (s Service) goos() string {
	if strings.TrimSpace(s.GOOS) != "" {
		return s.GOOS
	}
	return runtime.GOOS
}

func (s Service) goarch() string {
	if strings.TrimSpace(s.GOARCH) != "" {
		return s.GOARCH
	}
	return runtime.GOARCH
}
