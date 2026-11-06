package azuredevops

import (
	"context"
	"errors"
	"fmt"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/identity"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/location"
)

// CurrentUserID returns the identity ID authenticated for this gateway.
// It returns an error when connection data does not identify an authenticated
// user.
func (g *Gateway) CurrentUserID(ctx context.Context) (string, error) {
	data, err := g.locationClient.GetConnectionData(
		ctx, location.GetConnectionDataArgs{},
	)
	if err != nil {
		return "", fmt.Errorf("get connection data: %w", normalizeError(err))
	}
	if data.AuthenticatedUser == nil || data.AuthenticatedUser.Id == nil {
		return "", errors.New("connection data has no authenticated user")
	}
	return data.AuthenticatedUser.Id.String(), nil
}

// ReviewerID resolves reviewer to an Azure DevOps identity ID.
// It first searches exact mail addresses,
// then falls back to Azure DevOps' general identity search.
func (g *Gateway) ReviewerID(ctx context.Context, reviewer string) (string, error) {
	for _, searchFilter := range []string{"MailAddress", "General"} {
		identities, err := g.identityClient.ReadIdentities(
			ctx,
			identity.ReadIdentitiesArgs{
				SearchFilter: &searchFilter,
				FilterValue:  &reviewer,
			},
		)
		if err != nil {
			return "", normalizeError(err)
		}
		for _, id := range *identities {
			if id.Id != nil {
				return id.Id.String(), nil
			}
		}
	}
	return "", fmt.Errorf("reviewer %q not found", reviewer)
}
