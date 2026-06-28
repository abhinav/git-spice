package shamhub

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git/giturl"
)

func TestForge_ParseRepositoryPath_knownForge(t *testing.T) {
	f := &Forge{Options: Options{URL: "https://shamhub.example"}}
	remoteURL, err := giturl.Parse("git@shamhub-alias:example/repo.git")
	require.NoError(t, err)

	rid, err := f.ParseRepositoryPath(remoteURL.Path)
	require.NoError(t, err)

	assert.Equal(t, "example/repo", rid.String())
	assert.Equal(t,
		"https://shamhub.example/example/repo/change/123",
		rid.ChangeURL(ChangeID(123)))
}
