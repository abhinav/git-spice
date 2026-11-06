package storage

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestGitBackend_batchWrites(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"Update", "CompareAndSwap"} {
		t.Run(operation, func(t *testing.T) {
			repo, _, err := git.Init(t.Context(), t.TempDir(), git.InitOptions{Log: silogtest.New(t)})
			require.NoError(t, err)
			backend := NewGitBackend(GitConfig{
				Repo: repo, Ref: "refs/data", AuthorName: "Test", AuthorEmail: "test@example.com", Log: silogtest.New(t),
			})
			require.NoError(t, backend.Update(t.Context(), UpdateRequest{
				Sets: []SetRequest{{Key: "first", Value: "old"}}, Message: "seed store",
			}))
			snapshot, err := backend.Snapshot(t.Context())
			require.NoError(t, err)
			req := UpdateRequest{
				Message: "write batch",
				Sets: []SetRequest{
					{Key: "first", Value: "new"},
					{Key: "nested/long", Value: strings.Repeat("long value", 1000)},
					{Key: "duplicate", Value: "overwritten"},
					{Key: "third", Value: "newline\nNUL\x00"},
					{Key: "duplicate", Value: "last"},
				},
			}
			if operation == "Update" {
				err = backend.Update(t.Context(), req)
			} else {
				err = backend.CompareAndSwap(t.Context(), snapshot, req)
			}
			require.NoError(t, err)
			for key, want := range map[string]string{
				"first": "new", "nested/long": strings.Repeat("long value", 1000), "third": "newline\nNUL\x00", "duplicate": "last",
			} {
				var got string
				require.NoError(t, backend.Get(t.Context(), key, &got))
				assert.Equal(t, want, got)
			}
			var pinned string
			require.NoError(t, snapshot.Get(t.Context(), "first", &pinned))
			assert.Equal(t, "old", pinned)
		})
	}
}

func TestGitBackend_batchEncodingFailure(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"Update", "CompareAndSwap"} {
		t.Run(operation, func(t *testing.T) {
			repo, _, err := git.Init(t.Context(), t.TempDir(), git.InitOptions{Log: silogtest.New(t)})
			require.NoError(t, err)
			backend := NewGitBackend(GitConfig{
				Repo: repo, Ref: "refs/data", AuthorName: "Test", AuthorEmail: "test@example.com", Log: silogtest.New(t),
			})
			require.NoError(t, backend.Update(t.Context(), UpdateRequest{
				Sets: []SetRequest{{Key: "first", Value: "old"}}, Message: "seed store",
			}))
			before, err := repo.PeelToCommit(t.Context(), "refs/data")
			require.NoError(t, err)
			snapshot, err := backend.Snapshot(t.Context())
			require.NoError(t, err)
			req := UpdateRequest{
				Message: "invalid batch",
				Sets: []SetRequest{
					{Key: "first", Value: "new"},
					{Key: "second", Value: "staged but not published"},
					{Key: "invalid", Value: make(chan int)},
				},
			}
			if operation == "Update" {
				err = backend.Update(t.Context(), req)
			} else {
				err = backend.CompareAndSwap(t.Context(), snapshot, req)
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "encode JSON")
			after, err := repo.PeelToCommit(t.Context(), "refs/data")
			require.NoError(t, err)
			assert.Equal(t, before, after, "encoding failure must not publish any changes")
			var got string
			require.NoError(t, backend.Get(t.Context(), "first", &got))
			assert.Equal(t, "old", got)
			assert.ErrorIs(t, backend.Get(t.Context(), "second", &got), ErrNotExist)
		})
	}
}
