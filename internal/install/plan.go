package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ExistingPiState struct {
	Exists  bool
	Path    string
	Kind    string
	Mode    fs.FileMode
	Size    int64
	ModTime time.Time
}

type FullPiBackupPlan struct {
	SourcePath   string
	BackupPath   string
	ManifestPath string
}

type FullPiBackupResult struct {
	SourcePath     string `json:"source_path"`
	BackupPath     string `json:"backup_path"`
	ManifestPath   string `json:"manifest_path"`
	CreatedAt      string `json:"created_at"`
	SourceSnapshot string `json:"source_snapshot"`
	EntriesCopied  int    `json:"entries_copied"`
	FilesCopied    int    `json:"files_copied"`
	DirsCopied     int    `json:"dirs_copied"`
	SymlinksCopied int    `json:"symlinks_copied"`
}

type PiInstallPlan struct {
	Request               PiInstallRequest
	Layout                PiLayout
	ExistingPi            ExistingPiState
	ManagedBackupRoot     string
	ManagedFileActions    []ManagedFileAction
	ManagedAgentConflicts []string
	FullBackup            *FullPiBackupPlan
	Snapshot              string
}

type InstallCommandOptions struct {
	DryRun    bool
	AssumeYes bool
}

func (s Service) PlanPiInstall(req PiInstallRequest) (PiInstallPlan, error) {
	if strings.TrimSpace(req.HomeDir) == "" {
		return PiInstallPlan{}, fmt.Errorf("home dir is required")
	}
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	}
	components, err := req.normalizedComponents()
	if err != nil {
		return PiInstallPlan{}, err
	}
	req.Target = req.targetOrDefault()
	req.Components = components
	req.Definition = req.definitionOrDefault()
	layout := ResolvePiLayout(req.HomeDir)
	managedBackupRoot := filepath.Join(layout.AgentDir, "backups", req.Now.UTC().Format("20060102T150405Z"))
	plan := PiInstallPlan{Request: req, Layout: layout, ManagedBackupRoot: managedBackupRoot}

	info, err := os.Lstat(layout.PiDir)
	if err == nil {
		kind := piEntryKind(info.Mode())
		plan.ExistingPi = ExistingPiState{Exists: true, Path: layout.PiDir, Kind: kind, Mode: info.Mode(), Size: info.Size(), ModTime: info.ModTime().UTC()}
		if kind != "directory" {
			return PiInstallPlan{}, fmt.Errorf("refusing to install: ~/.pi at %s is %s; move it aside or replace it with a real directory, then rerun lore install", layout.PiDir, kind)
		}
		configDir := strings.TrimSpace(req.LoreConfigDir)
		if configDir == "" {
			configDir = filepath.Join(req.HomeDir, ".lore")
		}
		backupPath := filepath.Join(configDir, "backups", "pi", req.Now.UTC().Format("20060102T150405Z"))
		plan.FullBackup = &FullPiBackupPlan{SourcePath: layout.PiDir, BackupPath: backupPath, ManifestPath: filepath.Join(backupPath, "lore-pi-backup.json")}
	} else if !os.IsNotExist(err) {
		return PiInstallPlan{}, fmt.Errorf("inspect existing ~/.pi: %w", err)
	}

	rendered, err := renderPiFiles(layout, req)
	if err != nil {
		return PiInstallPlan{}, err
	}
	if err := validateRenderedPiFiles(rendered); err != nil {
		return PiInstallPlan{}, err
	}
	actions, err := planManagedFileActions(rendered, managedBackupRoot)
	if err != nil {
		return PiInstallPlan{}, err
	}
	overlayActions, overlayConflicts, err := planManagedAgentOverlayActions(layout, req, managedBackupRoot)
	if err != nil {
		return PiInstallPlan{}, err
	}
	actions = append(actions, overlayActions...)
	plan.ManagedAgentConflicts = append(plan.ManagedAgentConflicts, overlayConflicts...)
	cleanupAction, err := planLegacyDelegationCleanup(layout, managedBackupRoot)
	if err != nil {
		return PiInstallPlan{}, err
	}
	if cleanupAction != nil {
		actions = append(actions, *cleanupAction)
	}
	plan.ManagedFileActions = actions

	snapshot, err := snapshotTree(layout.PiDir)
	if err != nil {
		return PiInstallPlan{}, err
	}
	plan.Snapshot = snapshot
	return plan, nil
}

func (s Service) ExecutePiInstall(plan PiInstallPlan, opts InstallCommandOptions) (PiInstallResult, error) {
	currentSnapshot, err := snapshotTree(plan.Layout.PiDir)
	if err != nil {
		return PiInstallResult{}, err
	}
	if currentSnapshot != plan.Snapshot {
		return PiInstallResult{}, fmt.Errorf("install plan drift detected for %s", plan.Layout.PiDir)
	}
	if opts.DryRun {
		return PiInstallResult{Layout: plan.Layout}, nil
	}
	var fullBackup *FullPiBackupResult
	if plan.ExistingPi.Exists && plan.FullBackup != nil {
		report, err := createFullPiBackup(*plan.FullBackup, plan.Snapshot, plan.Request.Now)
		if err != nil {
			return PiInstallResult{}, err
		}
		fullBackup = &report
	}
	result, err := s.InstallPi(plan.Request)
	if err != nil {
		return PiInstallResult{}, err
	}
	if err := validateInstallResultAgainstPlan(plan, result); err != nil {
		return PiInstallResult{}, err
	}
	result.FullBackup = fullBackup
	result.Manifest.FullPiBackup = fullBackup
	if fullBackup != nil {
		manifestBytes, err := os.ReadFile(result.Layout.ManifestPath)
		if err != nil {
			return PiInstallResult{}, fmt.Errorf("read manifest for full backup update: %w", err)
		}
		loadedManifest, err := LoadManifest(result.Layout.ManifestPath)
		if err != nil {
			return PiInstallResult{}, err
		}
		loadedManifest.FullPiBackup = fullBackup
		if err := loadedManifest.Validate(result.Layout); err != nil {
			return PiInstallResult{}, fmt.Errorf("validate manifest full backup metadata: %w", err)
		}
		updatedBytes, err := marshalManifest(loadedManifest)
		if err != nil {
			return PiInstallResult{}, err
		}
		if string(manifestBytes) != string(updatedBytes) {
			if err := writeFileAtomic(result.Layout.ManifestPath, updatedBytes, 0o600); err != nil {
				return PiInstallResult{}, fmt.Errorf("write manifest full backup metadata: %w", err)
			}
		}
		result.Manifest = loadedManifest
	}
	return result, nil
}

func planManagedAgentOverlayActions(layout PiLayout, req PiInstallRequest, backupRoot string) ([]ManagedFileAction, []string, error) {
	existing, err := LoadManifest(layout.ManifestPath)
	if err != nil {
		if !strings.Contains(err.Error(), "read manifest") {
			return nil, nil, err
		}
		existing = Manifest{}
	}
	renderRequest, err := req.renderRequest()
	if err != nil {
		return nil, nil, err
	}
	renderRequest.SettingsPath = layout.SettingsPath
	registry, err := NewRegistry(defaultPiAdapter())
	if err != nil {
		return nil, nil, err
	}
	adapter, err := registry.Resolve(renderRequest.Target)
	if err != nil {
		return nil, nil, err
	}
	rendered, err := adapter.RenderManagedAgents(context.Background(), renderRequest)
	if err != nil {
		return nil, nil, err
	}
	managedPaths := make(map[string]struct{}, len(existing.ManagedAgentOverlays))
	for _, overlay := range existing.ManagedAgentOverlays {
		managedPaths[filepath.Clean(overlay.Path)] = struct{}{}
	}
	actions := make([]ManagedFileAction, 0, len(rendered)+len(existing.ManagedAgentOverlays))
	conflicts := make([]string, 0)
	renderedPaths := make(map[string]struct{}, len(rendered))
	for _, file := range rendered {
		absolutePath := absolutePiPath(layout, file.RelativePath)
		renderedPaths[filepath.Clean(absolutePath)] = struct{}{}
		if _, err := os.ReadFile(absolutePath); err == nil {
			if _, managed := managedPaths[filepath.Clean(absolutePath)]; !managed {
				conflicts = append(conflicts, file.RelativePath)
				continue
			}
		} else if !os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("read managed overlay candidate %s: %w", file.RelativePath, err)
		}
		_, action, err := planRenderedFileAction(renderedPiFile{component: file.Component, relativePath: file.RelativePath, absolutePath: absolutePath, content: file.Content, mergeMode: file.MergeMode}, backupRoot)
		if err != nil {
			return nil, nil, err
		}
		actions = append(actions, action)
	}
	for _, overlay := range existing.ManagedAgentOverlays {
		cleanPath := filepath.Clean(overlay.Path)
		if _, keep := renderedPaths[cleanPath]; keep {
			continue
		}
		relativePath, err := filepath.Rel(layout.AgentDir, overlay.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve managed overlay cleanup path %s: %w", overlay.Path, err)
		}
		actions = append(actions, ManagedFileAction{
			RelativePath: filepath.ToSlash(relativePath),
			AbsolutePath: overlay.Path,
			Action:       "delete",
			BackupPath:   filepath.Join(backupRoot, filepath.ToSlash(relativePath)),
		})
	}
	return actions, conflicts, nil
}

func validateInstallResultAgainstPlan(plan PiInstallPlan, result PiInstallResult) error {
	planned := map[string]string{}
	for _, action := range plan.ManagedFileActions {
		planned[action.RelativePath] = action.Action
	}
	actual := map[string]string{}
	for _, path := range result.Summary.Created {
		actual[path] = "create"
	}
	for _, path := range result.Summary.Updated {
		actual[path] = "update"
	}
	for _, path := range result.Summary.Deleted {
		actual[path] = "delete"
	}
	for _, path := range result.Summary.Unchanged {
		actual[path] = "unchanged"
	}
	if len(planned) != len(actual) {
		return fmt.Errorf("install plan action drift detected: planned=%d actual=%d", len(planned), len(actual))
	}
	for path, want := range planned {
		if got := actual[path]; got != want {
			return fmt.Errorf("install plan action drift detected for %s: planned=%s actual=%s", path, want, got)
		}
	}
	return nil
}

func piEntryKind(mode fs.FileMode) string {
	switch {
	case mode.IsDir():
		return "directory"
	case mode&os.ModeSymlink != 0:
		return "symlink"
	case mode.IsRegular():
		return "file"
	default:
		return "unexpected type"
	}
}

func snapshotTree(root string) (string, error) {
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return "missing", nil
	}
	if err != nil {
		return "", fmt.Errorf("snapshot %s: %w", root, err)
	}
	entries := []string{fmt.Sprintf(".:%s:%d:%d", info.Mode().String(), info.Size(), info.ModTime().UTC().UnixNano())}
	if !info.Mode().IsDir() {
		return strings.Join(entries, "\n"), nil
	}
	dirEntries, err := os.ReadDir(root)
	if err != nil {
		return "", fmt.Errorf("snapshot %s: %w", root, err)
	}
	for _, entry := range dirEntries {
		path := filepath.Join(root, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return "", fmt.Errorf("snapshot %s: %w", root, err)
		}
		line := fmt.Sprintf("%s:%s:%d:%d", filepath.ToSlash(entry.Name()), info.Mode().String(), info.Size(), info.ModTime().UTC().UnixNano())
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return "", fmt.Errorf("snapshot %s: %w", root, err)
			}
			line += ":" + target
		}
		entries = append(entries, line)
	}
	sort.Strings(entries)
	sum := sha256.Sum256([]byte(strings.Join(entries, "\n")))
	return hex.EncodeToString(sum[:]), nil
}

type treeCopyReport struct {
	EntriesCopied  int
	FilesCopied    int
	DirsCopied     int
	SymlinksCopied int
}

func createFullPiBackup(plan FullPiBackupPlan, sourceSnapshot string, now time.Time) (FullPiBackupResult, error) {
	report, err := copyTreePreserveSymlinks(plan.SourcePath, plan.BackupPath)
	if err != nil {
		return FullPiBackupResult{}, err
	}
	result := FullPiBackupResult{
		SourcePath:     plan.SourcePath,
		BackupPath:     plan.BackupPath,
		ManifestPath:   plan.ManifestPath,
		CreatedAt:      now.UTC().Format(time.RFC3339),
		SourceSnapshot: sourceSnapshot,
		EntriesCopied:  report.EntriesCopied,
		FilesCopied:    report.FilesCopied,
		DirsCopied:     report.DirsCopied,
		SymlinksCopied: report.SymlinksCopied,
	}
	manifestBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return FullPiBackupResult{}, fmt.Errorf("encode full backup manifest: %w", err)
	}
	manifestBytes = append(manifestBytes, '\n')
	if err := writeFileAtomic(plan.ManifestPath, manifestBytes, 0o600); err != nil {
		return FullPiBackupResult{}, fmt.Errorf("write full backup manifest: %w", err)
	}
	return result, nil
}

func copyTreePreserveSymlinks(src, dst string) (treeCopyReport, error) {
	if src == dst {
		return treeCopyReport{}, fmt.Errorf("backup path must differ from source path")
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return treeCopyReport{}, fmt.Errorf("create backup root %s: %w", dst, err)
	}
	report := treeCopyReport{}
	walkErr := filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("backup walk %s: %w", path, walkErr)
		}
		if path == src {
			return nil
		}
		report.EntriesCopied++
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("backup rel %s: %w", path, err)
		}
		targetPath := filepath.Join(dst, rel)
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("backup stat %s: %w", path, err)
		}
		mode := info.Mode()
		switch {
		case mode&os.ModeSymlink != 0:
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("backup readlink %s: %w", path, err)
			}
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fmt.Errorf("backup mkdir %s: %w", targetPath, err)
			}
			if err := os.Symlink(linkTarget, targetPath); err != nil {
				return fmt.Errorf("backup symlink %s: %w", path, err)
			}
			report.SymlinksCopied++
		case mode.IsDir():
			if err := os.MkdirAll(targetPath, mode.Perm()); err != nil {
				return fmt.Errorf("backup mkdir %s: %w", targetPath, err)
			}
			report.DirsCopied++
		case mode.IsRegular():
			if err := copyFile(path, targetPath, mode.Perm()); err != nil {
				return fmt.Errorf("backup copy %s: %w", path, err)
			}
			report.FilesCopied++
		default:
			return fmt.Errorf("backup unsupported file type at %s", path)
		}
		return nil
	})
	if walkErr != nil {
		return treeCopyReport{}, walkErr
	}
	return report, nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
	}()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
