package github

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
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

	threadsByChange, err := r.gateway.PullRequestReviewThreads(ctx, gqlIDs)
	if err != nil {
		return nil, fmt.Errorf("query review threads: %w", err)
	}

	results := make([]*forge.CommentCounts, len(threadsByChange))
	for i, threads := range threadsByChange {
		resolved := countResolved(threads.Nodes)

		// If there are more threads than the first page,
		// paginate to count all resolved threads.
		if threads.HasNextPage {
			remaining, err := r.countRemainingResolved(ctx, gqlIDs[i], threads.EndCursor)
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

func (r *Repository) resolveGraphQLIDs(
	ctx context.Context,
	ids []forge.ChangeID,
) ([]github.ID, error) {
	gqlIDs := make([]github.ID, len(ids))
	for i, id := range ids {
		pr := mustPR(id)
		gqlID, err := r.graphQLID(ctx, pr)
		if err != nil {
			return nil, fmt.Errorf("resolve ID %v: %w", id, err)
		}
		gqlIDs[i] = gqlID
	}
	return gqlIDs, nil
}

// _reviewThreadsPageSize is the number of review threads
// fetched per page when counting comment resolutions.
const _reviewThreadsPageSize = 100

// countRemainingResolved paginates through the remaining
// review threads for a single PR and counts resolved ones.
func (r *Repository) countRemainingResolved(
	ctx context.Context,
	gqlID github.ID,
	cursor string,
) (int, error) {
	resolved := 0
	for pageNum := 2; ; pageNum++ {
		threads, err := r.gateway.PullRequestReviewThreadsPage(ctx, gqlID, _reviewThreadsPageSize, cursor)
		if err != nil {
			return 0, fmt.Errorf(
				"review threads (page %d): %w", pageNum, err,
			)
		}

		resolved += countResolved(threads.Nodes)

		if !threads.HasNextPage {
			break
		}
		cursor = threads.EndCursor
	}

	return resolved, nil
}

func countResolved(nodes []bool) int {
	count := 0
	for _, n := range nodes {
		if n {
			count++
		}
	}
	return count
}
