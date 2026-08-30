package azuredevops

import (
	"context"
	"errors"
	"testing"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/core"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/webapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/silog"
)

func TestRepository_ListChangeComments_canUpdate(t *testing.T) {
	currentUserID := "11111111-1111-1111-1111-111111111111"
	otherUserID := "22222222-2222-2222-2222-222222222222"
	stub := &stubGitClient{
		getThreads: func(
			context.Context,
			git.GetThreadsArgs,
		) (*[]git.GitPullRequestCommentThread, error) {
			return &[]git.GitPullRequestCommentThread{{
				Id: new(7),
				Comments: &[]git.Comment{
					{Id: new(1), Content: new("mine"), Author: &webapi.IdentityRef{Id: &currentUserID}},
					{Id: new(2), Content: new("theirs"), Author: &webapi.IdentityRef{Id: &otherUserID}},
				},
			}}, nil
		},
	}
	repo := newTestRepository(stub)
	repo.client.currentUserID = func(context.Context) (string, error) {
		return currentUserID, nil
	}

	var got []*forge.ListChangeCommentItem
	for item, err := range repo.ListChangeComments(
		t.Context(), &PR{Number: 42}, &forge.ListChangeCommentsOptions{CanUpdate: true},
	) {
		require.NoError(t, err)
		got = append(got, item)
	}

	require.Len(t, got, 1)
	assert.Equal(t, "mine", got[0].Body)
}

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

	createPullRequest func(
		context.Context,
		git.CreatePullRequestArgs,
	) (*git.GitPullRequest, error)
	updatePullRequest func(
		context.Context,
		git.UpdatePullRequestArgs,
	) (*git.GitPullRequest, error)
	getRefs func(
		context.Context,
		git.GetRefsArgs,
	) (*git.GetRefsResponseValue, error)
	getPullRequest func(
		context.Context,
		git.GetPullRequestArgs,
	) (*git.GitPullRequest, error)
	getThreads func(
		context.Context,
		git.GetThreadsArgs,
	) (*[]git.GitPullRequestCommentThread, error)
	getPullRequests func(
		context.Context,
		git.GetPullRequestsArgs,
	) (*[]git.GitPullRequest, error)
	getRepository func(
		context.Context,
		git.GetRepositoryArgs,
	) (*git.GitRepository, error)
	getItems func(
		context.Context,
		git.GetItemsArgs,
	) (*[]git.GitItem, error)
	getItem func(
		context.Context,
		git.GetItemArgs,
	) (*git.GitItem, error)
	getPullRequestLabels func(
		context.Context,
		git.GetPullRequestLabelsArgs,
	) (*[]core.WebApiTagDefinition, error)
	getPullRequestReviewers func(
		context.Context,
		git.GetPullRequestReviewersArgs,
	) (*[]git.IdentityRefWithVote, error)
	createPullRequestLabel func(
		context.Context,
		git.CreatePullRequestLabelArgs,
	) (*core.WebApiTagDefinition, error)
	createUnmaterializedPullRequestReviewer func(
		context.Context,
		git.CreateUnmaterializedPullRequestReviewerArgs,
	) (*git.IdentityRefWithVote, error)
	createThread func(
		context.Context,
		git.CreateThreadArgs,
	) (*git.GitPullRequestCommentThread, error)
	getComment func(
		context.Context,
		git.GetCommentArgs,
	) (*git.Comment, error)
	updateComment func(
		context.Context,
		git.UpdateCommentArgs,
	) (*git.Comment, error)
	deleteComment func(context.Context, git.DeleteCommentArgs) error
}

func (s *stubGitClient) CreatePullRequest(
	ctx context.Context,
	args git.CreatePullRequestArgs,
) (*git.GitPullRequest, error) {
	return s.createPullRequest(ctx, args)
}

func (s *stubGitClient) UpdatePullRequest(
	ctx context.Context,
	args git.UpdatePullRequestArgs,
) (*git.GitPullRequest, error) {
	return s.updatePullRequest(ctx, args)
}

func (s *stubGitClient) GetRefs(
	ctx context.Context,
	args git.GetRefsArgs,
) (*git.GetRefsResponseValue, error) {
	return s.getRefs(ctx, args)
}

func (s *stubGitClient) GetPullRequest(
	ctx context.Context,
	args git.GetPullRequestArgs,
) (*git.GitPullRequest, error) {
	return s.getPullRequest(ctx, args)
}

func (s *stubGitClient) GetThreads(
	ctx context.Context,
	args git.GetThreadsArgs,
) (*[]git.GitPullRequestCommentThread, error) {
	return s.getThreads(ctx, args)
}

func (s *stubGitClient) GetPullRequests(
	ctx context.Context,
	args git.GetPullRequestsArgs,
) (*[]git.GitPullRequest, error) {
	return s.getPullRequests(ctx, args)
}

func (s *stubGitClient) GetRepository(
	ctx context.Context,
	args git.GetRepositoryArgs,
) (*git.GitRepository, error) {
	return s.getRepository(ctx, args)
}

func (s *stubGitClient) GetItems(
	ctx context.Context,
	args git.GetItemsArgs,
) (*[]git.GitItem, error) {
	return s.getItems(ctx, args)
}

func (s *stubGitClient) GetItem(
	ctx context.Context,
	args git.GetItemArgs,
) (*git.GitItem, error) {
	return s.getItem(ctx, args)
}

func (s *stubGitClient) GetPullRequestLabels(
	ctx context.Context,
	args git.GetPullRequestLabelsArgs,
) (*[]core.WebApiTagDefinition, error) {
	return s.getPullRequestLabels(ctx, args)
}

func (s *stubGitClient) GetPullRequestReviewers(
	ctx context.Context,
	args git.GetPullRequestReviewersArgs,
) (*[]git.IdentityRefWithVote, error) {
	return s.getPullRequestReviewers(ctx, args)
}

func (s *stubGitClient) CreatePullRequestLabel(
	ctx context.Context,
	args git.CreatePullRequestLabelArgs,
) (*core.WebApiTagDefinition, error) {
	return s.createPullRequestLabel(ctx, args)
}

func (s *stubGitClient) CreateUnmaterializedPullRequestReviewer(
	ctx context.Context,
	args git.CreateUnmaterializedPullRequestReviewerArgs,
) (*git.IdentityRefWithVote, error) {
	return s.createUnmaterializedPullRequestReviewer(ctx, args)
}

func (s *stubGitClient) CreateThread(
	ctx context.Context,
	args git.CreateThreadArgs,
) (*git.GitPullRequestCommentThread, error) {
	return s.createThread(ctx, args)
}

func (s *stubGitClient) GetComment(
	ctx context.Context,
	args git.GetCommentArgs,
) (*git.Comment, error) {
	return s.getComment(ctx, args)
}

func (s *stubGitClient) UpdateComment(
	ctx context.Context,
	args git.UpdateCommentArgs,
) (*git.Comment, error) {
	return s.updateComment(ctx, args)
}

func (s *stubGitClient) DeleteComment(
	ctx context.Context,
	args git.DeleteCommentArgs,
) error {
	return s.deleteComment(ctx, args)
}
