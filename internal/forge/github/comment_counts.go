package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
)

// CommentCountsByChange retrieves comment resolution counts for multiple PRs.
func (r *Repository) CommentCountsByChange(
	ctx context.Context,
	ids []forge.ChangeID,
) ([]*forge.CommentCounts, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	gqlIDs, err := r.resolveGraphQLIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	threads, err := r.queryReviewThreads(ctx, gqlIDs)
	if err != nil {
		return nil, err
	}

	return r.countReviewThreads(ctx, threads, gqlIDs)
}

func (r *Repository) resolveGraphQLIDs(
	ctx context.Context,
	ids []forge.ChangeID,
) ([]githubv4.ID, error) {
	gqlIDs := make([]githubv4.ID, len(ids))
	for i, id := range ids {
		pr := mustPR(id)
		var err error
		gqlIDs[i], err = r.graphQLID(ctx, pr)
		if err != nil {
			return nil, fmt.Errorf("resolve ID %v: %w", id, err)
		}
	}
	return gqlIDs, nil
}

// _reviewThreadsPageSize is the number of review threads
// fetched per page when counting comment resolutions.
const _reviewThreadsPageSize = 100

type reviewThreadNode struct {
	PullRequest struct {
		ReviewThreads reviewThreadsConnection `graphql:"reviewThreads(first: 100)"`
	} `graphql:"... on PullRequest"`
}

type reviewThreadsConnection struct {
	TotalCount int
	PageInfo   struct {
		EndCursor   githubv4.String `graphql:"endCursor"`
		HasNextPage bool            `graphql:"hasNextPage"`
	} `graphql:"pageInfo"`
	Nodes []struct {
		IsResolved bool
	}
}

func (r *Repository) queryReviewThreads(
	ctx context.Context,
	gqlIDs []githubv4.ID,
) ([]reviewThreadNode, error) {
	var q struct {
		Nodes []reviewThreadNode `graphql:"nodes(ids: $ids)"`
	}

	err := r.client.Query(ctx, &q, map[string]any{"ids": gqlIDs})
	if err != nil {
		return nil, fmt.Errorf("query review threads: %w", err)
	}

	return q.Nodes, nil
}

func (r *Repository) countReviewThreads(
	ctx context.Context,
	nodes []reviewThreadNode,
	gqlIDs []githubv4.ID,
) ([]*forge.CommentCounts, error) {
	results := make([]*forge.CommentCounts, len(nodes))
	for i, node := range nodes {
		threads := node.PullRequest.ReviewThreads
		resolved := countResolved(threads.Nodes)

		// If there are more threads than the first page,
		// paginate to count all resolved threads.
		if threads.PageInfo.HasNextPage {
			remaining, err := r.countRemainingResolved(
				ctx, gqlIDs[i], threads.PageInfo.EndCursor,
			)
			if err != nil {
				return nil, err
			}
			resolved += remaining
		}

		results[i] = &forge.CommentCounts{
			Total:      threads.TotalCount,
			Resolved:   resolved,
			Unresolved: threads.TotalCount - resolved,
		}
	}
	return results, nil
}

// countRemainingResolved paginates through the remaining
// review threads for a single PR and counts resolved ones.
func (r *Repository) countRemainingResolved(
	ctx context.Context,
	gqlID githubv4.ID,
	cursor githubv4.String,
) (int, error) {
	var q struct {
		Node struct {
			PullRequest struct {
				ReviewThreads reviewThreadsConnection `graphql:"reviewThreads(first: $first, after: $after)"`
			} `graphql:"... on PullRequest"`
		} `graphql:"node(id: $id)"`
	}

	variables := map[string]any{
		"id":    gqlID,
		"first": githubv4.Int(_reviewThreadsPageSize),
		"after": cursor,
	}

	resolved := 0
	for pageNum := 2; ; pageNum++ {
		if err := r.client.Query(ctx, &q, variables); err != nil {
			return 0, fmt.Errorf(
				"review threads (page %d): %w", pageNum, err,
			)
		}

		threads := q.Node.PullRequest.ReviewThreads
		resolved += countResolved(threads.Nodes)

		if !threads.PageInfo.HasNextPage {
			break
		}
		variables["after"] = threads.PageInfo.EndCursor
	}

	return resolved, nil
}

func countResolved(nodes []struct{ IsResolved bool }) int {
	count := 0
	for _, n := range nodes {
		if n.IsResolved {
			count++
		}
	}
	return count
}
