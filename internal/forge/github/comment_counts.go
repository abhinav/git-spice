package github

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
)

// CommentCountsByChange retrieves comment resolution counts for multiple PRs.
func (r *Repository) CommentCountsByChange(
	ctx context.Context,
	ids []forge.ChangeID,
) ([]*forge.CommentCounts, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	gqlIDs, err := r.resolveGraphQLIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	countsByChange, err := r.gateway.PullRequestReviewThreadCounts(ctx, gqlIDs, nil)
	if err != nil {
		return nil, err
	}

	results := make([]*forge.CommentCounts, len(countsByChange))
	for i, counts := range countsByChange {
		results[i] = &forge.CommentCounts{
			Total:      counts.Total,
			Resolved:   counts.Resolved,
			Unresolved: counts.Total - counts.Resolved,
		}
	}
	return results, nil
}

func (r *Repository) resolveGraphQLIDs(
	ctx context.Context,
	ids []forge.ChangeID,
) ([]github.ID, error) {
	gqlIDs := make([]github.ID, len(ids))
	for i, id := range ids {
		pr := mustPR(id)
		gqlID, err := r.graphQLID(ctx, pr)
		if err != nil {
			return nil, fmt.Errorf("resolve ID %v: %w", id, err)
		}
		gqlIDs[i] = gqlID
	}
	return gqlIDs, nil
}
