package secret

import "errors"

// FallbackStash is a secret stash that falls back to a secondary stash
// if the primary stash fails.
type FallbackStash struct {
	Primary, Secondary Stash // required
}

// SaveSecret saves a secret to the primary stash.
// If the operation fails, it falls back to the secondary stash.
func (f *FallbackStash) SaveSecret(service, key, secret string) error {
	if err := f.Primary.SaveSecret(service, key, secret); err != nil {
		return f.Secondary.SaveSecret(service, key, secret)
	}
	return nil
}

// LoadSecret loads a secret from the primary stash.
// If the operation fails NOT because the secret is not found,
// it falls back to the secondary stash.
func (f *FallbackStash) LoadSecret(service, key string) (string, error) {
	secret, err := f.Primary.LoadSecret(service, key)
	if err != nil && !errors.Is(err, ErrNotFound) {
		secret, err = f.Secondary.LoadSecret(service, key)
	}
	return secret, err
}

// DeleteSecret deletes a secret from the primary stash,
// and if that fails, from the secondary stash.
func (f *FallbackStash) DeleteSecret(service, key string) error {
	if err := f.Primary.DeleteSecret(service, key); err != nil {
		return f.Secondary.DeleteSecret(service, key)
	}
	return nil
}
