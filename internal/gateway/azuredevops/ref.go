package azuredevops

import (
	"context"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
)

// RefExists reports whether repository contains the full ref suffix filter.
// For example, filter "heads/main" matches "refs/heads/main" exactly.
func (g *Gateway) RefExists(
	ctx context.Context,
	project string,
	repository string,
	filter string,
) (bool, error) {
	top := 10
	refs, err := g.gitClient.GetRefs(ctx, git.GetRefsArgs{
		Project:      &project,
		RepositoryId: &repository,
		Filter:       &filter,
		Top:          &top,
	})
	if err != nil {
		return false, normalizeError(err)
	}

	want := "refs/" + filter
	for _, ref := range refs.Value {
		if ref.Name != nil && *ref.Name == want {
			return true, nil
		}
	}
	return false, nil
}
