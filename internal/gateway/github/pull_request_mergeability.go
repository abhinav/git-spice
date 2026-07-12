package github

import (
	"context"
	"fmt"
)

// Mergeability is GitHub's mergeability projection for a pull request.
type Mergeability struct {
	// Mergeable is GitHub's conflict calculation.
	Mergeable MergeableState `json:"mergeable"`

	// MergeStateStatus combines branch protection and merge requirements.
	MergeStateStatus MergeStateStatus `json:"mergeStateStatus"`

	// IsDraft reports whether the pull request is a draft.
	IsDraft bool `json:"isDraft"`
}

// PullRequestMergeability loads mergeability for a pull request node ID.
func (c *Gateway) PullRequestMergeability(ctx context.Context, id ID) (*Mergeability, error) {
	var result struct {
		Node *Mergeability `json:"node"`
	}
	query := compactGraphQL(`
		query($id:ID!){
			node(id: $id){
				... on PullRequest{mergeable,mergeStateStatus,isDraft}
			}
		}
	`)
	if err := c.execute(ctx, query, struct {
		ID ID `json:"id"`
	}{id}, &result); err != nil {
		return nil, fmt.Errorf("query pull request mergeability: %w", err)
	}
	return result.Node, nil
}
