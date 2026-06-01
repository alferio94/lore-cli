package agentconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alferio94/lore-cli/internal/config"
)

// ErrNotFound is returned when agent-config.json does not exist.
var ErrNotFound = errors.New("agent-config.json not found")

// Store manages the agent-config.json file at a resolved Lore config directory.
type Store struct {
	// configDir is an optional override for the Lore config directory.
	// If empty, config.ResolveDir (LORE_CONFIG_DIR / UserConfigDir) is used.
	configDir string
}

// NewStore returns a Store that resolves its path using an optional config dir
// override or the standard Lore config directory resolution (via config.ResolveDir).
func NewStore(configDir string) Store {
	return Store{configDir: strings.TrimSpace(configDir)}
}

// Path returns the absolute agent-config.json path.
func (s Store) Path() (string, error) {
	dir, err := config.ResolveDir(s.configDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FileName), nil
}

// Load reads agent-config.json from disk and validates it.
// Returns ErrNotFound if the file does not exist.
func (s Store) Load() (Config, error) {
	path, err := s.Path()
	if err != nil {
		return Config{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, ErrNotFound
		}
		return Config{}, fmt.Errorf("read agent-config: %w", err)
	}

	cfg, err := FromJSON(string(data))
	if err != nil {
		return Config{}, fmt.Errorf("parse agent-config: %w", err)
	}
	return cfg, nil
}

// Save atomically writes a Config to agent-config.json.
// It normalises the JSON ordering via Config.ToJSON so equivalent logical
// inputs produce byte-identical output (idempotent/ deterministic).
func (s Store) Save(cfg Config) error {
	path, err := s.Path()
	if err != nil {
		return err
	}

	// Ensure parent directory exists with restricted permissions.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	// Set dir to 0700 even if it already existed.
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("chmod config dir: %w", err)
	}

	// Canonical JSON for idempotent writes.
	canon, err := cfg.ToJSON()
	if err != nil {
		return err
	}

	// Atomic write: create temp file in same directory, then rename.
	tmp, err := os.CreateTemp(dir, ".agent-config-*.json")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	closed := false
	removed := false

	defer func() {
		if !closed {
			_ = tmp.Close()
		}
		if !removed {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if _, err := tmp.WriteString(canon); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		removed = true
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}
	closed = true

	if err := os.Rename(tmpPath, path); err != nil {
		removed = true
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}
	removed = true

	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod agent-config: %w", err)
	}

	return nil
}

// EnsureDefault loads the existing agent-config.json or writes a fresh one
// from DefaultConfig. It returns the loaded or created config and a bool
// indicating whether a new file was written.
func (s Store) EnsureDefault() (Config, bool, error) {
	cfg, err := s.Load()
	if err == nil {
		// File exists; validate but do not rewrite.
		if validateErr := cfg.Validate(); validateErr != nil {
			return cfg, false, fmt.Errorf("existing agent-config is invalid: %w", validateErr)
		}
		return cfg, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Config{}, false, fmt.Errorf("load agent-config: %w", err)
	}

	// File does not exist; write defaults.
	cfg = DefaultConfig()
	if err := s.Save(cfg); err != nil {
		return Config{}, false, fmt.Errorf("save default agent-config: %w", err)
	}
	return cfg, true, nil
}

// Delete removes agent-config.json if it exists. It succeeds even if the file
// is already absent.
func (s Store) Delete() error {
	path, err := s.Path()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete agent-config: %w", err)
	}
	return nil
}