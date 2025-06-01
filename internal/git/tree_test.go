package git_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/sliceutil"
)

func TestParseMode(t *testing.T) {
	tests := []struct {
		mode string
		want git.Mode
	}{
		{"100644", git.RegularMode},
		{"040000", git.DirMode},
		{"000000", git.ZeroMode},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			got, err := git.ParseMode(tt.mode)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.mode, got.String())
		})
	}
}

func TestIntegrationListTreeAbsent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	repo, _, err := git.Init(ctx, t.TempDir(), git.InitOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	_, err = sliceutil.CollectErr(repo.ListTree(ctx, "abcdefgh", git.ListTreeOptions{}))
	require.Error(t, err)
}

func TestIntegrationMakeTree(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	repo, _, err := git.Init(ctx, t.TempDir(), git.InitOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	emptyFile, err := repo.WriteObject(ctx, git.BlobType, bytes.NewReader(nil))
	require.NoError(t, err)

	dirHash, numEnts, err := repo.MakeTree(ctx, sliceutil.All2[error]([]git.TreeEntry{
		{Type: git.BlobType, Name: "foo", Hash: emptyFile},
		{Type: git.BlobType, Name: "bar", Hash: emptyFile},
	}))
	require.NoError(t, err)
	assert.Equal(t, 2, numEnts)

	ents, err := sliceutil.CollectErr(repo.ListTree(ctx, dirHash, git.ListTreeOptions{}))
	require.NoError(t, err)

	assert.ElementsMatch(t, []git.TreeEntry{
		{Mode: git.RegularMode, Type: git.BlobType, Name: "foo", Hash: emptyFile},
		{Mode: git.RegularMode, Type: git.BlobType, Name: "bar", Hash: emptyFile},
	}, ents)

	t.Run("subdir", func(t *testing.T) {
		ctx := t.Context()
		newDirHash, numEnts, err := repo.MakeTree(ctx, sliceutil.All2[error]([]git.TreeEntry{
			{Type: git.BlobType, Name: "baz", Hash: emptyFile},
			{Type: git.TreeType, Name: "sub", Hash: dirHash},
		}))
		require.NoError(t, err)
		assert.Equal(t, 2, numEnts)

		ents, err := sliceutil.CollectErr(repo.ListTree(ctx, newDirHash, git.ListTreeOptions{
			Recurse: true,
		}))
		require.NoError(t, err)

		assert.ElementsMatch(t, []git.TreeEntry{
			{Mode: git.RegularMode, Type: git.BlobType, Name: "baz", Hash: emptyFile},
			{Mode: git.RegularMode, Type: git.BlobType, Name: "sub/foo", Hash: emptyFile},
			{Mode: git.RegularMode, Type: git.BlobType, Name: "sub/bar", Hash: emptyFile},
		}, ents)
	})
}

func TestIntegrationUpdateTree(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	repo, _, err := git.Init(ctx, t.TempDir(), git.InitOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	emptyFile, err := repo.WriteObject(ctx, git.BlobType, bytes.NewReader(nil))
	require.NoError(t, err)

	emptyTree, numEnts, err := repo.MakeTree(ctx, sliceutil.Empty2[git.TreeEntry, error]())
	require.NoError(t, err)
	assert.Equal(t, 0, numEnts)

	t.Run("no updates", func(t *testing.T) {
		got, err := repo.UpdateTree(ctx, git.UpdateTreeRequest{Tree: emptyTree})
		require.NoError(t, err)
		assert.Equal(t, emptyTree, got)
	})

	newHash, err := repo.UpdateTree(ctx, git.UpdateTreeRequest{
		Tree: emptyTree,
		Writes: []git.BlobInfo{
			{Path: "foo", Hash: emptyFile},
			{Path: "bar/baz", Hash: emptyFile},
			{Path: "qux/quux/qu", Hash: emptyFile},
		},
	})
	require.NoError(t, err)

	ents, err := sliceutil.CollectErr(repo.ListTree(ctx, newHash, git.ListTreeOptions{
		Recurse: true,
	}))
	require.NoError(t, err)
	assert.ElementsMatch(t, []git.TreeEntry{
		{Mode: git.RegularMode, Type: git.BlobType, Name: "foo", Hash: emptyFile},
		{Mode: git.RegularMode, Type: git.BlobType, Name: "bar/baz", Hash: emptyFile},
		{Mode: git.RegularMode, Type: git.BlobType, Name: "qux/quux/qu", Hash: emptyFile},
	}, ents)

	t.Run("overwrite", func(t *testing.T) {
		ctx := t.Context()
		newBlob, err := repo.WriteObject(ctx, git.BlobType, bytes.NewReader([]byte("hello")))
		require.NoError(t, err)

		overwrittenHash, err := repo.UpdateTree(ctx, git.UpdateTreeRequest{
			Tree: newHash,
			Writes: []git.BlobInfo{
				{Mode: git.RegularMode, Path: "foo", Hash: newBlob},
			},
		})
		require.NoError(t, err)

		ents, err := sliceutil.CollectErr(repo.ListTree(ctx, overwrittenHash, git.ListTreeOptions{
			Recurse: true,
		}))
		require.NoError(t, err)
		assert.ElementsMatch(t, []git.TreeEntry{
			{Mode: git.RegularMode, Type: git.BlobType, Name: "foo", Hash: newBlob},
			{Mode: git.RegularMode, Type: git.BlobType, Name: "bar/baz", Hash: emptyFile},
			{Mode: git.RegularMode, Type: git.BlobType, Name: "qux/quux/qu", Hash: emptyFile},
		}, ents)
	})

	t.Run("delete", func(t *testing.T) {
		ctx := t.Context()
		deletedHash, err := repo.UpdateTree(ctx, git.UpdateTreeRequest{
			Tree:    newHash,
			Deletes: []string{"bar/baz"},
		})
		require.NoError(t, err)

		ents, err := sliceutil.CollectErr(repo.ListTree(ctx, deletedHash, git.ListTreeOptions{
			Recurse: true,
		}))
		require.NoError(t, err)
		assert.ElementsMatch(t, []git.TreeEntry{
			{Mode: git.RegularMode, Type: git.BlobType, Name: "foo", Hash: emptyFile},
			{Mode: git.RegularMode, Type: git.BlobType, Name: "qux/quux/qu", Hash: emptyFile},
		}, ents)
	})

	// empty directories are pruned from the tree.
	t.Run("clear empty dirs", func(t *testing.T) {
		ctx := t.Context()
		deletedHash, err := repo.UpdateTree(ctx, git.UpdateTreeRequest{
			Tree:    newHash,
			Deletes: []string{"qux/quux/qu"},
		})
		require.NoError(t, err)

		ents, err := sliceutil.CollectErr(repo.ListTree(ctx, deletedHash, git.ListTreeOptions{}))
		require.NoError(t, err)
		assert.ElementsMatch(t, []git.TreeEntry{
			{Mode: git.DirMode, Type: git.TreeType, Name: "bar", Hash: "94b2978d84f4cbb7449c092255b38a1e1b40da42"},
			{Mode: git.RegularMode, Type: git.BlobType, Name: "foo", Hash: emptyFile},
		}, ents)

		ents, err = sliceutil.CollectErr(repo.ListTree(ctx, deletedHash, git.ListTreeOptions{
			Recurse: true,
		}))
		require.NoError(t, err)
		assert.ElementsMatch(t, []git.TreeEntry{
			{Mode: git.RegularMode, Type: git.BlobType, Name: "foo", Hash: emptyFile},
			{Mode: git.RegularMode, Type: git.BlobType, Name: "bar/baz", Hash: emptyFile},
		}, ents)
	})

	t.Run("delete all files", func(t *testing.T) {
		ctx := t.Context()
		deletedHash, err := repo.UpdateTree(ctx, git.UpdateTreeRequest{
			Tree:    newHash,
			Deletes: []string{"foo", "bar/baz", "qux/quux/qu"},
		})
		require.NoError(t, err)

		ents, err := sliceutil.CollectErr(repo.ListTree(ctx, deletedHash, git.ListTreeOptions{}))
		require.NoError(t, err)
		assert.Empty(t, ents)
	})
}
