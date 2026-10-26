package storage

import (
	"errors"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestStorageBackend(t *testing.T) {
	t.Run("MapSync", func(t *testing.T) {
		testStorageBackend(t, SyncBackend(make(MapBackend)))
	})

	t.Run("Git", func(t *testing.T) {
		ctx := t.Context()
		repo, _, err := git.Init(ctx, t.TempDir(), git.InitOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		testStorageBackend(t, NewGitBackend(GitConfig{
			Repo:        repo,
			Ref:         "refs/heads/test",
			AuthorName:  "Test",
			AuthorEmail: "test@example.com",
			Log:         silogtest.New(t),
		}))
	})
}

func testStorageBackend(t *testing.T, backend Backend) {
	ctx := t.Context()
	db := NewDB(backend)

	t.Run("ClearEmpty", func(t *testing.T) {
		assert.NoError(t, db.Clear(ctx, "clear empty"))
	})

	t.Run("Get/DoesNotExist", func(t *testing.T) {
		var got string
		err := db.Get(ctx, "does/not/exist", &got)
		assert.ErrorIs(t, err, ErrNotExist)
	})

	t.Run("GetMany/DoesNotExist", func(t *testing.T) {
		var got string
		err := db.GetMany(ctx, []GetRequest{
			{Key: "does/not/exist", Value: &got},
		})
		assert.ErrorIs(t, err, ErrNotExist)
		assert.EqualError(t, err, "missing: [does/not/exist]")
		missing, ok := errors.AsType[*MissingKeysError](err)
		require.True(t, ok)
		assert.Equal(t, []int{0}, missing.Indexes)
		assert.Equal(t, []string{"does/not/exist"}, missing.Keys)
	})

	t.Run("GetMany/AllMissing", func(t *testing.T) {
		first, second := "unchanged", "also unchanged"
		err := db.GetMany(ctx, []GetRequest{
			{Key: "absent", Value: &first},
			{Key: "also-absent\nwith\ttab", Value: &second},
			{Key: "absent", Value: &first},
		})
		missing, ok := errors.AsType[*MissingKeysError](err)
		require.True(t, ok, "expected missing keys, got %v", err)
		assert.Equal(t, []int{0, 1, 2}, missing.Indexes)
		assert.Equal(t, []string{"absent", "also-absent\nwith\ttab", "absent"}, missing.Keys)
		assert.EqualError(t, err, "missing: [absent also-absent\nwith\ttab absent]")
		assert.Equal(t, "unchanged", first)
		assert.Equal(t, "also unchanged", second)
	})

	t.Run("GetMany/MixedMissing", func(t *testing.T) {
		require.NoError(t, db.Set(ctx, "present", "value", "seed partial reads"))
		defer func() { assert.NoError(t, db.Clear(ctx, "clear partial reads")) }()

		values := []string{"old", "old", "old", "old", "old"}
		err := db.GetMany(ctx, []GetRequest{
			{Key: "missing", Value: &values[0]},
			{Key: "present", Value: &values[1]},
			{Key: "other-missing", Value: &values[2]},
			{Key: "present", Value: &values[3]},
			{Key: "missing", Value: &values[4]},
		})
		assert.ErrorIs(t, err, ErrNotExist)
		missing, ok := errors.AsType[*MissingKeysError](err)
		require.True(t, ok, "expected missing keys, got %v", err)
		assert.Equal(t, []int{0, 2, 4}, missing.Indexes)
		assert.Equal(t, []string{"missing", "other-missing", "missing"}, missing.Keys)
		assert.Equal(t, []string{"old", "value", "old", "value", "old"}, values)
	})

	t.Run("GetMany/DecodeFailureAfterMissing", func(t *testing.T) {
		require.NoError(t, db.Set(ctx, "bad-number", "not a number", "seed decode failure"))
		defer func() { assert.NoError(t, db.Clear(ctx, "clear decode failure")) }()

		var missingValue, number int
		err := db.GetMany(ctx, []GetRequest{
			{Key: "missing", Value: &missingValue},
			{Key: "bad-number", Value: &number},
		})
		require.Error(t, err)
		_, partial := errors.AsType[*MissingKeysError](err)
		assert.False(t, partial, "decode failures must not guarantee partial results")
		assert.NotErrorIs(t, err, ErrNotExist)
	})

	t.Run("GetMany/DecoderReturnsNotFound", func(t *testing.T) {
		require.NoError(t, db.Set(ctx, "present", "value", "seed decoder error"))
		defer func() { assert.NoError(t, db.Clear(ctx, "clear decoder error")) }()

		var value notFoundJSON
		err := db.GetMany(ctx, []GetRequest{{Key: "present", Value: &value}})
		require.Error(t, err)
		_, partial := errors.AsType[*MissingKeysError](err)
		assert.False(t, partial, "a decoder error is not a missing storage key")
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

	t.Run("CompareAndSwap", func(t *testing.T) {
		defer func() {
			assert.NoError(t, db.Clear(ctx, "clear"))
		}()

		require.NoError(t, db.Set(ctx, "value", "old", "set value"))
		snapshot, err := db.Snapshot(ctx)
		require.NoError(t, err)
		require.NoError(t, db.CompareAndSwap(
			ctx,
			snapshot,
			UpdateRequest{
				Sets:    []SetRequest{{Key: "value", Value: "new"}},
				Message: "replace value",
			},
		))

		var got string
		require.NoError(t, db.Get(ctx, "value", &got))
		assert.Equal(t, "new", got)

		err = db.CompareAndSwap(
			ctx,
			snapshot,
			UpdateRequest{
				Sets:    []SetRequest{{Key: "value", Value: "stale"}},
				Message: "stale update",
			},
		)
		assert.ErrorIs(t, err, ErrConflict)
	})

	t.Run("Move", func(t *testing.T) {
		defer func() {
			assert.NoError(t, db.Clear(ctx, "clear"))
		}()

		require.NoError(t, db.Set(ctx, "old", "value", "set old"))
		require.NoError(t, db.Update(ctx, UpdateRequest{
			Moves:   []MoveRequest{{From: "old", To: "new"}},
			Message: "move value",
		}))

		var got string
		assert.ErrorIs(t, db.Get(ctx, "old", &got), ErrNotExist)
		require.NoError(t, db.Get(ctx, "new", &got))
		assert.Equal(t, "value", got)
	})

	t.Run("Move/Absent", func(t *testing.T) {
		defer func() {
			assert.NoError(t, db.Clear(ctx, "clear"))
		}()

		require.NoError(t, db.Set(ctx, "new", "stale", "set stale value"))
		require.NoError(t, db.Update(ctx, UpdateRequest{
			Moves:   []MoveRequest{{From: "old", To: "new"}},
			Message: "move absent value",
		}))

		var got string
		assert.ErrorIs(t, db.Get(ctx, "new", &got), ErrNotExist)
	})

	t.Run("SetAndGetMany", func(t *testing.T) {
		defer func() {
			assert.NoError(t, db.Clear(ctx, "clear"))
		}()

		require.NoError(t, db.Set(ctx, "foo", "bar", "set foo"))
		require.NoError(t, db.Set(ctx, "baz", "qux", "set baz"))

		var got1, got2 string
		require.NoError(t, db.GetMany(ctx, []GetRequest{
			{Key: "foo", Value: &got1},
			{Key: "baz", Value: &got2},
		}))
		assert.Equal(t, "bar", got1)
		assert.Equal(t, "qux", got2)
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
		const NumWorkers, NumSets = 2, 2

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

type notFoundJSON struct{}

func (*notFoundJSON) UnmarshalJSON([]byte) error {
	return ErrNotExist
}
