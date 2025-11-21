package git_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
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

func TestIntegrationListTree_specialCharsFromFiles(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	dir := t.TempDir()
	repo, _, err := git.Init(ctx, dir, git.InitOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	// Create actual files with special characters in their names
	specialNames := []string{
		"θ-theta",
		"✅-checkmark",
		"👨‍💻-developer",
		"café",
		"日本語",
	}

	// Create a subdirectory to hold the files
	featureDir := filepath.Join(dir, "feature")
	require.NoError(t, os.MkdirAll(featureDir, 0o755))

	for _, name := range specialNames {
		filePath := filepath.Join(featureDir, name)
		require.NoError(t, os.WriteFile(filePath, []byte("content"), 0o644))
	}

	// Add files to git index and commit
	addCmd := exec.Command("git", "add", "feature")
	addCmd.Dir = dir
	require.NoError(t, addCmd.Run())

	commitCmd := exec.Command("git", "commit", "-m", "Add files with special characters")
	commitCmd.Dir = dir
	commitCmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	require.NoError(t, commitCmd.Run())

	// Get the commit hash
	revParseCmd := exec.Command("git", "rev-parse", "HEAD")
	revParseCmd.Dir = dir
	commitHashBytes, err := revParseCmd.Output()
	require.NoError(t, err)
	commitHash := git.Hash(string(bytes.TrimSpace(commitHashBytes)))

	// Get the tree from the commit
	treeHash, err := repo.PeelToTree(ctx, commitHash.String())
	require.NoError(t, err)

	// List the tree recursively
	ents, err := sliceutil.CollectErr(repo.ListTree(ctx, treeHash, git.ListTreeOptions{
		Recurse: true,
	}))
	require.NoError(t, err)

	// Extract names from the returned entries
	var gotNames []string
	for _, ent := range ents {
		gotNames = append(gotNames, ent.Name)
	}

	// Build expected names with feature/ prefix
	var expectedNames []string
	for _, name := range specialNames {
		expectedNames = append(expectedNames, "feature/"+name)
	}

	// Verify all special character names are returned exactly as they were created
	assert.ElementsMatch(t, expectedNames, gotNames,
		"ListTree should return file names with special characters exactly as they were created")
}

func TestIntegrationListTree_specialCharsFromMakeTree(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	repo, _, err := git.Init(ctx, t.TempDir(), git.InitOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	emptyFile, err := repo.WriteObject(ctx, git.BlobType, bytes.NewReader(nil))
	require.NoError(t, err)

	// Create tree entries with special characters in their names
	// (without slashes since MakeTree doesn't allow them in individual entry names)
	specialNames := []string{
		"θ-theta",
		"✅-checkmark",
		"👨‍💻-developer",
		"café",
		"日本語",
	}

	var entries []git.TreeEntry
	for _, name := range specialNames {
		entries = append(entries, git.TreeEntry{
			Type: git.BlobType,
			Name: name,
			Hash: emptyFile,
		})
	}

	treeHash, numEnts, err := repo.MakeTree(ctx, sliceutil.All2[error](entries))
	require.NoError(t, err)
	assert.Equal(t, len(specialNames), numEnts)

	// List the tree and verify names match exactly
	ents, err := sliceutil.CollectErr(repo.ListTree(ctx, treeHash, git.ListTreeOptions{}))
	require.NoError(t, err)

	// Extract names from the returned entries
	var gotNames []string
	for _, ent := range ents {
		gotNames = append(gotNames, ent.Name)
	}

	// Verify all special character names are returned exactly as they were created
	assert.ElementsMatch(t, specialNames, gotNames,
		"ListTree should return names with special characters exactly as they were created")
}
