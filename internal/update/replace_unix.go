//go:build !windows

package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/alferio94/lore-cli/internal/version"
)

func (s Service) applyUnix(ctx context.Context, plan Plan) (Result, error) {
	if plan.Asset.URL == "" || plan.ChecksumURL == "" {
		return Result{}, fmt.Errorf("update plan is incomplete")
	}
	targetPath := strings.TrimSpace(plan.Target.ResolvedPath)
	if targetPath == "" {
		return Result{}, fmt.Errorf("update target is missing")
	}

	archivePath, cleanup, err := s.downloadToTemp(ctx, plan.Asset.URL, plan.Asset.Name)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()

	checksumBody, err := s.downloadBytes(ctx, plan.ChecksumURL)
	if err != nil {
		return Result{}, err
	}
	want, err := checksumForAsset(string(checksumBody), plan.Asset.Name)
	if err != nil {
		return Result{}, err
	}
	got, err := fileSHA256(archivePath)
	if err != nil {
		return Result{}, err
	}
	if !strings.EqualFold(got, want) {
		return Result{}, fmt.Errorf("checksum mismatch for %s", plan.Asset.Name)
	}

	candidatePath, candidateMode, err := extractUnixBinary(archivePath)
	if err != nil {
		return Result{}, err
	}
	installed := version.Info{Version: plan.LatestTag}
	if s.CandidateVersion != nil {
		info, err := s.CandidateVersion(ctx, candidatePath)
		if err != nil {
			return Result{}, err
		}
		if info.Version != "" {
			installed = info.Normalized()
		}
		if err := verifyInstalledVersion(installed, plan.LatestTag); err != nil {
			return Result{}, fmt.Errorf("candidate validation failed: %w", err)
		}
	}

	backupPath, err := backupCurrentBinary(targetPath)
	if err != nil {
		return Result{}, err
	}
	restored := false
	defer func() {
		if restored {
			_ = os.Remove(backupPath)
		}
	}()

	if err := installCandidateBinary(targetPath, candidatePath, candidateMode); err != nil {
		if restoreErr := restoreBackup(targetPath, backupPath); restoreErr != nil {
			return Result{}, fmt.Errorf("replace failed: %v (rollback failed: %v)", err, restoreErr)
		}
		restored = true
		return Result{}, fmt.Errorf("replace failed: %w", err)
	}

	if s.CandidateVersion != nil {
		info, err := s.CandidateVersion(ctx, targetPath)
		if err != nil {
			if restoreErr := restoreBackup(targetPath, backupPath); restoreErr != nil {
				return Result{}, fmt.Errorf("post-replace validation failed: %v (rollback failed: %v)", err, restoreErr)
			}
			restored = true
			return Result{}, fmt.Errorf("post-replace validation failed: %w", err)
		}
		if info.Version != "" {
			installed = info.Normalized()
		}
		if err := verifyInstalledVersion(installed, plan.LatestTag); err != nil {
			if restoreErr := restoreBackup(targetPath, backupPath); restoreErr != nil {
				return Result{}, fmt.Errorf("post-replace validation failed: %v (rollback failed: %v)", err, restoreErr)
			}
			restored = true
			return Result{}, fmt.Errorf("post-replace validation failed: %w", err)
		}
	}

	return Result{Status: ResultStatusApplied, Installed: installed, BackupPath: backupPath}, nil
}

func (s Service) downloadToTemp(ctx context.Context, url, name string) (string, func(), error) {
	data, err := s.downloadBytes(ctx, url)
	if err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp("", "lore-update-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	path := filepath.Join(dir, filepath.Base(name))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		cleanup()
		return "", nil, err
	}
	return path, cleanup, nil
}

func extractUnixBinary(archivePath string) (string, os.FileMode, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return "", 0, err
	}
	defer gz.Close()

	tarReader := tar.NewReader(gz)
	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", 0, err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(hdr.Name) != unixArchiveBinaryName {
			continue
		}
		outPath := filepath.Join(filepath.Dir(archivePath), unixArchiveBinaryName)
		out, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&os.ModePerm)
		if err != nil {
			return "", 0, err
		}
		if _, err := io.Copy(out, tarReader); err != nil {
			out.Close()
			return "", 0, err
		}
		if err := out.Close(); err != nil {
			return "", 0, err
		}
		return outPath, os.FileMode(hdr.Mode) & os.ModePerm, nil
	}
	return "", 0, fmt.Errorf("archive did not contain %s", unixArchiveBinaryName)
}

func backupCurrentBinary(targetPath string) (string, error) {
	if _, err := os.Stat(targetPath); err != nil {
		return "", err
	}
	backupPath := targetPath + ".bak"
	_ = os.Remove(backupPath)
	if err := os.Rename(targetPath, backupPath); err != nil {
		return "", err
	}
	return backupPath, nil
}

func installCandidateBinary(targetPath, candidatePath string, mode os.FileMode) error {
	data, err := os.ReadFile(candidatePath)
	if err != nil {
		return err
	}
	tempPath := targetPath + ".tmp"
	if err := os.WriteFile(tempPath, data, mode&os.ModePerm); err != nil {
		return err
	}
	if err := os.Chmod(tempPath, mode&os.ModePerm); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Rename(tempPath, targetPath); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func restoreBackup(targetPath, backupPath string) error {
	_ = os.Remove(targetPath)
	return os.Rename(backupPath, targetPath)
}

func verifyInstalledVersion(info version.Info, wantTag string) error {
	got := info.Normalized()
	if strings.TrimSpace(wantTag) == "" {
		return nil
	}
	if got.Version != wantTag {
		return fmt.Errorf("reported version %q does not match expected tag %q", got.Version, wantTag)
	}
	return nil
}

func (s Service) downloadBytes(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func checksumForAsset(sums, assetName string) (string, error) {
	for _, line := range strings.Split(sums, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[len(fields)-1] == assetName {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("checksum entry for %s not found", assetName)
}

