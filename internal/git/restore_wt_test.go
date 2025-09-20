package git_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/text"
	"go.uber.org/mock/gomock"
)

func TestWorktree_Restore(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		_, wt := git.NewFakeRepository(t, "", mockExecer)
		ctx := t.Context()

		mockExecer.EXPECT().
			Run(gomock.Any()).
			DoAndReturn(func(cmd *exec.Cmd) error {
				assert.Equal(t, []string{"git", "restore", "--", "file1.txt", "file2.txt"}, cmd.Args)
				return nil
			})

		err := wt.Restore(ctx, &git.RestoreRequest{
			PathSpecs: []string{"file1.txt", "file2.txt"},
		})
		require.NoError(t, err)
	})

	t.Run("SingleFile", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		_, wt := git.NewFakeRepository(t, "", mockExecer)
		ctx := t.Context()

		mockExecer.EXPECT().
			Run(gomock.Any()).
			DoAndReturn(func(cmd *exec.Cmd) error {
				assert.Equal(t, []string{"git", "restore", "--", "."}, cmd.Args)
				return nil
			})

		err := wt.Restore(ctx, &git.RestoreRequest{
			PathSpecs: []string{"."},
		})
		require.NoError(t, err)
	})

	t.Run("CommandFailure", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		_, wt := git.NewFakeRepository(t, "", mockExecer)
		ctx := t.Context()

		mockExecer.EXPECT().
			Run(gomock.Any()).
			Return(errors.New("git command failed"))

		err := wt.Restore(ctx, &git.RestoreRequest{
			PathSpecs: []string{"file.txt"},
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "git restore")
	})
}

func TestWorktree_RestoreIntegration(t *testing.T) {
	t.Parallel()

	t.Run("RestoreOneFile", func(t *testing.T) {
		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			as 'Test User <test@example.com>'
			at '2025-09-20T21:28:29Z'

			git init
			git add file1.txt file2.txt
			git commit -m 'Initial commit'

			# Modify files in working tree
			mv file1.modified.txt file1.txt
			mv file2.modified.txt file2.txt

			-- file1.txt --
			original content 1
			-- file2.txt --
			original content 2
			-- file1.modified.txt --
			modified content 1
			-- file2.modified.txt --
			modified content 2
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		ctx := t.Context()
		wt, err := git.OpenWorktree(ctx, fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		require.NoError(t, wt.Restore(ctx, &git.RestoreRequest{
			PathSpecs: []string{"file1.txt"},
		}))

		file1, err := os.ReadFile(filepath.Join(fixture.Dir(), "file1.txt"))
		require.NoError(t, err)
		assert.Equal(t, "original content 1", strings.TrimSpace(string(file1)))

		file2, err := os.ReadFile(filepath.Join(fixture.Dir(), "file2.txt"))
		require.NoError(t, err)
		assert.Equal(t, "modified content 2", strings.TrimSpace(string(file2)))
	})

	t.Run("RestoreAllFiles", func(t *testing.T) {
		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			as 'Test User <test@example.com>'
			at '2025-09-20T21:28:29Z'

			git init
			git add original.txt
			git commit -m 'Initial commit'

			# Stage new file and modify existing file
			git add new.txt
			mv original.modified.txt original.txt

			-- original.txt --
			original content
			-- new.txt --
			new file content
			-- original.modified.txt --
			modified original
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		ctx := t.Context()
		wt, err := git.OpenWorktree(ctx, fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		require.NoError(t, wt.Restore(ctx, &git.RestoreRequest{
			PathSpecs: []string{"."},
		}))

		body, err := os.ReadFile(filepath.Join(fixture.Dir(), "original.txt"))
		require.NoError(t, err)
		assert.Equal(t, "original content", strings.TrimSpace(string(body)))
		assert.FileExists(t, filepath.Join(fixture.Dir(), "new.txt"))
	})

	t.Run("AbsentFile", func(t *testing.T) {
		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			as 'Test User <test@example.com>'
			at '2025-09-20T21:28:29Z'

			git init
			git add existing.txt
			git commit -m 'Initial commit'

			-- existing.txt --
			existing content
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		ctx := t.Context()
		wt, err := git.OpenWorktree(ctx, fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		err = wt.Restore(ctx, &git.RestoreRequest{
			PathSpecs: []string{"nonexistent.txt"},
		})

		// Git restore fails for nonexistent files.
		assert.Error(t, err)
		assert.ErrorContains(t, err, "git restore")
	})
}
