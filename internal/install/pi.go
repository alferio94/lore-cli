package install

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed assets/pi/*
var piAssets embed.FS

type PiLayout struct {
	HomeDir       string
	PiDir         string
	AgentDir      string
	ExtensionsDir string
	SettingsPath  string
	ManifestPath  string
	ManagedFiles  []string
}

type PiInstallRequest struct {
	HomeDir        string
	ServerURL      string
	LoreBinaryPath string
	LoreConfigDir  string
	LoreCLIVersion string
	SavedToken     string
	Now            time.Time
}

type InstallSummary struct {
	Created   []string
	Updated   []string
	Unchanged []string
	BackedUp  []string
	Failed    []string
}

type PiInstallResult struct {
	Layout   PiLayout
	Manifest Manifest
	Summary  InstallSummary
}

type renderedPiFile struct {
	relativePath string
	absolutePath string
	content      []byte
	mergeJSON    bool
}

var managedPiExtensionRelativePaths = []string{
	filepath.Join("extensions", "lore-memory.ts"),
	filepath.Join("extensions", "lore-delegation.ts"),
	filepath.Join("extensions", "lore-footer.ts"),
}

func ResolvePiLayout(homeDir string) PiLayout {
	agentDir := filepath.Join(homeDir, ".pi", "agent")
	extensionsDir := filepath.Join(agentDir, "extensions")
	return PiLayout{
		HomeDir:       homeDir,
		PiDir:         filepath.Join(homeDir, ".pi"),
		AgentDir:      agentDir,
		ExtensionsDir: extensionsDir,
		SettingsPath:  filepath.Join(agentDir, "settings.json"),
		ManifestPath:  filepath.Join(agentDir, "lore-install.json"),
		ManagedFiles: []string{
			filepath.Join(extensionsDir, "lore-memory.ts"),
			filepath.Join(extensionsDir, "lore-delegation.ts"),
			filepath.Join(extensionsDir, "lore-footer.ts"),
			filepath.Join(agentDir, "settings.json"),
		},
	}
}

func (s Service) InstallPi(req PiInstallRequest) (PiInstallResult, error) {
	if strings.TrimSpace(req.HomeDir) == "" {
		return PiInstallResult{}, fmt.Errorf("home dir is required")
	}
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
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
	manifest := Manifest{
		SchemaVersion: "1",
		Target:        string(TargetPi),
		AuthMode:      "cli-request",
		ServerURL:     strings.TrimSpace(req.ServerURL),
		LoreBinary:    strings.TrimSpace(req.LoreBinaryPath),
		LoreConfigDir: strings.TrimSpace(req.LoreConfigDir),
		ManagedFiles:  append([]string(nil), layout.ManagedFiles...),
		BackupRoot:    backupRoot,
		InstalledAt:   req.Now.UTC().Format(time.RFC3339),
		CLIVersion:    strings.TrimSpace(req.LoreCLIVersion),
	}
	result := PiInstallResult{Layout: layout, Manifest: manifest}

	if err := os.MkdirAll(layout.ExtensionsDir, 0o755); err != nil {
		return PiInstallResult{}, fmt.Errorf("create Pi extensions dir: %w", err)
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
		case "created":
			result.Summary.Created = append(result.Summary.Created, file.relativePath)
		case "updated":
			result.Summary.Updated = append(result.Summary.Updated, file.relativePath)
		case "unchanged":
			result.Summary.Unchanged = append(result.Summary.Unchanged, file.relativePath)
		}
		if state == "updated" {
			if _, err := os.Stat(filepath.Join(backupRoot, file.relativePath)); err == nil {
				result.Summary.BackedUp = append(result.Summary.BackedUp, file.relativePath)
			}
		}
	}

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return PiInstallResult{}, fmt.Errorf("encode manifest: %w", err)
	}
	manifestBytes = append(manifestBytes, '\n')
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
	replacements := map[string]string{
		"{{LORE_SERVER_URL}}":    strings.TrimSpace(req.ServerURL),
		"{{LORE_BINARY_PATH}}":   strings.TrimSpace(req.LoreBinaryPath),
		"{{LORE_CONFIG_DIR}}":    strings.TrimSpace(req.LoreConfigDir),
		"{{LORE_CLI_VERSION}}":   strings.TrimSpace(req.LoreCLIVersion),
		"{{LORE_SETTINGS_PATH}}": layout.SettingsPath,
	}
	files := []struct {
		assetPath    string
		relativePath string
		absolutePath string
		mergeJSON    bool
	}{
		{assetPath: "assets/pi/lore-memory.ts", relativePath: managedPiExtensionRelativePaths[0], absolutePath: filepath.Join(layout.ExtensionsDir, "lore-memory.ts")},
		{assetPath: "assets/pi/lore-delegation.ts", relativePath: managedPiExtensionRelativePaths[1], absolutePath: filepath.Join(layout.ExtensionsDir, "lore-delegation.ts")},
		{assetPath: "assets/pi/lore-footer.ts", relativePath: managedPiExtensionRelativePaths[2], absolutePath: filepath.Join(layout.ExtensionsDir, "lore-footer.ts")},
		{assetPath: "assets/pi/settings.json", relativePath: "settings.json", absolutePath: layout.SettingsPath, mergeJSON: true},
	}

	rendered := make([]renderedPiFile, 0, len(files))
	for _, file := range files {
		content, err := piAssets.ReadFile(file.assetPath)
		if err != nil {
			return nil, fmt.Errorf("read asset %s: %w", file.assetPath, err)
		}
		resolved := string(content)
		for placeholder, value := range replacements {
			resolved = strings.ReplaceAll(resolved, placeholder, value)
		}
		rendered = append(rendered, renderedPiFile{relativePath: file.relativePath, absolutePath: file.absolutePath, content: []byte(resolved), mergeJSON: file.mergeJSON})
	}
	return rendered, nil
}

func validateRenderedPiFiles(files []renderedPiFile) error {
	byPath := make(map[string]renderedPiFile, len(files))
	for _, file := range files {
		byPath[file.relativePath] = file
	}
	for _, relativePath := range managedPiExtensionRelativePaths {
		file, ok := byPath[relativePath]
		if !ok {
			return fmt.Errorf("validate rendered Pi assets: %s missing", relativePath)
		}
		if !strings.Contains(string(file.content), "export default function") {
			return fmt.Errorf("validate rendered Pi assets: %s missing documented export default function factory", relativePath)
		}
	}
	return nil
}

func applyRenderedFile(file renderedPiFile, backupRoot string) ([]byte, string, error) {
	desired := file.content
	existing, err := os.ReadFile(file.absolutePath)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return nil, "", fmt.Errorf("read existing file: %w", err)
	}
	if file.mergeJSON {
		desired, err = mergeJSONAdditive(existing, desired)
		if err != nil {
			return nil, "", err
		}
	}
	if exists && string(existing) == string(desired) {
		return desired, "unchanged", nil
	}
	if exists {
		backupPath := filepath.Join(backupRoot, file.relativePath)
		if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
			return nil, "", fmt.Errorf("create backup dir: %w", err)
		}
		if err := writeFileAtomic(backupPath, existing, 0o600); err != nil {
			return nil, "", fmt.Errorf("write backup: %w", err)
		}
	}
	if err := writeFileAtomic(file.absolutePath, desired, 0o600); err != nil {
		return nil, "", fmt.Errorf("write managed file: %w", err)
	}
	if exists {
		return desired, "updated", nil
	}
	return desired, "created", nil
}

func mergeJSONAdditive(existing, desired []byte) ([]byte, error) {
	base := map[string]any{}
	if len(strings.TrimSpace(string(existing))) > 0 {
		if err := json.Unmarshal(existing, &base); err != nil {
			return nil, fmt.Errorf("decode existing settings.json: %w", err)
		}
	}
	overlay := map[string]any{}
	if err := json.Unmarshal(desired, &overlay); err != nil {
		return nil, fmt.Errorf("decode rendered settings.json: %w", err)
	}
	merged := mergeMaps(base, overlay)
	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode merged settings.json: %w", err)
	}
	return append(data, '\n'), nil
}

func mergeMaps(base, overlay map[string]any) map[string]any {
	merged := make(map[string]any, len(base)+len(overlay))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range overlay {
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

func validateManagedContents(contents map[string][]byte, req PiInstallRequest) []string {
	var findings []string
	requiredSnippets := map[string][]string{
		managedPiExtensionRelativePaths[0]: {
			"\"api\", \"request\"",
			"\"api\", \"mcp-call\"",
			"lore_project_context",
			"export default function",
			"Text",
			"renderCall(",
			"renderResult(",
			"text: formatContent(payload.data)",
			"pi.registerTool",
			"name: \"lore_search\"",
			"name: \"lore_save\"",
			"name: \"lore_get_observation\"",
			"name: \"lore_context\"",
			"name: \"lore_project_list\"",
			"name: \"lore_project_create\"",
			"name: \"lore_project_get\"",
			"name: \"lore_skill_save\"",
			"name: \"lore_skill_list\"",
			"name: \"lore_skill_get\"",
			"/v1/memories",
			"/v1/projects",
			"/v1/skills",
		},
		managedPiExtensionRelativePaths[1]: {"export default function"},
		managedPiExtensionRelativePaths[2]: {"export default function", "ctx.ui.setFooter", "getContextUsage", "getExtensionStatuses"},
	}
	forbiddenSnippets := map[string][]string{
		managedPiExtensionRelativePaths[0]: {
			"name: \"lore_update\"",
			"name: \"lore_delete\"",
			"name: \"lore_timeline\"",
			"name: \"lore_stats\"",
			"name: \"lore_session_summary\"",
			"unsupportedLegacyTool",
			"/v1/search",
			"/v1/observations",
			"/v1/context",
			"/v1/stats",
			"/v1/timeline",
			"/v1/sessions",
		},
	}
	pathsToValidate := append(append([]string(nil), managedPiExtensionRelativePaths...), "settings.json")
	for _, relativePath := range pathsToValidate {
		content, ok := contents[relativePath]
		if !ok {
			findings = append(findings, fmt.Sprintf("%s missing after install", relativePath))
			continue
		}
		text := string(content)
		if strings.TrimSpace(req.SavedToken) != "" && strings.Contains(text, req.SavedToken) {
			findings = append(findings, fmt.Sprintf("%s contains saved auth material", relativePath))
		}
		if strings.Contains(relativePath, ".ts") {
			if relativePath == managedPiExtensionRelativePaths[0] && strings.TrimSpace(req.ServerURL) != "" && !strings.Contains(text, req.ServerURL) {
				findings = append(findings, fmt.Sprintf("%s missing server URL %q", relativePath, req.ServerURL))
			}
			for _, snippet := range requiredSnippets[relativePath] {
				if !strings.Contains(text, snippet) {
					findings = append(findings, fmt.Sprintf("%s missing required contract snippet %q", relativePath, snippet))
				}
			}
			for _, snippet := range forbiddenSnippets[relativePath] {
				if strings.Contains(text, snippet) {
					findings = append(findings, fmt.Sprintf("%s contains forbidden legacy memory contract snippet %q", relativePath, snippet))
				}
			}
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
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp file: %w", err)
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
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod file: %w", err)
	}
	cleanup = false
	return nil
}
