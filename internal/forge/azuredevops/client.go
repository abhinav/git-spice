package azuredevops

import (
	"context"
	"fmt"
	"net/http"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/identity"
)

// azureDevOpsClient wraps the Azure DevOps SDK clients.
type azureDevOpsClient struct {
	connection     *azuredevops.Connection
	gitClient      git.Client
	identityClient identity.Client
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
	// Create connection with PAT authentication.
	// The Azure DevOps SDK uses PAT for basic auth.
	connection := azuredevops.NewPatConnection(baseURL, token.AccessToken)

	var (
		gitClient      git.Client
		identityClient identity.Client
	)
	if httpClient == nil {
		var err error
		gitClient, err = git.NewClient(ctx, connection)
		if err != nil {
			return nil, fmt.Errorf("create git client: %w", err)
		}
		identityClient, _ = identity.NewClient(ctx, connection)
	} else {
		client := azuredevops.NewClientWithOptions(
			connection,
			baseURL,
			azuredevops.WithHTTPClient(httpClient),
		)
		gitClient = &git.ClientImpl{Client: *client}
		identityClient = &identity.ClientImpl{Client: *client}
	}

	return &azureDevOpsClient{
		connection:     connection,
		gitClient:      gitClient,
		identityClient: identityClient,
	}, nil
}
