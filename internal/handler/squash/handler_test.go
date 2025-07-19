package squash

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.uber.org/mock/gomock"
)

func TestHandler_SquashBranch(t *testing.T) {
	mockStore := NewMockStore(gomock.NewController(t))
	mockStore.EXPECT().
		Trunk().
		Return("main").
		AnyTimes()

	t.Run("TrunkBranch", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		handler := &Handler{
			Log:        silog.Nop(),
			Store:      mockStore,
			Repository: NewMockGitRepository(ctrl),
			Worktree:   NewMockGitWorktree(ctrl),
			Service:    NewMockService(ctrl),
			Restack:    NewMockRestackHandler(ctrl),
		}

		err := handler.SquashBranch(t.Context(), "main", nil)
		assert.ErrorContains(t, err, "cannot squash the trunk branch")
	})

	t.Run("BranchNotRestacked", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			VerifyRestacked(t.Context(), "feature").
			Return(&spice.BranchNeedsRestackError{Base: "main"})

		handler := &Handler{
			Log:        silog.Nop(),
			Store:      mockStore,
			Service:    mockService,
			Repository: NewMockGitRepository(ctrl),
			Restack:    NewMockRestackHandler(ctrl),
			Worktree:   NewMockGitWorktree(ctrl),
		}

		err := handler.SquashBranch(t.Context(), "feature", &Options{})
		assert.ErrorContains(t, err, "branch feature needs to be restacked before it can be squashed")
	})

	t.Run("CommitAborted", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		branchName := "feature"
		baseHash := git.Hash("abc123")
		headHash := git.Hash("def456")

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			VerifyRestacked(t.Context(), branchName).
			Return(nil)
		mockService.EXPECT().
			LookupBranch(t.Context(), branchName).
			Return(&spice.LookupBranchResponse{
				Head:     headHash,
				BaseHash: baseHash,
			}, nil)

		mockRepo := NewMockGitRepository(ctrl)
		mockRepo.EXPECT().
			CommitMessageRange(t.Context(), headHash.String(), baseHash.String()).
			Return([]git.CommitMessage{
				{Subject: "Add feature", Body: "Implementation"},
			}, nil)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			DetachHead(t.Context(), branchName).
			Return(nil)
		mockWorktree.EXPECT().
			Reset(t.Context(), baseHash.String(), git.ResetOptions{Mode: git.ResetSoft}).
			Return(nil)

		commitErr := errors.New("commit aborted")
		mockWorktree.EXPECT().
			Commit(t.Context(), gomock.Any()).
			Return(commitErr)
		mockWorktree.EXPECT().
			Checkout(t.Context(), branchName).
			Return(nil)

		handler := &Handler{
			Log:        silog.Nop(),
			Repository: mockRepo,
			Worktree:   mockWorktree,
			Store:      mockStore,
			Service:    mockService,
			Restack:    NewMockRestackHandler(ctrl),
		}

		err := handler.SquashBranch(t.Context(), branchName, &Options{})
		assert.ErrorIs(t, err, commitErr)
	})
}

func TestCommitMessageTemplate(t *testing.T) {
	tests := []struct {
		name string
		give []git.CommitMessage
		want string

		commentPrefix string // defaults to "#"
	}{
		{
			name: "Empty",
			want: "# No commits to squash.\n",
		},
		{
			name: "Single",
			give: []git.CommitMessage{
				{
					Subject: "Initial commit",
					Body:    "This is the first commit.",
				},
			},
			want: joinLines(
				"Initial commit",
				"",
				"This is the first commit.",
			),
		},
		{
			name: "Two",
			give: []git.CommitMessage{
				{
					Subject: "Initial commit",
					Body:    "This is the first commit.",
				},
				{
					Subject: "Add feature",
					Body:    "This adds a new feature.",
				},
			},
			want: joinLines(
				"# This is a combination of 2 commits.",
				"# This is the 1st commit message:",
				"",
				"Add feature",
				"",
				"This adds a new feature.",
				"",
				"# This is the commit message #2:",
				"",
				"Initial commit",
				"",
				"This is the first commit.",
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := commitMessageTemplate(tt.commentPrefix, tt.give)
			assert.Equal(t, tt.want, got)
		})
	}
}

func joinLines(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}
