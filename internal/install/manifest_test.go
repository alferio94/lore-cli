package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadManifestCurrentSchemaRoundTrip(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolvePiLayout(homeDir)
	manifest := validManifestForTest(layout)
	data, err := marshalManifest(manifest)
	if err != nil {
		t.Fatalf("marshalManifest() error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	if loaded.SchemaVersion != PortableManifestSchemaVersion || loaded.ManagedFiles[0].Path != manifest.ManagedFiles[0].Path {
		t.Fatalf("loaded manifest = %+v, want current-schema round trip", loaded)
	}
	for _, managed := range loaded.ManagedFiles {
		if strings.Contains(managed.Path, "lore-delegation.ts") {
			t.Fatalf("loaded manifest unexpectedly references legacy delegation: %+v", managed)
		}
	}
}

func TestLoadManifestRejectsInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	_, err := LoadManifest(path)
	if err == nil || !strings.Contains(err.Error(), "decode manifest") {
		t.Fatalf("LoadManifest() err = %v, want decode manifest error", err)
	}
}

func TestLoadManifestRejectsMissingFile(t *testing.T) {
	_, err := LoadManifest(filepath.Join(t.TempDir(), "missing.json"))
	if err == nil || !strings.Contains(err.Error(), "read manifest") {
		t.Fatalf("LoadManifest() err = %v, want read manifest error", err)
	}
}

func TestUpgradeLegacyManifestAssignsPortableComponentMetadata(t *testing.T) {
	layout := ResolvePiLayout(t.TempDir())
	legacy := legacyManifest{
		SchemaVersion: LegacyPiManifestSchemaVersion,
		Target:        string(TargetPi),
		AuthMode:      "cli-request",
		ServerURL:     "https://example.test",
		LoreBinary:    "/usr/local/bin/lore",
		LoreConfigDir: "/tmp/lore",
		ManagedFiles: []string{
			layout.ManagedFiles[0],
			filepath.Join(layout.ExtensionsDir, "lore-delegation.ts"),
			layout.ManagedFiles[1],
			layout.ManagedFiles[2],
		},
		BackupRoot:  filepath.Join(layout.AgentDir, "backups", "20260525T020304Z"),
		InstalledAt: time.Date(2026, 5, 25, 2, 3, 4, 0, time.UTC).Format(time.RFC3339),
		CLIVersion:  "dev",
	}

	manifest := upgradeLegacyManifest(legacy)
	if manifest.SchemaVersion != PortableManifestSchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", manifest.SchemaVersion, PortableManifestSchemaVersion)
	}
	if got := manifest.Components; len(got) != 2 || got[0] != ComponentCorePack || got[1] != ComponentPiExtensions {
		t.Fatalf("Components = %v, want default portable component set", got)
	}
	if len(manifest.ManagedFiles) != 3 {
		t.Fatalf("len(ManagedFiles) = %d, want 3 without legacy delegation", len(manifest.ManagedFiles))
	}
	if manifest.ManagedFiles[0].Component != ComponentPiExtensions || manifest.ManagedFiles[0].MergeMode != MergeModeReplace {
		t.Fatalf("managed lore-memory record = %+v, want pi-extensions/replace", manifest.ManagedFiles[0])
	}
	if manifest.ManagedFiles[1].Component != ComponentPiExtensions || manifest.ManagedFiles[1].MergeMode != MergeModeReplace {
		t.Fatalf("managed lore-footer record = %+v, want pi-extensions/replace", manifest.ManagedFiles[1])
	}
	if manifest.ManagedFiles[2].Component != ComponentCorePack || manifest.ManagedFiles[2].MergeMode != MergeModeAdditiveJSON {
		t.Fatalf("managed settings record = %+v, want core-pack/additive-json", manifest.ManagedFiles[2])
	}
	for _, managed := range manifest.ManagedFiles {
		if strings.Contains(managed.Path, "lore-delegation.ts") {
			t.Fatalf("legacy delegation path unexpectedly preserved in upgraded manifest: %+v", managed)
		}
	}
}

func TestManifestValidateRejectsInvalidPortableFields(t *testing.T) {
	homeDir := t.TempDir()
	layout := ResolvePiLayout(homeDir)
	base := validManifestForTest(layout)
	fullBackup := *base.FullPiBackup

	tests := []struct {
		name    string
		mutate  func(*Manifest)
		wantErr string
	}{
		{name: "wrong schema", mutate: func(m *Manifest) { m.SchemaVersion = LegacyPiManifestSchemaVersion }, wantErr: "schema_version"},
		{name: "wrong target", mutate: func(m *Manifest) { m.Target = TargetClaudeCode }, wantErr: "target"},
		{name: "wrong auth mode", mutate: func(m *Manifest) { m.AuthMode = "token" }, wantErr: "auth_mode"},
		{name: "missing components", mutate: func(m *Manifest) { m.Components = nil }, wantErr: "components are required"},
		{name: "managed path mismatch", mutate: func(m *Manifest) { m.ManagedFiles[0].Path = "settings-wrong.json" }, wantErr: "managed_files[0].path"},
		{name: "missing component", mutate: func(m *Manifest) { m.ManagedFiles[0].Component = "" }, wantErr: "managed_files[0].component is required"},
		{name: "missing merge mode", mutate: func(m *Manifest) { m.ManagedFiles[0].MergeMode = "" }, wantErr: "managed_files[0].merge_mode is required"},
		{name: "missing content hash", mutate: func(m *Manifest) { m.ManagedFiles[0].ContentHash = "" }, wantErr: "managed_files[0].content_hash is required"},
		{name: "backup outside pi backups", mutate: func(m *Manifest) { m.BackupRoot = filepath.Join(homeDir, "elsewhere") }, wantErr: "backup_root"},
		{name: "invalid installed at", mutate: func(m *Manifest) { m.InstalledAt = "not-a-time" }, wantErr: "installed_at"},
		{name: "full backup source mismatch", mutate: func(m *Manifest) {
			m.FullPiBackup = &FullPiBackupResult{SourcePath: filepath.Join(homeDir, "other-pi"), BackupPath: fullBackup.BackupPath, ManifestPath: fullBackup.ManifestPath, CreatedAt: fullBackup.CreatedAt}
		}, wantErr: "full_pi_backup.source_path"},
		{name: "full backup manifest mismatch", mutate: func(m *Manifest) {
			m.FullPiBackup = &FullPiBackupResult{SourcePath: fullBackup.SourcePath, BackupPath: fullBackup.BackupPath, ManifestPath: filepath.Join(homeDir, "wrong.json"), CreatedAt: fullBackup.CreatedAt}
		}, wantErr: "full_pi_backup.manifest_path"},
		{name: "full backup created at invalid", mutate: func(m *Manifest) {
			m.FullPiBackup = &FullPiBackupResult{SourcePath: fullBackup.SourcePath, BackupPath: fullBackup.BackupPath, ManifestPath: fullBackup.ManifestPath, CreatedAt: "bad-time"}
		}, wantErr: "full_pi_backup.created_at"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := base
			manifest.ManagedFiles = append([]ManagedFileRecord(nil), base.ManagedFiles...)
			manifest.Components = append([]ComponentID(nil), base.Components...)
			manifest.FullPiBackup = &FullPiBackupResult{SourcePath: fullBackup.SourcePath, BackupPath: fullBackup.BackupPath, ManifestPath: fullBackup.ManifestPath, CreatedAt: fullBackup.CreatedAt}
			tt.mutate(&manifest)
			err := manifest.Validate(layout)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() err = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func validManifestForTest(layout PiLayout) Manifest {
	installedAt := time.Date(2026, 5, 25, 2, 3, 4, 0, time.UTC).Format(time.RFC3339)
	backupRoot := filepath.Join(layout.AgentDir, "backups", "20260525T020304Z")
	fullBackupPath := filepath.Join(backupRoot, "full")
	return Manifest{
		SchemaVersion: PortableManifestSchemaVersion,
		Target:        TargetPi,
		AuthMode:      "cli-request",
		ServerURL:     "https://example.test",
		LoreBinary:    "/usr/local/bin/lore",
		LoreConfigDir: filepath.Join(layout.HomeDir, ".lore"),
		Components:    []ComponentID{ComponentCorePack, ComponentPiExtensions},
		ManagedFiles: []ManagedFileRecord{
			{Path: layout.ManagedFiles[0], Component: ComponentPiExtensions, MergeMode: MergeModeReplace, ContentHash: contentHash([]byte("memory"))},
			{Path: layout.ManagedFiles[1], Component: ComponentPiExtensions, MergeMode: MergeModeReplace, ContentHash: contentHash([]byte("footer"))},
			{Path: layout.ManagedFiles[2], Component: ComponentCorePack, MergeMode: MergeModeAdditiveJSON, ContentHash: contentHash([]byte("settings"))},
		},
		BackupRoot:  backupRoot,
		InstalledAt: installedAt,
		CLIVersion:  "dev",
		FullPiBackup: &FullPiBackupResult{
			SourcePath:   layout.PiDir,
			BackupPath:   fullBackupPath,
			ManifestPath: filepath.Join(fullBackupPath, "lore-pi-backup.json"),
			CreatedAt:    installedAt,
		},
	}
}

func TestMarshalManifestNeverReturnsInvalidJSONForPortableShape(t *testing.T) {
	layout := ResolvePiLayout(t.TempDir())
	if _, err := marshalManifest(validManifestForTest(layout)); err != nil {
		t.Fatalf("marshalManifest() error = %v, want nil", err)
	}
}

func TestLoadManifestCurrentSchemaTracksManagedAgentOverlays(t *testing.T) {
	layout := ResolvePiLayout(t.TempDir())
	manifest := validManifestForTest(layout)
	manifest.ManagedAgentOverlays = []ManagedAgentOverlayRecord{{
		AgentName:   "lore-worker",
		Path:        filepath.Join(layout.ManagedAgentsDir, "lore-managed-lore-worker.md"),
		ContentHash: contentHash([]byte("worker")),
	}}

	data, err := marshalManifest(manifest)
	if err != nil {
		t.Fatalf("marshalManifest() error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	if len(loaded.ManagedAgentOverlays) != 1 || loaded.ManagedAgentOverlays[0].AgentName != "lore-worker" {
		t.Fatalf("ManagedAgentOverlays = %+v, want lore-worker overlay metadata", loaded.ManagedAgentOverlays)
	}
}

func TestManifestValidateForLayoutUsesSharedHarnessLayoutGroundwork(t *testing.T) {
	layout := ResolvePiLayout(t.TempDir())
	manifest := validManifestForTest(layout)

	if err := manifest.ValidateForLayout(layout.HarnessLayout(), layout.ManagedFiles, filepath.Join(layout.AgentDir, "backups")); err != nil {
		t.Fatalf("ValidateForLayout() error = %v, want nil", err)
	}

	broken := manifest
	broken.Target = TargetAntigravity
	if err := broken.ValidateForLayout(layout.HarnessLayout(), layout.ManagedFiles, filepath.Join(layout.AgentDir, "backups")); err == nil || !strings.Contains(err.Error(), "target") {
		t.Fatalf("ValidateForLayout() err = %v, want shared target mismatch rejection", err)
	}
}
