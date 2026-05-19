package auth

import (
	"errors"
	"testing"

	keyring "github.com/zalando/go-keyring"
)

func TestNewCredentialStoreReturnsKeychainStore(t *testing.T) {
	if _, ok := NewCredentialStore().(KeychainStore); !ok {
		t.Fatalf("NewCredentialStore() did not return KeychainStore")
	}
}

func TestKeychainStoreUsesBackendAndTranslatesNotFound(t *testing.T) {
	origSet, origGet, origDelete := keyringSet, keyringGet, keyringDelete
	defer func() {
		keyringSet, keyringGet, keyringDelete = origSet, origGet, origDelete
	}()

	var gotService, gotAccount, gotSecret string
	keyringSet = func(service, account, secret string) error {
		gotService, gotAccount, gotSecret = service, account, secret
		return nil
	}
	keyringGet = func(service, account string) (string, error) {
		if service != ServiceName || account != "acct-1" {
			t.Fatalf("Get() backend args = %q %q", service, account)
		}
		return "secret-token", nil
	}
	keyringDelete = func(service, account string) error {
		if service != ServiceName || account != "acct-1" {
			t.Fatalf("Delete() backend args = %q %q", service, account)
		}
		return nil
	}

	store := KeychainStore{}
	if err := store.Set(ServiceName, "acct-1", "secret-token"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if gotService != ServiceName || gotAccount != "acct-1" || gotSecret != "secret-token" {
		t.Fatalf("Set() backend args = %q %q %q", gotService, gotAccount, gotSecret)
	}
	secret, err := store.Get(ServiceName, "acct-1")
	if err != nil || secret != "secret-token" {
		t.Fatalf("Get() = %q, %v", secret, err)
	}
	if err := store.Delete(ServiceName, "acct-1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	keyringGet = func(string, string) (string, error) { return "", keyring.ErrNotFound }
	if _, err := store.Get(ServiceName, "acct-1"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("Get() err = %v, want ErrCredentialNotFound", err)
	}
	keyringDelete = func(string, string) error { return keyring.ErrNotFound }
	if err := store.Delete(ServiceName, "acct-1"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("Delete() err = %v, want ErrCredentialNotFound", err)
	}
}
