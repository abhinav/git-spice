package git_test

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/git"
	"go.uber.org/mock/gomock"
)

func TestRepository_Var_errorQuotesName(t *testing.T) {
	mockExecer := git.NewMockExecer(gomock.NewController(t))
	repo, _ := git.NewFakeRepository(t, "", mockExecer)

	mockExecer.EXPECT().
		Output(gomock.Any()).
		DoAndReturn(func(cmd *exec.Cmd) ([]byte, error) {
			assert.Equal(t, []string{"git", "var", " "}, cmd.Args)
			return nil, errors.New("git command failed")
		})

	_, err := repo.Var(t.Context(), " ")
	assert.ErrorContains(t, err, `git var " "`)
}

func TestWorktree_Var_errorQuotesName(t *testing.T) {
	mockExecer := git.NewMockExecer(gomock.NewController(t))
	_, wt := git.NewFakeRepository(t, "", mockExecer)

	mockExecer.EXPECT().
		Output(gomock.Any()).
		DoAndReturn(func(cmd *exec.Cmd) ([]byte, error) {
			assert.Equal(t, []string{"git", "var", " "}, cmd.Args)
			return nil, errors.New("git command failed")
		})

	_, err := wt.Var(t.Context(), " ")
	assert.ErrorContains(t, err, `git var " "`)
}
