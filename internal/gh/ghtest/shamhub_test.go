package ghtest_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/gh/ghtest"
)

func TestShamHub(t *testing.T) {
	hub, err := ghtest.NewShamHub(ghtest.ShamHubConfig{})
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, hub.Close())
	})

	address, err := hub.NewRepository("abhinav", "gs")
	require.NoError(t, err)

	t.Logf("Git root: %s", hub.GitRoot())
	t.Logf("Repository address: %s", address)

	time.Sleep(5 * time.Minute)
}
