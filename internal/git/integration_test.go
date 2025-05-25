package git_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.abhg.dev/gs/internal/text"
)

func TestIntegrationCommitListing(t *testing.T) {
	t.Parallel()

	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'

		at '2024-08-27T21:48:32Z'
		git init
		git config core.editor 'mockedit'

		git add init.txt
		git commit -m 'Initial commit'

		at '2024-08-27T21:52:12Z'
		git add feature1.txt
		env MOCKEDIT_GIVE=$WORK/input/feature1-commit.txt
		git commit

		at '2024-08-27T22:10:11Z'
		git add feature2.txt
		env MOCKEDIT_GIVE=$WORK/input/feature2-commit.txt
		git commit

		-- init.txt --
		-- feature1.txt --
		feature 1
		-- feature2.txt --
		feature 2
		-- input/feature1-commit.txt --
		Add feature1

		This is the first feature.
		-- input/feature2-commit.txt --
		Add feature2

		This is the second feature.
	`)))
	require.NoError(t, err)

	ctx := t.Context()
	repo, err := git.Open(ctx, fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	t.Run("ListCommitsDetails", func(t *testing.T) {
		ctx := t.Context()
		commits, err := sliceutil.CollectErr(repo.ListCommitsDetails(ctx, git.CommitRangeFrom("HEAD").Limit(2)))
		require.NoError(t, err)

		// Normalize to UTC
		for i := range commits {
			commits[i].AuthorDate = commits[i].AuthorDate.UTC()
		}

		assert.Equal(t, []git.CommitDetail{
			{
				Hash:       "9045e63b1d4d4e6e2db3fa03d4eb98a6ed653f3a",
				ShortHash:  "9045e63",
				Subject:    "Add feature2",
				AuthorDate: time.Date(2024, 8, 27, 22, 10, 11, 0, time.UTC),
			},
			{
				Hash:       "acca66548dc31594dc3bc669c804e98eda1edc3d",
				ShortHash:  "acca665",
				Subject:    "Add feature1",
				AuthorDate: time.Date(2024, 8, 27, 21, 52, 12, 0, time.UTC),
			},
		}, commits)
	})

	t.Run("CommitSubject", func(t *testing.T) {
		ctx := t.Context()
		subject, err := repo.CommitSubject(ctx, "HEAD")
		require.NoError(t, err)
		assert.Equal(t, "Add feature2", subject)

		subject, err = repo.CommitSubject(ctx, "HEAD^")
		require.NoError(t, err)
		assert.Equal(t, "Add feature1", subject)
	})

	t.Run("CommitMessageRange", func(t *testing.T) {
		ctx := t.Context()
		msgs, err := repo.CommitMessageRange(ctx, "HEAD", "HEAD~2")
		require.NoError(t, err)

		assert.Equal(t, []git.CommitMessage{
			{
				Subject: "Add feature2",
				Body:    "This is the second feature.",
			},
			{
				Subject: "Add feature1",
				Body:    "This is the first feature.",
			},
		}, msgs)
	})
}
