package github

import (
	"context"
	"fmt"
)

// ReviewThreads is one pull request's review-thread connection.
type ReviewThreads struct {
	// TotalCount is the number of review threads in the connection.
	TotalCount int

	// Nodes contains the IsResolved value for each thread on this page.
	Nodes []bool

	// EndCursor is usable as after when HasNextPage is true.
	EndCursor string

	// HasNextPage reports whether another page follows EndCursor.
	HasNextPage bool
}

// PullRequestReviewThreads loads up to 100 review threads for each ID.
// Each result has the same position as its input ID and exposes continuation
// metadata when more than 100 threads exist.
func (c *Gateway) PullRequestReviewThreads(ctx context.Context, ids []ID) ([]*ReviewThreads, error) {
	var result struct {
		Nodes []struct {
			ReviewThreads reviewThreadsConnection `json:"reviewThreads"`
		} `json:"nodes"`
	}
	query := compactGraphQL(`
		query($ids:[ID!]!){
			nodes(ids: $ids){
				... on PullRequest{
					reviewThreads(first: 100){
						totalCount,
						pageInfo{endCursor,hasNextPage},
						nodes{isResolved}
					}
				}
			}
		}
	`)
	if err := c.execute(ctx, query, struct {
		IDs []ID `json:"ids"`
	}{ids}, &result); err != nil {
		return nil, fmt.Errorf("query pull request review threads: %w", err)
	}
	threads := make([]*ReviewThreads, len(result.Nodes))
	for i, node := range result.Nodes {
		threads[i] = reviewThreads(&node.ReviewThreads)
	}
	return threads, nil
}

// PullRequestReviewThreadsPage loads first review threads after a non-empty
// cursor. First must be from 1 through 100.
func (c *Gateway) PullRequestReviewThreadsPage(ctx context.Context, id ID, first int, after string) (*ReviewThreads, error) {
	var result struct {
		Node struct {
			ReviewThreads reviewThreadsConnection `json:"reviewThreads"`
		} `json:"node"`
	}
	variables := struct {
		After string `json:"after"`
		First int    `json:"first"`
		ID    ID     `json:"id"`
	}{after, first, id}
	query := compactGraphQL(`
		query($after:String!$first:Int!$id:ID!){
			node(id: $id){
				... on PullRequest{
					reviewThreads(first: $first, after: $after){
						totalCount,
						pageInfo{endCursor,hasNextPage},
						nodes{isResolved}
					}
				}
			}
		}
	`)
	if err := c.execute(ctx, query, variables, &result); err != nil {
		return nil, fmt.Errorf("query pull request review threads page: %w", err)
	}
	return reviewThreads(&result.Node.ReviewThreads), nil
}

type reviewThreadsConnection struct {
	TotalCount int `json:"totalCount"`
	PageInfo   struct {
		EndCursor   string `json:"endCursor"`
		HasNextPage bool   `json:"hasNextPage"`
	} `json:"pageInfo"`
	Nodes []struct {
		IsResolved bool `json:"isResolved"`
	} `json:"nodes"`
}

func reviewThreads(connection *reviewThreadsConnection) *ReviewThreads {
	resolved := make([]bool, len(connection.Nodes))
	for i, node := range connection.Nodes {
		resolved[i] = node.IsResolved
	}
	return &ReviewThreads{
		TotalCount:  connection.TotalCount,
		Nodes:       resolved,
		EndCursor:   connection.PageInfo.EndCursor,
		HasNextPage: connection.PageInfo.HasNextPage,
	}
}
