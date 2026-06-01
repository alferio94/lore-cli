package agentconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func tempStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	return &Store{configDir: dir}, dir
}

func TestStorePath(t *testing.T) {
	store := &Store{configDir: "/test/lore"}
	path, err := store.Path()
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}
	want := filepath.Join("/test/lore", FileName)
	if path != want {
		t.Errorf("Path() = %q, want %q", path, want)
	}
}

func TestStorePathDefaultDir(t *testing.T) {
	store := &Store{}
	// Without configDir or env override, it will try UserConfigDir.
	// Just check it doesn't panic and returns a path ending in FileName.
	path, err := store.Path()
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}
	if filepath.Base(path) != FileName {
		t.Errorf("Path() = %q, should end in %q", path, FileName)
	}
}

func TestStorePathWithEnvOverride(t *testing.T) {
	orig := os.Getenv("LORE_CONFIG_DIR")
	os.Setenv("LORE_CONFIG_DIR", "/env/override")
	defer func() { os.Setenv("LORE_CONFIG_DIR", orig) }()

	store := &Store{} // no configDir override
	path, err := store.Path()
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}
	if path != filepath.Join("/env/override", FileName) {
		t.Errorf("Path() = %q, want %q", path, filepath.Join("/env/override", FileName))
	}
}

func TestStoreLoadNotFound(t *testing.T) {
	store, _ := tempStore(t)
	_, err := store.Load()
	if err != ErrNotFound {
		t.Errorf("Load() = %v, want ErrNotFound", err)
	}
}

func TestStoreSaveAndLoad(t *testing.T) {
	store, _ := tempStore(t)
	cfg := DefaultConfig()
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := loaded.Validate(); err != nil {
		t.Errorf("Loaded config Validate() = %v, want nil", err)
	}
}

func TestStoreSaveIdempotent(t *testing.T) {
	store, _ := tempStore(t)
	cfg := DefaultConfig()

	// Save twice; both should succeed and produce identical file content.
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save() first error = %v", err)
	}
	path, err := store.Path()
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}
	data1, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() first error = %v", err)
	}

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save() second error = %v", err)
	}
	data2, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() second error = %v", err)
	}

	if string(data1) != string(data2) {
		t.Error("Idempotent Save() should produce byte-identical file content")
	}
}

func TestStoreSaveCreatesDir(t *testing.T) {
	store, baseDir := tempStore(t)
	subDir := filepath.Join(baseDir, "sub", "dir")
	store = &Store{configDir: subDir}

	cfg := DefaultConfig()
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save() should create parent dirs: %v", err)
	}

	path, err := store.Path()
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("File should exist: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("File permissions = %o, want 0600", info.Mode().Perm())
	}
}

func TestStoreEnsureDefaultCreatesFile(t *testing.T) {
	store, _ := tempStore(t)
	cfg, created, err := store.EnsureDefault()
	if err != nil {
		t.Fatalf("EnsureDefault() error = %v", err)
	}
	if !created {
		t.Error("EnsureDefault() should report created=true on first call")
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Created config Validate() = %v, want nil", err)
	}
}

func TestStoreEnsureDefaultReturnsExisting(t *testing.T) {
	store, _ := tempStore(t)
	// Pre-write a file.
	cfg := DefaultConfig()
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, created, err := store.EnsureDefault()
	if err != nil {
		t.Fatalf("EnsureDefault() error = %v", err)
	}
	if created {
		t.Error("EnsureDefault() should report created=false when file exists")
	}
	if err := loaded.Validate(); err != nil {
		t.Errorf("Loaded config Validate() = %v, want nil", err)
	}
}

func TestStoreEnsureDefaultRejectsInvalidFile(t *testing.T) {
	store, _ := tempStore(t)
	path, err := store.Path()
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}
	// Write an invalid config file directly.
	if err := os.WriteFile(path, []byte(`{"schema_version":99}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, _, err = store.EnsureDefault()
	if err == nil {
		t.Error("EnsureDefault() should reject invalid existing file")
	}
}

func TestStoreDelete(t *testing.T) {
	store, _ := tempStore(t)
	cfg := DefaultConfig()
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := store.Delete(); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	// Should be able to call Delete again without error (idempotent).
	if err := store.Delete(); err != nil {
		t.Fatalf("Delete() second call error = %v", err)
	}
}

func TestStoreDeleteNoFile(t *testing.T) {
	store, _ := tempStore(t)
	// Delete when no file exists should succeed.
	if err := store.Delete(); err != nil {
		t.Fatalf("Delete() with no file error = %v", err)
	}
}

func TestStoreLoadMalformedJSON(t *testing.T) {
	store, _ := tempStore(t)
	path, err := store.Path()
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(`not json`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	_, err = store.Load()
	if err == nil {
		t.Error("Load() should reject malformed JSON")
	}
}

func TestStoreSaveIdempotentFromDifferentDefaultCalls(t *testing.T) {
	store, _ := tempStore(t)
	// Two separate DefaultConfig() calls should produce byte-identical output.
	cfg1 := DefaultConfig()
	cfg2 := DefaultConfig()

	if err := store.Save(cfg1); err != nil {
		t.Fatalf("Save(cfg1) error = %v", err)
	}
	path, _ := store.Path()
	data1, _ := os.ReadFile(path)

	if err := store.Save(cfg2); err != nil {
		t.Fatalf("Save(cfg2) error = %v", err)
	}
	data2, _ := os.ReadFile(path)

	if string(data1) != string(data2) {
		t.Error("Two DefaultConfig() saves should produce identical file content")
	}
}

func TestStoreSaveOverwritesWithDifferentConfig(t *testing.T) {
	store, _ := tempStore(t)
	cfg1 := DefaultConfig()
	cfg2 := DefaultConfig()
	cfg2.SDDAgents["sdd-init"] = Agent{Model: "gpt-4"}

	if err := store.Save(cfg1); err != nil {
		t.Fatalf("Save(cfg1) error = %v", err)
	}
	if err := store.Save(cfg2); err != nil {
		t.Fatalf("Save(cfg2) error = %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.SDDAgents["sdd-init"].Model != "gpt-4" {
		t.Errorf("Loaded model = %q, want gpt-4", loaded.SDDAgents["sdd-init"].Model)
	}
}
