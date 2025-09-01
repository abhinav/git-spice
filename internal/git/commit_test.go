package git_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hexops/autogold/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.abhg.dev/gs/internal/text"
	"go.uber.org/mock/gomock"
)

func TestCommitAheadBehind(t *testing.T) {
	t.Parallel()

	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		at '2025-03-16T18:19:20Z'

		cd upstream
		git init
		git commit --allow-empty -m 'Initial commit'

		git checkout -b feat1
		git add feat1.txt
		git commit -m 'Add feat1'
		git branch feat2
		git branch feat3
		git branch feat4

		cd ..
		git clone upstream fork
		cd fork

		git checkout feat1
		git checkout feat3

		git checkout feat2
		cp $WORK/extra/feat2.txt .
		git add feat2.txt
		git commit -m 'Add feat2'

		cd ../upstream
		git checkout feat3
		git add feat3.txt
		git commit -m 'Add feat3'

		git checkout feat4
		git add feat4.txt
		git commit -m 'Add feat4'

		cd ../fork
		git checkout feat4
		cp $WORK/extra/feat4.txt .
		git add feat4.txt
		git commit -m 'Add feat4-fork'
		git fetch

		-- upstream/feat1.txt --
		feat1
		-- upstream/feat3.txt --
		feat3
		-- upstream/feat4.txt --
		feat4
		-- extra/feat2.txt --
		feat2
		-- extra/feat4.txt --
		feat4-fork
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	// From the point of view of the fork:
	//
	//   - feat1 is in sync with upstream
	//   - feat2 is ahead by 1 commit (the one we just made)
	//   - feat3 is behind by 1 commit (the one we just made in upstream)
	//   - feat4 is ahead and behind by 1 commit
	ctx := t.Context()
	fork, err := git.Open(ctx, filepath.Join(fixture.Dir(), "fork"), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	t.Run("Synced", func(t *testing.T) {
		t.Parallel()

		ahead, behind, err := fork.CommitAheadBehind(ctx, "origin/feat1", "feat1")
		require.NoError(t, err)
		assert.Zero(t, ahead, "expected 0 commits ahead")
		assert.Zero(t, behind, "expected 0 commits behind")
	})

	t.Run("Ahead", func(t *testing.T) {
		t.Parallel()

		ahead, behind, err := fork.CommitAheadBehind(ctx, "origin/feat2", "feat2")
		require.NoError(t, err)
		assert.Equal(t, 1, ahead, "expected 1 commit ahead")
		assert.Zero(t, behind, "expected 0 commits behind")
	})

	t.Run("Behind", func(t *testing.T) {
		t.Parallel()

		ahead, behind, err := fork.CommitAheadBehind(ctx, "origin/feat3", "feat3")
		require.NoError(t, err)
		assert.Zero(t, ahead, "expected 0 commits ahead")
		assert.Equal(t, 1, behind, "expected 1 commit behind")
	})

	t.Run("Both", func(t *testing.T) {
		t.Parallel()

		ahead, behind, err := fork.CommitAheadBehind(ctx, "origin/feat4", "feat4")
		require.NoError(t, err)
		assert.Equal(t, 1, ahead, "expected 1 commit ahead")
		assert.Equal(t, 1, behind, "expected 1 commit behind")
	})
}

func TestRepository_ReadCommit(t *testing.T) {
	tests := []struct {
		name string
		give string
		want *git.CommitObject
	}{
		{
			name: "SimpleCommit",
			give: joinNull(
				"a1b2c3d4e5f6789012345678901234567890abcd",   // hash
				"tree123456789012345678901234567890abcdef12", // tree
				"parent78901234567890123456789012345678901a", // parent
				"Test User",                              // author name
				"test@example.com",                       // author email
				"2023-05-01T10:30:00Z",                   // author date
				"Test User",                              // committer name
				"test@example.com",                       // committer email
				"2023-05-01T10:30:00Z",                   // committer date
				"Add feature X",                          // subject
				"This adds the feature X functionality.", // body
			),
			want: &git.CommitObject{
				Hash:    "a1b2c3d4e5f6789012345678901234567890abcd",
				Tree:    "tree123456789012345678901234567890abcdef12",
				Parents: []git.Hash{"parent78901234567890123456789012345678901a"},
				Author: git.Signature{
					Name:  "Test User",
					Email: "test@example.com",
					Time:  time.Date(2023, 5, 1, 10, 30, 0, 0, time.UTC),
				},
				Committer: git.Signature{
					Name:  "Test User",
					Email: "test@example.com",
					Time:  time.Date(2023, 5, 1, 10, 30, 0, 0, time.UTC),
				},
				Subject: "Add feature X",
				Body:    "This adds the feature X functionality.",
			},
		},
		{
			name: "InitialCommitWithNoParents",
			give: joinNull(
				"initial123456789012345678901234567890abc", // hash
				"tree456789012345678901234567890abcdef123", // tree
				"",                     // no parents
				"Test User",            // author name
				"test@example.com",     // author email
				"2023-04-15T14:22:33Z", // author date
				"Test User",            // committer name
				"test@example.com",     // committer email
				"2023-04-15T14:22:33Z", // committer date
				"Initial commit",       // subject
				"",                     // empty body
			),
			want: &git.CommitObject{
				Hash: "initial123456789012345678901234567890abc",
				Tree: "tree456789012345678901234567890abcdef123",
				Author: git.Signature{
					Name:  "Test User",
					Email: "test@example.com",
					Time:  time.Date(2023, 4, 15, 14, 22, 33, 0, time.UTC),
				},
				Committer: git.Signature{
					Name:  "Test User",
					Email: "test@example.com",
					Time:  time.Date(2023, 4, 15, 14, 22, 33, 0, time.UTC),
				},
				Subject: "Initial commit",
			},
		},
		{
			name: "MergeCommitWithMultipleParents",
			give: joinNull(
				"merge12345678901234567890123456789012345", // hash
				"tree789012345678901234567890abcdef123456", // tree
				"parent1234567890123456789012345678901234 "+
					"parent5678901234567890123456789012345678", // multiple parents
				"Test User",                            // author name
				"test@example.com",                     // author email
				"2023-06-10T09:15:45Z",                 // author date
				"Test User",                            // committer name
				"test@example.com",                     // committer email
				"2023-06-10T09:15:45Z",                 // committer date
				"Merge branch 'feature'",               // subject
				"Merge pull request #123\n\nFeatures:", // multi-line body
			),
			want: &git.CommitObject{
				Hash: "merge12345678901234567890123456789012345",
				Tree: "tree789012345678901234567890abcdef123456",
				Parents: []git.Hash{
					"parent1234567890123456789012345678901234",
					"parent5678901234567890123456789012345678",
				},
				Author: git.Signature{
					Name:  "Test User",
					Email: "test@example.com",
					Time:  time.Date(2023, 6, 10, 9, 15, 45, 0, time.UTC),
				},
				Committer: git.Signature{
					Name:  "Test User",
					Email: "test@example.com",
					Time:  time.Date(2023, 6, 10, 9, 15, 45, 0, time.UTC),
				},
				Subject: "Merge branch 'feature'",
				Body:    "Merge pull request #123\n\nFeatures:",
			},
		},
		{
			name: "CommitWithDifferentAuthorAndCommitter",
			give: joinNull(
				"commit567890123456789012345678901234567890", // hash
				"tree234567890123456789012345678901234567",   // tree
				"parent890123456789012345678901234567890ab",  // parent
				"Alice Author",         // author name
				"alice@example.com",    // author email
				"2023-07-20T16:45:12Z", // author date
				"Charlie Committer",    // committer name
				"charlie@example.com",  // committer email
				"2023-07-20T17:00:00Z", // committer date
				"Fix critical bug",     // subject
				"",                     // empty body
			),
			want: &git.CommitObject{
				Hash:    "commit567890123456789012345678901234567890",
				Tree:    "tree234567890123456789012345678901234567",
				Parents: []git.Hash{"parent890123456789012345678901234567890ab"},
				Author: git.Signature{
					Name:  "Alice Author",
					Email: "alice@example.com",
					Time:  time.Date(2023, 7, 20, 16, 45, 12, 0, time.UTC),
				},
				Committer: git.Signature{
					Name:  "Charlie Committer",
					Email: "charlie@example.com",
					Time:  time.Date(2023, 7, 20, 17, 0, 0, 0, time.UTC),
				},
				Subject: "Fix critical bug",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockExecer := git.NewMockExecer(gomock.NewController(t))
			repo, _ := git.NewFakeRepository(t, "", mockExecer)

			mockExecer.EXPECT().
				Output(gomock.Any()).
				Return([]byte(tt.give), nil)

			got, err := repo.ReadCommit(t.Context(), "test-ref")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRepository_ReadCommit_errors(t *testing.T) {
	tests := []struct {
		name    string
		give    string
		wantErr string
	}{
		{
			name:    "EmptyOutput",
			give:    "",
			wantErr: "no tree hash",
		},
		{
			name: "MissingTreeHash",
			give: joinNull(
				"commit123456789012345678901234567890abcdef",
			),
			wantErr: "no tree hash",
		},
		{
			name: "MissingParentHashes",
			give: joinNull(
				"commit123456789012345678901234567890abcdef",
				"tree456789012345678901234567890abcdef123",
			),
			wantErr: "no parent hashes",
		},
		{
			name: "MissingAuthorName",
			give: joinNull(
				"commit123456789012345678901234567890abcdef",
				"tree456789012345678901234567890abcdef123",
				"parent789012345678901234567890abcdef1234",
			),
			wantErr: "parse author: no name",
		},
		{
			name: "MissingAuthorEmail",
			give: joinNull(
				"commit123456789012345678901234567890abcdef",
				"tree456789012345678901234567890abcdef123",
				"parent789012345678901234567890abcdef1234",
				"Test User",
			),
			wantErr: "parse author: no email",
		},
		{
			name: "InvalidAuthorTime",
			give: joinNull(
				"commit123456789012345678901234567890abcdef",
				"tree456789012345678901234567890abcdef123",
				"parent789012345678901234567890abcdef1234",
				"Test User",
				"test@example.com",
				"invalid-time",
				"", // missing remaining fields
			),
			wantErr: "parse time",
		},
		{
			name: "MissingSubject",
			give: joinNull(
				"commit123456789012345678901234567890abcdef",
				"tree456789012345678901234567890abcdef123",
				"parent789012345678901234567890abcdef1234",
				"Test User",
				"test@example.com",
				"2023-05-01T10:30:00Z",
				"Test User",
				"test@example.com",
				"2023-05-01T10:30:00Z",
			),
			wantErr: "no subject",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockExecer := git.NewMockExecer(gomock.NewController(t))
			repo, _ := git.NewFakeRepository(t, "", mockExecer)

			mockExecer.EXPECT().
				Output(gomock.Any()).
				Return([]byte(tt.give), nil)

			_, err := repo.ReadCommit(t.Context(), "test-ref")
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestRepository_ReadCommit_integration(t *testing.T) {
	t.Parallel()

	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`

		git init
		git config core.editor 'mockedit'

		as 'Test Author <test@author.com>'
		at '2025-06-20T21:28:29Z'
		git add initial.txt
		git commit -m 'Initial commit'

		as 'Different Author <different@author.com>'
		at '2025-06-21T10:15:30Z'
		git add feature.txt
		env MOCKEDIT_GIVE=$WORK/input/feature-commit.txt
		git commit

		git checkout -b feature-branch
		as 'Feature Author <feature@author.com>'
		at '2025-06-22T14:45:00Z'
		git add feature-branch.txt
		git commit -m 'Add feature branch file'

		git checkout main
		as 'Test Author <test@author.com>'
		at '2025-06-23T09:30:15Z'
		git merge feature-branch --no-ff -m 'Merge feature branch'

		-- initial.txt --
		initial content
		-- feature.txt --
		feature content
		-- feature-branch.txt --
		branch content
		-- input/feature-commit.txt --
		Add feature file

		This commit adds a new feature file
		with multi-line commit message.
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	repo, err := git.Open(t.Context(), fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	commits, err := sliceutil.CollectErr(repo.ListCommits(t.Context(), git.CommitRangeFrom("HEAD")))
	require.NoError(t, err)

	autogold.Expect([]git.Hash{
		"65cce60f7a1a1f4f80ac03b668bf9321a446ef30",
		"3730f7992fd7fa7fb0ec1785ce3df1af39c50ac4",
		"1ca834323fa624b86a2afb48c70c333347ed214f",
		"0c86d0963a32be85ff6c7340d5ea98d74e5b9592", // initial commit
	}).Equal(t, commits)

	t.Run("InitialCommit", func(t *testing.T) {
		// commits[3] is the initial commit (oldest)
		commit, err := repo.ReadCommit(t.Context(), commits[3].String())
		require.NoError(t, err)

		assert.Equal(t, "Initial commit", commit.Subject)
		assert.Empty(t, commit.Body)
		assert.Equal(t, "Test Author", commit.Author.Name)
		assert.Equal(t, "test@author.com", commit.Author.Email)
		assert.Equal(t, time.Date(2025, 6, 20, 21, 28, 29, 0, time.UTC), commit.Author.Time)
		assert.Empty(t, commit.Parents, "initial commit should have no parents")
		assert.NotEmpty(t, commit.Hash)
		assert.NotEmpty(t, commit.Tree)
	})

	t.Run("CommitWithMultiLineBody", func(t *testing.T) {
		// commits[2] is the multi-line body commit
		commit, err := repo.ReadCommit(t.Context(), commits[2].String())
		require.NoError(t, err)

		assert.Equal(t, "Add feature file", commit.Subject)
		assert.Equal(t, "This commit adds a new feature file\n"+
			"with multi-line commit message.\n", commit.Body)
		assert.Equal(t, "Different Author", commit.Author.Name)
		assert.Equal(t, "different@author.com", commit.Author.Email)
		assert.Equal(t, time.Date(2025, 6, 21, 10, 15, 30, 0, time.UTC), commit.Author.Time)
		assert.Len(t, commit.Parents, 1, "regular commit should have one parent")
		assert.NotEmpty(t, commit.Hash)
		assert.NotEmpty(t, commit.Tree)
	})

	t.Run("MergeCommit", func(t *testing.T) {
		// commits[0] is the merge commit (HEAD)
		commit, err := repo.ReadCommit(t.Context(), commits[0].String())
		require.NoError(t, err)

		assert.Equal(t, "Merge feature branch", commit.Subject)
		assert.Empty(t, commit.Body)
		assert.Equal(t, "Test Author", commit.Author.Name)
		assert.Equal(t, "test@author.com", commit.Author.Email)
		assert.Equal(t, time.Date(2025, 6, 23, 9, 30, 15, 0, time.UTC), commit.Author.Time)
		assert.Len(t, commit.Parents, 2, "merge commit should have two parents")
		assert.NotEmpty(t, commit.Hash)
		assert.NotEmpty(t, commit.Tree)
	})

	t.Run("CommitByShortHash", func(t *testing.T) {
		// Use commits[2] for the short hash test
		fullHash := commits[2].String()
		shortHash := fullHash[:7] // Truncate to 7 characters for short hash

		shortCommit, err := repo.ReadCommit(t.Context(), shortHash)
		require.NoError(t, err)

		assert.Equal(t, git.Hash(fullHash), shortCommit.Hash)
		assert.Equal(t, "Add feature file", shortCommit.Subject)
	})
}

// joinNull joins strings with null bytes for testing git log output parsing.
func joinNull(parts ...string) string {
	return strings.Join(parts, "\x00")
}
