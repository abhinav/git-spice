package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
)

// IsMerged reports whether a change has been merged.
func (r *Repository) IsMerged(ctx context.Context, id forge.ChangeID) (bool, error) {
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
		"number": githubv4.Int(id),
	})
	if err != nil {
		return false, fmt.Errorf("query failed: %w", err)
	}

	return q.Repository.PullRequest.Merged, nil
}
