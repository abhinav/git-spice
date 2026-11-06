package azuredevops

import (
	"context"
	"testing"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/webapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestGateway_AddComment_closesThread(t *testing.T) {
	client := NewMockGitClient(gomock.NewController(t))
	client.EXPECT().CreateThread(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, args git.CreateThreadArgs) (*git.GitPullRequestCommentThread, error) {
			require.NotNil(t, args.CommentThread.Status)
			assert.Equal(t, git.CommentThreadStatusValues.Closed, *args.CommentThread.Status)
			return &git.GitPullRequestCommentThread{
				Id:       new(10),
				Comments: &[]git.Comment{{Id: new(1)}},
			}, nil
		},
	)
	gateway := &Gateway{gitClient: client}

	threadID, commentID, err := gateway.AddComment(
		t.Context(), "project", "repository", 42, "stack comment",
	)
	require.NoError(t, err)
	assert.Equal(t, 10, threadID)
	assert.Equal(t, 1, commentID)
}

func TestGateway_Threads(t *testing.T) {
	client := NewMockGitClient(gomock.NewController(t))
	client.EXPECT().GetThreads(gomock.Any(), gomock.Any()).Return(
		&[]git.GitPullRequestCommentThread{{
			Id:        new(10),
			IsDeleted: new(true),
			Status:    &git.CommentThreadStatusValues.Fixed,
			Comments: &[]git.Comment{{
				Id:          new(1),
				Content:     new("service update"),
				Author:      &webapi.IdentityRef{Id: new("author-id")},
				IsDeleted:   new(false),
				CommentType: &git.CommentTypeValues.System,
			}},
		}},
		nil,
	)
	gateway := &Gateway{gitClient: client}

	threads, err := gateway.Threads(
		t.Context(), "project", "repository", 42,
	)

	require.NoError(t, err)
	assert.Equal(t, []Thread{{
		ID:       10,
		Deleted:  true,
		Resolved: true,
		Comments: []Comment{{
			ID:       1,
			Body:     "service update",
			AuthorID: "author-id",
			System:   true,
		}},
	}}, threads)
}
