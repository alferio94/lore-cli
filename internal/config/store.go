package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	EnvConfigDir = "LORE_CONFIG_DIR"
	configFile   = "config.json"
	appDirName   = "lore"
)

var ErrNotFound = errors.New("config not found")

// Config stores the single-session MVP auth state.
type Config struct {
	Version   int    `json:"version"`
	ServerURL string `json:"server_url"`
	APIToken  string `json:"api_token"`
}

// Store resolves and manages the Lore CLI config file.
type Store struct {
	configDir string
}

// NewStore returns a config store that optionally prefers an injected config dir.
func NewStore(configDir string) Store {
	return Store{configDir: strings.TrimSpace(configDir)}
}

// ResolveDir resolves the effective config dir using injected path, env override, or UserConfigDir fallback.
func ResolveDir(configDir string) (string, error) {
	if dir := strings.TrimSpace(configDir); dir != "" {
		return dir, nil
	}
	if dir := strings.TrimSpace(os.Getenv(EnvConfigDir)); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(base, appDirName), nil
}

// Path returns the full config file path.
func (s Store) Path() (string, error) {
	dir, err := ResolveDir(s.configDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFile), nil
}

// Load reads config from disk.
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
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	cfg.ServerURL = strings.TrimSpace(cfg.ServerURL)
	return cfg, nil
}

// Save validates, normalizes, and atomically writes config to disk.
func (s Store) Save(cfg Config) error {
	path, err := s.Path()
	if err != nil {
		return err
	}

	normalized, err := NormalizeServerURL(cfg.ServerURL)
	if err != nil {
		return err
	}
	cfg.ServerURL = normalized
	cfg.Version = 1

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("chmod config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp config: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod config: %w", err)
	}
	cleanup = false
	return nil
}

// Delete removes local config and succeeds when nothing remains.
func (s Store) Delete() error {
	path, err := s.Path()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete config: %w", err)
	}
	return nil
}

// NormalizeServerURL trims whitespace, requires http/https, and removes trailing slashes.
func NormalizeServerURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("server URL is required")
	}
	if !(strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://")) {
		return "", errors.New("server URL must start with http:// or https://")
	}
	return strings.TrimRight(trimmed, "/"), nil
}

// RedactToken hides raw token values in user-visible output.
func RedactToken(token string) string {
	if strings.TrimSpace(token) == "" {
		return "<missing>"
	}
	return "<redacted>"
}

// Redacted returns a token-safe copy of the config for rendering.
func (c Config) Redacted() Config {
	clone := c
	clone.APIToken = RedactToken(clone.APIToken)
	return clone
}
