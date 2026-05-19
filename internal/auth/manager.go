package auth

import (
	"errors"
	"fmt"
	"strings"

	"github.com/alferio94/lore-cli/internal/config"
)

const ConfigVersion = config.CurrentVersion

type ErrorCode string

const (
	ErrConfigNotFound        ErrorCode = "config_not_found"
	ErrInvalidConfig         ErrorCode = "invalid_config"
	ErrCredentialUnavailable ErrorCode = "credential_unavailable"
	ErrCredentialMissing     ErrorCode = "credential_missing"
	ErrConfigWriteFailed     ErrorCode = "config_write_failed"
)

type Error struct {
	Code ErrorCode
	Op   string
	Err  error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Err == nil {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %v", e.Op, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

type ConfigStore interface {
	Load() (config.Config, error)
	Save(config.Config) error
	Delete() error
	Path() (string, error)
}

type Session struct {
	ServerURL         string
	Token             string
	ConfigPath        string
	CredentialAccount string
}

type Manager struct {
	ConfigStore ConfigStore
	Credentials CredentialStore
	ServiceName string
}

func (m Manager) Save(serverURL, token string) error {
	normalized, err := config.NormalizeServerURL(serverURL)
	if err != nil {
		return &Error{Code: ErrInvalidConfig, Op: "normalize server url", Err: err}
	}
	secret := strings.TrimSpace(token)
	if secret == "" {
		return &Error{Code: ErrInvalidConfig, Op: "validate token", Err: errors.New("token is required")}
	}
	path, err := m.path()
	if err != nil {
		return err
	}
	account, err := DeriveCredentialAccount(normalized, path)
	if err != nil {
		return &Error{Code: ErrInvalidConfig, Op: "derive credential account", Err: err}
	}
	if err := m.credentialStore().Set(m.serviceName(), account, secret); err != nil {
		return &Error{Code: ErrCredentialUnavailable, Op: "store credential", Err: err}
	}
	if err := m.ConfigStore.Save(config.Config{Version: ConfigVersion, ServerURL: normalized, CredentialAccount: account}); err != nil {
		_ = m.credentialStore().Delete(m.serviceName(), account)
		return &Error{Code: ErrConfigWriteFailed, Op: "save config", Err: err}
	}
	return nil
}

func (m Manager) Load() (Session, error) {
	cfg, err := m.ConfigStore.Load()
	if err != nil {
		if errors.Is(err, config.ErrNotFound) {
			return Session{}, &Error{Code: ErrConfigNotFound, Op: "load config", Err: err}
		}
		return Session{}, &Error{Code: ErrInvalidConfig, Op: "load config", Err: err}
	}
	return m.loadFromConfig(cfg)
}

func (m Manager) Logout() error {
	cfg, err := m.ConfigStore.Load()
	if err != nil {
		if errors.Is(err, config.ErrNotFound) {
			return m.ConfigStore.Delete()
		}
		return &Error{Code: ErrInvalidConfig, Op: "load config", Err: err}
	}
	path, err := m.path()
	if err != nil {
		return err
	}
	account, err := credentialAccount(cfg, path)
	if err == nil && account != "" {
		if err := m.credentialStore().Delete(m.serviceName(), account); err != nil && !errors.Is(err, ErrCredentialNotFound) {
			return &Error{Code: ErrCredentialUnavailable, Op: "delete credential", Err: err}
		}
	}
	if err := m.ConfigStore.Delete(); err != nil {
		return &Error{Code: ErrConfigWriteFailed, Op: "delete config", Err: err}
	}
	return nil
}

func (m Manager) loadFromConfig(cfg config.Config) (Session, error) {
	path, err := m.path()
	if err != nil {
		return Session{}, err
	}
	normalized, err := config.NormalizeServerURL(cfg.ServerURL)
	if err != nil {
		return Session{}, &Error{Code: ErrInvalidConfig, Op: "normalize server url", Err: err}
	}
	account, err := credentialAccount(cfg, path)
	if err != nil {
		return Session{}, &Error{Code: ErrInvalidConfig, Op: "derive credential account", Err: err}
	}
	if legacy := strings.TrimSpace(cfg.APIToken); legacy != "" {
		return m.migrateLegacy(path, normalized, account, legacy)
	}
	token, err := m.credentialStore().Get(m.serviceName(), account)
	if err != nil {
		if errors.Is(err, ErrCredentialNotFound) {
			return Session{}, &Error{Code: ErrCredentialMissing, Op: "load credential", Err: err}
		}
		return Session{}, &Error{Code: ErrCredentialUnavailable, Op: "load credential", Err: err}
	}
	if strings.TrimSpace(token) == "" {
		return Session{}, &Error{Code: ErrCredentialMissing, Op: "load credential", Err: errors.New("credential secret is empty")}
	}
	return Session{ServerURL: normalized, Token: token, ConfigPath: path, CredentialAccount: account}, nil
}

func (m Manager) migrateLegacy(path, serverURL, account, token string) (Session, error) {
	if err := m.credentialStore().Set(m.serviceName(), account, token); err != nil {
		if cleanupErr := m.scrubOrDeleteLegacyConfig(serverURL, account, err); cleanupErr != nil {
			return Session{}, cleanupErr
		}
		return Session{}, &Error{Code: ErrCredentialUnavailable, Op: "migrate credential", Err: err}
	}
	if err := m.ConfigStore.Save(config.Config{Version: ConfigVersion, ServerURL: serverURL, CredentialAccount: account}); err != nil {
		_ = m.credentialStore().Delete(m.serviceName(), account)
		return Session{}, &Error{Code: ErrConfigWriteFailed, Op: "scrub legacy config", Err: err}
	}
	return Session{ServerURL: serverURL, Token: token, ConfigPath: path, CredentialAccount: account}, nil
}

func (m Manager) scrubOrDeleteLegacyConfig(serverURL, account string, cause error) error {
	cfg := config.Config{Version: ConfigVersion, ServerURL: serverURL, CredentialAccount: account}
	if err := m.ConfigStore.Save(cfg); err == nil {
		return nil
	} else if deleteErr := m.ConfigStore.Delete(); deleteErr != nil {
		return &Error{Code: ErrConfigWriteFailed, Op: "delete legacy config", Err: fmt.Errorf("metadata-only rewrite failed after credential migration failure (%v); delete fallback failed: %w", err, deleteErr)}
	}
	return nil
}

func credentialAccount(cfg config.Config, path string) (string, error) {
	if account := strings.TrimSpace(cfg.CredentialAccount); account != "" {
		return account, nil
	}
	return DeriveCredentialAccount(cfg.ServerURL, path)
}

func (m Manager) path() (string, error) {
	path, err := m.ConfigStore.Path()
	if err != nil {
		return "", &Error{Code: ErrInvalidConfig, Op: "resolve config path", Err: err}
	}
	return path, nil
}

func (m Manager) credentialStore() CredentialStore {
	if m.Credentials != nil {
		return m.Credentials
	}
	return NewCredentialStore()
}

func (m Manager) serviceName() string {
	if service := strings.TrimSpace(m.ServiceName); service != "" {
		return service
	}
	return ServiceName
}
