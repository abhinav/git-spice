package secret

import (
	"errors"

	"github.com/zalando/go-keyring"
)

// Keyring is a secure secret store that uses the system's keychain
// if available.
//
// Its zero value is ready for use.
type Keyring struct{}

var _ Stash = (*Keyring)(nil)

func keyringService(service string) string {
	return "git-spice:" + service
}

// SaveSecret saves a secret in the keyring.
func (*Keyring) SaveSecret(service, key, secret string) error {
	return keyring.Set(keyringService(service), key, secret)
}

// LoadSecret loads a secret from the keyring.
func (*Keyring) LoadSecret(service, key string) (string, error) {
	secret, err := keyring.Get(keyringService(service), key)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	return secret, err
}

// DeleteSecret deletes a secret from the keyring.
func (*Keyring) DeleteSecret(service, key string) error {
	err := keyring.Delete(keyringService(service), key)
	if errors.Is(err, keyring.ErrNotFound) {
		err = nil
	}
	return err
}
