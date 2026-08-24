package azuredevops

import (
	"bytes"
	"context"
	"testing"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/silog"
)

func TestRepository_SubmitChange_ignoresAssignees(t *testing.T) {
	var logBuffer bytes.Buffer
	var gotArgs git.CreatePullRequestArgs
	stub := &stubGitClient{
		createPullRequest: func(
			_ context.Context,
			args git.CreatePullRequestArgs,
		) (*git.GitPullRequest, error) {
			gotArgs = args
			prID := 42
			return &git.GitPullRequest{PullRequestId: &prID}, nil
		},
	}

	repo := newTestRepository(stub)
	repo.log = silog.New(&logBuffer, nil)

	_, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject:   "Test PR",
		Base:      "main",
		Head:      "feature",
		Assignees: []string{"alice"},
	})
	require.NoError(t, err)

	assert.Nil(t, gotArgs.GitPullRequestToCreate.Reviewers,
		"assignees must not be mapped to reviewers")
	assert.Contains(t, logBuffer.String(),
		"Azure DevOps does not support PR assignees; ignoring --assign flags")
}

func TestRepository_EditChange_ignoresAssignees(t *testing.T) {
	var logBuffer bytes.Buffer
	stub := &stubGitClient{}

	repo := newTestRepository(stub)
	repo.log = silog.New(&logBuffer, nil)

	err := repo.EditChange(t.Context(), &PR{Number: 42}, forge.EditChangeOptions{
		AddAssignees: []string{"alice"},
	})
	require.NoError(t, err)

	assert.Contains(t, logBuffer.String(),
		"Azure DevOps does not support PR assignees; ignoring --assign flags")
}

func TestRepository_EditChange_ignoresAssigneesWithOtherUpdates(t *testing.T) {
	var logBuffer bytes.Buffer
	var gotArgs git.UpdatePullRequestArgs
	stub := &stubGitClient{
		updatePullRequest: func(
			_ context.Context,
			args git.UpdatePullRequestArgs,
		) (*git.GitPullRequest, error) {
			gotArgs = args
			return &git.GitPullRequest{}, nil
		},
	}

	repo := newTestRepository(stub)
	repo.log = silog.New(&logBuffer, nil)

	err := repo.EditChange(t.Context(), &PR{Number: 42}, forge.EditChangeOptions{
		Base:         "develop",
		AddAssignees: []string{"alice"},
	})
	require.NoError(t, err)

	require.NotNil(t, gotArgs.GitPullRequestToUpdate.TargetRefName)
	assert.Equal(t,
		"refs/heads/develop",
		*gotArgs.GitPullRequestToUpdate.TargetRefName,
	)
	assert.Contains(t, logBuffer.String(),
		"Azure DevOps does not support PR assignees; ignoring --assign flags")
}
