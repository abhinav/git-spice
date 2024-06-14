package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
)

// ChangeIsMerged reports whether a change has been merged.
func (r *Repository) ChangeIsMerged(ctx context.Context, id forge.ChangeID) (bool, error) {
	// TODO: Bulk ChangesAreMerged that takes a list of IDs.

	var q struct {
		Repository struct {
			PullRequest struct {
				Merged bool `graphql:"merged"`
			} `graphql:"pullRequest(number: $number)"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}
	err := r.client.Query(ctx, &q, map[string]any{
		"owner":  githubv4.String(r.owner),
		"repo":   githubv4.String(r.repo),
		"number": githubv4.Int(mustPR(id).Number),
	})
	if err != nil {
		return false, fmt.Errorf("query failed: %w", err)
	}

	return q.Repository.PullRequest.Merged, nil
}
