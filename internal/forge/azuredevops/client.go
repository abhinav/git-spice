package azuredevops

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/core"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/identity"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/location"
)

// azureDevOpsClient wraps the Azure DevOps SDK clients.
type azureDevOpsClient struct {
	connection     *azuredevops.Connection
	gitClient      gitClient
	identityClient identity.Client
	currentUserID  func(context.Context) (string, error)
}

//go:generate go tool mockgen -destination=mocks_test.go -package=azuredevops -write_package_comment=false -typed -mock_names=gitClient=MockGitClient . gitClient

type gitClient interface {
	CreatePullRequest(context.Context, git.CreatePullRequestArgs) (*git.GitPullRequest, error)
	CreatePullRequestLabel(context.Context, git.CreatePullRequestLabelArgs) (*core.WebApiTagDefinition, error)
	CreatePullRequestReviewer(context.Context, git.CreatePullRequestReviewerArgs) (*git.IdentityRefWithVote, error)
	CreateThread(context.Context, git.CreateThreadArgs) (*git.GitPullRequestCommentThread, error)
	CreateUnmaterializedPullRequestReviewer(context.Context, git.CreateUnmaterializedPullRequestReviewerArgs) (*git.IdentityRefWithVote, error)
	DeleteComment(context.Context, git.DeleteCommentArgs) error
	GetComment(context.Context, git.GetCommentArgs) (*git.Comment, error)
	GetItem(context.Context, git.GetItemArgs) (*git.GitItem, error)
	GetItems(context.Context, git.GetItemsArgs) (*[]git.GitItem, error)
	GetPullRequest(context.Context, git.GetPullRequestArgs) (*git.GitPullRequest, error)
	GetPullRequestLabels(context.Context, git.GetPullRequestLabelsArgs) (*[]core.WebApiTagDefinition, error)
	GetPullRequestReviewers(context.Context, git.GetPullRequestReviewersArgs) (*[]git.IdentityRefWithVote, error)
	GetPullRequests(context.Context, git.GetPullRequestsArgs) (*[]git.GitPullRequest, error)
	GetRefs(context.Context, git.GetRefsArgs) (*git.GetRefsResponseValue, error)
	GetRepository(context.Context, git.GetRepositoryArgs) (*git.GitRepository, error)
	GetThreads(context.Context, git.GetThreadsArgs) (*[]git.GitPullRequestCommentThread, error)
	UpdateComment(context.Context, git.UpdateCommentArgs) (*git.Comment, error)
	UpdatePullRequest(context.Context, git.UpdatePullRequestArgs) (*git.GitPullRequest, error)
}

func newAzureDevOpsClient(
	ctx context.Context,
	baseURL string,
	token *AuthenticationToken,
) (*azureDevOpsClient, error) {
	return newAzureDevOpsClientWithHTTPClient(ctx, baseURL, token, nil)
}

func newAzureDevOpsClientWithHTTPClient(
	ctx context.Context,
	baseURL string,
	token *AuthenticationToken,
	httpClient *http.Client,
) (*azureDevOpsClient, error) {
	connection := newAzureDevOpsConnection(baseURL, token)

	var (
		gitClient      git.Client
		identityClient identity.Client
		locationClient location.Client
	)
	if httpClient == nil {
		var err error
		gitClient, err = git.NewClient(ctx, connection)
		if err != nil {
			return nil, fmt.Errorf("create git client: %w", err)
		}
		identityClient, _ = identity.NewClient(ctx, connection)
		locationClient = location.NewClient(ctx, connection)
	} else {
		client := azuredevops.NewClientWithOptions(
			connection,
			baseURL,
			azuredevops.WithHTTPClient(httpClient),
		)
		gitClient = &git.ClientImpl{Client: *client}
		identityClient = &identity.ClientImpl{Client: *client}
		locationClient = &location.ClientImpl{Client: *client}
	}

	return &azureDevOpsClient{
		connection:     connection,
		gitClient:      gitClient,
		identityClient: identityClient,
		currentUserID: func(ctx context.Context) (string, error) {
			data, err := locationClient.GetConnectionData(
				ctx, location.GetConnectionDataArgs{},
			)
			if err != nil {
				return "", fmt.Errorf("get connection data: %w", err)
			}
			if data.AuthenticatedUser == nil || data.AuthenticatedUser.Id == nil {
				return "", errors.New("connection data has no authenticated user")
			}
			return data.AuthenticatedUser.Id.String(), nil
		},
	}, nil
}

func newAzureDevOpsConnection(
	baseURL string,
	token *AuthenticationToken,
) *azuredevops.Connection {
	if token.AuthType != AuthTypeAzureCLI {
		return azuredevops.NewPatConnection(baseURL, token.AccessToken)
	}

	connection := azuredevops.NewAnonymousConnection(baseURL)
	connection.AuthorizationString = "Bearer " + token.AccessToken
	return connection
}
