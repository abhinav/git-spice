package azuredevops

import (
	"context"
	"errors"
	"testing"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/silog"
)

func TestRepository_PostChangeComment_threadStatus(t *testing.T) {
	// Navigation comments should be posted
	// with "closed" thread status
	// so they don't block merge in Azure DevOps.

	var gotArgs git.CreateThreadArgs
	threadID := 10
	commentID := 1
	stub := &stubGitClient{
		createThread: func(
			_ context.Context,
			args git.CreateThreadArgs,
		) (*git.GitPullRequestCommentThread, error) {
			gotArgs = args
			return &git.GitPullRequestCommentThread{
				Id: &threadID,
				Comments: &[]git.Comment{
					{Id: &commentID},
				},
			}, nil
		},
	}

	repo := newTestRepository(stub)

	_, err := repo.PostChangeComment(
		t.Context(), &PR{Number: 42}, "stack comment",
	)
	require.NoError(t, err)

	require.NotNil(t, gotArgs.CommentThread.Status,
		"thread status must be set")
	assert.Equal(t,
		git.CommentThreadStatusValues.Closed,
		*gotArgs.CommentThread.Status,
		"thread status should be closed",
	)
}

func TestRepository_DeleteChangeComment(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		var gotArgs git.DeleteCommentArgs
		stub := &stubGitClient{
			deleteComment: func(
				_ context.Context,
				args git.DeleteCommentArgs,
			) error {
				gotArgs = args
				return nil
			},
		}

		repo := newTestRepository(stub)

		err := repo.DeleteChangeComment(t.Context(), &PRComment{
			PRID:      42,
			ThreadID:  7,
			CommentID: 3,
		})
		require.NoError(t, err)

		assert.Equal(t, "myproject", *gotArgs.Project)
		assert.Equal(t, "myrepo", *gotArgs.RepositoryId)
		assert.Equal(t, 42, *gotArgs.PullRequestId)
		assert.Equal(t, 7, *gotArgs.ThreadId)
		assert.Equal(t, 3, *gotArgs.CommentId)
	})

	t.Run("Error", func(t *testing.T) {
		giveErr := errors.New("great sadness")
		stub := &stubGitClient{
			deleteComment: func(
				context.Context,
				git.DeleteCommentArgs,
			) error {
				return giveErr
			},
		}

		repo := newTestRepository(stub)

		err := repo.DeleteChangeComment(t.Context(), &PRComment{
			PRID:      1,
			ThreadID:  2,
			CommentID: 3,
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, giveErr)
		assert.ErrorContains(t, err, "delete comment")
	})
}

// newTestRepository builds a Repository
// with a stubbed git client for testing.
func newTestRepository(gitClient git.Client) *Repository {
	return &Repository{
		repoID: &RepositoryID{
			organization: "myorg",
			project:      "myproject",
			repository:   "myrepo",
		},
		log: silog.Nop(),
		client: &azureDevOpsClient{
			gitClient: gitClient,
		},
	}
}

// stubGitClient is a test double for [git.Client]
// that allows overriding individual methods.
// Unset methods will panic if called.
type stubGitClient struct {
	git.Client // embedded to satisfy the interface

	createThread  func(context.Context, git.CreateThreadArgs) (*git.GitPullRequestCommentThread, error)
	deleteComment func(context.Context, git.DeleteCommentArgs) error
}

func (s *stubGitClient) CreateThread(
	ctx context.Context,
	args git.CreateThreadArgs,
) (*git.GitPullRequestCommentThread, error) {
	return s.createThread(ctx, args)
}

func (s *stubGitClient) DeleteComment(
	ctx context.Context,
	args git.DeleteCommentArgs,
) error {
	return s.deleteComment(ctx, args)
}
