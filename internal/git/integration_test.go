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

func TestIntegrationPeelToCommit_specialChars(t *testing.T) {
	t.Parallel()

	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		at '2025-11-20T10:00:00Z'

		git init
		git commit --allow-empty -m 'Initial commit'

		# Create branches with special characters
		git checkout -b 'feature/θ-theta'
		git add theta.txt
		git commit -m 'Add theta feature'

		git checkout -b 'feature/✅-checkmark'
		git add checkmark.txt
		git commit -m 'Add checkmark feature'

		git checkout -b 'feature/👨‍💻-developer'
		git add developer.txt
		git commit -m 'Add developer feature'

		git checkout -b 'feature/café'
		git add cafe.txt
		git commit -m 'Add café feature'

		git checkout -b 'feature/日本語'
		git add japanese.txt
		git commit -m 'Add Japanese feature'

		-- theta.txt --
		Theta content
		-- checkmark.txt --
		Checkmark content
		-- developer.txt --
		Developer content
		-- cafe.txt --
		Café content
		-- japanese.txt --
		Japanese content
	`)))
	require.NoError(t, err)

	ctx := t.Context()
	repo, err := git.Open(ctx, fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	tests := []struct {
		name   string
		branch string
	}{
		{"Greek theta", "feature/θ-theta"},
		{"Unicode checkmark", "feature/✅-checkmark"},
		{"Combined emoji", "feature/👨‍💻-developer"},
		{"Accented character", "feature/café"},
		{"Japanese characters", "feature/日本語"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			hash, err := repo.PeelToCommit(ctx, tt.branch)
			require.NoError(t, err)
			assert.NotEmpty(t, hash)
			assert.NotEqual(t, git.ZeroHash, hash)
		})
	}
}
