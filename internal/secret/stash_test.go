package secret_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
	"go.abhg.dev/gs/internal/logutil"
	"go.abhg.dev/gs/internal/secret"
)

func TestMain(m *testing.M) {
	// There does not appear to be a way to undo the mock,
	// so do it for the test binary's lifetime
	// instead of trying to do it for a single test.
	keyring.MockInit()

	os.Exit(m.Run())
}

func TestStash(t *testing.T) {
	t.Run("Memory", func(t *testing.T) {
		testStash(t, new(secret.MemoryStash))
	})

	t.Run("Keyring", func(t *testing.T) {
		testStash(t, new(secret.Keyring))
	})

	t.Run("Insecure", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "secrets.json")
		stash := secret.InsecureStash{
			Path: file,
			Log:  logutil.TestLogger(t),
		}
		testStash(t, &stash)
	})

	t.Run("Insecure/NestedDir", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "nested", "dir", "secrets.json")
		stash := secret.InsecureStash{
			Path: file,
			Log:  logutil.TestLogger(t),
		}
		testStash(t, &stash)
	})

	t.Run("Fallback/PrimaryBroken", func(t *testing.T) {
		testStash(t, &secret.FallbackStash{
			Primary: &brokenStash{
				err: errors.New("great sadness"),
			},
			Secondary: new(secret.MemoryStash),
		})
	})

	t.Run("Fallback/PrimaryOkay", func(t *testing.T) {
		testStash(t, &secret.FallbackStash{
			Primary: new(secret.MemoryStash),
			Secondary: &brokenStash{
				err: errors.New("great sadness"),
			},
		})
	})
}

func testStash(t *testing.T, stash secret.Stash) {
	const _service = "test-service"

	t.Run("LoadMissing", func(t *testing.T) {
		_, err := stash.LoadSecret(_service, "missing")
		require.ErrorIs(t, err, secret.ErrNotFound)
	})

	require.NoError(t, stash.SaveSecret(_service, "key", "secret"))

	t.Run("Load", func(t *testing.T) {
		secret, err := stash.LoadSecret(_service, "key")
		require.NoError(t, err)
		assert.Equal(t, "secret", secret)
	})

	t.Run("LoadAnotherMissing", func(t *testing.T) {
		_, err := stash.LoadSecret(_service, "another-key")
		require.ErrorIs(t, err, secret.ErrNotFound)
	})

	t.Run("Overwrite", func(t *testing.T) {
		require.NoError(t, stash.SaveSecret(_service, "key", "new"))

		secret, err := stash.LoadSecret(_service, "key")
		require.NoError(t, err)
		assert.Equal(t, "new", secret)
	})

	t.Run("Delete", func(t *testing.T) {
		require.NoError(t, stash.DeleteSecret(_service, "key"))

		_, err := stash.LoadSecret(_service, "key")
		require.ErrorIs(t, err, secret.ErrNotFound)
	})

	t.Run("DeleteMissing", func(t *testing.T) {
		require.NoError(t, stash.DeleteSecret(_service, "missing"))
	})
}

// brokenStash is a Stash that always returns an error.
type brokenStash struct {
	err error
}

func (b *brokenStash) SaveSecret(service, key, secret string) error {
	return b.err
}

func (b *brokenStash) LoadSecret(service, key string) (string, error) {
	return "", b.err
}

func (b *brokenStash) DeleteSecret(service, key string) error {
	return b.err
}
