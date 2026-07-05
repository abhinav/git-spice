package secretserver_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/secret/secretserver"
)

func TestClient(t *testing.T) {
	srv, err := secretserver.NewServer(new(secret.MemoryStash))
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, srv.Close())
	})

	client, err := secretserver.NewClient(srv.URL())
	require.NoError(t, err)

	_, err = client.LoadSecret("shamhub:demo", "token")
	require.ErrorIs(t, err, secret.ErrNotFound)

	require.NoError(t, client.SaveSecret("shamhub:demo", "token", "secret"))

	got, err := client.LoadSecret("shamhub:demo", "token")
	require.NoError(t, err)
	assert.Equal(t, "secret", got)

	require.NoError(t, client.DeleteSecret("shamhub:demo", "token"))

	_, err = client.LoadSecret("shamhub:demo", "token")
	require.ErrorIs(t, err, secret.ErrNotFound)
}
