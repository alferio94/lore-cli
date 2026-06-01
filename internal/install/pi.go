package install

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/alferio94/lore-cli/internal/agentpack"
)

//go:embed assets/pi/*
var installAssets embed.FS

type PiLayout struct {
	HomeDir          string
	PiDir            string
	AgentDir         string
	ExtensionsDir    string
	ThemesDir        string
	ManagedAgentsDir string
	SettingsPath     string
	ManifestPath     string
	AlferioThemePath string
	MCPConfigPath    string
	ManagedFiles     []string
}

type PiInstallRequest struct {
	HomeDir         string
	ServerURL       string
	LoreBinaryPath  string
	LoreConfigDir   string
	LoreCLIVersion  string
	SavedToken      string
	Target          TargetID
	Components      []ComponentID
	Definition      agentpack.Definition
	RuntimeContract RuntimeContract
	Now             time.Time
}

func (r PiInstallRequest) InstallRequest() InstallRequest {
	return InstallRequest{
		HomeDir:         r.HomeDir,
		ServerURL:       r.ServerURL,
		SavedToken:      r.SavedToken,
		LoreBinaryPath:  r.LoreBinaryPath,
		LoreConfigDir:   r.LoreConfigDir,
		LoreCLIVersion:  r.LoreCLIVersion,
		Target:          r.targetOrDefault(),
		Components:      append([]ComponentID(nil), r.Components...),
		Definition:      r.definitionOrDefault(),
		RuntimeContract: r.runtimeContractOrDefault(),
		Now:             r.Now,
	}
}

type ManagedFileAction struct {
	RelativePath string `json:"relative_path"`
	AbsolutePath string `json:"absolute_path"`
	Action       string `json:"action"`
	BackupPath   string `json:"backup_path,omitempty"`
}

type InstallSummary struct {
	Created    []string
	Updated    []string
	Deleted    []string
	Unchanged  []string
	BackedUp   []string
	Conflicted []string
	Failed     []string
}

type PiInstallResult struct {
	Layout     PiLayout
	Manifest   Manifest
	Summary    InstallSummary
	FullBackup *FullPiBackupResult
}

func (r PiInstallResult) InstallResult() InstallResult {
	return InstallResult{
		Target:   r.Layout.HarnessLayout().Target,
		Layout:   r.Layout.HarnessLayout(),
		Summary:  r.Summary,
		Manifest: r.Manifest,
	}
}

type renderedPiFile struct {
	component    ComponentID
	relativePath string
	absolutePath string
	content      []byte
	mergeMode    MergeMode
}

// piHostedMCPPackageSource is the canonical hosted MCP package source.
// Defined in adapter_pi.go to avoid import cycles; references the module-level constant.

// piHostedMCPPackageSource returns the canonical HTTPS pinned package source for the hosted MCP adapter.
// Defined in adapter_pi.go; referenced here to avoid import cycles.

func ResolvePiLayout(homeDir string) PiLayout {
	agentDir := filepath.Join(homeDir, ".pi", "agent")
	extensionsDir := filepath.Join(agentDir, "extensions")
	themesDir := filepath.Join(agentDir, "themes")
	managedAgentsDir := filepath.Join(agentDir, "agents")
	return PiLayout{
		HomeDir:          homeDir,
		PiDir:            filepath.Join(homeDir, ".pi"),
		AgentDir:         agentDir,
		ExtensionsDir:    extensionsDir,
		ThemesDir:        themesDir,
		ManagedAgentsDir: managedAgentsDir,
		SettingsPath:     filepath.Join(agentDir, "settings.json"),
		ManifestPath:     filepath.Join(agentDir, "lore-install.json"),
		AlferioThemePath: filepath.Join(themesDir, "alferio.json"),
		MCPConfigPath:    filepath.Join(agentDir, "mcp.json"),
		// Managed files: settings.json + mcp.json (hosted MCP default) + extended skills
		// Order matches adapter render order (settings.json first, then mcp.json, then extended skills).
		// lore-memory assets are optional and only managed when pi-extensions is explicitly selected.
		ManagedFiles: []string{
			filepath.Join(agentDir, "settings.json"),
			filepath.Join(agentDir, "mcp.json"),
			// Extended skills (CLI-managed under agent dir, in relative-path sort order)
			filepath.Join(agentDir, "skills", "judgment-day", "SKILL.md"),
			filepath.Join(agentDir, "skills", "skill-creator", "SKILL.md"),
			filepath.Join(agentDir, "skills", "skill-registry", "SKILL.md"),
		},
	}
}

func (l PiLayout) HarnessLayout() HarnessLayout {
	return HarnessLayout{
		Target:       TargetPi,
		RootDir:      l.AgentDir,
		ManifestPath: l.ManifestPath,
		Paths: map[string]string{
			"pi_dir":             l.PiDir,
			"agent_dir":          l.AgentDir,
			"extensions_dir":     l.ExtensionsDir,
			"themes_dir":         l.ThemesDir,
			"managed_agents_dir": l.ManagedAgentsDir,
			"settings":           l.SettingsPath,
			"manifest":           l.ManifestPath,
			"theme":              l.AlferioThemePath,
		},
	}
}

func (r PiInstallRequest) targetOrDefault() TargetID {
	if strings.TrimSpace(string(r.Target)) == "" {
		return TargetPi
	}
	return r.Target
}

func (r PiInstallRequest) definitionOrDefault() agentpack.Definition {
	if r.Definition.SchemaVersion == 0 {
		return agentpack.DefaultDefinition()
	}
	return r.Definition
}

func (r PiInstallRequest) normalizedComponents() ([]ComponentID, error) {
	if r.targetOrDefault() != TargetPi {
		if target, err := ResolveInstallTarget(r.targetOrDefault()); err != nil {
			return nil, err
		} else if target.ID == TargetAntigravity {
			return nil, fmt.Errorf("target %q must use the Antigravity prompt + skills install flow instead of the Pi-native install path", r.targetOrDefault())
		}
		return nil, fmt.Errorf("target %q must use its own harness-owned install flow", r.targetOrDefault())
	}
	components, err := NormalizeComponentSelection(r.targetOrDefault(), r.Components)
	if err != nil {
		return nil, err
	}
	// lore-server-mcp is the default; pi-extensions is optional (rollback/testing only).
	// Do not require pi-extensions for a valid default Pi install.
	return components, nil
}

func (r PiInstallRequest) renderRequest() (RenderRequest, error) {
	components, err := r.normalizedComponents()
	if err != nil {
		return RenderRequest{}, err
	}
	request := RenderRequest{
		Target:          r.targetOrDefault(),
		Definition:      r.definitionOrDefault(),
		Components:      components,
		ServerURL:       strings.TrimSpace(r.ServerURL),
		SavedToken:      strings.TrimSpace(r.SavedToken),
		LoreBinaryPath:  strings.TrimSpace(r.LoreBinaryPath),
		LoreConfigDir:   strings.TrimSpace(r.LoreConfigDir),
		LoreCLIVersion:  strings.TrimSpace(r.LoreCLIVersion),
		RuntimeContract: r.runtimeContractOrDefault(),
	}
	if r.Definition.SchemaVersion == 0 {
		request.Assets = agentpack.DefaultOperationalAssets()
		request.Definition = request.Assets.Definition()
	}
	if err := request.Validate(); err != nil {
		return RenderRequest{}, err
	}
	return request, nil
}

func (s Service) InstallPi(req PiInstallRequest) (PiInstallResult, error) {
	if strings.TrimSpace(req.HomeDir) == "" {
		return PiInstallResult{}, fmt.Errorf("home dir is required")
	}
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	}

	if err := validateRuntimeContractCompatibility(req.runtimeContractOrDefault()); err != nil {
		return PiInstallResult{}, err
	}
	if err := req.InstallRequest().Validate(); err != nil {
		return PiInstallResult{}, err
	}

	layout := ResolvePiLayout(req.HomeDir)
	backupRoot := filepath.Join(layout.AgentDir, "backups", req.Now.UTC().Format("20060102T150405Z"))
	rendered, err := renderPiFiles(layout, req)
	if err != nil {
		return PiInstallResult{}, err
	}
	if err := validateRenderedPiFiles(rendered); err != nil {
		return PiInstallResult{}, err
	}

	components, err := req.normalizedComponents()
	if err != nil {
		return PiInstallResult{}, err
	}
	manifest := Manifest{
		SchemaVersion: PortableManifestSchemaVersion,
		Target:        req.targetOrDefault(),
		AuthMode:      "cli-request",
		ServerURL:     strings.TrimSpace(req.ServerURL),
		LoreBinary:    strings.TrimSpace(req.LoreBinaryPath),
		LoreConfigDir: strings.TrimSpace(req.LoreConfigDir),
		Components:    append([]ComponentID(nil), components...),
		BackupRoot:    backupRoot,
		InstalledAt:   req.Now.UTC().Format(time.RFC3339),
		CLIVersion:    strings.TrimSpace(req.LoreCLIVersion),
	}
	result := PiInstallResult{Layout: layout, Manifest: manifest}

	if err := os.MkdirAll(layout.ExtensionsDir, 0o755); err != nil {
		return PiInstallResult{}, fmt.Errorf("create Pi extensions dir: %w", err)
	}
	if err := os.MkdirAll(layout.ManagedAgentsDir, 0o755); err != nil {
		return PiInstallResult{}, fmt.Errorf("create Pi managed agents dir: %w", err)
	}
	if err := bootstrapPiTheme(layout); err != nil {
		return PiInstallResult{}, err
	}

	cleanupAction, err := planLegacyDelegationCleanup(layout, backupRoot)
	if err != nil {
		return PiInstallResult{}, err
	}
	if cleanupAction != nil {
		state, err := applyManagedDelete(*cleanupAction)
		if err != nil {
			return PiInstallResult{}, fmt.Errorf("clean up legacy delegation file: %w", err)
		}
		if state == "delete" {
			result.Summary.Deleted = append(result.Summary.Deleted, cleanupAction.RelativePath)
			result.Summary.BackedUp = append(result.Summary.BackedUp, cleanupAction.RelativePath)
		}
	}

	validatedContents := make(map[string][]byte, len(rendered))
	for _, file := range rendered {
		finalContent, state, err := applyRenderedFile(file, backupRoot)
		if err != nil {
			result.Summary.Failed = append(result.Summary.Failed, fmt.Sprintf("%s: %v", file.relativePath, err))
			continue
		}
		validatedContents[file.relativePath] = finalContent
		switch state {
		case "create":
			result.Summary.Created = append(result.Summary.Created, file.relativePath)
		case "update":
			result.Summary.Updated = append(result.Summary.Updated, file.relativePath)
		case "unchanged":
			result.Summary.Unchanged = append(result.Summary.Unchanged, file.relativePath)
		}
		if state == "update" {
			if _, err := os.Stat(filepath.Join(backupRoot, file.relativePath)); err == nil {
				result.Summary.BackedUp = append(result.Summary.BackedUp, file.relativePath)
			}
		}
	}
	manifest.ManagedFiles = buildManagedFileRecords(rendered, validatedContents)

	existingManifest, _ := LoadManifest(layout.ManifestPath)
	managedOverlayRecords, managedOverlaySummary, err := applyManagedAgentOverlays(layout, req, existingManifest, backupRoot)
	if err != nil {
		return PiInstallResult{}, err
	}
	manifest.ManagedAgentOverlays = managedOverlayRecords
	result.Summary.Created = append(result.Summary.Created, managedOverlaySummary.Created...)
	result.Summary.Updated = append(result.Summary.Updated, managedOverlaySummary.Updated...)
	result.Summary.Deleted = append(result.Summary.Deleted, managedOverlaySummary.Deleted...)
	result.Summary.Unchanged = append(result.Summary.Unchanged, managedOverlaySummary.Unchanged...)
	result.Summary.BackedUp = append(result.Summary.BackedUp, managedOverlaySummary.BackedUp...)
	result.Summary.Conflicted = append(result.Summary.Conflicted, managedOverlaySummary.Conflicted...)

	manifestBytes, err := marshalManifest(manifest)
	if err != nil {
		return PiInstallResult{}, err
	}
	if err := writeFileAtomic(layout.ManifestPath, manifestBytes, 0o600); err != nil {
		return PiInstallResult{}, fmt.Errorf("write manifest: %w", err)
	}
	loadedManifest, err := LoadManifest(layout.ManifestPath)
	if err != nil {
		return PiInstallResult{}, err
	}
	if err := loadedManifest.Validate(layout); err != nil {
		return PiInstallResult{}, fmt.Errorf("validate manifest: %w", err)
	}
	result.Manifest = loadedManifest

	for _, finding := range validateManagedContents(validatedContents, req) {
		result.Summary.Failed = append(result.Summary.Failed, finding)
	}
	return result, nil
}

func renderPiFiles(layout PiLayout, req PiInstallRequest) ([]renderedPiFile, error) {
	renderRequest, err := req.renderRequest()
	if err != nil {
		return nil, err
	}
	renderRequest.SettingsPath = layout.SettingsPath
	registry, err := NewRegistry(defaultPiAdapter())
	if err != nil {
		return nil, err
	}
	adapter, err := registry.Resolve(renderRequest.Target)
	if err != nil {
		return nil, err
	}
	rendered, err := adapter.Render(context.Background(), renderRequest)
	if err != nil {
		return nil, err
	}

	// Include extended skills only for CLI-owned agent directory paths.
	extendedRendered, err := adapter.RenderExtendedSkills(context.Background(), renderRequest, layout)
	if err != nil {
		return nil, err
	}
	rendered = append(rendered, extendedRendered...)

	files := make([]renderedPiFile, 0, len(rendered))
	for _, file := range rendered {
		files = append(files, renderedPiFile{
			component:    file.Component,
			relativePath: file.RelativePath,
			absolutePath: absolutePiPath(layout, file.RelativePath),
			content:      file.Content,
			mergeMode:    file.MergeMode,
		})
	}
	return files, nil
}

func absolutePiPath(layout PiLayout, relativePath string) string {
	if relativePath == "settings.json" {
		return layout.SettingsPath
	}
	return filepath.Join(layout.AgentDir, filepath.FromSlash(relativePath))
}

func validateRenderedPiFiles(files []renderedPiFile) error {
	byPath := make(map[string]renderedPiFile, len(files))
	for _, file := range files {
		byPath[file.relativePath] = file
	}
	// lore-memory assets are optional (pi-extensions component). The default hosted MCP
	// install does not include them. Only validate them if they are present.
	for _, relativePath := range managedPiExtensionRelativePaths {
		file, ok := byPath[relativePath]
		if !ok {
			// lore-memory is optional; skip validation when not rendered.
			continue
		}
		if !strings.Contains(string(file.content), "export default function") {
			return fmt.Errorf("validate rendered Pi assets: %s missing documented export default function factory", relativePath)
		}
	}
	// Always validate that mcp.json is present when lore-server-mcp is in the default set.
	if _, ok := byPath[managedMCPConfigRelativePath]; !ok {
		return fmt.Errorf("validate rendered Pi assets: %s missing", managedMCPConfigRelativePath)
	}
	// Validate that settings.json includes the hosted MCP package (after placeholder replacement).
	// The placeholder {{LORE_HOSTED_MCP_PACKAGE}} should be replaced with the actual package source.
	if settings, ok := byPath["settings.json"]; ok {
		// Check that the placeholder has been replaced (not present in rendered content).
		if strings.Contains(string(settings.content), "{{LORE_HOSTED_MCP_PACKAGE}}") {
			return fmt.Errorf("validate rendered Pi assets: settings.json missing hosted MCP package placeholder replacement")
		}
	} else {
		return fmt.Errorf("validate rendered Pi assets: settings.json missing")
	}
	return nil
}

func planLegacyDelegationCleanup(layout PiLayout, backupRoot string) (*ManagedFileAction, error) {
	absolutePath := filepath.Join(layout.AgentDir, filepath.FromSlash(legacyPiDelegationRelativePath))
	info, err := os.Lstat(absolutePath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect legacy delegation file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("legacy delegation file at %s is %s; move it aside or replace it with a regular file before reinstalling", absolutePath, piEntryKind(info.Mode()))
	}
	action := ManagedFileAction{
		RelativePath: legacyPiDelegationRelativePath,
		AbsolutePath: absolutePath,
		Action:       "delete",
		BackupPath:   filepath.Join(backupRoot, legacyPiDelegationRelativePath),
	}
	return &action, nil
}

func applyManagedDelete(action ManagedFileAction) (string, error) {
	existing, err := os.ReadFile(action.AbsolutePath)
	if err != nil {
		return "", fmt.Errorf("read existing file: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(action.BackupPath), 0o755); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}
	if err := writeFileAtomic(action.BackupPath, existing, 0o600); err != nil {
		return "", fmt.Errorf("write backup: %w", err)
	}
	if err := os.Remove(action.AbsolutePath); err != nil {
		return "", fmt.Errorf("delete legacy file: %w", err)
	}
	return action.Action, nil
}

func planRenderedFileAction(file renderedPiFile, backupRoot string) ([]byte, ManagedFileAction, error) {
	desired := file.content
	existing, err := os.ReadFile(file.absolutePath)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return nil, ManagedFileAction{}, fmt.Errorf("read existing file: %w", err)
	}
	if file.mergeMode == MergeModeAdditiveJSON {
		desired, err = mergeJSONAdditive(existing, desired)
		if err != nil {
			return nil, ManagedFileAction{}, err
		}
	}
	action := ManagedFileAction{RelativePath: file.relativePath, AbsolutePath: file.absolutePath}
	if exists && string(existing) == string(desired) {
		action.Action = "unchanged"
		return desired, action, nil
	}
	if exists {
		action.Action = "update"
		action.BackupPath = filepath.Join(backupRoot, file.relativePath)
		return desired, action, nil
	}
	action.Action = "create"
	return desired, action, nil
}

func planManagedFileActions(files []renderedPiFile, backupRoot string) ([]ManagedFileAction, error) {
	actions := make([]ManagedFileAction, 0, len(files))
	for _, file := range files {
		_, action, err := planRenderedFileAction(file, backupRoot)
		if err != nil {
			return nil, err
		}
		actions = append(actions, action)
	}
	return actions, nil
}

func applyRenderedFile(file renderedPiFile, backupRoot string) ([]byte, string, error) {
	desired, action, err := planRenderedFileAction(file, backupRoot)
	if err != nil {
		return nil, "", err
	}
	if action.Action == "unchanged" {
		return desired, action.Action, nil
	}
	if action.Action == "update" {
		existing, err := os.ReadFile(file.absolutePath)
		if err != nil {
			return nil, "", fmt.Errorf("read existing file: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(action.BackupPath), 0o755); err != nil {
			return nil, "", fmt.Errorf("create backup dir: %w", err)
		}
		if err := writeFileAtomic(action.BackupPath, existing, 0o600); err != nil {
			return nil, "", fmt.Errorf("write backup: %w", err)
		}
	}
	if err := writeFileAtomic(file.absolutePath, desired, 0o600); err != nil {
		return nil, "", fmt.Errorf("write managed file: %w", err)
	}
	return desired, action.Action, nil
}

func bootstrapPiTheme(layout PiLayout) error {
	if _, err := os.Lstat(layout.AlferioThemePath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect Pi theme bootstrap path: %w", err)
	}
	content, err := installAssets.ReadFile("assets/pi/alferio.json")
	if err != nil {
		return fmt.Errorf("read Pi theme asset: %w", err)
	}
	if err := os.MkdirAll(layout.ThemesDir, 0o755); err != nil {
		return fmt.Errorf("create Pi themes dir: %w", err)
	}
	if err := writeFileAtomic(layout.AlferioThemePath, content, 0o600); err != nil {
		return fmt.Errorf("write Pi theme bootstrap: %w", err)
	}
	return nil
}

func mergeJSONAdditive(existing, desired []byte) ([]byte, error) {
	return mergeJSONObject(existing, desired, "settings.json", "settings.json", "settings.json")
}

func mergeMaps(base, overlay map[string]any) map[string]any {
	merged := make(map[string]any, len(base)+len(overlay))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range overlay {
		if key == "packages" {
			merged[key] = mergePackageLists(merged[key], value)
			continue
		}
		if key == "theme" {
			merged[key] = mergeThemeSetting(merged[key], value)
			continue
		}
		overlayMap, overlayIsMap := value.(map[string]any)
		baseMap, baseIsMap := merged[key].(map[string]any)
		if overlayIsMap && baseIsMap {
			merged[key] = mergeMaps(baseMap, overlayMap)
			continue
		}
		merged[key] = value
	}
	return merged
}

func mergeThemeSetting(base, overlay any) any {
	if text, ok := base.(string); ok && strings.TrimSpace(text) != "" {
		return base
	}
	return overlay
}

func mergePackageLists(base, overlay any) any {
	baseList, baseOK := packageList(base)
	overlayList, overlayOK := packageList(overlay)
	if !baseOK || !overlayOK {
		return overlay
	}
	merged := make([]any, 0, len(baseList)+len(overlayList))
	seen := make(map[string]struct{}, len(baseList)+len(overlayList))
	for _, entry := range baseList {
		merged = append(merged, entry)
		if key, ok := packageEntryKey(entry); ok {
			seen[key] = struct{}{}
		}
	}
	for _, entry := range overlayList {
		key, ok := packageEntryKey(entry)
		if ok {
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
		}
		merged = append(merged, entry)
	}
	return merged
}

func packageList(value any) ([]any, bool) {
	list, ok := value.([]any)
	return list, ok
}

func packageEntryKey(value any) (string, bool) {
	if text, ok := value.(string); ok {
		text = strings.TrimSpace(text)
		if text == "" {
			return "", false
		}
		return "str:" + text, true
	}
	encoded, err := json.Marshal(value)
	if err != nil || string(encoded) == "null" {
		return "", false
	}
	return "json:" + string(encoded), true
}

func buildManagedFileRecords(files []renderedPiFile, contents map[string][]byte) []ManagedFileRecord {
	records := make([]ManagedFileRecord, 0, len(files))
	for _, file := range files {
		content, ok := contents[file.relativePath]
		if !ok {
			continue
		}
		records = append(records, ManagedFileRecord{
			Path:        file.absolutePath,
			Component:   file.component,
			MergeMode:   file.mergeMode,
			ContentHash: contentHash(content),
		})
	}
	return records
}

func applyManagedAgentOverlays(layout PiLayout, req PiInstallRequest, existing Manifest, backupRoot string) ([]ManagedAgentOverlayRecord, InstallSummary, error) {
	renderRequest, err := req.renderRequest()
	if err != nil {
		return nil, InstallSummary{}, err
	}
	renderRequest.SettingsPath = layout.SettingsPath
	registry, err := NewRegistry(defaultPiAdapter())
	if err != nil {
		return nil, InstallSummary{}, err
	}
	adapter, err := registry.Resolve(renderRequest.Target)
	if err != nil {
		return nil, InstallSummary{}, err
	}
	rendered, err := adapter.RenderManagedAgents(context.Background(), renderRequest)
	if err != nil {
		return nil, InstallSummary{}, err
	}
	managedPaths := make(map[string]struct{}, len(existing.ManagedAgentOverlays))
	for _, overlay := range existing.ManagedAgentOverlays {
		managedPaths[filepath.Clean(overlay.Path)] = struct{}{}
	}
	renderedPaths := make(map[string]struct{}, len(rendered))
	records := make([]ManagedAgentOverlayRecord, 0, len(rendered))
	summary := InstallSummary{}
	for _, file := range rendered {
		absolutePath := absolutePiPath(layout, file.RelativePath)
		renderedPaths[filepath.Clean(absolutePath)] = struct{}{}
		if _, err := os.ReadFile(absolutePath); err == nil {
			if _, managed := managedPaths[filepath.Clean(absolutePath)]; !managed {
				summary.Conflicted = append(summary.Conflicted, file.RelativePath)
				continue
			}
		} else if !os.IsNotExist(err) {
			return nil, InstallSummary{}, fmt.Errorf("read managed overlay candidate %s: %w", file.RelativePath, err)
		}
		renderedFile := renderedPiFile{component: file.Component, relativePath: file.RelativePath, absolutePath: absolutePath, content: file.Content, mergeMode: file.MergeMode}
		finalContent, action, err := applyRenderedFile(renderedFile, backupRoot)
		if err != nil {
			return nil, InstallSummary{}, fmt.Errorf("apply managed overlay %s: %w", file.RelativePath, err)
		}
		switch action {
		case "create":
			summary.Created = append(summary.Created, file.RelativePath)
		case "update":
			summary.Updated = append(summary.Updated, file.RelativePath)
			if _, err := os.Stat(filepath.Join(backupRoot, file.RelativePath)); err == nil {
				summary.BackedUp = append(summary.BackedUp, file.RelativePath)
			}
		case "unchanged":
			summary.Unchanged = append(summary.Unchanged, file.RelativePath)
		}
		records = append(records, ManagedAgentOverlayRecord{AgentName: strings.TrimSuffix(strings.TrimPrefix(filepath.Base(file.RelativePath), req.runtimeContractOrDefault().AgentResolution.ManagedFilenamePrefix), ".md"), Path: absolutePath, ContentHash: contentHash(finalContent)})
	}
	for _, overlay := range existing.ManagedAgentOverlays {
		cleanPath := filepath.Clean(overlay.Path)
		if _, keep := renderedPaths[cleanPath]; keep {
			continue
		}
		relativePath, err := filepath.Rel(layout.AgentDir, overlay.Path)
		if err != nil {
			return nil, InstallSummary{}, fmt.Errorf("resolve managed overlay cleanup path %s: %w", overlay.Path, err)
		}
		state, err := applyManagedDelete(ManagedFileAction{
			RelativePath: filepath.ToSlash(relativePath),
			AbsolutePath: overlay.Path,
			Action:       "delete",
			BackupPath:   filepath.Join(backupRoot, filepath.ToSlash(relativePath)),
		})
		if err != nil {
			return nil, InstallSummary{}, fmt.Errorf("delete stale managed overlay %s: %w", overlay.Path, err)
		}
		if state == "delete" {
			summary.Deleted = append(summary.Deleted, filepath.ToSlash(relativePath))
			summary.BackedUp = append(summary.BackedUp, filepath.ToSlash(relativePath))
		}
	}
	return records, summary, nil
}

func contentHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func validateManagedContents(contents map[string][]byte, req PiInstallRequest) []string {
	var findings []string
	// lore-memory assets are optional; they are only present when pi-extensions is explicitly selected.
	// The default hosted MCP install does not include them. Only validate lore-memory when present.

	// Validate lore-memory.ts (index 0) only when present.
	memoryPath := managedPiExtensionRelativePaths[0]
	memoryContent, memoryPresent := contents[memoryPath]
	if memoryPresent {
		text := string(memoryContent)
		if strings.TrimSpace(req.SavedToken) != "" && strings.Contains(text, req.SavedToken) {
			findings = append(findings, fmt.Sprintf("%s contains saved auth material", memoryPath))
		}
		if !strings.Contains(text, "\"api\", \"request\"") {
			findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", memoryPath, "\"api\", \"request\""))
		}
		if !strings.Contains(text, "export default function") {
			findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", memoryPath, "export default function"))
		}
		if !strings.Contains(text, "Text") {
			findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", memoryPath, "Text"))
		}
		if !strings.Contains(text, "renderCall(") {
			findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", memoryPath, "renderCall("))
		}
		if !strings.Contains(text, "renderResult(") {
			findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", memoryPath, "renderResult("))
		}
		if !strings.Contains(text, "text: formatContent(payload.data)") {
			findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", memoryPath, "text: formatContent(payload.data)"))
		}
		if !strings.Contains(text, "pi.registerTool") {
			findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", memoryPath, "pi.registerTool"))
		}
		if !strings.Contains(text, "name: \"lore_search\"") {
			findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", memoryPath, "name: \"lore_search\""))
		}
		if !strings.Contains(text, "name: \"lore_save\"") {
			findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", memoryPath, "name: \"lore_save\""))
		}
		if !strings.Contains(text, "name: \"lore_get_observation\"") {
			findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", memoryPath, "name: \"lore_get_observation\""))
		}
		if !strings.Contains(text, "name: \"lore_context\"") {
			findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", memoryPath, "name: \"lore_context\""))
		}
		if !strings.Contains(text, "name: \"lore_project_list\"") {
			findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", memoryPath, "name: \"lore_project_list\""))
		}
		if !strings.Contains(text, "name: \"lore_project_create\"") {
			findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", memoryPath, "name: \"lore_project_create\""))
		}
		if !strings.Contains(text, "name: \"lore_project_get\"") {
			findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", memoryPath, "name: \"lore_project_get\""))
		}
		if !strings.Contains(text, "name: \"lore_skill_save\"") {
			findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", memoryPath, "name: \"lore_skill_save\""))
		}
		if !strings.Contains(text, "name: \"lore_skill_list\"") {
			findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", memoryPath, "name: \"lore_skill_list\""))
		}
		if !strings.Contains(text, "name: \"lore_skill_get\"") {
			findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", memoryPath, "name: \"lore_skill_get\""))
		}
		if !strings.Contains(text, "/v1/memories") {
			findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", memoryPath, "/v1/memories"))
		}
		if !strings.Contains(text, "/v1/projects") {
			findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", memoryPath, "/v1/projects"))
		}
		if !strings.Contains(text, "/v1/skills") {
			findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", memoryPath, "/v1/skills"))
		}
		// Check forbidden legacy snippets for lore-memory.ts
		if strings.Contains(text, "name: \"lore_update\"") {
			findings = append(findings, fmt.Sprintf("%s contains forbidden legacy memory contract snippet %q", memoryPath, "name: \"lore_update\""))
		}
		if strings.Contains(text, "name: \"lore_delete\"") {
			findings = append(findings, fmt.Sprintf("%s contains forbidden legacy memory contract snippet %q", memoryPath, "name: \"lore_delete\""))
		}
		if strings.Contains(text, "name: \"lore_timeline\"") {
			findings = append(findings, fmt.Sprintf("%s contains forbidden legacy memory contract snippet %q", memoryPath, "name: \"lore_timeline\""))
		}
		if strings.Contains(text, "name: \"lore_stats\"") {
			findings = append(findings, fmt.Sprintf("%s contains forbidden legacy memory contract snippet %q", memoryPath, "name: \"lore_stats\""))
		}
		if strings.Contains(text, "name: \"lore_session_summary\"") {
			findings = append(findings, fmt.Sprintf("%s contains forbidden legacy memory contract snippet %q", memoryPath, "name: \"lore_session_summary\""))
		}
		if strings.Contains(text, "unsupportedLegacyTool") {
			findings = append(findings, fmt.Sprintf("%s contains forbidden legacy memory contract snippet %q", memoryPath, "unsupportedLegacyTool"))
		}
		if strings.Contains(text, "/v1/search") {
			findings = append(findings, fmt.Sprintf("%s contains forbidden legacy memory contract snippet %q", memoryPath, "/v1/search"))
		}
		if strings.Contains(text, "/v1/observations") {
			findings = append(findings, fmt.Sprintf("%s contains forbidden legacy memory contract snippet %q", memoryPath, "/v1/observations"))
		}
		if strings.Contains(text, "/v1/context") {
			findings = append(findings, fmt.Sprintf("%s contains forbidden legacy memory contract snippet %q", memoryPath, "/v1/context"))
		}
		if strings.Contains(text, "/v1/stats") {
			findings = append(findings, fmt.Sprintf("%s contains forbidden legacy memory contract snippet %q", memoryPath, "/v1/stats"))
		}
		if strings.Contains(text, "/v1/timeline") {
			findings = append(findings, fmt.Sprintf("%s contains forbidden legacy memory contract snippet %q", memoryPath, "/v1/timeline"))
		}
		if strings.Contains(text, "/v1/sessions") {
			findings = append(findings, fmt.Sprintf("%s contains forbidden legacy memory contract snippet %q", memoryPath, "/v1/sessions"))
		}
	}

	// Validate lore-footer.ts (index 1) only when present.
	footerPath := managedPiExtensionRelativePaths[1]
	footerContent, footerPresent := contents[footerPath]
	if footerPresent {
		footerText := string(footerContent)
		if !strings.Contains(footerText, "export default function") {
			findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", footerPath, "export default function"))
		}
		if !strings.Contains(footerText, "ctx.ui.setFooter") {
			findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", footerPath, "ctx.ui.setFooter"))
		}
		if !strings.Contains(footerText, "getContextUsage") {
			findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", footerPath, "getContextUsage"))
		}
		if !strings.Contains(footerText, "getExtensionStatuses") {
			findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", footerPath, "getExtensionStatuses"))
		}
	}
	// Validate mcp.json if present (hosted MCP default component).
	mcpContent, ok := contents[managedMCPConfigRelativePath]
	if ok {
		mcpText := string(mcpContent)
		// Hosted MCP uses HTTP endpoint config (url + Authorization) — not stdio command.
		hasLoreName := strings.Contains(mcpText, "lore")
		hasURL := strings.Contains(mcpText, "url")
		hasAuth := strings.Contains(mcpText, "Authorization") || strings.Contains(mcpText, "headers")
		if !hasLoreName || !hasURL || !hasAuth {
			findings = append(findings, fmt.Sprintf("%s missing hosted MCP configuration", managedMCPConfigRelativePath))
		}
	}
	return findings
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := chmodWithBestEffort(runtime.GOOS, "chmod temp file", func() error {
		return tmp.Chmod(mode)
	}); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace file: %w", err)
	}
	if err := chmodWithBestEffort(runtime.GOOS, "chmod file", func() error {
		return os.Chmod(path, mode)
	}); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func chmodWithBestEffort(goos, context string, apply func() error) error {
	if err := apply(); err != nil {
		if goos == "windows" {
			return nil
		}
		return fmt.Errorf("%s: %w", context, err)
	}
	return nil
}
