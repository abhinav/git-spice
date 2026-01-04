package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
)

// RefExists checks if a reference exists in the repository.
// ref must be a fully qualified reference name,
func (r *Repository) RefExists(ctx context.Context, ref string) (bool, error) {
	var q struct {
		Repository struct {
			Ref struct {
				Name githubv4.String `graphql:"name"`
			} `graphql:"ref(qualifiedName: $ref)"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}

	if err := r.gh4.Query(ctx, &q, map[string]any{
		"owner": githubv4.String(r.owner),
		"repo":  githubv4.String(r.repo),
		"ref":   githubv4.String(ref),
	}); err != nil {
		return false, fmt.Errorf("check ref existence: %w", err)
	}

	return q.Repository.Ref.Name != "", nil
}
