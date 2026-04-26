package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
)

// ChangeStatuses retrieves compact statuses for the given changes in bulk.
func (r *Repository) ChangeStatuses(ctx context.Context, ids []forge.ChangeID) ([]forge.ChangeStatus, error) {
	var q struct {
		Nodes []struct {
			PullRequest struct {
				State      githubv4.PullRequestState `graphql:"state"`
				HeadRefOid githubv4.GitObjectID      `graphql:"headRefOid"`
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

	statuses := make([]forge.ChangeStatus, len(ids))
	for i, pr := range q.Nodes {
		statuses[i] = forge.ChangeStatus{
			State:    forgeChangeState(pr.PullRequest.State),
			HeadHash: git.Hash(pr.PullRequest.HeadRefOid),
		}
	}

	return statuses, nil
}
