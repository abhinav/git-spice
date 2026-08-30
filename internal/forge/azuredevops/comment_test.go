package azuredevops

import (
	"context"
	"errors"
	"testing"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/webapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/silog"
	"go.uber.org/mock/gomock"
)

func TestRepository_ListChangeComments_canUpdate(t *testing.T) {
	currentUserID := "11111111-1111-1111-1111-111111111111"
	otherUserID := "22222222-2222-2222-2222-222222222222"
	client := NewMockGitClient(gomock.NewController(t))
	client.EXPECT().GetThreads(gomock.Any(), gomock.Any()).DoAndReturn(
		func(
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
	)
	repo := newTestRepository(client)
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
	client := NewMockGitClient(gomock.NewController(t))
	client.EXPECT().CreateThread(gomock.Any(), gomock.Any()).DoAndReturn(
		func(
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
	)

	repo := newTestRepository(client)

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
		client := NewMockGitClient(gomock.NewController(t))
		client.EXPECT().DeleteComment(gomock.Any(), gomock.Any()).DoAndReturn(
			func(
				_ context.Context,
				args git.DeleteCommentArgs,
			) error {
				gotArgs = args
				return nil
			},
		)

		repo := newTestRepository(client)

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
		client := NewMockGitClient(gomock.NewController(t))
		client.EXPECT().DeleteComment(gomock.Any(), gomock.Any()).DoAndReturn(
			func(
				context.Context,
				git.DeleteCommentArgs,
			) error {
				return giveErr
			},
		)

		repo := newTestRepository(client)

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

func newTestRepository(gitClient gitClient) *Repository {
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
