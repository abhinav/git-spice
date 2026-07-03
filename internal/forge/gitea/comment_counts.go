package gitea

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
)

const _commentCountsReviewPageSize = 50

// CommentCountsByChange returns comment resolution counts for pull requests.
//
// Gitea issue comments have no resolution concept at the comment level,
// and Gitea pull review comments expose no resolution state that git-spice
// can map portably.
func (r *Repository) CommentCountsByChange(
	ctx context.Context,
	ids []forge.ChangeID,
) ([]*forge.CommentCounts, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	results := make([]*forge.CommentCounts, len(ids))
	for i, id := range ids {
		counts, err := r.reviewCommentCounts(ctx, mustPR(id).Number)
		if err != nil {
			return nil, fmt.Errorf("get counts for %v: %w", id, err)
		}
		results[i] = counts
	}
	return results, nil
}

func (r *Repository) reviewCommentCounts(
	ctx context.Context,
	prNumber int64,
) (*forge.CommentCounts, error) {
	var total int
	opts := &giteagw.ListPullReviewsOptions{
		ListOptions: giteagw.ListOptions{
			Limit: _commentCountsReviewPageSize,
		},
	}

	for {
		reviews, resp, err := r.client.PullReviewList(
			ctx,
			r.owner,
			r.repo,
			prNumber,
			opts,
		)
		if err != nil {
			return nil, fmt.Errorf("list reviews: %w", err)
		}

		for _, review := range reviews {
			comments, _, err := r.client.PullReviewCommentList(
				ctx,
				r.owner,
				r.repo,
				prNumber,
				review.ID,
			)
			if err != nil {
				return nil, fmt.Errorf("list review comments: %w", err)
			}
			total += len(comments)
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = int64(resp.NextPage)
	}

	return &forge.CommentCounts{
		Total:      total,
		Unresolved: total,
	}, nil
}
