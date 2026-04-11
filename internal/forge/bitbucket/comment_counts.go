package bitbucket

import (
	"context"
	"fmt"

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

	results := make([]*forge.CommentCounts, len(ids))
	for i, id := range ids {
		counts, err := r.commentCounts(ctx, mustPR(id).Number)
		if err != nil {
			return nil, fmt.Errorf("get counts for %v: %w", id, err)
		}
		results[i] = counts
	}

	return results, nil
}

func (r *Repository) commentCounts(
	ctx context.Context,
	prID int64,
) (*forge.CommentCounts, error) {
	var total, resolved int
	for c, err := range r.listPullRequestComments(ctx, prID) {
		if err != nil {
			return nil, err
		}

		// Only inline code review comments are resolvable.
		if c.Inline == nil {
			continue
		}
		total++
		if c.Resolution != nil {
			resolved++
		}
	}

	return &forge.CommentCounts{
		Total:      total,
		Resolved:   resolved,
		Unresolved: total - resolved,
	}, nil
}
