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

	path := fmt.Sprintf(
		"/repositories/%s/%s/pullrequests/%d/comments?pagelen=%d",
		r.workspace, r.repo, prID, _listChangeCommentsPageSize,
	)

	for path != "" {
		comments, nextPath, err := r.fetchCommentPage(ctx, path)
		if err != nil {
			return nil, err
		}

		for _, c := range comments {
			if !isResolvable(&c) {
				continue
			}
			total++
			if c.Resolution != nil {
				resolved++
			}
		}

		path = nextPath
	}

	return &forge.CommentCounts{
		Total:      total,
		Resolved:   resolved,
		Unresolved: total - resolved,
	}, nil
}

// isResolvable checks if a comment is resolvable.
// Only inline (code review) comments are resolvable in Bitbucket.
func isResolvable(c *apiComment) bool {
	return c.Inline != nil
}
