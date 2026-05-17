package git_test

import (
	"bytes"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.uber.org/mock/gomock"
)

func TestRepository_Stripspace(t *testing.T) {
	t.Parallel()

	t.Run("Default", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		repo, _ := git.NewFakeRepository(t, "", mockExecer)
		ctx := t.Context()

		mockExecer.EXPECT().
			Run(gomock.Any()).
			DoAndReturn(func(cmd *exec.Cmd) error {
				assert.Equal(t, []string{
					"git", "stripspace",
				}, cmd.Args)

				// Verify stdin is wired up.
				input, err := io.ReadAll(cmd.Stdin)
				if !assert.NoError(t, err) {
					return err
				}
				assert.Equal(t, "hello  \n", string(input))

				// Write to stdout to verify output wiring.
				_, _ = io.WriteString(cmd.Stdout, "hello\n")
				return nil
			})

		var buf bytes.Buffer
		err := repo.Stripspace(
			ctx,
			strings.NewReader("hello  \n"),
			&buf,
			nil,
		)
		require.NoError(t, err)
		assert.Equal(t, "hello\n", buf.String())
	})

	t.Run("StripComments", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		repo, _ := git.NewFakeRepository(t, "", mockExecer)
		ctx := t.Context()

		mockExecer.EXPECT().
			Run(gomock.Any()).
			DoAndReturn(func(cmd *exec.Cmd) error {
				assert.Equal(t, []string{
					"git", "stripspace",
					"--strip-comments",
				}, cmd.Args)
				return nil
			})

		err := repo.Stripspace(
			ctx,
			strings.NewReader(""),
			io.Discard,
			&git.StripspaceOptions{StripComments: true},
		)
		require.NoError(t, err)
	})

	t.Run("CommentLines", func(t *testing.T) {
		mockExecer := git.NewMockExecer(gomock.NewController(t))
		repo, _ := git.NewFakeRepository(t, "", mockExecer)
		ctx := t.Context()

		mockExecer.EXPECT().
			Run(gomock.Any()).
			DoAndReturn(func(cmd *exec.Cmd) error {
				assert.Equal(t, []string{
					"git", "stripspace",
					"--comment-lines",
				}, cmd.Args)
				return nil
			})

		err := repo.Stripspace(
			ctx,
			strings.NewReader(""),
			io.Discard,
			&git.StripspaceOptions{CommentLines: true},
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

		err := repo.Stripspace(
			ctx,
			strings.NewReader(""),
			io.Discard,
			nil,
		)
		assert.Error(t, err)
		assert.ErrorContains(t, err, "stripspace")
	})
}
