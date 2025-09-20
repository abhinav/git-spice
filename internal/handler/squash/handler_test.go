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
			CheckoutBranch(t.Context(), branchName).
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

	t.Run("NoEdit", func(t *testing.T) {
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
				{Subject: "Fix bug", Body: "Fixed issue"},
			}, nil)
		mockRepo.EXPECT().
			SetRef(t.Context(), gomock.Any()).
			Return(nil)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			DetachHead(t.Context(), branchName).
			Return(nil)
		mockWorktree.EXPECT().
			Reset(t.Context(), baseHash.String(), git.ResetOptions{Mode: git.ResetSoft}).
			Return(nil)
		mockWorktree.EXPECT().
			Commit(t.Context(), git.CommitRequest{Message: joinLines(
				"Fix bug",
				"",
				"Fixed issue",
				"",
				"Add feature",
				"",
				"Implementation",
			)}).
			Return(nil)
		mockWorktree.EXPECT().
			Head(t.Context()).
			Return(git.Hash("new123"), nil)
		mockWorktree.EXPECT().
			CheckoutBranch(t.Context(), branchName).
			Return(nil)

		mockRestack := NewMockRestackHandler(ctrl)
		mockRestack.EXPECT().
			RestackUpstack(t.Context(), branchName, nil).
			Return(nil)

		handler := &Handler{
			Log:        silog.Nop(),
			Repository: mockRepo,
			Worktree:   mockWorktree,
			Store:      mockStore,
			Service:    mockService,
			Restack:    mockRestack,
		}

		err := handler.SquashBranch(t.Context(), branchName, &Options{NoEdit: true})
		assert.NoError(t, err)
	})

	t.Run("NoEditWithMessage", func(t *testing.T) {
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
			SetRef(t.Context(), gomock.Any()).
			Return(nil)

		explicitMessage := "Custom commit message"

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			DetachHead(t.Context(), branchName).
			Return(nil)
		mockWorktree.EXPECT().
			Reset(t.Context(), baseHash.String(), git.ResetOptions{Mode: git.ResetSoft}).
			Return(nil)
		mockWorktree.EXPECT().
			Commit(t.Context(), git.CommitRequest{Message: explicitMessage}).
			Return(nil)
		mockWorktree.EXPECT().
			Head(t.Context()).
			Return(git.Hash("new123"), nil)
		mockWorktree.EXPECT().
			CheckoutBranch(t.Context(), branchName).
			Return(nil)

		mockRestack := NewMockRestackHandler(ctrl)
		mockRestack.EXPECT().
			RestackUpstack(t.Context(), branchName, nil).
			Return(nil)

		handler := &Handler{
			Log:        silog.Nop(),
			Repository: mockRepo,
			Worktree:   mockWorktree,
			Store:      mockStore,
			Service:    mockService,
			Restack:    mockRestack,
		}

		err := handler.SquashBranch(t.Context(), branchName, &Options{
			NoEdit:  true,
			Message: explicitMessage,
		})
		assert.NoError(t, err)
	})

	t.Run("NoMessage", func(t *testing.T) {
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
		mockRepo.EXPECT().
			SetRef(t.Context(), gomock.Any()).
			Return(nil)

		expectedTemplate := "Add feature\n\nImplementation\n"

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			DetachHead(t.Context(), branchName).
			Return(nil)
		mockWorktree.EXPECT().
			Reset(t.Context(), baseHash.String(), git.ResetOptions{Mode: git.ResetSoft}).
			Return(nil)
		mockWorktree.EXPECT().
			Commit(t.Context(), git.CommitRequest{
				Message:  "",
				Template: expectedTemplate,
				NoVerify: false,
			}).
			Return(nil)
		mockWorktree.EXPECT().
			Head(t.Context()).
			Return(git.Hash("new123"), nil)
		mockWorktree.EXPECT().
			CheckoutBranch(t.Context(), branchName).
			Return(nil)

		mockRestack := NewMockRestackHandler(ctrl)
		mockRestack.EXPECT().
			RestackUpstack(t.Context(), branchName, nil).
			Return(nil)

		handler := &Handler{
			Log:        silog.Nop(),
			Repository: mockRepo,
			Worktree:   mockWorktree,
			Store:      mockStore,
			Service:    mockService,
			Restack:    mockRestack,
		}

		err := handler.SquashBranch(t.Context(), branchName, &Options{NoEdit: false})
		assert.NoError(t, err)
	})
}

func TestCommitMessageTemplate(t *testing.T) {
	tests := []struct {
		name string
		give []git.CommitMessage
		want string

		commentPrefix string // defaults to "#"
		noComments    bool   // if true, no comments are added
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
		{
			name: "TwoNoComments",
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
				"Add feature",
				"",
				"This adds a new feature.",
				"",
				"Initial commit",
				"",
				"This is the first commit.",
			),
			noComments: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := commitMessageTemplate(
				tt.give,
				tt.commentPrefix,
				!tt.noComments,
			)
			assert.Equal(t, tt.want, got)
		})
	}
}

func joinLines(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}
