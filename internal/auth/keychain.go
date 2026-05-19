package auth

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/alferio94/lore-cli/internal/config"
	keyring "github.com/zalando/go-keyring"
)

type KeychainStore struct{}

var (
	keyringSet    = keyring.Set
	keyringGet    = keyring.Get
	keyringDelete = keyring.Delete
)

func (KeychainStore) Set(service, account, secret string) error {
	return keyringSet(service, account, secret)
}

func (KeychainStore) Get(service, account string) (string, error) {
	secret, err := keyringGet(service, account)
	if errorsIsKeyringNotFound(err) {
		return "", ErrCredentialNotFound
	}
	return secret, err
}

func (KeychainStore) Delete(service, account string) error {
	err := keyringDelete(service, account)
	if errorsIsKeyringNotFound(err) {
		return ErrCredentialNotFound
	}
	return err
}

func DeriveCredentialAccount(serverURL, configPath string) (string, error) {
	normalized, err := config.NormalizeServerURL(serverURL)
	if err != nil {
		return "", err
	}
	resolvedDir, err := filepath.Abs(filepath.Dir(strings.TrimSpace(configPath)))
	if err != nil {
		return "", fmt.Errorf("resolve config dir: %w", err)
	}
	sum := sha256.Sum256([]byte(filepath.Clean(resolvedDir)))
	return fmt.Sprintf("%s#%x", normalized, sum[:6]), nil
}

func errorsIsKeyringNotFound(err error) bool {
	return err == keyring.ErrNotFound
}
