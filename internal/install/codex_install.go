package install

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alferio94/lore-cli/internal/agentconfig"
	"github.com/alferio94/lore-cli/internal/agentpack"
)

// PlanCodexInstall creates an install plan for Codex target.
func (s Service) PlanCodexInstall(req InstallRequest) (InstallPlan, error) {
	req.Target = TargetCodex
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	}
	components, err := NormalizeComponentSelection(TargetCodex, req.Components)
	if err != nil {
		return InstallPlan{}, err
	}
	req.Components = components
	if err := req.Validate(); err != nil {
		return InstallPlan{}, err
	}

	// Ensure agent-config.json exists, then load it so custom models drive rendering.
	var agentCfg agentconfig.Config
	if s.AgentConfigStore != nil {
		_, _, err = s.AgentConfigStore.EnsureDefault()
		if err != nil {
			return InstallPlan{}, fmt.Errorf("ensure agent-config: %w", err)
		}
		// Load persisted config to capture any custom model overrides.
		agentCfg, err = s.AgentConfigStore.Load()
		if err != nil {
			return InstallPlan{}, fmt.Errorf("load agent-config: %w", err)
		}
	}
	req.AgentConfig = agentCfg

	layout := ResolveCodexLayout(req.HomeDir)
	rendered, err := renderCodexFiles(req)
	if err != nil {
		return InstallPlan{}, err
	}
	backupRoot := filepath.Join(layout.RootDir, "backups", req.Now.UTC().Format("20060102T150405Z"))
	plannedFiles, _, managedPaths, err := planCodexManagedFileActions(layout, rendered, backupRoot)
	if err != nil {
		return InstallPlan{}, err
	}
	cleanupAction, err := planCodexLegacyPromptCleanup(layout, backupRoot)
	if err != nil {
		return InstallPlan{}, err
	}
	if cleanupAction != nil {
		plannedFiles = append(plannedFiles, *cleanupAction)
	}
	manifest, _, err := buildCodexManifest(layout, req, rendered)
	if err != nil {
		return InstallPlan{}, err
	}
	manifest.ManagedFiles = buildCodexManifestManagedFileRecords(rendered, managedPaths)
	manifestAction, err := planCodexManifestAction(layout.ManifestPath, backupRoot, manifest)
	if err != nil {
		return InstallPlan{}, err
	}
	plannedFiles = append(plannedFiles, manifestAction)
	return InstallPlan{Request: req, Layout: layout, Components: components, Files: plannedFiles}, nil
}

// ExecuteCodexInstall applies the Codex install plan.
func (s Service) ExecuteCodexInstall(plan InstallPlan, opts InstallCommandOptions) (InstallResult, error) {
	if plan.Layout.Target != TargetCodex {
		return InstallResult{}, fmt.Errorf("plan target %q is not codex", plan.Layout.Target)
	}
	if opts.DryRun {
		return InstallResult{Target: TargetCodex, Layout: plan.Layout}, nil
	}

	// Re-render to ensure we have the correct content.
	rendered, err := renderCodexFiles(plan.Request)
	if err != nil {
		return InstallResult{}, err
	}
	backupRoot := filepath.Join(plan.Layout.RootDir, "backups", plan.Request.Now.UTC().Format("20060102T150405Z"))
	plannedFiles, desiredContents, managedPaths, err := planCodexManagedFileActions(plan.Layout, rendered, backupRoot)
	if err != nil {
		return InstallResult{}, err
	}
	cleanupAction, err := planCodexLegacyPromptCleanup(plan.Layout, backupRoot)
	if err != nil {
		return InstallResult{}, err
	}
	if cleanupAction != nil {
		plannedFiles = append(plannedFiles, *cleanupAction)
	}
	manifest, _, err := buildCodexManifest(plan.Layout, plan.Request, rendered)
	if err != nil {
		return InstallResult{}, err
	}
	manifest.ManagedFiles = buildCodexManifestManagedFileRecords(rendered, managedPaths)
	manifestAction, err := planCodexManifestAction(plan.Layout.ManifestPath, backupRoot, manifest)
	if err != nil {
		return InstallResult{}, err
	}
	plannedFiles = append(plannedFiles, manifestAction)

	if err := validateSharedInstallResultAgainstPlan(InstallPlan{Request: plan.Request, Layout: plan.Layout, Components: plan.Components, Files: plannedFiles}, InstallResult{Target: TargetCodex, Layout: plan.Layout, Summary: summarizePlannedActions(plannedFiles)}); err != nil {
		return InstallResult{}, err
	}

	result := InstallResult{Target: TargetCodex, Layout: plan.Layout}
	canonicalPromptApplied := false
	for _, file := range rendered {
		relativePath := filepath.ToSlash(file.RelativePath)
		desired := desiredContents[relativePath]
		action := lookupPlanFileAction(plannedFiles, relativePath)
		if err := applyCodexPlannedContent(action, desired); err != nil {
			result.Summary.Failed = append(result.Summary.Failed, fmt.Sprintf("%s: %v", relativePath, err))
			return result, fmt.Errorf("apply Codex %s: %w", relativePath, err)
		}
		appendInstallSummaryAction(&result.Summary, action.RelativePath, action.Action)
		if relativePath == "AGENTS.md" {
			canonicalPromptApplied = true
		}
	}
	if cleanupAction != nil {
		if !canonicalPromptApplied {
			err := fmt.Errorf("skipped legacy cleanup because AGENTS.md was not installed")
			result.Summary.Failed = append(result.Summary.Failed, fmt.Sprintf("%s: %v", cleanupAction.RelativePath, err))
			return result, fmt.Errorf("apply Codex %s: %w", cleanupAction.RelativePath, err)
		} else if err := applyCodexPlannedContent(*cleanupAction, nil); err != nil {
			result.Summary.Failed = append(result.Summary.Failed, fmt.Sprintf("%s: %v", cleanupAction.RelativePath, err))
			return result, fmt.Errorf("apply Codex %s: %w", cleanupAction.RelativePath, err)
		} else {
			appendInstallSummaryAction(&result.Summary, cleanupAction.RelativePath, cleanupAction.Action)
			if cleanupAction.BackupPath != "" {
				result.Summary.BackedUp = append(result.Summary.BackedUp, cleanupAction.RelativePath)
			}
		}
	}

	manifestBytes, err := marshalManifest(manifest)
	if err != nil {
		return InstallResult{}, err
	}
	if err := applyCodexPlannedContent(manifestAction, manifestBytes); err != nil {
		return InstallResult{}, err
	}
	appendInstallSummaryAction(&result.Summary, manifestAction.RelativePath, manifestAction.Action)

	// Validate manifest.
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

// renderCodexFiles renders all Codex target files via the adapter.
func renderCodexFiles(req InstallRequest) ([]RenderedFile, error) {
	registry, err := defaultInstallRegistry()
	if err != nil {
		return nil, err
	}
	adapter, err := registry.Resolve(TargetCodex)
	if err != nil {
		return nil, err
	}

	// Use AgentConfig from the request if populated (PlanCodexInstall already loaded it).
	// Otherwise, attempt to load from the store for callers that bypass PlanCodexInstall.
	agentCfg := req.AgentConfig
	if agentCfg.SchemaVersion == 0 {
		if store := getAgentConfigStoreForRender(req); store != nil {
			if cfg, err := store.Load(); err == nil {
				agentCfg = cfg
			}
		}
	}

	renderReq := RenderRequest{
		Target:         TargetCodex,
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
	return adapter.Render(context.Background(), renderReq)
}

// getAgentConfigStoreForRender returns an AgentConfigStore for the render request.
// This allows the render to access agent-config.json content.
var getAgentConfigStoreForRender func(InstallRequest) AgentConfigStore = func(req InstallRequest) AgentConfigStore {
	// Default implementation looks for store in request context.
	// Production code should inject the store via the Service.
	return nil
}

func planCodexManagedFileActions(layout HarnessLayout, rendered []RenderedFile, backupRoot string) ([]PlanFileAction, map[string][]byte, []string, error) {
	actions := make([]PlanFileAction, 0, len(rendered))
	desiredContents := make(map[string][]byte, len(rendered))
	managedPaths := make([]string, 0, len(rendered))
	for _, file := range rendered {
		desired, action, err := planCodexRenderedFileAction(layout, file, backupRoot)
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

func planCodexRenderedFileAction(layout HarnessLayout, file RenderedFile, backupRoot string) ([]byte, PlanFileAction, error) {
	absolutePath := codexAbsolutePath(layout, file.RelativePath)
	desired := file.Content
	existing, err := os.ReadFile(absolutePath)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return nil, PlanFileAction{}, fmt.Errorf("read existing file: %w", err)
	}
	if filepath.ToSlash(file.RelativePath) == codexConfigTomlRelativePath {
		desired, err = mergeCodexMCPConfig(existing, desired, isCodexConfigTomlManaged(layout, absolutePath, existing))
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
		action.BackupPath = filepath.Join(backupRoot, codexBackupRelativePath(file.RelativePath))
		return desired, action, nil
	}
	action.Action = "create"
	return desired, action, nil
}

func planCodexLegacyPromptCleanup(layout HarnessLayout, backupRoot string) (*PlanFileAction, error) {
	legacyPath := filepath.Join(layout.RootDir, "agents.md")
	existing, err := os.ReadFile(legacyPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read legacy codex agents.md: %w", err)
	}
	if name, ok := actualCodexPromptEntryName(layout.RootDir, "agents.md"); ok && name != "agents.md" {
		return nil, nil
	}
	aliasRisk := aliasesCodexCanonicalPrompt(layout, legacyPath)
	if !isCodexLegacyPromptManaged(layout, legacyPath, existing) {
		if aliasRisk {
			return nil, fmt.Errorf("refusing to install Codex AGENTS.md because unowned legacy ~/.codex/agents.md exists on a case-insensitive filesystem; move or rename agents.md and rerun lore install")
		}
		return nil, nil
	}
	if aliasRisk {
		return nil, nil
	}
	return &PlanFileAction{
		Component:    ComponentCorePack,
		RelativePath: "agents.md",
		AbsolutePath: legacyPath,
		Action:       "delete",
		BackupPath:   filepath.Join(backupRoot, codexBackupRelativePath("agents.md")),
	}, nil
}

func actualCodexPromptEntryName(dir, want string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return preferCodexPromptEntryName(names, want)
}

func preferCodexPromptEntryName(names []string, want string) (string, bool) {
	for _, name := range names {
		if name == want {
			return name, true
		}
	}
	for _, name := range names {
		if strings.EqualFold(name, want) {
			return name, true
		}
	}
	return "", false
}

var aliasesCodexCanonicalPrompt = func(layout HarnessLayout, legacyPath string) bool {
	legacyInfo, legacyErr := os.Stat(legacyPath)
	canonicalInfo, canonicalErr := os.Stat(layout.Paths["agents_md"])
	if legacyErr != nil || canonicalErr != nil {
		return false
	}
	return os.SameFile(legacyInfo, canonicalInfo)
}

func isCodexLegacyPromptManaged(layout HarnessLayout, legacyPath string, existing []byte) bool {
	if manifest, err := LoadManifest(layout.ManifestPath); err == nil {
		for _, managed := range manifest.ManagedFiles {
			if filepath.Clean(managed.Path) == filepath.Clean(legacyPath) && managed.Component == ComponentCorePack {
				return true
			}
		}
	}
	text := string(existing)
	return strings.Contains(text, "This file is managed by `lore install --target codex`") && strings.Contains(text, "# Lore Configuration")
}

func isCodexConfigTomlManaged(layout HarnessLayout, configPath string, existing []byte) bool {
	if strings.Contains(string(existing), codexMCPBlockStartMarker) && strings.Contains(string(existing), codexMCPBlockEndMarker) {
		return true
	}
	if manifest, err := LoadManifest(layout.ManifestPath); err == nil {
		for _, managed := range manifest.ManagedFiles {
			if filepath.Clean(managed.Path) == filepath.Clean(configPath) && managed.Component == ComponentLoreServerMCP {
				return true
			}
		}
	}
	return false
}

func planCodexManifestAction(manifestPath, backupRoot string, manifest Manifest) (PlanFileAction, error) {
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

func buildCodexManifestManagedFileRecords(rendered []RenderedFile, managedPaths []string) []ManagedFileRecord {
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

func buildCodexManifest(layout HarnessLayout, req InstallRequest, files []RenderedFile) (Manifest, []string, error) {
	if layout.Target != TargetCodex {
		return Manifest{}, nil, fmt.Errorf("layout target %q does not match codex", layout.Target)
	}
	components, err := NormalizeComponentSelection(TargetCodex, req.Components)
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
		absolutePath := codexAbsolutePath(layout, file.RelativePath)
		managedPaths = append(managedPaths, absolutePath)
		records = append(records, ManagedFileRecord{
			Path:        absolutePath,
			Component:   file.Component,
			MergeMode:   file.MergeMode,
			ContentHash: contentHash(file.Content),
		})
	}
	manifest := Manifest{
		SchemaVersion: PortableManifestSchemaVersion,
		Target:        TargetCodex,
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

var applyCodexPlannedContent = func(action PlanFileAction, desired []byte) error {
	if action.Action == "unchanged" {
		return nil
	}
	if action.Action == "update" || action.Action == "delete" {
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
		if action.Action == "delete" {
			if err := os.Remove(action.AbsolutePath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("delete file: %w", err)
			}
			return nil
		}
	}
	return writeFileAtomic(action.AbsolutePath, desired, 0o600)
}

func codexBackupRelativePath(relativePath string) string {
	switch filepath.ToSlash(relativePath) {
	case "AGENTS.md":
		return "AGENTS.md"
	case "agents.md":
		return "legacy-agents.md"
	default:
		return filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(relativePath), "./"))
	}
}

func mergeCodexMCPConfig(existing, managed []byte, existingLoreOwned bool) ([]byte, error) {
	managedText := strings.TrimSpace(string(managed))
	if managedText == "" {
		return nil, fmt.Errorf("managed Codex MCP config block is required")
	}
	existingText := strings.ReplaceAll(string(existing), "\r\n", "\n")
	if strings.TrimSpace(existingText) == "" {
		return []byte(managedText + "\n"), nil
	}
	if strings.Contains(existingText, codexMCPBlockStartMarker) || strings.Contains(existingText, codexMCPBlockEndMarker) {
		start := strings.Index(existingText, codexMCPBlockStartMarker)
		end := strings.Index(existingText, codexMCPBlockEndMarker)
		if start < 0 || end < start {
			return nil, fmt.Errorf("existing config.toml contains an incomplete Lore-managed Codex MCP block")
		}
		end += len(codexMCPBlockEndMarker)
		prefix := strings.TrimRight(existingText[:start], "\n")
		suffix := strings.TrimLeft(existingText[end:], "\n")
		parts := make([]string, 0, 3)
		if strings.TrimSpace(prefix) != "" {
			parts = append(parts, prefix)
		}
		parts = append(parts, managedText)
		if strings.TrimSpace(suffix) != "" {
			parts = append(parts, suffix)
		}
		return []byte(strings.Join(parts, "\n\n") + "\n"), nil
	}
	if codexConfigHasLoreMCPBlock(existingText) && !existingLoreOwned {
		return nil, fmt.Errorf("refusing to overwrite unowned [mcp_servers.lore] block in ~/.codex/config.toml; remove it or rerun after a Lore-managed config.toml manifest exists")
	}

	stripped := existingText
	if existingLoreOwned {
		stripped = stripLegacyCodexLoreMCPBlock(existingText)
	}
	stripped = strings.TrimRight(stripped, "\n")
	if strings.TrimSpace(stripped) == "" {
		return []byte(managedText + "\n"), nil
	}
	return []byte(stripped + "\n\n" + managedText + "\n"), nil
}

func stripLegacyCodexLoreMCPBlock(existing string) string {
	lines := strings.Split(existing, "\n")
	kept := make([]string, 0, len(lines))
	skipping := false
	for _, line := range lines {
		if skipping {
			if isCodexLoreTableHeader(line) {
				continue
			}
			if isTOMLTableHeader(line) {
				skipping = false
			} else {
				continue
			}
		}
		if isCodexLoreTableHeader(line) {
			skipping = true
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func codexConfigHasLoreMCPBlock(existing string) bool {
	for _, line := range strings.Split(existing, "\n") {
		if isCodexLoreTableHeader(line) {
			return true
		}
	}
	return false
}

func isCodexLoreTableHeader(line string) bool {
	path, ok := parseTOMLTableHeaderPath(line)
	if !ok || len(path) < 2 || path[0] != "mcp_servers" || path[1] != "lore" {
		return false
	}
	if len(path) == 2 {
		return true
	}
	return len(path) == 3 && (path[2] == "headers" || path[2] == "http_headers")
}

func isTOMLTableHeader(line string) bool {
	_, ok := parseTOMLTableHeaderPath(line)
	return ok
}

func parseTOMLTableHeaderPath(line string) ([]string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "[") {
		return nil, false
	}
	arrayTable := strings.HasPrefix(trimmed, "[[")
	openLen := 1
	closeToken := "]"
	if arrayTable {
		openLen = 2
		closeToken = "]]"
	}
	closeAt := findTOMLHeaderClose(trimmed, openLen, closeToken)
	if closeAt < 0 {
		return nil, false
	}
	remainder := strings.TrimSpace(trimmed[closeAt+len(closeToken):])
	if remainder != "" && !strings.HasPrefix(remainder, "#") {
		return nil, false
	}
	return parseTOMLDottedKeyPath(trimmed[openLen:closeAt])
}

func findTOMLHeaderClose(line string, start int, closeToken string) int {
	quote := byte(0)
	escaped := false
	for i := start; i < len(line); i++ {
		ch := line[i]
		if quote != 0 {
			if quote == '"' && escaped {
				escaped = false
				continue
			}
			if quote == '"' && ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if strings.HasPrefix(line[i:], closeToken) {
			return i
		}
	}
	return -1
}

func parseTOMLDottedKeyPath(body string) ([]string, bool) {
	path := []string{}
	for i := 0; ; {
		i = skipTOMLSpaces(body, i)
		if i >= len(body) {
			return nil, false
		}
		var segment string
		var ok bool
		if body[i] == '"' || body[i] == '\'' {
			segment, i, ok = parseTOMLQuotedKey(body, i)
		} else {
			segment, i, ok = parseTOMLBareKey(body, i)
		}
		if !ok || segment == "" {
			return nil, false
		}
		path = append(path, segment)
		i = skipTOMLSpaces(body, i)
		if i >= len(body) {
			return path, true
		}
		if body[i] != '.' {
			return nil, false
		}
		i++
	}
}

func parseTOMLBareKey(body string, start int) (string, int, bool) {
	i := start
	for i < len(body) && isTOMLBareKeyChar(body[i]) {
		i++
	}
	if i == start {
		return "", start, false
	}
	return body[start:i], i, true
}

func isTOMLBareKeyChar(ch byte) bool {
	return ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-'
}

func parseTOMLQuotedKey(body string, start int) (string, int, bool) {
	quote := body[start]
	var b strings.Builder
	for i := start + 1; i < len(body); i++ {
		ch := body[i]
		if quote == '"' && ch == '\\' {
			if i+1 >= len(body) {
				return "", start, false
			}
			i++
			b.WriteByte(body[i])
			continue
		}
		if ch == quote {
			return b.String(), i + 1, true
		}
		b.WriteByte(ch)
	}
	return "", start, false
}

func skipTOMLSpaces(s string, start int) int {
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	return start
}
