package bitbucket

import (
	"context"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/forge"
)

// CommentCountsByChange retrieves comment resolution counts
// for multiple pull requests, in the same order as ids.
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

// commentCounts computes the resolvable-comment counts
// for a single pull request.
//
// Unpublished drafts and git-spice's own navigation comment
// don't participate in resolution counts.
func (r *Repository) commentCounts(
	ctx context.Context,
	prID int64,
) (*forge.CommentCounts, error) {
	var total, resolved int
	for c, err := range r.gw.ResolvableComments(ctx, prID) {
		if err != nil {
			return nil, err
		}

		if c.Pending || strings.Contains(c.Body, _navigationCommentMarker) {
			continue
		}

		total++
		if c.Resolved {
			resolved++
		}
	}

	return &forge.CommentCounts{
		Total:      total,
		Resolved:   resolved,
		Unresolved: total - resolved,
	}, nil
}
