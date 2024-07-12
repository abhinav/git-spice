// Package secret provides a layer for storing secretes.
package secret

import (
	"errors"

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
