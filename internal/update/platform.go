package update

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type TargetStatus string

type TargetReason string

const (
	TargetStatusOK     TargetStatus = "ok"
	TargetStatusUnsafe TargetStatus = "unsafe"

	ReasonNone            TargetReason = ""
	ReasonPathMismatch    TargetReason = "path_mismatch"
	ReasonSymlinkedTarget TargetReason = "symlinked_target"
)

type BinaryTarget struct {
	ExecutablePath string
	ResolvedPath   string
	PathPath       string
	GOOS           string
	GOARCH         string
	Status         TargetStatus
	Reason         TargetReason
}

type resolveTargetOptions struct {
	ExecPath string
	LookPath string
	GOOS     string
	GOARCH   string
}

func resolveBinaryTarget(opts resolveTargetOptions) (BinaryTarget, error) {
	execPath := filepath.Clean(strings.TrimSpace(opts.ExecPath))
	pathPath := filepath.Clean(strings.TrimSpace(opts.LookPath))
	resolved := execPath
	if info, err := os.Lstat(execPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if linkTarget, err := os.Readlink(execPath); err == nil {
			if !filepath.IsAbs(linkTarget) {
				linkTarget = filepath.Join(filepath.Dir(execPath), linkTarget)
			}
			resolved = filepath.Clean(linkTarget)
		}
	} else if realPath, err := filepath.EvalSymlinks(execPath); err == nil {
		resolved = filepath.Clean(realPath)
	}

	target := BinaryTarget{
		ExecutablePath: execPath,
		ResolvedPath:   resolved,
		PathPath:       pathPath,
		GOOS:           strings.TrimSpace(opts.GOOS),
		GOARCH:         strings.TrimSpace(opts.GOARCH),
		Status:         TargetStatusOK,
	}

	if pathPath != "" && execPath != pathPath {
		target.Status = TargetStatusUnsafe
		target.Reason = ReasonPathMismatch
		return target, nil
	}
	if resolved != execPath {
		target.Status = TargetStatusUnsafe
		target.Reason = ReasonSymlinkedTarget
	}
	return target, nil
}

func expectedAssetName(tag, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("lore-cli_%s_%s_%s%s", tag, goos, goarch, ext)
}

func compareVersions(current, latest string) UpdateStatus {
	current = strings.TrimSpace(current)
	latest = strings.TrimSpace(latest)
	if current == "dev" {
		return StatusDevBuild
	}
	cv, ok := parseSemver(current)
	if !ok {
		return StatusUnsupported
	}
	lv, ok := parseSemver(latest)
	if !ok {
		return StatusUnsupported
	}
	switch compareSemver(cv, lv) {
	case -1:
		return StatusAvailable
	default:
		return StatusUpToDate
	}
}

func parseSemver(v string) ([3]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var out [3]int
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

func compareSemver(a, b [3]int) int {
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}
