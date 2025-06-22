package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
)

// ChangesStates retrieves the states of the given changes in bulk.
func (r *Repository) ChangesStates(ctx context.Context, ids []forge.ChangeID) ([]forge.ChangeState, error) {
	var q struct {
		Nodes []struct {
			PullRequest struct {
				State githubv4.PullRequestState `graphql:"state"`
			} `graphql:"... on PullRequest"`
		} `graphql:"nodes(ids: $ids)"`
	}

	gqlIDs := make([]githubv4.ID, len(ids))
	for i, id := range ids {
		pr := mustPR(id)
		var err error
		gqlIDs[i], err = r.graphQLID(ctx, pr)
		if err != nil {
			return nil, fmt.Errorf("resolve ID %v: %w", id, err)
		}
	}

	if err := r.client.Query(ctx, &q, map[string]any{"ids": gqlIDs}); err != nil {
		return nil, fmt.Errorf("retrieve change states: %w", err)
	}

	states := make([]forge.ChangeState, len(ids))
	for i, pr := range q.Nodes {
		states[i] = forgeChangeState(pr.PullRequest.State)
	}

	return states, nil
}
