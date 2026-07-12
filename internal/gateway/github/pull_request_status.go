package github

import (
	"context"
	"fmt"
)

// ChangeStatus is GitHub's compact state and head revision for a pull request.
type ChangeStatus struct {
	// State is the pull request lifecycle state.
	State PullRequestState `json:"state"`

	// HeadRefOID is the Git object ID at the head of the pull request.
	HeadRefOID string `json:"headRefOid"`
}

// ChangeStatuses loads compact pull request states in node ID order.
func (c *Gateway) ChangeStatuses(ctx context.Context, ids []ID) ([]*ChangeStatus, error) {
	var result struct {
		Nodes []*ChangeStatus `json:"nodes"`
	}
	query := compactGraphQL(`
		query($ids:[ID!]!){
			nodes(ids: $ids){
				... on PullRequest{state,headRefOid}
			}
		}
	`)
	if err := c.execute(ctx, query, struct {
		IDs []ID `json:"ids"`
	}{ids}, &result); err != nil {
		return nil, fmt.Errorf("query change statuses: %w", err)
	}
	return result.Nodes, nil
}
