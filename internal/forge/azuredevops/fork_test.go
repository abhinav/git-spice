package azuredevops

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
)

func TestRepository_SubmitChange_pushRepository(t *testing.T) {
	var gotArgs git.CreatePullRequestArgs
	stub := &stubGitClient{
		getRepository: func(
			_ context.Context,
			args git.GetRepositoryArgs,
		) (*git.GitRepository, error) {
			assert.Equal(t, "forkproject", *args.Project)
			assert.Equal(t, "myfork", *args.RepositoryId)
			return &git.GitRepository{Name: new("myfork")}, nil
		},
		createPullRequest: func(
			_ context.Context,
			args git.CreatePullRequestArgs,
		) (*git.GitPullRequest, error) {
			gotArgs = args
			return &git.GitPullRequest{PullRequestId: new(42)}, nil
		},
	}
	repo := newTestRepository(stub)
	pushRepository := &RepositoryID{
		organization: "myorg",
		project:      "forkproject",
		repository:   "myfork",
	}

	_, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject:        "Test PR",
		Base:           "main",
		Head:           "feature",
		PushRepository: pushRepository,
	})
	require.NoError(t, err)

	require.NotNil(t, gotArgs.GitPullRequestToCreate.ForkSource)
	require.NotNil(t, gotArgs.GitPullRequestToCreate.ForkSource.Repository)
	assert.Equal(t, "myfork", *gotArgs.GitPullRequestToCreate.ForkSource.Repository.Name)
}

func TestRepository_FindChangesByBranch_pushRepository(t *testing.T) {
	var gotArgs git.GetPullRequestsArgs
	stub := &stubGitClient{
		getPullRequests: func(
			_ context.Context,
			args git.GetPullRequestsArgs,
		) (*[]git.GitPullRequest, error) {
			gotArgs = args
			return new([]git.GitPullRequest), nil
		},
	}
	repo := newTestRepository(stub)
	pushRepositoryID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	pushRepository := &RepositoryID{
		organization: "myorg",
		project:      "forkproject",
		repository:   "myfork",
	}
	repo.client.gitClient = &stubGitClient{
		getRepository: func(
			_ context.Context,
			args git.GetRepositoryArgs,
		) (*git.GitRepository, error) {
			assert.Equal(t, "forkproject", *args.Project)
			assert.Equal(t, "myfork", *args.RepositoryId)
			return &git.GitRepository{Id: &pushRepositoryID}, nil
		},
		getPullRequests: stub.getPullRequests,
	}

	_, err := repo.FindChangesByBranch(t.Context(), "feature", forge.FindChangesOptions{
		PushRepository: pushRepository,
	})
	require.NoError(t, err)

	require.NotNil(t, gotArgs.SearchCriteria.SourceRepositoryId)
	assert.Equal(t, pushRepositoryID, *gotArgs.SearchCriteria.SourceRepositoryId)
}
