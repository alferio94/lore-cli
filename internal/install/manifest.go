package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ManagedFileRecord struct {
	Path        string      `json:"path"`
	Component   ComponentID `json:"component"`
	MergeMode   MergeMode   `json:"merge_mode"`
	ContentHash string      `json:"content_hash"`
}

type ManagedAgentOverlayRecord struct {
	AgentName   string `json:"agent_name"`
	Path        string `json:"path"`
	ContentHash string `json:"content_hash"`
}

type Manifest struct {
	SchemaVersion        string                      `json:"schema_version"`
	Target               TargetID                    `json:"target"`
	AuthMode             string                      `json:"auth_mode"`
	ServerURL            string                      `json:"server_url"`
	LoreBinary           string                      `json:"lore_binary_path"`
	LoreConfigDir        string                      `json:"lore_config_dir"`
	Components           []ComponentID               `json:"components,omitempty"`
	ManagedFiles         []ManagedFileRecord         `json:"managed_files"`
	ManagedAgentOverlays []ManagedAgentOverlayRecord `json:"managed_agent_overlays,omitempty"`
	BackupRoot           string                      `json:"backup_root"`
	InstalledAt          string                      `json:"installed_at"`
	CLIVersion           string                      `json:"lore_cli_version"`
	FullPiBackup         *FullPiBackupResult         `json:"full_pi_backup,omitempty"`
}

type legacyManifest struct {
	SchemaVersion string              `json:"schema_version"`
	Target        string              `json:"target"`
	AuthMode      string              `json:"auth_mode"`
	ServerURL     string              `json:"server_url"`
	LoreBinary    string              `json:"lore_binary_path"`
	LoreConfigDir string              `json:"lore_config_dir"`
	ManagedFiles  []string            `json:"managed_files"`
	BackupRoot    string              `json:"backup_root"`
	InstalledAt   string              `json:"installed_at"`
	CLIVersion    string              `json:"lore_cli_version"`
	FullPiBackup  *FullPiBackupResult `json:"full_pi_backup,omitempty"`
}

func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var raw struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if raw.SchemaVersion == LegacyPiManifestSchemaVersion {
		var legacy legacyManifest
		if err := json.Unmarshal(data, &legacy); err != nil {
			return Manifest{}, fmt.Errorf("decode manifest: %w", err)
		}
		return upgradeLegacyManifest(legacy), nil
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	return manifest, nil
}

func marshalManifest(manifest Manifest) ([]byte, error) {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode manifest: %w", err)
	}
	return append(data, '\n'), nil
}

func upgradeLegacyManifest(legacy legacyManifest) Manifest {
	components := []ComponentID{ComponentCorePack, ComponentPiExtensions}
	records := make([]ManagedFileRecord, 0, len(legacy.ManagedFiles))
	for _, path := range legacy.ManagedFiles {
		if filepath.Base(path) == filepath.Base(legacyPiDelegationRelativePath) {
			continue
		}
		component := ComponentPiExtensions
		mergeMode := MergeModeReplace
		if filepath.Base(path) == "settings.json" {
			component = ComponentCorePack
			mergeMode = MergeModeAdditiveJSON
		}
		records = append(records, ManagedFileRecord{
			Path:        path,
			Component:   component,
			MergeMode:   mergeMode,
			ContentHash: contentHash([]byte(path)),
		})
	}
	return Manifest{
		SchemaVersion: PortableManifestSchemaVersion,
		Target:        TargetID(strings.TrimSpace(legacy.Target)),
		AuthMode:      legacy.AuthMode,
		ServerURL:     legacy.ServerURL,
		LoreBinary:    legacy.LoreBinary,
		LoreConfigDir: legacy.LoreConfigDir,
		Components:    components,
		ManagedFiles:  records,
		BackupRoot:    legacy.BackupRoot,
		InstalledAt:   legacy.InstalledAt,
		CLIVersion:    legacy.CLIVersion,
		FullPiBackup:  legacy.FullPiBackup,
	}
}

func (m Manifest) Validate(layout PiLayout) error {
	if err := m.ValidateForLayout(layout.HarnessLayout(), layout.ManagedFiles, filepath.Join(layout.AgentDir, "backups")); err != nil {
		return err
	}
	if m.FullPiBackup != nil {
		if filepath.Clean(m.FullPiBackup.SourcePath) != filepath.Clean(layout.PiDir) {
			return fmt.Errorf("full_pi_backup.source_path = %q, want %q", m.FullPiBackup.SourcePath, layout.PiDir)
		}
	}
	return nil
}

func (m Manifest) ValidateForLayout(layout HarnessLayout, managedFiles []string, backupRootDir string) error {
	if m.SchemaVersion != PortableManifestSchemaVersion {
		return fmt.Errorf("schema_version = %q, want %q", m.SchemaVersion, PortableManifestSchemaVersion)
	}
	if m.Target != layout.Target {
		return fmt.Errorf("target = %q, want %q", m.Target, layout.Target)
	}
	switch m.AuthMode {
	case "cli-request", "config-only":
		// valid
	default:
		return fmt.Errorf("auth_mode = %q, want %q or %q", m.AuthMode, "cli-request", "config-only")
	}
	if m.AuthMode == "config-only" {
		// config-only targets validate ManagedFiles, BackupRoot, and InstalledAt.
		// These checks are the fail-closed guard for the Codex/config-only layout.
		if len(m.ManagedFiles) == 0 {
			return fmt.Errorf("managed_files are required")
		}
		for i, mf := range m.ManagedFiles {
			if strings.TrimSpace(mf.Path) == "" {
				return fmt.Errorf("managed_files[%d].path is required", i)
			}
			if mf.Component == "" {
				return fmt.Errorf("managed_files[%d].component is required", i)
			}
			if mf.MergeMode == "" {
				return fmt.Errorf("managed_files[%d].merge_mode is required", i)
			}
		}
		backupPrefix := filepath.Clean(backupRootDir) + string(os.PathSeparator)
		if !strings.HasPrefix(filepath.Clean(m.BackupRoot), backupPrefix) {
			return fmt.Errorf("backup_root = %q, want path under %q", m.BackupRoot, backupRootDir)
		}
		if _, err := time.Parse(time.RFC3339, m.InstalledAt); err != nil {
			return fmt.Errorf("installed_at: %w", err)
		}
		return nil
	}
	// cli-request targets require full auth info
	if strings.TrimSpace(m.ServerURL) == "" {
		return fmt.Errorf("server_url is required")
	}
	if strings.TrimSpace(m.LoreBinary) == "" {
		return fmt.Errorf("lore_binary_path is required")
	}
	if strings.TrimSpace(m.LoreConfigDir) == "" {
		return fmt.Errorf("lore_config_dir is required")
	}
	if len(m.Components) == 0 {
		return fmt.Errorf("components are required")
	}
	if len(m.ManagedFiles) != len(managedFiles) {
		return fmt.Errorf("managed_files length = %d, want %d", len(m.ManagedFiles), len(managedFiles))
	}
	for i, want := range managedFiles {
		got := m.ManagedFiles[i]
		if filepath.Clean(got.Path) != filepath.Clean(want) {
			return fmt.Errorf("managed_files[%d].path = %q, want %q", i, got.Path, want)
		}
		if got.Component == "" {
			return fmt.Errorf("managed_files[%d].component is required", i)
		}
		if got.MergeMode == "" {
			return fmt.Errorf("managed_files[%d].merge_mode is required", i)
		}
		if strings.TrimSpace(got.ContentHash) == "" && m.SchemaVersion == PortableManifestSchemaVersion {
			return fmt.Errorf("managed_files[%d].content_hash is required", i)
		}
	}
	backupPrefix := filepath.Clean(backupRootDir) + string(os.PathSeparator)
	if !strings.HasPrefix(filepath.Clean(m.BackupRoot), backupPrefix) {
		return fmt.Errorf("backup_root = %q, want path under %q", m.BackupRoot, backupRootDir)
	}
	if _, err := time.Parse(time.RFC3339, m.InstalledAt); err != nil {
		return fmt.Errorf("installed_at: %w", err)
	}
	for i, overlay := range m.ManagedAgentOverlays {
		if strings.TrimSpace(overlay.AgentName) == "" {
			return fmt.Errorf("managed_agent_overlays[%d].agent_name is required", i)
		}
		if strings.TrimSpace(overlay.Path) == "" {
			return fmt.Errorf("managed_agent_overlays[%d].path is required", i)
		}
		if strings.TrimSpace(overlay.ContentHash) == "" {
			return fmt.Errorf("managed_agent_overlays[%d].content_hash is required", i)
		}
	}
	if m.FullPiBackup != nil {
		if filepath.Clean(m.FullPiBackup.ManifestPath) != filepath.Clean(filepath.Join(m.FullPiBackup.BackupPath, "lore-pi-backup.json")) {
			return fmt.Errorf("full_pi_backup.manifest_path = %q, want path under backup directory", m.FullPiBackup.ManifestPath)
		}
		if _, err := time.Parse(time.RFC3339, m.FullPiBackup.CreatedAt); err != nil {
			return fmt.Errorf("full_pi_backup.created_at: %w", err)
		}
	}
	return nil
}
