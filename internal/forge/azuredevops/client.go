package azuredevops

import (
	"context"
	"fmt"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
)

// azureDevOpsClient wraps the Azure DevOps SDK clients.
type azureDevOpsClient struct {
	connection *azuredevops.Connection
	gitClient  git.Client
}

func newAzureDevOpsClient(
	ctx context.Context,
	baseURL string,
	token *AuthenticationToken,
) (*azureDevOpsClient, error) {
	// Create connection with PAT authentication.
	// The Azure DevOps SDK uses PAT for basic auth.
	connection := azuredevops.NewPatConnection(baseURL, token.AccessToken)

	// Create Git client.
	gitClient, err := git.NewClient(ctx, connection)
	if err != nil {
		return nil, fmt.Errorf("create git client: %w", err)
	}

	return &azureDevOpsClient{
		connection: connection,
		gitClient:  gitClient,
	}, nil
}
