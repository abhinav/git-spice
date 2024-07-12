package secret_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
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
