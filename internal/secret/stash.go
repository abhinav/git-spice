// Package secret provides a layer for storing secretes.
package secret

import (
	"errors"
	"sync"

	"github.com/zalando/go-keyring"
)

var (
	// ErrNotFound is returned when a secret is not found.
	ErrNotFound = errors.New("secret not found")

	// ErrKeyringUnsupported indicates that secure storage
	// via the system keychain is not supported on the current platform.
	ErrKeyringUnsupported = keyring.ErrUnsupportedPlatform
)

// Stash stores and retrieves secrets.
type Stash interface {
	SaveSecret(service, key, secret string) error
	LoadSecret(service, key string) (string, error)

	// DeleteSecret deletes a secret from the stash.
	// It is a no-op if the secret does not exist.
	DeleteSecret(service, key string) error
}

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

type memoryStashKey struct{ service, key string }

// MemoryStash is an in-memory secret store for testing.
// Its zero value is ready for use.
type MemoryStash struct {
	m sync.Map // {service, key} -> secret
}

var _ Stash = (*MemoryStash)(nil)

// SaveSecret saves a secret in the memory stash.
func (m *MemoryStash) SaveSecret(service string, key string, secret string) error {
	m.m.Store(memoryStashKey{service, key}, secret)
	return nil
}

// LoadSecret loads a secret from the memory stash.
func (m *MemoryStash) LoadSecret(service string, key string) (string, error) {
	secret, ok := m.m.Load(memoryStashKey{service, key})
	if !ok {
		return "", ErrNotFound
	}
	return secret.(string), nil
}

// DeleteSecret deletes a secret from the memory stash.
func (m *MemoryStash) DeleteSecret(service string, key string) error {
	m.m.Delete(memoryStashKey{service, key})
	return nil
}
