package install

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alferio94/lore-cli/internal/agentconfig"
	"github.com/alferio94/lore-cli/internal/agentpack"
)

// PlanOpenCodeInstall creates an install plan for the OpenCode target.
// It is the foundation-slice entrypoint: it normalizes components, loads
// the agent-config.json store, renders the adapter-managed files, and
// produces a manifest. opencode.json rendering and the additive
// merge/backup logic are layered on top in renderOpenCodeFiles (no
// merge with existing user content in this slice — fresh write only;
// merge/backup is added in the 2.x regression slice).
func (s Service) PlanOpenCodeInstall(req InstallRequest) (InstallPlan, error) {
	req.Target = TargetOpenCode
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	}
	components, err := NormalizeComponentSelection(TargetOpenCode, req.Components)
	if err != nil {
		return InstallPlan{}, err
	}
	req.Components = components
	if err := req.Validate(); err != nil {
		return InstallPlan{}, err
	}

	var agentCfg agentconfig.Config
	if s.AgentConfigStore != nil {
		if _, _, err = s.AgentConfigStore.EnsureDefault(); err != nil {
			return InstallPlan{}, fmt.Errorf("ensure agent-config: %w", err)
		}
		agentCfg, err = s.AgentConfigStore.Load()
		if err != nil {
			return InstallPlan{}, fmt.Errorf("load agent-config: %w", err)
		}
	}
	req.AgentConfig = agentCfg

	layout := ResolveOpenCodeLayout(req.HomeDir)
	rendered, err := renderOpenCodeFiles(req)
	if err != nil {
		return InstallPlan{}, err
	}
	backupRoot := filepath.Join(layout.RootDir, "backups", req.Now.UTC().Format("20060102T150405Z"))
	plannedFiles, desiredContents, managedPaths, err := planOpenCodeManagedFileActions(layout, rendered, backupRoot)
	if err != nil {
		return InstallPlan{}, err
	}
	manifest, _, err := buildOpenCodeManifest(layout, req, rendered, desiredContents)
	if err != nil {
		return InstallPlan{}, err
	}
	manifest.ManagedFiles = buildOpenCodeManifestManagedFileRecords(rendered, desiredContents, managedPaths)
	manifestAction, err := planOpenCodeManifestAction(layout.ManifestPath, backupRoot, manifest)
	if err != nil {
		return InstallPlan{}, err
	}
	plannedFiles = append(plannedFiles, manifestAction)
	return InstallPlan{Request: req, Layout: layout, Components: components, Files: plannedFiles}, nil
}

// ExecuteOpenCodeInstall applies the OpenCode install plan. It is the
// foundation-slice entrypoint; opencode.json rendering uses the fresh
// write path (no merge with existing user content). Merge-aware apply
// is added in the 2.x regression slice.
func (s Service) ExecuteOpenCodeInstall(plan InstallPlan, opts InstallCommandOptions) (InstallResult, error) {
	if plan.Layout.Target != TargetOpenCode {
		return InstallResult{}, fmt.Errorf("plan target %q is not opencode", plan.Layout.Target)
	}
	if opts.DryRun {
		return InstallResult{Target: TargetOpenCode, Layout: plan.Layout}, nil
	}

	rendered, err := renderOpenCodeFiles(plan.Request)
	if err != nil {
		return InstallResult{}, err
	}
	backupRoot := filepath.Join(plan.Layout.RootDir, "backups", plan.Request.Now.UTC().Format("20060102T150405Z"))
	plannedFiles, desiredContents, managedPaths, err := planOpenCodeManagedFileActions(plan.Layout, rendered, backupRoot)
	if err != nil {
		return InstallResult{}, err
	}
	manifest, _, err := buildOpenCodeManifest(plan.Layout, plan.Request, rendered, desiredContents)
	if err != nil {
		return InstallResult{}, err
	}
	manifest.ManagedFiles = buildOpenCodeManifestManagedFileRecords(rendered, desiredContents, managedPaths)
	manifestAction, err := planOpenCodeManifestAction(plan.Layout.ManifestPath, backupRoot, manifest)
	if err != nil {
		return InstallResult{}, err
	}
	plannedFiles = append(plannedFiles, manifestAction)

	if err := validateSharedInstallResultAgainstPlan(
		InstallPlan{Request: plan.Request, Layout: plan.Layout, Components: plan.Components, Files: plannedFiles},
		InstallResult{Target: TargetOpenCode, Layout: plan.Layout, Summary: summarizePlannedActions(plannedFiles)},
	); err != nil {
		return InstallResult{}, err
	}

	result := InstallResult{Target: TargetOpenCode, Layout: plan.Layout}
	for _, file := range rendered {
		relativePath := filepath.ToSlash(file.RelativePath)
		desired := desiredContents[relativePath]
		action := lookupPlanFileAction(plannedFiles, relativePath)
		if err := applyOpenCodePlannedContent(action, desired); err != nil {
			result.Summary.Failed = append(result.Summary.Failed, fmt.Sprintf("%s: %v", relativePath, err))
			continue
		}
		appendInstallSummaryAction(&result.Summary, action.RelativePath, action.Action)
	}

	manifestBytes, err := marshalManifest(manifest)
	if err != nil {
		return InstallResult{}, err
	}
	if err := applyOpenCodePlannedContent(manifestAction, manifestBytes); err != nil {
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

// renderOpenCodeFiles renders the full set of files the OpenCode
// installer must produce: the adapter-managed markdown surfaces plus the
// opencode.json file (lore block alone, or lore+mcp.lore when
// lore-server-mcp is selected). The adapter does not produce
// opencode.json so the install pipeline can centralize that decision
// here.
func renderOpenCodeFiles(req InstallRequest) ([]RenderedFile, error) {
	registry, err := defaultInstallRegistry()
	if err != nil {
		return nil, err
	}
	adapter, err := registry.Resolve(TargetOpenCode)
	if err != nil {
		return nil, err
	}

	agentCfg := req.AgentConfig
	if agentCfg.SchemaVersion == 0 {
		if store := getAgentConfigStoreForRender(req); store != nil {
			if cfg, err := store.Load(); err == nil {
				agentCfg = cfg
			}
		}
	}

	renderReq := RenderRequest{
		Target:         TargetOpenCode,
		Definition:     req.Definition,
		Components:     req.Components,
		ServerURL:      req.ServerURL,
		SavedToken:     req.SavedToken,
		LoreBinaryPath: req.LoreBinaryPath,
		LoreConfigDir:  req.LoreConfigDir,
		LoreCLIVersion: req.LoreCLIVersion,
		AgentConfig:    agentCfg,
	}
	if req.Definition.SchemaVersion == 0 {
		renderReq.Assets = agentpack.DefaultOperationalAssets()
		renderReq.Definition = renderReq.Assets.Definition()
	}
	rendered, err := adapter.Render(context.Background(), renderReq)
	if err != nil {
		return nil, err
	}

	hasMCPEffective := containsComponent(req.Components, ComponentLoreServerMCP) && strings.TrimSpace(req.ServerURL) != "" && strings.TrimSpace(req.SavedToken) != ""
	var configBytes []byte
	if hasMCPEffective {
		configBytes, err = renderOpenCodeMCPConfig(agentCfg, req.ServerURL, req.SavedToken)
	} else {
		configBytes, err = renderOpenCodeNativeConfig(agentCfg)
	}
	if err != nil {
		return nil, err
	}
	rendered = append(rendered, RenderedFile{
		Component:    ComponentCorePack,
		RelativePath: opencodeConfigFileName,
		MergeMode:    MergeModeAdditiveJSON,
		Content:      configBytes,
	})

	sort.Slice(rendered, func(i, j int) bool { return rendered[i].RelativePath < rendered[j].RelativePath })
	return rendered, nil
}

func planOpenCodeManagedFileActions(layout HarnessLayout, rendered []RenderedFile, backupRoot string) ([]PlanFileAction, map[string][]byte, []string, error) {
	actions := make([]PlanFileAction, 0, len(rendered))
	desiredContents := make(map[string][]byte, len(rendered))
	managedPaths := make([]string, 0, len(rendered))
	for _, file := range rendered {
		desired, action, err := planOpenCodeRenderedFileAction(layout, file, backupRoot)
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

func planOpenCodeRenderedFileAction(layout HarnessLayout, file RenderedFile, backupRoot string) ([]byte, PlanFileAction, error) {
	absolutePath := openCodeAbsolutePath(layout, file.RelativePath)
	desired := file.Content
	existing, err := os.ReadFile(absolutePath)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return nil, PlanFileAction{}, fmt.Errorf("read existing file: %w", err)
	}
	if file.MergeMode == MergeModeAdditiveJSON {
		desired, err = mergeOpenCodeConfigJSON(existing, desired, file.RelativePath)
		if err != nil {
			// Fail-closed mcp.lore ownership conflict: the existing
			// opencode.json carries a non-Lore-owned mcp.lore block.
			// The installer backs up the existing file BEFORE
			// surfacing the conflict so the user can recover without
			// losing their original configuration. The plan records
			// the conflict as a `conflicted` action with the
			// backup path so dry-run output is honest about the
			// state on disk.
			if conflict := AsOpenCodeMCPConfigOwnershipConflict(err); conflict != nil && exists {
				conflict.BackupPath = filepath.Join(backupRoot, openCodeBackupRelativePath(file.RelativePath))
				if backupErr := writeOpenCodeConflictBackup(conflict.BackupPath, absolutePath, existing); backupErr != nil {
					return nil, PlanFileAction{}, fmt.Errorf("%w (and failed to back up the existing file: %v)", conflict, backupErr)
				}
				return nil, PlanFileAction{
					Component:    file.Component,
					RelativePath: filepath.ToSlash(file.RelativePath),
					AbsolutePath: absolutePath,
					MergeMode:    file.MergeMode,
					Action:       "conflicted",
					BackupPath:   conflict.BackupPath,
				}, conflict
			}
			return nil, PlanFileAction{}, err
		}
	}
	action := PlanFileAction{Component: file.Component, RelativePath: filepath.ToSlash(file.RelativePath), AbsolutePath: absolutePath, MergeMode: file.MergeMode}
	if exists && string(existing) == string(desired) {
		action.Action = "unchanged"
		return desired, action, nil
	}
	if exists {
		action.Action = "update"
		action.BackupPath = filepath.Join(backupRoot, openCodeBackupRelativePath(file.RelativePath))
		return desired, action, nil
	}
	action.Action = "create"
	return desired, action, nil
}

// writeOpenCodeConflictBackup writes the existing on-disk file to the
// conflict backup path. It is called from the fail-closed mcp.lore
// ownership path so the user can recover the original
// configuration after the installer aborts. The backup is owned by
// the installer's managed backup root and uses the standard 0o600
// permissions to match the rest of the installer's backup surface.
func writeOpenCodeConflictBackup(backupPath, absolutePath string, existing []byte) error {
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
		return fmt.Errorf("create conflict backup dir: %w", err)
	}
	if err := writeFileAtomic(backupPath, existing, 0o600); err != nil {
		return fmt.Errorf("write conflict backup: %w", err)
	}
	return nil
}

func planOpenCodeManifestAction(manifestPath, backupRoot string, manifest Manifest) (PlanFileAction, error) {
	manifestBytes, err := marshalManifest(manifest)
	if err != nil {
		return PlanFileAction{}, err
	}
	existing, err := os.ReadFile(manifestPath)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return PlanFileAction{}, fmt.Errorf("read existing manifest: %w", err)
	}
	action := PlanFileAction{RelativePath: opencodeManifestFileName, AbsolutePath: manifestPath}
	if exists && string(existing) == string(manifestBytes) {
		action.Action = "unchanged"
		return action, nil
	}
	if exists {
		action.Action = "update"
		action.BackupPath = filepath.Join(backupRoot, opencodeManifestFileName)
		return action, nil
	}
	action.Action = "create"
	return action, nil
}

func buildOpenCodeManifestManagedFileRecords(files []RenderedFile, desiredContents map[string][]byte, managedPaths []string) []ManagedFileRecord {
	records := make([]ManagedFileRecord, 0, len(managedPaths))
	for _, path := range managedPaths {
		var component ComponentID
		var mergeMode MergeMode
		var content []byte
		matched := false
		for _, f := range files {
			if openCodeAbsolutePathFromPath(f.RelativePath, path) {
				component = f.Component
				mergeMode = f.MergeMode
				content = desiredContents[filepath.ToSlash(f.RelativePath)]
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		records = append(records, ManagedFileRecord{
			Path:        path,
			Component:   component,
			MergeMode:   mergeMode,
			ContentHash: contentHash(content),
		})
	}
	return records
}

func openCodeAbsolutePathFromPath(relativePath, absolutePath string) bool {
	relative := filepath.ToSlash(relativePath)
	switch relative {
	case opencodeAgentsFileName:
		return strings.HasSuffix(absolutePath, "/AGENTS.md") || strings.HasSuffix(absolutePath, "AGENTS.md")
	case opencodeConfigFileName:
		return strings.HasSuffix(absolutePath, "/opencode.json") || strings.HasSuffix(absolutePath, "opencode.json")
	case opencodeManifestFileName:
		return strings.HasSuffix(absolutePath, "/lore-install.json") || strings.HasSuffix(absolutePath, "lore-install.json")
	default:
		return strings.HasSuffix(absolutePath, relativePath)
	}
}

func buildOpenCodeManifest(layout HarnessLayout, req InstallRequest, files []RenderedFile, desiredContents map[string][]byte) (Manifest, []string, error) {
	if layout.Target != TargetOpenCode {
		return Manifest{}, nil, fmt.Errorf("layout target %q does not match opencode", layout.Target)
	}
	components, err := NormalizeComponentSelection(TargetOpenCode, req.Components)
	if err != nil {
		return Manifest{}, nil, err
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	records := make([]ManagedFileRecord, 0, len(files))
	managedPaths := make([]string, 0, len(files))
	for _, file := range files {
		absolutePath := openCodeAbsolutePath(layout, file.RelativePath)
		managedPaths = append(managedPaths, absolutePath)
		records = append(records, ManagedFileRecord{
			Path:        absolutePath,
			Component:   file.Component,
			MergeMode:   file.MergeMode,
			ContentHash: contentHash(desiredContents[filepath.ToSlash(file.RelativePath)]),
		})
	}
	manifest := Manifest{
		SchemaVersion: PortableManifestSchemaVersion,
		Target:        TargetOpenCode,
		AuthMode:      "config-only",
		ServerURL:     strings.TrimSpace(req.ServerURL),
		LoreBinary:    strings.TrimSpace(req.LoreBinaryPath),
		LoreConfigDir: strings.TrimSpace(req.LoreConfigDir),
		Components:    append([]ComponentID(nil), components...),
		ManagedFiles:  records,
		BackupRoot:    filepath.Join(layout.RootDir, "backups", now.UTC().Format("20060102T150405Z")),
		InstalledAt:   now.UTC().Format(time.RFC3339),
		CLIVersion:    strings.TrimSpace(req.LoreCLIVersion),
	}
	return manifest, managedPaths, nil
}

func applyOpenCodePlannedContent(action PlanFileAction, desired []byte) error {
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

func openCodeAbsolutePath(layout HarnessLayout, relativePath string) string {
	cleanRelativePath := filepath.ToSlash(relativePath)
	switch cleanRelativePath {
	case opencodeAgentsFileName:
		return layout.Paths[opencodeAgentsPathKey]
	case opencodeConfigFileName:
		return layout.Paths[opencodeJSONPathKey]
	case opencodeManifestFileName:
		return layout.Paths[opencodeManifestPathKey]
	default:
		return filepath.Join(layout.RootDir, filepath.FromSlash(cleanRelativePath))
	}
}

func openCodeBackupRelativePath(relativePath string) string {
	return filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(relativePath), "./"))
}
