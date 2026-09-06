package git_test

import (
	"errors"
	"io"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.uber.org/mock/gomock"
)

func TestWorktree_OpenBranchDiff(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		_, wt := git.NewFakeRepository(t, "", mockExecer)

		mockExecer.EXPECT().
			Start(gomock.Any()).
			DoAndReturn(func(cmd *exec.Cmd) error {
				assert.Equal(t, []string{
					"git", "diff", "main...feature",
				}, cmd.Args)
				_, err := io.WriteString(cmd.Stdout, "diff output\n")
				return errors.Join(err, cmd.Stdout.(io.Closer).Close())
			})
		mockExecer.EXPECT().
			Wait(gomock.Any()).
			Return(nil)

		diff, err := wt.OpenBranchDiff(t.Context(), "main", "feature")
		require.NoError(t, err)
		got, err := io.ReadAll(diff)
		require.NoError(t, err)
		require.NoError(t, diff.Close())
		assert.Equal(t, "diff output\n", string(got))
	})

	t.Run("StartFailure", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		_, wt := git.NewFakeRepository(t, "", mockExecer)

		mockExecer.EXPECT().
			Start(gomock.Any()).
			Return(errors.New("git did not start"))

		_, err := wt.OpenBranchDiff(t.Context(), "main", "feature")
		assert.ErrorContains(t, err, "start diff: git did not start")
	})

	t.Run("CommandFailure", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		_, wt := git.NewFakeRepository(t, "", mockExecer)

		mockExecer.EXPECT().
			Start(gomock.Any()).
			DoAndReturn(func(cmd *exec.Cmd) error {
				return cmd.Stdout.(io.Closer).Close()
			})
		mockExecer.EXPECT().
			Wait(gomock.Any()).
			Return(errors.New("git command failed"))

		diff, err := wt.OpenBranchDiff(t.Context(), "main", "feature")
		require.NoError(t, err)
		_, err = io.ReadAll(diff)
		require.NoError(t, err)
		assert.ErrorContains(t, diff.Close(), "diff: git command failed")
	})
}
