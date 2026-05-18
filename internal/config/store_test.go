package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveDirPrecedence(t *testing.T) {
	t.Setenv(EnvConfigDir, filepath.Join(t.TempDir(), "env"))

	dir, err := ResolveDir(filepath.Join(t.TempDir(), "explicit"))
	if err != nil {
		t.Fatalf("ResolveDir() error = %v", err)
	}
	if !strings.Contains(dir, "explicit") {
		t.Fatalf("expected explicit dir, got %q", dir)
	}

	dir, err = ResolveDir("")
	if err != nil {
		t.Fatalf("ResolveDir() error = %v", err)
	}
	if !strings.Contains(dir, "env") {
		t.Fatalf("expected env dir, got %q", dir)
	}
}

func TestNormalizeServerURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "http", raw: " http://example.test/ ", want: "http://example.test"},
		{name: "https", raw: "https://example.test///", want: "https://example.test"},
		{name: "missing", raw: "   ", wantErr: true},
		{name: "bad scheme", raw: "ftp://example.test", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeServerURL(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeServerURL() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeServerURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStoreSaveLoadDeleteRoundTrip(t *testing.T) {
	store := NewStore(t.TempDir())
	original := Config{
		ServerURL: " https://example.test/ ",
		APIToken:  "secret-token",
	}

	if err := store.Save(original); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	path, err := store.Path()
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Version != 1 {
		t.Fatalf("Version = %d, want 1", loaded.Version)
	}
	if loaded.ServerURL != "https://example.test" {
		t.Fatalf("ServerURL = %q, want normalized value", loaded.ServerURL)
	}
	if loaded.APIToken != original.APIToken {
		t.Fatalf("APIToken mismatch after load")
	}
	assertNoTempFiles(t, filepath.Dir(path))
	assertPermissions(t, filepath.Dir(path), path)

	if err := store.Delete(); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	_, err = store.Load()
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load() after Delete error = %v, want ErrNotFound", err)
	}

	if err := store.Delete(); err != nil {
		t.Fatalf("Delete() second call error = %v", err)
	}
}

func TestStoreSaveDoesNotPersistInvalidURL(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Save(Config{ServerURL: "bad://example.test", APIToken: "secret-token"}); err == nil {
		t.Fatalf("expected Save() error for invalid URL")
	}

	_, err := store.Load()
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load() error = %v, want ErrNotFound", err)
	}
}

func TestLoadMissingConfig(t *testing.T) {
	store := NewStore(t.TempDir())
	_, err := store.Load()
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load() error = %v, want ErrNotFound", err)
	}
}

func TestRedaction(t *testing.T) {
	cfg := Config{ServerURL: "https://example.test", APIToken: "secret-token"}.Redacted()
	if cfg.APIToken != "<redacted>" {
		t.Fatalf("redacted token = %q", cfg.APIToken)
	}
	if strings.Contains(cfg.APIToken, "secret-token") {
		t.Fatalf("redacted token leaked raw token")
	}
	if RedactToken("") != "<missing>" {
		t.Fatalf("RedactToken(empty) = %q", RedactToken(""))
	}
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".config-") {
			t.Fatalf("unexpected temp file left behind: %s", entry.Name())
		}
	}
}

func assertPermissions(t *testing.T, dir, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat(dir) error = %v", err)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(file) error = %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("dir perms = %o, want 700", got)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("file perms = %o, want 600", got)
	}
}
