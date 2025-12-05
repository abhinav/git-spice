package git_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.abhg.dev/gs/internal/text"
)

func TestRefspec_Matches(t *testing.T) {
	tests := []struct {
		name    string
		refspec git.Refspec
		ref     string
		want    bool
	}{
		// Exact match cases
		{
			name:    "ExactMatch",
			refspec: "refs/heads/main",
			ref:     "refs/heads/main",
			want:    true,
		},
		{
			name:    "ExactMatchNoMatch",
			refspec: "refs/heads/main",
			ref:     "refs/heads/feature",
			want:    false,
		},
		{
			name:    "ExactMatchCaseSensitive",
			refspec: "refs/heads/Main",
			ref:     "refs/heads/main",
			want:    false,
		},

		// Wildcard pattern cases - prefix only
		{
			name:    "WildcardPrefixMatch",
			refspec: "refs/heads/*",
			ref:     "refs/heads/feature",
			want:    true,
		},
		{
			name:    "WildcardPrefixMatchNested",
			refspec: "refs/heads/*",
			ref:     "refs/heads/feature/foo",
			want:    true,
		},
		{
			name:    "WildcardPrefixNoMatch",
			refspec: "refs/heads/*",
			ref:     "refs/tags/v1.0",
			want:    false,
		},
		{
			name:    "WildcardPrefixTooShort",
			refspec: "refs/heads/*",
			ref:     "refs/heads/",
			want:    true, // empty suffix, so this matches
		},

		// Wildcard pattern cases - prefix and suffix
		{
			name:    "WildcardPrefixAndSuffixMatch",
			refspec: "refs/heads/*/main",
			ref:     "refs/heads/feature/main",
			want:    true,
		},
		{
			name:    "WildcardPrefixAndSuffixMatchLonger",
			refspec: "refs/heads/*/main",
			ref:     "refs/heads/team/feature/main",
			want:    true,
		},
		{
			name:    "WildcardPrefixAndSuffixNoMatch",
			refspec: "refs/heads/*/main",
			ref:     "refs/heads/feature/develop",
			want:    false,
		},
		{
			name:    "WildcardPrefixAndSuffixTooShort",
			refspec: "refs/heads/*/main",
			ref:     "refs/heads/main",
			want:    false, // not long enough for prefix + suffix
		},

		// Wildcard at start
		{
			name:    "WildcardAtStartMatch",
			refspec: "*/main",
			ref:     "refs/heads/main",
			want:    true,
		},
		{
			name:    "WildcardAtStartNoMatch",
			refspec: "*/main",
			ref:     "refs/heads/feature",
			want:    false,
		},

		// Refspec format handling - with '+' prefix
		{
			name:    "ForcePushPrefixExact",
			refspec: "+refs/heads/main",
			ref:     "refs/heads/main",
			want:    true,
		},
		{
			name:    "ForcePushPrefixWildcard",
			refspec: "+refs/heads/*",
			ref:     "refs/heads/feature",
			want:    true,
		},

		// Refspec format handling - with destination
		{
			name:    "WithDestinationExact",
			refspec: "refs/heads/main:refs/remotes/origin/main",
			ref:     "refs/heads/main",
			want:    true,
		},
		{
			name:    "WithDestinationWildcard",
			refspec: "refs/heads/*:refs/remotes/origin/*",
			ref:     "refs/heads/feature",
			want:    true,
		},
		{
			name:    "WithDestinationNoMatch",
			refspec: "refs/heads/main:refs/remotes/origin/main",
			ref:     "refs/heads/feature",
			want:    false,
		},

		// Refspec format handling - with both '+' and destination
		{
			name:    "ForcePushWithDestination",
			refspec: "+refs/heads/*:refs/remotes/origin/*",
			ref:     "refs/heads/feature",
			want:    true,
		},

		// Real-world examples from Issue #962
		{
			name:    "Issue962MinimalRefspecMatch",
			refspec: "+refs/heads/main:refs/remotes/origin/main",
			ref:     "refs/heads/main",
			want:    true,
		},
		{
			name:    "Issue962MinimalRefspecNoMatch",
			refspec: "+refs/heads/main:refs/remotes/origin/main",
			ref:     "refs/heads/feature1",
			want:    false,
		},
		{
			name:    "Issue962StandardRefspec",
			refspec: "+refs/heads/*:refs/remotes/origin/*",
			ref:     "refs/heads/feature1",
			want:    true,
		},

		// Edge cases
		{
			name:    "EmptyRefspec",
			refspec: "",
			ref:     "refs/heads/main",
			want:    false,
		},
		{
			name:    "EmptyRef",
			refspec: "refs/heads/*",
			ref:     "",
			want:    false,
		},
		{
			name:    "OnlyWildcard",
			refspec: "*",
			ref:     "anything",
			want:    true,
		},
		{
			name:    "OnlyWildcardWithColon",
			refspec: "*:refs/remotes/origin/*",
			ref:     "refs/heads/main",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.refspec.Matches(tt.ref)
			assert.Equal(t, tt.want, got,
				"Refspec(%q).Matches(%q) = %v, want %v",
				tt.refspec, tt.ref, got, tt.want)
		})
	}
}

func TestSetRef(t *testing.T) {
	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		at '2024-09-14T15:55:40Z'

		git init
		git commit --allow-empty -m 'Initial commit'

		git add feat1.txt
		git commit -m 'Add feat1'

		git add feat2.txt
		git commit -m 'Add feat2'

		git add feat3.txt
		git commit -m 'Add feat3'

		-- feat1.txt --
		Feature 1
		-- feat2.txt --
		Feature 2
		-- feat3.txt --
		Feature 3
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	repo, err := git.Open(t.Context(), fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	ctx := t.Context()
	branches, err := sliceutil.CollectErr(repo.LocalBranches(ctx, nil))
	require.NoError(t, err)
	if assert.Len(t, branches, 1) {
		assert.Equal(t, "main", branches[0].Name)
	}

	feat3Hash, err := repo.PeelToCommit(ctx, "HEAD")
	require.NoError(t, err)

	require.NoError(t, repo.SetRef(ctx, git.SetRefRequest{
		Ref:     "refs/heads/my-feature",
		Hash:    feat3Hash,
		OldHash: git.ZeroHash,
	}))

	branches, err = sliceutil.CollectErr(repo.LocalBranches(ctx, nil))
	require.NoError(t, err)
	if assert.Len(t, branches, 2) {
		names := []string{branches[0].Name, branches[1].Name}
		assert.ElementsMatch(t, []string{"main", "my-feature"}, names)
	}

	branchHead, err := repo.PeelToCommit(ctx, "my-feature")
	require.NoError(t, err)
	assert.Equal(t, feat3Hash, branchHead)

	t.Run("UpdateBranch", func(t *testing.T) {
		feat2Hash, err := repo.PeelToCommit(ctx, "HEAD^")
		require.NoError(t, err)

		err = repo.SetRef(ctx, git.SetRefRequest{
			Ref:     "refs/heads/my-feature",
			Hash:    feat2Hash,
			OldHash: feat3Hash,
			Reason:  "Moving my-feature back to feat2",
		})
		require.NoError(t, err)

		branchHead, err := repo.PeelToCommit(ctx, "my-feature")
		require.NoError(t, err)
		assert.Equal(t, feat2Hash, branchHead)
	})

	t.Run("AlreadyExists", func(t *testing.T) {
		feat1Hash, err := repo.PeelToCommit(ctx, "HEAD^^")
		require.NoError(t, err)

		err = repo.SetRef(ctx, git.SetRefRequest{
			Ref:     "refs/heads/my-feature",
			Hash:    feat1Hash,
			OldHash: git.ZeroHash,
		})
		require.Error(t, err)

		branchHead, err := repo.PeelToCommit(ctx, "my-feature")
		require.NoError(t, err)
		assert.NotEqual(t, feat1Hash, branchHead)
	})
}
