package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Manifest struct {
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

func (m Manifest) Validate(layout PiLayout) error {
	if m.SchemaVersion != "1" {
		return fmt.Errorf("schema_version = %q, want %q", m.SchemaVersion, "1")
	}
	if m.Target != string(TargetPi) {
		return fmt.Errorf("target = %q, want %q", m.Target, TargetPi)
	}
	if m.AuthMode != "cli-request" {
		return fmt.Errorf("auth_mode = %q, want %q", m.AuthMode, "cli-request")
	}
	if strings.TrimSpace(m.ServerURL) == "" {
		return fmt.Errorf("server_url is required")
	}
	if strings.TrimSpace(m.LoreBinary) == "" {
		return fmt.Errorf("lore_binary_path is required")
	}
	if strings.TrimSpace(m.LoreConfigDir) == "" {
		return fmt.Errorf("lore_config_dir is required")
	}
	if len(m.ManagedFiles) != len(layout.ManagedFiles) {
		return fmt.Errorf("managed_files length = %d, want %d", len(m.ManagedFiles), len(layout.ManagedFiles))
	}
	for i, want := range layout.ManagedFiles {
		if got := m.ManagedFiles[i]; got != want {
			return fmt.Errorf("managed_files[%d] = %q, want %q", i, got, want)
		}
	}
	backupPrefix := filepath.Join(layout.AgentDir, "backups") + string(os.PathSeparator)
	if !strings.HasPrefix(filepath.Clean(m.BackupRoot), backupPrefix) {
		return fmt.Errorf("backup_root = %q, want path under %q", m.BackupRoot, filepath.Join(layout.AgentDir, "backups"))
	}
	if _, err := time.Parse(time.RFC3339, m.InstalledAt); err != nil {
		return fmt.Errorf("installed_at: %w", err)
	}
	if m.FullPiBackup != nil {
		if filepath.Clean(m.FullPiBackup.SourcePath) != filepath.Clean(layout.PiDir) {
			return fmt.Errorf("full_pi_backup.source_path = %q, want %q", m.FullPiBackup.SourcePath, layout.PiDir)
		}
		if filepath.Clean(m.FullPiBackup.ManifestPath) != filepath.Clean(filepath.Join(m.FullPiBackup.BackupPath, "lore-pi-backup.json")) {
			return fmt.Errorf("full_pi_backup.manifest_path = %q, want path under backup directory", m.FullPiBackup.ManifestPath)
		}
		if _, err := time.Parse(time.RFC3339, m.FullPiBackup.CreatedAt); err != nil {
			return fmt.Errorf("full_pi_backup.created_at: %w", err)
		}
	}
	return nil
}
