package auth

import "errors"

const ServiceName = "lore-cli"

var ErrCredentialNotFound = errors.New("credential not found")

type CredentialStore interface {
	Set(service, account, secret string) error
	Get(service, account string) (string, error)
	Delete(service, account string) error
}

func NewCredentialStore() CredentialStore {
	return KeychainStore{}
}
