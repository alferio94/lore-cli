package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alferio94/lore-cli/internal/install"
	"github.com/alferio94/lore-cli/internal/version"
)

func TestCheckSelectsLatestReleaseAssetAndChecksum(t *testing.T) {
	t.Parallel()

	const (
		currentVersion = "v0.2.5"
		latestVersion  = "v0.2.6"
		assetName      = "lore-cli_v0.2.6_darwin_arm64.tar.gz"
	)

	serverURL := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/repos/alferio94/lore-cli/releases/latest"; got != want {
			t.Fatalf("release path = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("Accept"), "application/vnd.github+json"; got != want {
			t.Fatalf("Accept header = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("User-Agent"), "lore-cli/"+currentVersion; got != want {
			t.Fatalf("User-Agent = %q, want %q", got, want)
		}
		w.Header().Set("ETag", `"etag-v0.2.6"`)
		_, _ = fmt.Fprintf(w, `{"tag_name":"v0.2.6","assets":[{"name":%q,"browser_download_url":%q},{"name":"SHA256SUMS","browser_download_url":%q}]}`,
			assetName,
			serverURL+"/downloads/"+assetName,
			serverURL+"/downloads/SHA256SUMS",
		)
	}))
	serverURL = server.URL
	defer server.Close()

	cacheDir := t.TempDir()
	svc := Service{
		HTTP:          server.Client(),
		Now:           func() time.Time { return time.Date(2026, 5, 20, 22, 0, 0, 0, time.UTC) },
		ExecPath:      func() (string, error) { return "/usr/local/bin/lore", nil },
		LookPath:      func(string) (string, error) { return "/usr/local/bin/lore", nil },
		ConfigDir:     func() (string, error) { return cacheDir, nil },
		GitHubBaseURL: server.URL,
		GOOS:          "darwin",
		GOARCH:        "arm64",
		BuildInfo:     version.Info{Version: currentVersion},
	}

	plan, err := svc.Check(context.Background(), CheckOptions{})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if got, want := plan.Status, StatusAvailable; got != want {
		t.Fatalf("plan.Status = %q, want %q", got, want)
	}
	if got, want := plan.LatestTag, latestVersion; got != want {
		t.Fatalf("plan.LatestTag = %q, want %q", got, want)
	}
	if got, want := plan.Asset.Name, assetName; got != want {
		t.Fatalf("plan.Asset.Name = %q, want %q", got, want)
	}
	if got, want := filepath.Base(plan.ChecksumURL), "SHA256SUMS"; got != want {
		t.Fatalf("plan.ChecksumURL = %q, want basename %q", got, want)
	}
	if got, want := plan.CacheSource, CacheSourceNetwork; got != want {
		t.Fatalf("plan.CacheSource = %q, want %q", got, want)
	}
}

func TestCheckUsesCachedReleaseAfterETagNotModified(t *testing.T) {
	t.Parallel()

	const etag = `"etag-v0.2.6"`
	cacheDir := t.TempDir()
	if err := writeUpdateCache(filepath.Join(cacheDir, "update-check.json"), updateCache{
		ETag:        etag,
		CheckedAt:   time.Date(2026, 5, 20, 20, 0, 0, 0, time.UTC),
		LatestTag:   "v0.2.6",
		AssetName:   "lore-cli_v0.2.6_linux_amd64.tar.gz",
		AssetURL:    "https://example.test/downloads/lore-cli_v0.2.6_linux_amd64.tar.gz",
		ChecksumURL: "https://example.test/downloads/SHA256SUMS",
	}); err != nil {
		t.Fatalf("writeUpdateCache() error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("If-None-Match"), etag; got != want {
			t.Fatalf("If-None-Match = %q, want %q", got, want)
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	svc := Service{
		HTTP:          server.Client(),
		Now:           func() time.Time { return time.Date(2026, 5, 20, 22, 0, 0, 0, time.UTC) },
		ExecPath:      func() (string, error) { return "/usr/local/bin/lore", nil },
		LookPath:      func(string) (string, error) { return "/usr/local/bin/lore", nil },
		ConfigDir:     func() (string, error) { return cacheDir, nil },
		GitHubBaseURL: server.URL,
		GOOS:          "linux",
		GOARCH:        "amd64",
		BuildInfo:     version.Info{Version: "v0.2.5"},
	}

	plan, err := svc.Check(context.Background(), CheckOptions{})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if got, want := plan.CacheSource, CacheSourceNotModified; got != want {
		t.Fatalf("plan.CacheSource = %q, want %q", got, want)
	}
	if got, want := plan.Asset.Name, "lore-cli_v0.2.6_linux_amd64.tar.gz"; got != want {
		t.Fatalf("plan.Asset.Name = %q, want %q", got, want)
	}
	if got, want := plan.LatestTag, "v0.2.6"; got != want {
		t.Fatalf("plan.LatestTag = %q, want %q", got, want)
	}
}

func TestCompareVersionsUsesSemanticOrdering(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		current string
		latest  string
		want    UpdateStatus
	}{
		{name: "newer release available", current: "v0.2.5", latest: "v0.2.10", want: StatusAvailable},
		{name: "already current", current: "v0.2.10", latest: "v0.2.10", want: StatusUpToDate},
		{name: "dev build refuses automatic update", current: "dev", latest: "v0.2.10", want: StatusDevBuild},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := compareVersions(tc.current, tc.latest); got != tc.want {
				t.Fatalf("compareVersions(%q, %q) = %q, want %q", tc.current, tc.latest, got, tc.want)
			}
		})
	}
}

func TestApplyReplacesUnixBinaryWithBackupAndChecksumVerification(t *testing.T) {
	t.Parallel()

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "lore")
	if err := os.WriteFile(targetPath, []byte("current-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	piDir := filepath.Join(t.TempDir(), ".pi")
	if err := os.MkdirAll(piDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	piMarker := filepath.Join(piDir, "marker.txt")
	if err := os.WriteFile(piMarker, []byte("unchanged"), 0o644); err != nil {
		t.Fatalf("WriteFile(marker) error = %v", err)
	}

	assetName := "lore-cli_v0.2.6_linux_amd64.tar.gz"
	archiveBytes := mustUnixArchive(t, unixArchiveBinaryName, []byte("next-binary"), 0o750)
	checksum := sha256Hex(archiveBytes)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/downloads/" + assetName:
			_, _ = w.Write(archiveBytes)
		case "/downloads/SHA256SUMS":
			_, _ = w.Write([]byte(checksum + "  " + assetName + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	probed := []string{}
	svc := Service{
		HTTP:      server.Client(),
		Now:       func() time.Time { return time.Date(2026, 5, 20, 22, 0, 0, 0, time.UTC) },
		ExecPath:  func() (string, error) { return targetPath, nil },
		LookPath:  func(string) (string, error) { return targetPath, nil },
		ConfigDir: func() (string, error) { return filepath.Join(piDir, "config"), nil },
		CandidateVersion: func(_ context.Context, path string) (version.Info, error) {
			probed = append(probed, path)
			return version.Info{Version: "v0.2.6"}, nil
		},
	}

	plan := Plan{
		Status:      StatusAvailable,
		LatestTag:   "v0.2.6",
		Target:      BinaryTarget{ExecutablePath: targetPath, ResolvedPath: targetPath, PathPath: targetPath},
		Asset:       ReleaseAsset{Name: assetName, URL: server.URL + "/downloads/" + assetName},
		ChecksumURL: server.URL + "/downloads/SHA256SUMS",
	}

	result, err := svc.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if got, want := result.Status, ResultStatusApplied; got != want {
		t.Fatalf("result.Status = %q, want %q", got, want)
	}
	if got, want := result.Installed.Version, "v0.2.6"; got != want {
		t.Fatalf("result.Installed.Version = %q, want %q", got, want)
	}
	if result.BackupPath == "" {
		t.Fatal("result.BackupPath = empty, want persisted backup path")
	}
	if got, want := string(mustReadFile(t, targetPath)), "next-binary"; got != want {
		t.Fatalf("target bytes = %q, want %q", got, want)
	}
	if got, want := string(mustReadFile(t, result.BackupPath)), "current-binary"; got != want {
		t.Fatalf("backup bytes = %q, want %q", got, want)
	}
	if got, want := fileModePerm(t, targetPath), os.FileMode(0o750); got != want {
		t.Fatalf("target mode = %#o, want %#o", got, want)
	}
	if len(probed) != 2 {
		t.Fatalf("CandidateVersion calls = %d, want 2", len(probed))
	}
	if probed[0] == targetPath {
		t.Fatalf("first probe path = %q, want extracted candidate path", probed[0])
	}
	if got, want := probed[1], targetPath; got != want {
		t.Fatalf("second probe path = %q, want %q", got, want)
	}
	if got, want := string(mustReadFile(t, piMarker)), "unchanged"; got != want {
		t.Fatalf("pi marker bytes = %q, want %q", got, want)
	}
}

func TestApplyRollsBackUnixBinaryWhenPostReplaceProbeReportsWrongVersion(t *testing.T) {
	t.Parallel()

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "lore")
	if err := os.WriteFile(targetPath, []byte("current-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	assetName := "lore-cli_v0.2.6_linux_amd64.tar.gz"
	archiveBytes := mustUnixArchive(t, unixArchiveBinaryName, []byte("next-binary"), 0o755)
	checksum := sha256Hex(archiveBytes)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/downloads/" + assetName:
			_, _ = w.Write(archiveBytes)
		case "/downloads/SHA256SUMS":
			_, _ = w.Write([]byte(checksum + "  " + assetName + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	svc := Service{
		HTTP:      server.Client(),
		Now:       func() time.Time { return time.Date(2026, 5, 20, 22, 0, 0, 0, time.UTC) },
		ExecPath:  func() (string, error) { return targetPath, nil },
		LookPath:  func(string) (string, error) { return targetPath, nil },
		ConfigDir: func() (string, error) { return t.TempDir(), nil },
		CandidateVersion: func(_ context.Context, path string) (version.Info, error) {
			if path == targetPath {
				return version.Info{Version: "v9.9.9", Commit: "abc1234"}, nil
			}
			return version.Info{Version: "v0.2.6", Commit: "abc1234"}, nil
		},
	}

	plan := Plan{
		Status:      StatusAvailable,
		LatestTag:   "v0.2.6",
		Target:      BinaryTarget{ExecutablePath: targetPath, ResolvedPath: targetPath, PathPath: targetPath},
		Asset:       ReleaseAsset{Name: assetName, URL: server.URL + "/downloads/" + assetName},
		ChecksumURL: server.URL + "/downloads/SHA256SUMS",
	}

	_, err := svc.Apply(context.Background(), plan)
	if err == nil {
		t.Fatal("Apply() error = nil, want rollback-triggering version mismatch")
	}
	if !strings.Contains(err.Error(), "post-replace validation failed") || !strings.Contains(err.Error(), "expected tag") {
		t.Fatalf("Apply() error = %v, want post-replace version validation failure", err)
	}
	if got, want := string(mustReadFile(t, targetPath)), "current-binary"; got != want {
		t.Fatalf("target bytes after rollback = %q, want %q", got, want)
	}
	if _, err := os.Stat(targetPath + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("backup after rollback err = %v, want not exists", err)
	}
}

func TestApplyRollsBackUnixBinaryWhenPostReplaceProbeFails(t *testing.T) {
	t.Parallel()

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "lore")
	if err := os.WriteFile(targetPath, []byte("current-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	assetName := "lore-cli_v0.2.6_linux_amd64.tar.gz"
	archiveBytes := mustUnixArchive(t, unixArchiveBinaryName, []byte("next-binary"), 0o755)
	checksum := sha256Hex(archiveBytes)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/downloads/" + assetName:
			_, _ = w.Write(archiveBytes)
		case "/downloads/SHA256SUMS":
			_, _ = w.Write([]byte(checksum + "  " + assetName + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	svc := Service{
		HTTP:      server.Client(),
		Now:       func() time.Time { return time.Date(2026, 5, 20, 22, 0, 0, 0, time.UTC) },
		ExecPath:  func() (string, error) { return targetPath, nil },
		LookPath:  func(string) (string, error) { return targetPath, nil },
		ConfigDir: func() (string, error) { return t.TempDir(), nil },
		CandidateVersion: func(_ context.Context, path string) (version.Info, error) {
			if path == targetPath {
				return version.Info{}, fmt.Errorf("probe failed")
			}
			return version.Info{Version: "v0.2.6"}, nil
		},
	}

	plan := Plan{
		Status:      StatusAvailable,
		LatestTag:   "v0.2.6",
		Target:      BinaryTarget{ExecutablePath: targetPath, ResolvedPath: targetPath, PathPath: targetPath},
		Asset:       ReleaseAsset{Name: assetName, URL: server.URL + "/downloads/" + assetName},
		ChecksumURL: server.URL + "/downloads/SHA256SUMS",
	}

	_, err := svc.Apply(context.Background(), plan)
	if err == nil {
		t.Fatal("Apply() error = nil, want rollback-triggering probe failure")
	}
	if !strings.Contains(err.Error(), "post-replace validation failed") {
		t.Fatalf("Apply() error = %v, want post-replace validation failure", err)
	}
	if got, want := string(mustReadFile(t, targetPath)), "current-binary"; got != want {
		t.Fatalf("target bytes after rollback = %q, want %q", got, want)
	}
	if _, err := os.Stat(targetPath + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("backup after rollback err = %v, want not exists", err)
	}
}

func TestApplyFailsClosedOnChecksumMismatch(t *testing.T) {
	t.Parallel()

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "lore")
	if err := os.WriteFile(targetPath, []byte("current-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	assetName := "lore-cli_v0.2.6_linux_amd64.tar.gz"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/downloads/" + assetName:
			_, _ = w.Write([]byte("pretend archive bytes"))
		case "/downloads/SHA256SUMS":
			_, _ = w.Write([]byte("deadbeef  " + assetName + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	svc := Service{
		HTTP:      server.Client(),
		Now:       func() time.Time { return time.Date(2026, 5, 20, 22, 0, 0, 0, time.UTC) },
		ExecPath:  func() (string, error) { return targetPath, nil },
		LookPath:  func(string) (string, error) { return targetPath, nil },
		ConfigDir: func() (string, error) { return t.TempDir(), nil },
		CandidateVersion: func(context.Context, string) (version.Info, error) {
			return version.Info{Version: "v0.2.6"}, nil
		},
	}

	plan := Plan{
		Status:    StatusAvailable,
		LatestTag: "v0.2.6",
		Target: BinaryTarget{
			ExecutablePath: targetPath,
			ResolvedPath:   targetPath,
			PathPath:       targetPath,
		},
		Asset: ReleaseAsset{
			Name: "lore-cli_v0.2.6_linux_amd64.tar.gz",
			URL:  server.URL + "/downloads/" + assetName,
		},
		ChecksumURL: server.URL + "/downloads/SHA256SUMS",
	}

	_, err := svc.Apply(context.Background(), plan)
	if err == nil {
		t.Fatal("Apply() error = nil, want checksum mismatch refusal")
	}
	if got, want := string(mustReadFile(t, targetPath)), "current-binary"; got != want {
		t.Fatalf("target bytes = %q, want %q", got, want)
	}
}

func mustUnixArchive(t *testing.T, name string, data []byte, mode int64) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data))}); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar.Close() error = %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip.Close() error = %v", err)
	}
	return buf.Bytes()
}

func fileModePerm(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", path, err)
	}
	return info.Mode() & os.ModePerm
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return data
}

// TestUpdateServiceIgnoresExtendedSkills verifies the update service is binary-only
// and never accesses Pi or Antigravity skill directories. Both harnesses share the
// same update Service; the test creates skill files for both and confirms the
// Check() output contains no skill-related references and skill files are untouched.
func TestUpdateServiceIgnoresExtendedSkills(t *testing.T) {
	t.Parallel()

	// Set up Pi harness skill directories and a fake existing binary.
	piHome := t.TempDir()
	piLayout := resolvePiLayoutForTest(t, piHome)
	piSkillDir := filepath.Join(piLayout.AgentDir, "skills", "judgment-day")
	piSkillPath := filepath.Join(piSkillDir, "SKILL.md")
	if err := os.MkdirAll(piSkillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll Pi skill dir: %v", err)
	}
	if err := os.WriteFile(piSkillPath, []byte("# Pi skill content"), 0o600); err != nil {
		t.Fatalf("WriteFile Pi skill: %v", err)
	}

	// Set up Antigravity harness skill directories.
	agHome := t.TempDir()
	agLayout := resolveAntigravityLayoutForTest(t, agHome)
	agSkillDir := filepath.Join(agLayout.Paths["skills_dir"], "skill-creator")
	agSkillPath := filepath.Join(agSkillDir, "SKILL.md")
	if err := os.MkdirAll(agSkillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll Antigravity skill dir: %v", err)
	}
	if err := os.WriteFile(agSkillPath, []byte("# Antigravity skill content"), 0o600); err != nil {
		t.Fatalf("WriteFile Antigravity skill: %v", err)
	}

	// Write a fake existing binary so resolveTarget passes.
	cacheDir := t.TempDir()
	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "lore")
	if err := os.WriteFile(targetPath, []byte("current-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile target: %v", err)
	}

	// Capture base URL before server construction since httptest.Server.URL is not
	// available inside the handler's composite literal at construction time.
	var baseURL string
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := fmt.Sprintf(`{"tag_name":"v0.2.6","assets":[{"name":"lore-cli_v0.2.6_darwin_arm64.tar.gz","browser_download_url":"%s/lore-cli_v0.2.6_darwin_arm64.tar.gz"},{"name":"SHA256SUMS","browser_download_url":"%s/SHA256SUMS"}]}`, baseURL, baseURL)
		_, _ = fmt.Fprint(w, body)
	}))
	baseURL = mockSrv.URL // set after construction
	defer mockSrv.Close()

	svc := Service{
		HTTP:          mockSrv.Client(),
		Now:           func() time.Time { return time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC) },
		ExecPath:      func() (string, error) { return targetPath, nil },
		LookPath:      func(string) (string, error) { return targetPath, nil },
		ConfigDir:     func() (string, error) { return cacheDir, nil },
		GitHubBaseURL: baseURL,
		GOOS:          "darwin",
		GOARCH:        "arm64",
		BuildInfo:     version.Info{Version: "v0.2.5"},
	}

	// Check: update service must return a clean binary-only plan.
	plan, err := svc.Check(context.Background(), CheckOptions{})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if plan.Target.Status != TargetStatusOK {
		t.Fatalf("target.Status = %q (reason=%q), want ok; executable=%q resolved=%q path=%q", plan.Target.Status, plan.Target.Reason, plan.Target.ExecutablePath, plan.Target.ResolvedPath, plan.Target.PathPath)
	}
	if plan.Status != StatusAvailable {
		t.Fatalf("plan.Status = %q, want available", plan.Status)
	}

	// Assert: no skill directory paths appear in the plan.
	planStr := fmt.Sprintf("%+v", plan)
	if strings.Contains(planStr, "judgment-day") {
		t.Fatal("Plan contains Pi skill path 'judgment-day'; update should be binary-only")
	}
	if strings.Contains(planStr, "skill-creator") {
		t.Fatal("Plan contains Antigravity skill path 'skill-creator'; update should be binary-only")
	}
	if strings.Contains(planStr, "skill-registry") {
		t.Fatal("Plan contains skill path 'skill-registry'; update should be binary-only")
	}
	if strings.Contains(planStr, piLayout.AgentDir) {
		t.Fatal("Plan contains Pi agent directory; update should be binary-only")
	}
	if strings.Contains(planStr, agLayout.RootDir) {
		t.Fatal("Plan contains Antigravity root directory; update should be binary-only")
	}

	// Assert: Pi and Antigravity skill files are completely untouched on disk.
	if got := mustReadFileStr(t, piSkillPath); got != "# Pi skill content" {
		t.Fatalf("Pi skill file was modified: %q", got)
	}
	if got := mustReadFileStr(t, agSkillPath); got != "# Antigravity skill content" {
		t.Fatalf("Antigravity skill file was modified: %q", got)
	}
}

func mustReadFileStr(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	return string(data)
}

func resolvePiLayoutForTest(t *testing.T, homeDir string) install.PiLayout {
	t.Helper()
	agentDir := filepath.Join(homeDir, ".pi", "agent")
	return install.PiLayout{
		HomeDir:          homeDir,
		PiDir:            filepath.Join(homeDir, ".pi"),
		AgentDir:         agentDir,
		ExtensionsDir:     filepath.Join(agentDir, "extensions"),
		ManagedAgentsDir: filepath.Join(agentDir, "agents"),
		SettingsPath:     filepath.Join(agentDir, "settings.json"),
		ManifestPath:     filepath.Join(agentDir, "lore-install.json"),
	}
}

func resolveAntigravityLayoutForTest(t *testing.T, homeDir string) install.HarnessLayout {
	t.Helper()
	geminiDir := filepath.Join(homeDir, ".gemini")
	rootDir := filepath.Join(geminiDir, "antigravity-cli")
	return install.HarnessLayout{
		Target:      install.TargetAntigravity,
		RootDir:     rootDir,
		ManifestPath: filepath.Join(rootDir, "lore-install.json"),
		Paths: map[string]string{
			"skills_dir": filepath.Join(rootDir, "skills"),
			"gemini_dir": geminiDir,
		},
	}
}
