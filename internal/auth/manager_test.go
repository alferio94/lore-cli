package auth

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/config"
)

func TestManagerSaveAndLoadRoundTrip(t *testing.T) {
	store := &fakeConfigStore{path: filepath.Join(t.TempDir(), "config.json")}
	creds := &fakeCredentialStore{}
	manager := Manager{ConfigStore: store, Credentials: creds}

	if err := manager.Save(" https://example.test/ ", " secret-token "); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if store.saved.Version != ConfigVersion {
		t.Fatalf("saved version = %d, want %d", store.saved.Version, ConfigVersion)
	}
	if store.saved.ServerURL != "https://example.test" {
		t.Fatalf("saved server = %q", store.saved.ServerURL)
	}
	if store.saved.CredentialAccount == "" {
		t.Fatalf("expected credential account to be saved")
	}
	if store.saved.APIToken != "" {
		t.Fatalf("expected metadata-only config save, got token %q", store.saved.APIToken)
	}
	if got := creds.secrets[store.saved.CredentialAccount]; got != "secret-token" {
		t.Fatalf("stored credential = %q, want secret-token", got)
	}

	session, err := manager.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if session.ServerURL != "https://example.test" || session.Token != "secret-token" {
		t.Fatalf("session = %+v", session)
	}
	if session.CredentialAccount != store.saved.CredentialAccount {
		t.Fatalf("session credential account = %q, want %q", session.CredentialAccount, store.saved.CredentialAccount)
	}
}

func TestManagerLoadMigratesLegacyToken(t *testing.T) {
	store := &fakeConfigStore{
		path:   filepath.Join(t.TempDir(), "config.json"),
		loaded: config.Config{ServerURL: "https://example.test", APIToken: "legacy-token"},
	}
	creds := &fakeCredentialStore{}
	manager := Manager{ConfigStore: store, Credentials: creds}

	session, err := manager.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if session.Token != "legacy-token" {
		t.Fatalf("session token = %q, want legacy-token", session.Token)
	}
	if store.saveCalls != 1 {
		t.Fatalf("saveCalls = %d, want 1", store.saveCalls)
	}
	if store.saved.APIToken != "" {
		t.Fatalf("migrated config retained token %q", store.saved.APIToken)
	}
	if store.saved.CredentialAccount == "" {
		t.Fatalf("expected migrated credential account")
	}
	if got := creds.secrets[store.saved.CredentialAccount]; got != "legacy-token" {
		t.Fatalf("stored migrated credential = %q", got)
	}
}

func TestManagerLoadFailsClosedWhenLegacyMigrationCredentialBackendUnavailable(t *testing.T) {
	store := &fakeConfigStore{
		path:   filepath.Join(t.TempDir(), "config.json"),
		loaded: config.Config{ServerURL: "https://example.test", APIToken: "legacy-token"},
	}
	creds := &fakeCredentialStore{setErr: errors.New("keychain locked")}
	manager := Manager{ConfigStore: store, Credentials: creds}

	session, err := manager.Load()
	if err == nil {
		t.Fatalf("expected Load() error")
	}
	var authErr *Error
	if !errors.As(err, &authErr) || authErr.Code != ErrCredentialUnavailable {
		t.Fatalf("Load() error = %v, want credential unavailable", err)
	}
	if session != (Session{}) {
		t.Fatalf("session = %+v, want zero value on failed migration", session)
	}
	if store.saveCalls != 1 {
		t.Fatalf("saveCalls = %d, want 1 best-effort scrub write", store.saveCalls)
	}
	if store.deleteCalls != 0 {
		t.Fatalf("deleteCalls = %d, want 0 when scrub rewrite succeeds", store.deleteCalls)
	}
	if store.saved.APIToken != "" {
		t.Fatalf("failed migration rewrite retained token %q", store.saved.APIToken)
	}
	if store.saved.CredentialAccount == "" {
		t.Fatalf("expected credential account on failed migration rewrite")
	}
	if len(creds.secrets) != 0 {
		t.Fatalf("stored credentials = %v, want none on failed migration", creds.secrets)
	}
}

func TestManagerLoadDeletesLegacyConfigWhenScrubRewriteFails(t *testing.T) {
	store := &fakeConfigStore{
		path:      filepath.Join(t.TempDir(), "config.json"),
		loaded:    config.Config{ServerURL: "https://example.test", APIToken: "legacy-token"},
		saveErr:   errors.New("disk full"),
		deleteErr: nil,
	}
	creds := &fakeCredentialStore{setErr: errors.New("keychain locked")}
	manager := Manager{ConfigStore: store, Credentials: creds}

	session, err := manager.Load()
	if err == nil {
		t.Fatalf("expected Load() error")
	}
	var authErr *Error
	if !errors.As(err, &authErr) || authErr.Code != ErrCredentialUnavailable {
		t.Fatalf("Load() error = %v, want credential unavailable after delete fallback", err)
	}
	if session != (Session{}) {
		t.Fatalf("session = %+v, want zero value on failed migration", session)
	}
	if store.saveCalls != 1 || store.deleteCalls != 1 || !store.deleted {
		t.Fatalf("store cleanup = saveCalls:%d deleteCalls:%d deleted:%v, want save then delete fallback", store.saveCalls, store.deleteCalls, store.deleted)
	}
	if len(creds.secrets) != 0 {
		t.Fatalf("stored credentials = %v, want none on failed migration", creds.secrets)
	}
}

func TestManagerLoadFailsWhenLegacyCleanupCannotScrubOrDelete(t *testing.T) {
	store := &fakeConfigStore{
		path:      filepath.Join(t.TempDir(), "config.json"),
		loaded:    config.Config{ServerURL: "https://example.test", APIToken: "legacy-token"},
		saveErr:   errors.New("disk full"),
		deleteErr: errors.New("permission denied"),
	}
	creds := &fakeCredentialStore{setErr: errors.New("keychain locked")}
	manager := Manager{ConfigStore: store, Credentials: creds}

	_, err := manager.Load()
	if err == nil {
		t.Fatalf("expected Load() error")
	}
	var authErr *Error
	if !errors.As(err, &authErr) || authErr.Code != ErrConfigWriteFailed || authErr.Op != "delete legacy config" {
		t.Fatalf("Load() error = %v, want config cleanup failure", err)
	}
	if store.saveCalls != 1 || store.deleteCalls != 1 {
		t.Fatalf("store cleanup = saveCalls:%d deleteCalls:%d, want scrub attempt plus delete fallback", store.saveCalls, store.deleteCalls)
	}
}

func TestManagerSaveFailsClosedWhenCredentialBackendUnavailable(t *testing.T) {
	store := &fakeConfigStore{path: filepath.Join(t.TempDir(), "config.json")}
	creds := &fakeCredentialStore{setErr: errors.New("keychain locked")}
	manager := Manager{ConfigStore: store, Credentials: creds}

	err := manager.Save("https://example.test", "secret-token")
	if err == nil {
		t.Fatalf("expected Save() error")
	}
	var authErr *Error
	if !errors.As(err, &authErr) || authErr.Code != ErrCredentialUnavailable {
		t.Fatalf("Save() error = %v, want credential unavailable", err)
	}
	if store.saveCalls != 0 {
		t.Fatalf("saveCalls = %d, want 0", store.saveCalls)
	}
}

func TestManagerLogoutIgnoresMissingCredential(t *testing.T) {
	store := &fakeConfigStore{
		path:   filepath.Join(t.TempDir(), "config.json"),
		loaded: config.Config{ServerURL: "https://example.test", CredentialAccount: "acct-1"},
	}
	creds := &fakeCredentialStore{deleteErr: ErrCredentialNotFound}
	manager := Manager{ConfigStore: store, Credentials: creds}

	if err := manager.Logout(); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if !store.deleted {
		t.Fatalf("expected config delete")
	}
}

func TestDeriveCredentialAccountUsesNormalizedServerAndConfigFingerprint(t *testing.T) {
	rootA := filepath.Join(t.TempDir(), "a", "config.json")
	rootB := filepath.Join(t.TempDir(), "b", "config.json")

	accountA, err := DeriveCredentialAccount(" https://example.test/ ", rootA)
	if err != nil {
		t.Fatalf("DeriveCredentialAccount() error = %v", err)
	}
	accountB, err := DeriveCredentialAccount("https://example.test", rootB)
	if err != nil {
		t.Fatalf("DeriveCredentialAccount() error = %v", err)
	}
	if !strings.HasPrefix(accountA, "https://example.test#") {
		t.Fatalf("accountA = %q, want normalized prefix", accountA)
	}
	if accountA == accountB {
		t.Fatalf("accounts should differ across config roots: %q", accountA)
	}
}

type fakeConfigStore struct {
	path        string
	loaded      config.Config
	loadErr     error
	saved       config.Config
	saveErr     error
	saveCalls   int
	deleted     bool
	deleteErr   error
	deleteCalls int
}

func (f *fakeConfigStore) Load() (config.Config, error) {
	if f.loadErr != nil {
		return config.Config{}, f.loadErr
	}
	return f.loaded, nil
}

func (f *fakeConfigStore) Save(cfg config.Config) error {
	f.saveCalls++
	f.saved = cfg
	f.loaded = cfg
	return f.saveErr
}

func (f *fakeConfigStore) Delete() error {
	f.deleteCalls++
	f.deleted = true
	return f.deleteErr
}

func (f *fakeConfigStore) Path() (string, error) {
	return f.path, nil
}

type fakeCredentialStore struct {
	secrets   map[string]string
	setErr    error
	getErr    error
	deleteErr error
}

func (f *fakeCredentialStore) Set(service, account, secret string) error {
	if f.setErr != nil {
		return f.setErr
	}
	if f.secrets == nil {
		f.secrets = map[string]string{}
	}
	f.secrets[account] = secret
	return nil
}

func (f *fakeCredentialStore) Get(service, account string) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	secret, ok := f.secrets[account]
	if !ok {
		return "", ErrCredentialNotFound
	}
	return secret, nil
}

func (f *fakeCredentialStore) Delete(service, account string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.secrets, account)
	return nil
}
