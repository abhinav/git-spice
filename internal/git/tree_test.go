package git

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/logtest"
)

func TestMakeTreeRecursive(t *testing.T) {
	ctx := context.Background()
	repo, err := Init(ctx, t.TempDir(), InitOptions{
		Log: logtest.New(t),
	})
	require.NoError(t, err)

	files := map[string]string{
		"top_level":                 "top level file",
		"dir/a":                     "file in dir",
		"dir/b":                     "another file in dir",
		"dir/subdir/c":              "file in subdir",
		"dir/subdir/d":              "another file in subdir",
		"dir/e":                     "back to dir",
		"super/deeply/nested/dir/f": "file in super deeply nested dir",
		"dir/subdir/g/h":            "back to subdir",
	}

	hash, err := MakeTreeRecursive(ctx, repo, func(yield func(BlobInfo) bool) {
		for path, body := range files {
			hash, err := repo.WriteObject(ctx, BlobType, strings.NewReader(body))
			require.NoError(t, err)

			info := BlobInfo{
				Path: path,
				Mode: 0o644,
				Hash: hash,
			}
			if !yield(info) {
				break
			}
		}

		// Overwrite a file.
		newBody := "overwritten file in subdir"
		files["dir/subdir/c"] = newBody
		hash, err := repo.WriteObject(ctx, BlobType, strings.NewReader(newBody))
		require.NoError(t, err)

		yield(BlobInfo{
			Path: "dir/subdir/c",
			Mode: 0o644,
			Hash: hash,
		})
	})
	require.NoError(t, err)

	// Verify the tree.
	items, err := repo.ListTree(ctx, hash, ListTreeOptions{Recurse: true})
	require.NoError(t, err)

	got := make(map[string]string)
	for item, err := range items {
		require.NoError(t, err)
		var buf strings.Builder
		require.NoError(t, repo.ReadObject(ctx, BlobType, item.Hash, &buf))

		got[item.Name] = buf.String()
	}

	require.Equal(t, files, got)
}
