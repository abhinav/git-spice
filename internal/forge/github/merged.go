package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
)

// ChangesAreMerged reports whether the given changes have been merged.
// The returned slice is in the same order as the input slice.
func (r *Repository) ChangesAreMerged(ctx context.Context, ids []forge.ChangeID) ([]bool, error) {
	var q struct {
		Nodes []struct {
			PullRequest struct {
				Merged bool `graphql:"merged"`
			} `graphql:"... on PullRequest"`
		} `graphql:"nodes(ids: $ids)"`
	}

	prs := make([]int, len(ids))
	gqlIDs := make([]githubv4.ID, len(ids))
	for i, id := range ids {
		// Since before the first stable v0.1.0,
		// the data store has tracked the GraphQL ID of each change,
		// so this won't actually make a network request.
		//
		// However, if a [PR] was constructed in-process
		// and not from the data store, we need to resolve it,
		// and that will make a network request.
		pr := mustPR(id)
		var err error
		prs[i] = pr.Number
		gqlIDs[i], err = r.graphQLID(ctx, pr)
		if err != nil {
			return nil, fmt.Errorf("resolve ID %v: %w", id, err)
		}
	}

	if err := r.client.Query(ctx, &q, map[string]any{"ids": gqlIDs}); err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	merged := make([]bool, len(ids))
	for i, pr := range q.Nodes {
		merged[i] = pr.PullRequest.Merged
	}

	return merged, nil
}
