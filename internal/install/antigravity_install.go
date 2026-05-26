package install

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alferio94/lore-cli/internal/agentpack"
)

func (s Service) PlanAntigravityInstall(req InstallRequest) (InstallPlan, error) {
	req.Target = TargetAntigravity
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	}
	components, err := NormalizeComponentSelection(TargetAntigravity, req.Components)
	if err != nil {
		return InstallPlan{}, err
	}
	req.Components = components
	if err := req.Validate(); err != nil {
		return InstallPlan{}, err
	}

	layout := ResolveAntigravityLayout(req.HomeDir)
	rendered, err := renderAntigravityFiles(req)
	if err != nil {
		return InstallPlan{}, err
	}
	backupRoot := filepath.Join(layout.RootDir, "backups", req.Now.UTC().Format("20060102T150405Z"))
	plannedFiles, _, managedPaths, err := planAntigravityManagedFileActions(layout, rendered, backupRoot)
	if err != nil {
		return InstallPlan{}, err
	}
	manifest, _, err := buildAntigravityManifest(layout, req, rendered)
	if err != nil {
		return InstallPlan{}, err
	}
	manifest.ManagedFiles = buildManifestManagedFileRecords(rendered, managedPaths)
	manifestAction, err := planAntigravityManifestAction(layout.ManifestPath, backupRoot, manifest)
	if err != nil {
		return InstallPlan{}, err
	}
	plannedFiles = append(plannedFiles, manifestAction)
	return InstallPlan{Request: req, Layout: layout, Components: components, Files: plannedFiles}, nil
}

func (s Service) ExecuteAntigravityInstall(plan InstallPlan, opts InstallCommandOptions) (InstallResult, error) {
	if plan.Layout.Target != TargetAntigravity {
		return InstallResult{}, fmt.Errorf("plan target %q is not antigravity", plan.Layout.Target)
	}
	if opts.DryRun {
		return InstallResult{Target: TargetAntigravity, Layout: plan.Layout}, nil
	}

	rendered, err := renderAntigravityFiles(plan.Request)
	if err != nil {
		return InstallResult{}, err
	}
	backupRoot := filepath.Join(plan.Layout.RootDir, "backups", plan.Request.Now.UTC().Format("20060102T150405Z"))
	plannedFiles, desiredContents, managedPaths, err := planAntigravityManagedFileActions(plan.Layout, rendered, backupRoot)
	if err != nil {
		return InstallResult{}, err
	}
	manifest, _, err := buildAntigravityManifest(plan.Layout, plan.Request, rendered)
	if err != nil {
		return InstallResult{}, err
	}
	manifest.ManagedFiles = buildManifestManagedFileRecords(rendered, managedPaths)
	manifestAction, err := planAntigravityManifestAction(plan.Layout.ManifestPath, backupRoot, manifest)
	if err != nil {
		return InstallResult{}, err
	}
	plannedFiles = append(plannedFiles, manifestAction)
	if err := validateSharedInstallResultAgainstPlan(InstallPlan{Request: plan.Request, Layout: plan.Layout, Components: plan.Components, Files: plannedFiles}, InstallResult{Target: TargetAntigravity, Layout: plan.Layout, Summary: summarizePlannedActions(plannedFiles)}); err != nil {
		return InstallResult{}, err
	}

	result := InstallResult{Target: TargetAntigravity, Layout: plan.Layout}
	for _, file := range rendered {
		relativePath := filepath.ToSlash(file.RelativePath)
		desired := desiredContents[relativePath]
		action := lookupPlanFileAction(plannedFiles, relativePath)
		if err := applyAntigravityPlannedContent(action, desired); err != nil {
			result.Summary.Failed = append(result.Summary.Failed, fmt.Sprintf("%s: %v", relativePath, err))
			continue
		}
		appendInstallSummaryAction(&result.Summary, action.RelativePath, action.Action)
	}

	manifestBytes, err := marshalManifest(manifest)
	if err != nil {
		return InstallResult{}, err
	}
	if err := applyAntigravityPlannedContent(manifestAction, manifestBytes); err != nil {
		return InstallResult{}, err
	}
	appendInstallSummaryAction(&result.Summary, manifestAction.RelativePath, manifestAction.Action)
	loadedManifest, err := LoadManifest(plan.Layout.ManifestPath)
	if err != nil {
		return InstallResult{}, err
	}
	if err := loadedManifest.ValidateForLayout(plan.Layout, managedPaths, filepath.Join(plan.Layout.RootDir, "backups")); err != nil {
		return InstallResult{}, err
	}
	result.Manifest = loadedManifest
	return result, nil
}

func renderAntigravityFiles(req InstallRequest) ([]RenderedFile, error) {
	registry, err := defaultInstallRegistry()
	if err != nil {
		return nil, err
	}
	adapter, err := registry.Resolve(TargetAntigravity)
	if err != nil {
		return nil, err
	}
	renderReq := RenderRequest{
		Target:         TargetAntigravity,
		Definition:     req.Definition,
		Components:     req.Components,
		ServerURL:      req.ServerURL,
		LoreBinaryPath: req.LoreBinaryPath,
		LoreConfigDir:  req.LoreConfigDir,
		LoreCLIVersion: req.LoreCLIVersion,
	}
	if req.Definition.SchemaVersion == 0 {
		renderReq.Assets = agentpack.DefaultOperationalAssets()
		renderReq.Definition = renderReq.Assets.Definition()
	}
	return adapter.Render(context.Background(), renderReq)
}

func planAntigravityManagedFileActions(layout HarnessLayout, rendered []RenderedFile, backupRoot string) ([]PlanFileAction, map[string][]byte, []string, error) {
	actions := make([]PlanFileAction, 0, len(rendered))
	desiredContents := make(map[string][]byte, len(rendered))
	managedPaths := make([]string, 0, len(rendered))
	for _, file := range rendered {
		desired, action, err := planAntigravityRenderedFileAction(layout, file, backupRoot)
		if err != nil {
			return nil, nil, nil, err
		}
		relativePath := filepath.ToSlash(file.RelativePath)
		desiredContents[relativePath] = desired
		actions = append(actions, action)
		managedPaths = append(managedPaths, action.AbsolutePath)
	}
	return actions, desiredContents, managedPaths, nil
}

func planAntigravityRenderedFileAction(layout HarnessLayout, file RenderedFile, backupRoot string) ([]byte, PlanFileAction, error) {
	absolutePath := antigravityAbsolutePath(layout, file.RelativePath)
	desired := file.Content
	existing, err := os.ReadFile(absolutePath)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return nil, PlanFileAction{}, fmt.Errorf("read existing file: %w", err)
	}
	switch filepath.ToSlash(file.RelativePath) {
	case filepath.ToSlash(filepath.Join("..", "GEMINI.md")):
		desired, err = mergeAntigravityPrompt(existing, desired)
		if err != nil {
			return nil, PlanFileAction{}, err
		}
	case filepath.ToSlash(filepath.Join("..", "config", "mcp_config.json")):
		desired, err = mergeAntigravityMCPConfig(existing, desired)
		if err != nil {
			return nil, PlanFileAction{}, err
		}
	}
	action := PlanFileAction{Component: file.Component, RelativePath: filepath.ToSlash(file.RelativePath), AbsolutePath: absolutePath}
	if exists && string(existing) == string(desired) {
		action.Action = "unchanged"
		return desired, action, nil
	}
	if exists {
		action.Action = "update"
		action.BackupPath = filepath.Join(backupRoot, antigravityBackupRelativePath(file.RelativePath))
		return desired, action, nil
	}
	action.Action = "create"
	return desired, action, nil
}

func planAntigravityManifestAction(manifestPath, backupRoot string, manifest Manifest) (PlanFileAction, error) {
	manifestBytes, err := marshalManifest(manifest)
	if err != nil {
		return PlanFileAction{}, err
	}
	existing, err := os.ReadFile(manifestPath)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return PlanFileAction{}, fmt.Errorf("read existing manifest: %w", err)
	}
	action := PlanFileAction{RelativePath: "lore-install.json", AbsolutePath: manifestPath}
	if exists && string(existing) == string(manifestBytes) {
		action.Action = "unchanged"
		return action, nil
	}
	if exists {
		action.Action = "update"
		action.BackupPath = filepath.Join(backupRoot, "lore-install.json")
		return action, nil
	}
	action.Action = "create"
	return action, nil
}

func buildManifestManagedFileRecords(rendered []RenderedFile, managedPaths []string) []ManagedFileRecord {
	records := make([]ManagedFileRecord, 0, len(rendered))
	for i, file := range rendered {
		records = append(records, ManagedFileRecord{
			Path:        managedPaths[i],
			Component:   file.Component,
			MergeMode:   file.MergeMode,
			ContentHash: contentHash(file.Content),
		})
	}
	return records
}

func applyAntigravityPlannedContent(action PlanFileAction, desired []byte) error {
	if action.Action == "unchanged" {
		return nil
	}
	if action.Action == "update" {
		existing, err := os.ReadFile(action.AbsolutePath)
		if err != nil {
			return fmt.Errorf("read existing file: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(action.BackupPath), 0o755); err != nil {
			return fmt.Errorf("create backup dir: %w", err)
		}
		if err := writeFileAtomic(action.BackupPath, existing, 0o600); err != nil {
			return fmt.Errorf("write backup: %w", err)
		}
	}
	return writeFileAtomic(action.AbsolutePath, desired, 0o600)
}

func summarizePlannedActions(actions []PlanFileAction) InstallSummary {
	var summary InstallSummary
	for _, action := range actions {
		appendInstallSummaryAction(&summary, action.RelativePath, action.Action)
	}
	return summary
}

func appendInstallSummaryAction(summary *InstallSummary, relativePath, action string) {
	switch action {
	case "create":
		summary.Created = append(summary.Created, relativePath)
	case "update":
		summary.Updated = append(summary.Updated, relativePath)
	case "delete":
		summary.Deleted = append(summary.Deleted, relativePath)
	case "unchanged":
		summary.Unchanged = append(summary.Unchanged, relativePath)
	}
}

func lookupPlanFileAction(actions []PlanFileAction, relativePath string) PlanFileAction {
	for _, action := range actions {
		if filepath.ToSlash(action.RelativePath) == filepath.ToSlash(relativePath) {
			return action
		}
	}
	return PlanFileAction{RelativePath: relativePath}
}

func antigravityBackupRelativePath(relativePath string) string {
	switch filepath.ToSlash(relativePath) {
	case filepath.ToSlash(filepath.Join("..", "GEMINI.md")):
		return filepath.ToSlash(filepath.Join("shared", "GEMINI.md"))
	case filepath.ToSlash(filepath.Join("..", "config", "mcp_config.json")):
		return filepath.ToSlash(filepath.Join("shared", "config", "mcp_config.json"))
	default:
		return filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(relativePath), "./"))
	}
}
