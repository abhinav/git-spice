package secret

import "sync"

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
