package github

import (
	"context"
	"fmt"
	"strconv"
)

// ReviewThreadCounts summarizes all review threads for one pull request.
type ReviewThreadCounts struct {
	// Total is the number of review threads reported by GitHub.
	Total int

	// Resolved is the number of threads marked resolved across every page.
	Resolved int
}

// PullRequestReviewThreadCounts returns one complete count for each input ID.
// Results have the same length and order as ids. The gateway retains the
// batched first-page request, then traverses each pull request's continuation
// pages. A nil opts or zero [PaginationOptions.ItemsPerPage] requests 100
// threads per page.
func (c *Gateway) PullRequestReviewThreadCounts(ctx context.Context, ids []ID, opts *PaginationOptions) ([]*ReviewThreadCounts, error) {
	itemsPerPage, err := paginationItemsPerPage(opts, 100)
	if err != nil {
		return nil, err
	}

	pages, err := c.pullRequestReviewThreadsFirstPages(ctx, ids, itemsPerPage)
	if err != nil {
		return nil, fmt.Errorf("query review threads: %w", err)
	}
	if len(pages) != len(ids) {
		return nil, fmt.Errorf("match review thread results: got %d nodes for %d IDs", len(pages), len(ids))
	}

	results := make([]*ReviewThreadCounts, len(ids))
	for i, page := range pages {
		total := page.totalCount
		resolved := countResolvedReviewThreads(page.nodes)
		cursor := page.endCursor
		for pageNum := 2; page.hasNextPage; pageNum++ {
			page, err = c.pullRequestReviewThreadsPage(ctx, ids[i], itemsPerPage, cursor)
			if err != nil {
				return nil, fmt.Errorf("review threads (page %d): %w", pageNum, err)
			}
			resolved += countResolvedReviewThreads(page.nodes)
			cursor = page.endCursor
		}

		results[i] = &ReviewThreadCounts{Total: total, Resolved: resolved}
	}
	return results, nil
}

// reviewThreadsPage is one pull request's review-thread connection page.
type reviewThreadsPage struct {
	// The total covers the full connection rather than only this page.
	totalCount int

	// The page contains these resolved states in GitHub's response order.
	nodes []bool

	// The cursor identifies the page boundary when another page follows.
	endCursor string

	// This value reports whether GitHub has another page after the cursor.
	hasNextPage bool
}

// pullRequestReviewThreadsFirstPages loads one review-thread page for each ID.
func (c *Gateway) pullRequestReviewThreadsFirstPages(ctx context.Context, ids []ID, first int) ([]*reviewThreadsPage, error) {
	var result struct {
		Nodes []struct {
			ReviewThreads reviewThreadsConnection `json:"reviewThreads"`
		} `json:"nodes"`
	}
	query := compactGraphQL(`
		query($ids:[ID!]!){
			nodes(ids: $ids){
				... on PullRequest{
					reviewThreads(first: ` + strconv.Itoa(first) + `){
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
	threads := make([]*reviewThreadsPage, len(result.Nodes))
	for i, node := range result.Nodes {
		threads[i] = reviewThreads(&node.ReviewThreads)
	}
	return threads, nil
}

// pullRequestReviewThreadsPage loads first review threads after a non-empty
// cursor. First must be from 1 through 100.
func (c *Gateway) pullRequestReviewThreadsPage(ctx context.Context, id ID, first int, after string) (*reviewThreadsPage, error) {
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

func reviewThreads(connection *reviewThreadsConnection) *reviewThreadsPage {
	resolved := make([]bool, len(connection.Nodes))
	for i, node := range connection.Nodes {
		resolved[i] = node.IsResolved
	}
	return &reviewThreadsPage{
		totalCount:  connection.TotalCount,
		nodes:       resolved,
		endCursor:   connection.PageInfo.EndCursor,
		hasNextPage: connection.PageInfo.HasNextPage,
	}
}

func countResolvedReviewThreads(nodes []bool) int {
	count := 0
	for _, resolved := range nodes {
		if resolved {
			count++
		}
	}
	return count
}
