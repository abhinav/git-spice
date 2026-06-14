package gitea

import (
	"context"

	"go.abhg.dev/gs/internal/forge"
)

// CommentCountsByChange returns comment resolution counts for pull requests.
//
// Gitea issue comments have no resolution concept at the comment level,
// so this returns zero for all counts.
// Pull request review thread resolution can be added later
// using Gitea's pull review API.
func (r *Repository) CommentCountsByChange(
	_ context.Context,
	ids []forge.ChangeID,
) ([]*forge.CommentCounts, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	results := make([]*forge.CommentCounts, len(ids))
	for i := range ids {
		results[i] = &forge.CommentCounts{}
	}
	return results, nil
}
