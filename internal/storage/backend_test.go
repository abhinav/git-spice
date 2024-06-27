package storage

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/logtest"
)

func TestStorageBackend(t *testing.T) {
	t.Run("Memory", func(t *testing.T) {
		testStorageBackend(t, NewMemBackend())
	})

	t.Run("Git", func(t *testing.T) {
		ctx := context.Background()
		repo, err := git.Init(ctx, t.TempDir(), git.InitOptions{
			Log: logtest.New(t),
		})
		require.NoError(t, err)

		testStorageBackend(t, NewGitBackend(GitConfig{
			Repo:        repo,
			Ref:         "refs/heads/test",
			AuthorName:  "Test",
			AuthorEmail: "test@example.com",
			Log:         logtest.New(t),
		}))
	})
}

func testStorageBackend(t *testing.T, backend Backend) {
	ctx := context.Background()
	db := NewDB(backend)

	t.Run("ClearEmpty", func(t *testing.T) {
		assert.NoError(t, db.Clear(ctx, "clear empty"))
	})

	t.Run("Get/DoesNotExist", func(t *testing.T) {
		var got string
		err := db.Get(ctx, "does/not/exist", &got)
		assert.ErrorIs(t, err, ErrNotExist)
	})

	t.Run("SetAndGet", func(t *testing.T) {
		defer func() {
			assert.NoError(t, db.Clear(ctx, "clear"))
		}()

		require.NoError(t, db.Set(ctx, "foo", "bar", "set foo"))

		var got string
		require.NoError(t, db.Get(ctx, "foo", &got))
		assert.Equal(t, "bar", got)

		require.NoError(t, db.Set(ctx, "foo", "baz", "set foo again"))
		require.NoError(t, db.Get(ctx, "foo", &got))
		assert.Equal(t, "baz", got)
	})

	t.Run("SetNested", func(t *testing.T) {
		defer func() {
			assert.NoError(t, db.Clear(ctx, "clear"))
		}()

		require.NoError(t, db.Set(ctx, "foo/bar", "baz", "set foo/bar"))
		require.NoError(t, db.Set(ctx, "baz/qux", "quux", "set baz/qux"))

		var got1, got2 string
		require.NoError(t, db.Get(ctx, "foo/bar", &got1))
		require.NoError(t, db.Get(ctx, "baz/qux", &got2))
		assert.Equal(t, "baz", got1)
		assert.Equal(t, "quux", got2)

		t.Run("AllKeys", func(t *testing.T) {
			keys, err := db.Keys(ctx, "")
			require.NoError(t, err)

			assert.ElementsMatch(t, []string{
				"foo/bar",
				"baz/qux",
			}, keys)
		})

		t.Run("DirKeys", func(t *testing.T) {
			keys, err := db.Keys(ctx, "foo")
			require.NoError(t, err)

			assert.ElementsMatch(t, []string{"bar"}, keys)
		})
	})

	t.Run("Keys/DoesNotExist", func(t *testing.T) {
		keys, err := db.Keys(ctx, "does/not/exist")
		require.NoError(t, err)
		assert.Empty(t, keys)
	})

	t.Run("ConcurrentSets", func(t *testing.T) {
		defer func() {
			assert.NoError(t, db.Clear(ctx, "clear"))
		}()

		// We can't get too parallel here because the Git backend
		// has a limit of 5 retries before it gives up on a Set.
		// We could use a file-lock, but in practice, git-spice is not
		// intended to be used with several concurrent operations
		// on the same repository.
		const NumWorkers, NumSets = 2, 5

		keys := make([]string, NumSets)
		for i := range keys {
			keys[i] = "key" + strconv.Itoa(i)
		}

		vals := make([][]string, NumWorkers)
		for i := range vals {
			vals[i] = make([]string, NumSets)
			for j := range vals[i] {
				vals[i][j] = "val" + strconv.Itoa(i) + "-" + strconv.Itoa(j)
			}
		}

		var ready, done sync.WaitGroup
		ready.Add(NumWorkers)
		done.Add(NumWorkers)
		for i := range NumWorkers {
			go func(workerIdx int) {
				defer done.Done()

				ready.Done() // I'm ready
				ready.Wait() // Wait for everyone to be ready

				for setIdx := range NumSets {
					assert.NoError(t, db.Set(ctx, keys[setIdx], vals[workerIdx][setIdx], "set"),
						"worker %d, set %d", workerIdx, setIdx)
				}
			}(i)
		}

		done.Wait()

		gotKeys, err := db.Keys(ctx, "")
		require.NoError(t, err)

		assert.ElementsMatch(t, keys, gotKeys)
	})
}
