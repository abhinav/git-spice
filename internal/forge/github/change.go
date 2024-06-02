package github

import (
	"context"
	"fmt"
	"strconv"

	"github.com/shurcooL/githubv4"
)

// ChangeID is a unique identifier for a change in a repository.
type ChangeID int

func (id ChangeID) String() string {
	return "#" + strconv.Itoa(int(id))
}

// IsMerged reports whether a change has been merged.
func (f *Forge) IsMerged(ctx context.Context, id ChangeID) (bool, error) {
	var q struct {
		Repository struct {
			PullRequest struct {
				Merged bool `graphql:"merged"`
			} `graphql:"pullRequest(number: $number)"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}
	err := f.client.Query(ctx, &q, map[string]any{
		"owner":  githubv4.String(f.owner),
		"repo":   githubv4.String(f.repo),
		"number": githubv4.Int(id),
	})
	if err != nil {
		return false, fmt.Errorf("query failed: %w", err)
	}

	return q.Repository.PullRequest.Merged, nil
}
