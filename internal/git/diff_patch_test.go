package git_test

import (
	"bytes"
	"errors"
	"io"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.uber.org/mock/gomock"
)

func TestRepository_DiffTreePatch(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		repo, _ := git.NewFakeRepository(t, "", mockExecer)
		ctx := t.Context()

		mockExecer.EXPECT().
			Run(gomock.Any()).
			DoAndReturn(func(cmd *exec.Cmd) error {
				assert.Equal(t, []string{
					"git", "diff-tree", "--patch",
					"abc123", "def456",
				}, cmd.Args)
				_, _ = io.WriteString(cmd.Stdout, "diff output\n")
				return nil
			})

		var buf bytes.Buffer
		err := repo.DiffTreePatch(ctx, &buf, "abc123", "def456")
		require.NoError(t, err)
		assert.Equal(t, "diff output\n", buf.String())
	})

	t.Run("CommandFailure", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		repo, _ := git.NewFakeRepository(t, "", mockExecer)
		ctx := t.Context()

		mockExecer.EXPECT().
			Run(gomock.Any()).
			Return(errors.New("git command failed"))

		var buf bytes.Buffer
		err := repo.DiffTreePatch(ctx, &buf, "abc123", "def456")
		assert.Error(t, err)
		assert.ErrorContains(t, err, "diff-tree")
	})
}
