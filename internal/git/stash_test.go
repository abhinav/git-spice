package git_test

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/text"
	"go.uber.org/mock/gomock"
)

func TestRepository_StashCreate(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		_, wt := git.NewFakeRepository(t, "", mockExecer)
		ctx := t.Context()

		mockExecer.EXPECT().
			Output(gomock.Any()).
			DoAndReturn(func(cmd *exec.Cmd) ([]byte, error) {
				assert.Equal(t, []string{"git", "stash", "create", "test message"}, cmd.Args)
				return []byte("abc123def456\n"), nil
			})

		hash, err := wt.StashCreate(ctx, "test message")
		require.NoError(t, err)
		assert.Equal(t, git.Hash("abc123def456"), hash)
	})

	t.Run("NoMessage", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		_, wt := git.NewFakeRepository(t, "", mockExecer)
		ctx := t.Context()

		mockExecer.EXPECT().
			Output(gomock.Any()).
			DoAndReturn(func(cmd *exec.Cmd) ([]byte, error) {
				assert.Equal(t, []string{"git", "stash", "create"}, cmd.Args)
				return []byte("abc123def456\n"), nil
			})

		hash, err := wt.StashCreate(ctx, "")
		require.NoError(t, err)
		assert.Equal(t, git.Hash("abc123def456"), hash)
	})

	t.Run("NoChanges", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		_, wt := git.NewFakeRepository(t, "", mockExecer)
		ctx := t.Context()

		mockExecer.EXPECT().
			Output(gomock.Any()).
			Return([]byte(""), nil)

		hash, err := wt.StashCreate(ctx, "test message")
		assert.ErrorIs(t, err, git.ErrNoChanges)
		assert.Equal(t, git.ZeroHash, hash)
	})

	t.Run("CommandFailure", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		_, wt := git.NewFakeRepository(t, "", mockExecer)
		ctx := t.Context()

		mockExecer.EXPECT().
			Output(gomock.Any()).
			Return(nil, errors.New("git command failed"))

		hash, err := wt.StashCreate(ctx, "test message")
		assert.Error(t, err)
		assert.ErrorContains(t, err, "stash create")
		assert.Equal(t, git.ZeroHash, hash)
	})
}

func TestRepository_StashStore(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		_, wt := git.NewFakeRepository(t, "", mockExecer)
		ctx := t.Context()

		mockExecer.EXPECT().
			Run(gomock.Any()).
			DoAndReturn(func(cmd *exec.Cmd) error {
				assert.Equal(t, []string{"git", "stash", "store", "-m", "test message", "abc123def456"}, cmd.Args)
				return nil
			})

		err := wt.StashStore(ctx, git.Hash("abc123def456"), "test message")
		require.NoError(t, err)
	})

	t.Run("NoMessage", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		_, wt := git.NewFakeRepository(t, "", mockExecer)
		ctx := t.Context()

		mockExecer.EXPECT().
			Run(gomock.Any()).
			DoAndReturn(func(cmd *exec.Cmd) error {
				assert.Equal(t, []string{"git", "stash", "store", "abc123def456"}, cmd.Args)
				return nil
			})

		err := wt.StashStore(ctx, git.Hash("abc123def456"), "")
		require.NoError(t, err)
	})

	t.Run("CommandFailure", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		_, wt := git.NewFakeRepository(t, "", mockExecer)
		ctx := t.Context()

		mockExecer.EXPECT().
			Run(gomock.Any()).
			Return(errors.New("git command failed"))

		err := wt.StashStore(ctx, git.Hash("abc123def456"), "test message")
		assert.Error(t, err)
		assert.ErrorContains(t, err, "stash store")
	})
}

func TestRepository_StashApply(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		_, wt := git.NewFakeRepository(t, "", mockExecer)
		ctx := t.Context()

		mockExecer.EXPECT().
			Run(gomock.Any()).
			DoAndReturn(func(cmd *exec.Cmd) error {
				assert.Equal(t, []string{"git", "stash", "apply", "stash@{1}"}, cmd.Args)
				return nil
			})

		err := wt.StashApply(ctx, "stash@{1}")
		require.NoError(t, err)
	})

	t.Run("NoIndex", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		_, wt := git.NewFakeRepository(t, "", mockExecer)
		ctx := t.Context()

		mockExecer.EXPECT().
			Run(gomock.Any()).
			DoAndReturn(func(cmd *exec.Cmd) error {
				assert.Equal(t, []string{"git", "stash", "apply"}, cmd.Args)
				return nil
			})

		err := wt.StashApply(ctx, "")
		require.NoError(t, err)
	})

	t.Run("CommandFailure", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		_, wt := git.NewFakeRepository(t, "", mockExecer)
		ctx := t.Context()

		mockExecer.EXPECT().
			Run(gomock.Any()).
			Return(errors.New("git command failed"))

		err := wt.StashApply(ctx, "stash@{0}")
		assert.Error(t, err)
		assert.ErrorContains(t, err, "stash apply")
	})
}

func TestRepository_StashIntegration(t *testing.T) {
	t.Parallel()

	t.Run("StashCreateAndStore", func(t *testing.T) {
		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			as 'Test User <test@example.com>'
			at '2025-08-23T06:07:08Z'

			git init
			git add file1.txt
			git commit -m 'Initial commit'

			# Make some changes to stash
			git add file2.txt
			mv file1.new.txt file1.txt

			-- file1.txt --
			original content
			-- file2.txt --
			new content
			-- file1.new.txt --
			modified
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		ctx := t.Context()
		wt, err := git.OpenWorktree(ctx, fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		hash, err := wt.StashCreate(ctx, "test stash message")
		require.NoError(t, err)
		assert.NotEqual(t, git.ZeroHash, hash)
		assert.Len(t, hash.String(), 40)

		err = wt.StashStore(ctx, hash, "stored test stash")
		require.NoError(t, err)
	})

	t.Run("Create", func(t *testing.T) {
		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			as 'Test User <test@example.com>'
			at '2025-08-30T06:07:08Z'

			git init
			git add original.txt
			git commit -m 'Initial commit'

			# Make changes to stash
			git add new.txt
			mv modified.txt original.txt

			-- original.txt --
			original content
			-- new.txt --
			new file content
			-- modified.txt --
			modified content
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		ctx := t.Context()
		wt, err := git.OpenWorktree(ctx, fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		hash, err := wt.StashCreate(ctx, "test stash for pop")
		require.NoError(t, err)
		assert.NotEqual(t, git.ZeroHash, hash)

		t.Run("ApplyFromHash", func(t *testing.T) {
			require.NoError(t, wt.Reset(ctx, "HEAD", git.ResetOptions{Mode: git.ResetHard}))

			require.NoError(t, wt.StashApply(ctx, hash.String()))
			assert.FileExists(t, fixture.Dir()+"/new.txt", "stashed file should exist after pop")
		})

		t.Run("StoreAndApply", func(t *testing.T) {
			require.NoError(t, wt.Reset(ctx, "HEAD", git.ResetOptions{Mode: git.ResetHard}))

			require.NoError(t, wt.StashStore(ctx, hash, "stored stash for pop test"))
			require.NoError(t, wt.StashApply(ctx, "stash@{0}"))
			assert.FileExists(t, fixture.Dir()+"/new.txt", "stashed file should exist after pop")
		})
	})

	t.Run("NoChangesToStash", func(t *testing.T) {
		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			as 'Test User <test@example.com>'
			at '2025-06-20T21:28:29Z'

			git init
			git add clean.txt
			git commit -m 'Clean state'

			-- clean.txt --
			clean content
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		ctx := t.Context()
		wt, err := git.OpenWorktree(ctx, fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		hash, err := wt.StashCreate(ctx, "should fail")
		assert.ErrorIs(t, err, git.ErrNoChanges)
		assert.Equal(t, git.ZeroHash, hash)
	})
}
