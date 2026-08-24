package azuredevops

import (
	"context"
	"errors"
	"net/http"
	"testing"

	azdo "github.com/microsoft/azure-devops-go-api/azuredevops/v7"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
)

func TestRepository_SubmitChange_unsubmittedBase(t *testing.T) {
	createErr := errors.New("target branch not found")
	stub := &stubGitClient{
		createPullRequest: func(
			_ context.Context,
			_ git.CreatePullRequestArgs,
		) (*git.GitPullRequest, error) {
			return nil, createErr
		},
		getRefs: func(
			_ context.Context,
			_ git.GetRefsArgs,
		) (*git.GetRefsResponseValue, error) {
			return &git.GetRefsResponseValue{}, nil
		},
	}

	repo := newTestRepository(stub)

	_, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Test PR",
		Base:    "missing-base",
		Head:    "feature",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, forge.ErrUnsubmittedBase)
	assert.ErrorIs(t, err, createErr)
}

func TestRepository_UpdateChangeComment_notFound(t *testing.T) {
	statusCode := http.StatusNotFound
	stub := &stubGitClient{
		getComment: func(
			context.Context,
			git.GetCommentArgs,
		) (*git.Comment, error) {
			return nil, azdo.WrappedError{StatusCode: &statusCode}
		},
	}

	repo := newTestRepository(stub)

	err := repo.UpdateChangeComment(t.Context(), &PRComment{
		PRID:      42,
		ThreadID:  7,
		CommentID: 3,
	}, "new body")
	require.Error(t, err)
	assert.ErrorIs(t, err, forge.ErrNotFound)
}
