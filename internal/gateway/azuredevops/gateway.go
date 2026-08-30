// Package azuredevops provides the Azure DevOps API operations used by
// git-spice.
//
// The package owns Azure DevOps SDK clients, authenticated request execution,
// and translation between SDK models and package-owned results.
// Callers supply an organization URL and credential,
// then adapt the gateway's results to their domain models.
// Credential discovery, persistence, and login flows remain outside this
// package.
package azuredevops

import (
	"context"
	"fmt"
	"net/http"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/core"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/identity"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/location"
)

// Authentication identifies the authorization scheme for an access token.
type Authentication int

const (
	// AuthenticationPAT sends the token using Azure DevOps PAT authentication.
	AuthenticationPAT Authentication = iota

	// AuthenticationBearer sends the token as an OAuth bearer token.
	AuthenticationBearer
)

// Options configures a Gateway.
type Options struct {
	// HTTPClient sends Azure DevOps API requests.
	// If nil, NewGateway lets the SDK construct each service client with its
	// default HTTP client.
	HTTPClient *http.Client
}

// Gateway is an organization-scoped Azure DevOps API boundary.
//
// It exposes only the operations required by git-spice,
// translates SDK models into package-owned results,
// and normalizes transport-specific failures into package errors.
type Gateway struct {
	// gitClient accesses Azure Repos Git resources.
	gitClient gitClient

	// identityClient resolves Azure DevOps identities.
	identityClient identityClient

	// locationClient identifies the user authenticated for this connection.
	locationClient locationClient
}

//go:generate go tool mockgen -destination=mocks_test.go -package=azuredevops -write_package_comment=false -typed -mock_names=gitClient=MockGitClient,identityClient=MockIdentityClient,locationClient=MockLocationClient . gitClient,identityClient,locationClient

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

type identityClient interface {
	ReadIdentities(context.Context, identity.ReadIdentitiesArgs) (*[]identity.Identity, error)
}

type locationClient interface {
	GetConnectionData(context.Context, location.GetConnectionDataArgs) (*location.ConnectionData, error)
}

// NewGateway builds an Azure DevOps gateway for one organization.
//
// organizationURL must identify the organization root expected by the SDK.
// The authentication scheme determines how token is sent.
// A nil opts or nil [Options.HTTPClient] uses the SDK's default service
// clients; a configured client transports every SDK request.
func NewGateway(
	ctx context.Context,
	organizationURL string,
	auth Authentication,
	token string,
	opts *Options,
) (*Gateway, error) {
	connection := newConnection(organizationURL, auth, token)
	if opts == nil || opts.HTTPClient == nil {
		gitClient, err := git.NewClient(ctx, connection)
		if err != nil {
			return nil, fmt.Errorf("create git client: %w", err)
		}
		identityClient, err := identity.NewClient(ctx, connection)
		if err != nil {
			return nil, fmt.Errorf("create identity client: %w", err)
		}
		return &Gateway{
			gitClient:      gitClient,
			identityClient: identityClient,
			locationClient: location.NewClient(ctx, connection),
		}, nil
	}

	client := azuredevops.NewClientWithOptions(
		connection,
		organizationURL,
		azuredevops.WithHTTPClient(opts.HTTPClient),
	)
	return &Gateway{
		gitClient:      &git.ClientImpl{Client: *client},
		identityClient: &identity.ClientImpl{Client: *client},
		locationClient: &location.ClientImpl{Client: *client},
	}, nil
}

func newConnection(
	organizationURL string,
	auth Authentication,
	token string,
) *azuredevops.Connection {
	if auth == AuthenticationPAT {
		return azuredevops.NewPatConnection(organizationURL, token)
	}

	connection := azuredevops.NewAnonymousConnection(organizationURL)
	connection.AuthorizationString = "Bearer " + token
	return connection
}
