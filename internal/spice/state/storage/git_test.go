package storage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/logutil"
)

func TestGitBackendUpdateNoChanges(t *testing.T) {
	ctx := context.Background()
	repo, err := git.Init(ctx, t.TempDir(), git.InitOptions{
		Log: logutil.TestLogger(t),
	})
	require.NoError(t, err)

	backend := NewGitBackend(GitConfig{
		Repo:        repo,
		Ref:         "refs/data",
		AuthorName:  "Test Author",
		AuthorEmail: "test@example.com",
		Log:         logutil.TestLogger(t),
	})

	db := NewDB(backend)
	require.NoError(t, db.Set(ctx, "foo", "bar", "initial set"))

	start, err := repo.PeelToCommit(ctx, "refs/data")
	require.NoError(t, err)

	require.NoError(t, db.Set(ctx, "foo", "bar", "shrug"))

	end, err := repo.PeelToCommit(ctx, "refs/data")
	require.NoError(t, err)

	assert.Equal(t, start, end,
		"there should be no changes in the repository")
}
