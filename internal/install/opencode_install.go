package install

import (
	"context"
	"errors"
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
		agentCfg, err = loadOpenCodeAgentConfigForPlan(s.AgentConfigStore)
		if err != nil {
			return InstallPlan{}, err
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
	// Manifest-scoped stale managed-file cleanup (e.g. the
	// `plugins/model-variants.ts` → `plugins/lore-models.ts`
	// rename introduced by the `add-opencode-lore-models-plugin`
	// change). The cleanup is bounded to the previous manifest's
	// `managed_files` records: user-owned plugin files without
	// prior manifest ownership are never deleted.
	staleActions, err := planOpenCodeStaleManagedPluginCleanup(layout, managedPaths, backupRoot)
	if err != nil {
		return InstallPlan{}, err
	}
	plannedFiles = append(plannedFiles, staleActions...)
	manifestAction, err := planOpenCodeManifestAction(layout.ManifestPath, backupRoot, manifest)
	if err != nil {
		return InstallPlan{}, err
	}
	plannedFiles = append(plannedFiles, manifestAction)
	return InstallPlan{Request: req, Layout: layout, Components: components, Files: plannedFiles}, nil
}

func loadOpenCodeAgentConfigForPlan(store AgentConfigStore) (agentconfig.Config, error) {
	cfg, err := store.Load()
	if err == nil {
		if validateErr := cfg.Validate(); validateErr != nil {
			return agentconfig.Config{}, fmt.Errorf("existing agent-config is invalid: %w", validateErr)
		}
		return cfg, nil
	}
	if errors.Is(err, agentconfig.ErrNotFound) {
		return agentconfig.DefaultConfig(), nil
	}
	return agentconfig.Config{}, fmt.Errorf("load agent-config: %w", err)
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
	if s.AgentConfigStore != nil {
		agentCfg, _, err := s.AgentConfigStore.EnsureDefault()
		if err != nil {
			return InstallResult{}, fmt.Errorf("ensure agent-config: %w", err)
		}
		plan.Request.AgentConfig = agentCfg
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
	// Manifest-scoped stale managed-file cleanup (e.g. the
	// `plugins/model-variants.ts` → `plugins/lore-models.ts`
	// rename). See `PlanOpenCodeInstall` for the planning
	// contract; the same function is reused here so the dry-run
	// plan and the live apply see identical actions.
	staleActions, err := planOpenCodeStaleManagedPluginCleanup(plan.Layout, managedPaths, backupRoot)
	if err != nil {
		return InstallResult{}, err
	}
	plannedFiles = append(plannedFiles, staleActions...)
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
	// Apply the manifest-scoped stale managed-file cleanup
	// (e.g. removing the previous `plugins/model-variants.ts`
	// after the `add-opencode-lore-models-plugin` rename). The
	// apply is bounded to actions in `plannedFiles` with
	// `Action == "delete"` and `Component == ComponentOpenCodePlugins`
	// so unrelated surfaces are unaffected.
	for _, action := range plannedFiles {
		if action.Action != "delete" || action.Component != ComponentOpenCodePlugins {
			continue
		}
		if err := applyOpenCodePlannedDelete(action); err != nil {
			result.Summary.Failed = append(result.Summary.Failed, fmt.Sprintf("%s: %v", action.RelativePath, err))
			continue
		}
		appendInstallSummaryAction(&result.Summary, action.RelativePath, action.Action)
		result.Summary.BackedUp = append(result.Summary.BackedUp, action.RelativePath)
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
	// The agent overlay is sourced from the effective definition
	// (already resolved by the earlier `if req.Definition.SchemaVersion == 0`
	// block). Threading the definition through the renderer lets
	// the primary `lore` orchestrator entry use the
	// `ProfileBalanced.RoleModels["orchestrator"]` model mapping.
	effectiveDefinition := renderReq.effectiveDefinition()
	// Reinstall preservation: read the on-disk `opencode.json`
	// (when present and valid JSON) for the per-agent
	// `model`/`variant` values the user may have set via the
	// in-OpenCode `lore-models` configuration flow. The values
	// flow into the managed `agent` overlay before merge so
	// user-chosen model/variant values are NOT reset to managed
	// defaults on the next install. The read is best-effort: a
	// missing, empty, or malformed file falls back to managed
	// defaults and the install pipeline proceeds normally
	// (the additive merge in `mergeOpenCodeConfigJSON` still
	// rejects malformed `opencode.json` upstream with a
	// JSON-decode error).
	layoutForExisting := ResolveOpenCodeLayout(req.HomeDir)
	existingAgent := effectiveOpenCodeExistingAgent(layoutForExisting)
	var configBytes []byte
	if hasMCPEffective {
		configBytes, err = renderOpenCodeMCPConfigWithExisting(effectiveDefinition, agentCfg, req.ServerURL, req.SavedToken, existingAgent)
	} else {
		configBytes, err = renderOpenCodeNativeConfigWithExisting(effectiveDefinition, agentCfg, existingAgent)
	}
	if err != nil {
		return nil, err
	}
	if err := validateOpenCodeStartupSafeConfig(configBytes, opencodeConfigFileName); err != nil {
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
			// Planning must remain pure, so this path records the
			// managed backup path for user guidance but does not write
			// a backup or mutate the filesystem during plan/dry-run.
			if conflict := AsOpenCodeMCPConfigOwnershipConflict(err); conflict != nil && exists {
				conflict.BackupPath = filepath.Join(backupRoot, openCodeBackupRelativePath(file.RelativePath))
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
		if filepath.ToSlash(file.RelativePath) == opencodeConfigFileName {
			if err := validateOpenCodeStartupSafeConfig(desired, file.RelativePath); err != nil {
				return nil, PlanFileAction{}, err
			}
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
	if action.Action == "delete" {
		return applyOpenCodePlannedDelete(action)
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

// applyOpenCodePlannedDelete is the manifest-scoped stale-cleanup
// apply path. It backs up the existing file to `action.BackupPath`
// (using the standard 0o600 permission) and then removes the file
// from disk. The backup is written BEFORE the delete so a failure
// in the backup step leaves the original file untouched. A
// non-existent file is a no-op (the cleanup is idempotent across
// reruns once a prior cleanup has already removed the file).
func applyOpenCodePlannedDelete(action PlanFileAction) error {
	existing, err := os.ReadFile(action.AbsolutePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read existing file: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(action.BackupPath), 0o755); err != nil {
		return fmt.Errorf("create backup dir: %w", err)
	}
	if err := writeFileAtomic(action.BackupPath, existing, 0o600); err != nil {
		return fmt.Errorf("write backup: %w", err)
	}
	if err := os.Remove(action.AbsolutePath); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("delete stale managed file: %w", err)
		}
	}
	return nil
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

// planOpenCodeStaleManagedPluginCleanup compares the previous
// `lore-install.json` manifest's `managed_files` to the newly
// rendered managed plugin set and emits a backup-first delete
// action for any path that was previously Lore-managed but is no
// longer rendered. The function is the manifest-scoped safety gate
// for the `add-opencode-lore-models-plugin` rename: the old
// `plugins/model-variants.ts` file is removed on upgrade when the
// previous manifest proves Lore owned it, but a similarly named
// file with no prior manifest ownership is left untouched.
//
// Implementation notes:
//
//   - The function ONLY inspects the previous manifest. It does
//     NOT scan the on-disk plugins directory, so a user-owned
//     `plugins/model-variants.ts` that was never Lore-managed
//     is never deleted.
//   - The previous manifest is loaded with `LoadManifest`, which
//     returns an empty `Manifest` (and a nil error) when the file
//     is missing or unreadable. A fresh install therefore emits
//     no cleanup action, even when `plugins/model-variants.ts`
//     happens to exist on disk.
//   - The backup path is rooted at `backupRoot` (the install-time
//     timestamped backup directory) so the deleted file is
//     recoverable from the install summary.
//   - Non-regular files (symlinks, directories) at the stale path
//     are NOT deleted: the cleanup is a no-op and the function
//     returns a nil action. This avoids accidentally removing a
//     directory the user happened to place at the same path.
//
// The returned action uses the same `PlanFileAction` shape as the
// rest of the OpenCode install plan and is appended to the
// `plannedFiles` slice in `PlanOpenCodeInstall` and
// `ExecuteOpenCodeInstall`.
func planOpenCodeStaleManagedPluginCleanup(layout HarnessLayout, newManagedPaths []string, backupRoot string) ([]PlanFileAction, error) {
	if layout.Target != TargetOpenCode {
		return nil, nil
	}
	// A missing or unreadable previous manifest is the
	// fresh-install path: no Lore-owned files to clean up. The
	// function returns a nil action and a nil error so the
	// install pipeline proceeds normally.
	previous, err := LoadManifest(layout.ManifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("load previous manifest: %w", err)
	}
	if len(previous.ManagedFiles) == 0 {
		return nil, nil
	}
	// Build the set of newly rendered managed absolute paths so
	// stale paths can be detected in O(N+M) instead of O(N*M).
	newPathSet := make(map[string]struct{}, len(newManagedPaths))
	for _, p := range newManagedPaths {
		newPathSet[filepath.Clean(p)] = struct{}{}
	}
	// Only the plugin component's managed paths are eligible for
	// stale-cleanup via this pass. Other managed surfaces
	// (AGENTS.md, opencode.json, tui.json) are owned by the
	// additive merge in `mergeOpenCodeConfigJSON` and never
	// become stale via the manifest-scoped path.
	actions := make([]PlanFileAction, 0)
	for _, record := range previous.ManagedFiles {
		if record.Component != ComponentOpenCodePlugins {
			continue
		}
		absolutePath := filepath.Clean(record.Path)
		if _, kept := newPathSet[absolutePath]; kept {
			continue
		}
		// Stale path: only act on a regular file that still
		// exists. A non-regular file is left alone (and the
		// action is skipped) to avoid accidentally clobbering
		// user content.
		info, err := os.Lstat(absolutePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("inspect stale managed plugin %s: %w", absolutePath, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		relativePath := openCodeStaleManagedPluginRelativePath(layout, absolutePath)
		if relativePath == "" || !isLegacyOpenCodeManagedPluginPath(relativePath) {
			continue
		}
		action := PlanFileAction{
			Component:    ComponentOpenCodePlugins,
			RelativePath: filepath.ToSlash(relativePath),
			AbsolutePath: absolutePath,
			MergeMode:    MergeModeReplace,
			Action:       "delete",
			BackupPath:   filepath.Join(backupRoot, openCodeBackupRelativePath(relativePath)),
		}
		actions = append(actions, action)
	}
	return actions, nil
}

// openCodeStaleManagedPluginRelativePath returns the install-pipeline
// relative path for a stale managed plugin file. The function
// rejects paths that escape the OpenCode harness root
// (defense-in-depth: a corrupted manifest could in theory record a
// path outside the layout).
func openCodeStaleManagedPluginRelativePath(layout HarnessLayout, absolutePath string) string {
	root := filepath.Clean(layout.RootDir)
	cleaned := filepath.Clean(absolutePath)
	if !strings.HasPrefix(cleaned, root+string(filepath.Separator)) && cleaned != root {
		return ""
	}
	rel, err := filepath.Rel(root, cleaned)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

func isLegacyOpenCodeManagedPluginPath(relativePath string) bool {
	relativePath = filepath.ToSlash(strings.TrimSpace(relativePath))
	return strings.HasPrefix(relativePath, "plugins/") && isLegacyOpenCodePluginReference(relativePath)
}
