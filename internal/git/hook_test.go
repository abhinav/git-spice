package git_test

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.uber.org/mock/gomock"
)

func TestRepository_HookRun(t *testing.T) {
	t.Parallel()

	t.Run("NoArgs", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		repo, _ := git.NewFakeRepository(t, "", mockExecer)
		ctx := t.Context()

		mockExecer.EXPECT().
			Run(gomock.Any()).
			DoAndReturn(func(cmd *exec.Cmd) error {
				assert.Equal(t, []string{
					"git", "hook", "run",
					"--ignore-missing", "commit-msg",
				}, cmd.Args)
				return nil
			})

		err := repo.HookRun(ctx, "commit-msg", nil)
		require.NoError(t, err)
	})

	t.Run("WithArgs", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		repo, _ := git.NewFakeRepository(t, "", mockExecer)
		ctx := t.Context()

		mockExecer.EXPECT().
			Run(gomock.Any()).
			DoAndReturn(func(cmd *exec.Cmd) error {
				assert.Equal(t, []string{
					"git", "hook", "run",
					"--ignore-missing",
					"prepare-commit-msg",
					"--", "file.txt",
				}, cmd.Args)
				return nil
			})

		err := repo.HookRun(ctx, "prepare-commit-msg", &git.HookRunOptions{
			Args: []string{"file.txt"},
		})
		require.NoError(t, err)
	})

	t.Run("WithEnv", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		repo, _ := git.NewFakeRepository(t, "", mockExecer)
		ctx := t.Context()

		mockExecer.EXPECT().
			Run(gomock.Any()).
			DoAndReturn(func(cmd *exec.Cmd) error {
				assert.Equal(t, []string{
					"git", "hook", "run",
					"--ignore-missing", "commit-msg",
					"--", "file.txt",
				}, cmd.Args)
				assert.Contains(t, cmd.Env, "GIT_INDEX_FILE=/tmp/index")
				return nil
			})

		err := repo.HookRun(
			ctx,
			"commit-msg",
			&git.HookRunOptions{
				Args: []string{"file.txt"},
				Env:  []string{"GIT_INDEX_FILE=/tmp/index"},
			},
		)
		require.NoError(t, err)
	})

	t.Run("CommandFailure", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		repo, _ := git.NewFakeRepository(t, "", mockExecer)
		ctx := t.Context()

		mockExecer.EXPECT().
			Run(gomock.Any()).
			Return(errors.New("git command failed"))

		err := repo.HookRun(ctx, "commit msg", nil)
		assert.Error(t, err)
		assert.ErrorContains(t, err, `hook run "commit msg"`)
	})
}
