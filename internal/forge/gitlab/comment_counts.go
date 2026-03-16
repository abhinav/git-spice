package gitlab

import (
	"context"
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.abhg.dev/gs/internal/forge"
)

// CommentCountsByChange retrieves comment resolution counts for multiple MRs.
func (r *Repository) CommentCountsByChange(
	ctx context.Context,
	ids []forge.ChangeID,
) ([]*forge.CommentCounts, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	results := make([]*forge.CommentCounts, len(ids))
	for i, id := range ids {
		counts, err := r.discussionCounts(ctx, mustMR(id).Number)
		if err != nil {
			return nil, fmt.Errorf("get counts for %v: %w", id, err)
		}
		results[i] = counts
	}

	return results, nil
}

func (r *Repository) discussionCounts(
	ctx context.Context,
	mrNumber int64,
) (*forge.CommentCounts, error) {
	var total, resolved int

	opts := &gitlab.ListMergeRequestDiscussionsOptions{
		ListOptions: gitlab.ListOptions{PerPage: 100},
	}

	for {
		discussions, resp, err := r.client.Discussions.ListMergeRequestDiscussions(
			r.repoID, mrNumber, opts,
			gitlab.WithContext(ctx),
		)
		if err != nil {
			return nil, fmt.Errorf("list discussions: %w", err)
		}

		for _, disc := range discussions {
			if !isResolvable(disc) {
				continue
			}
			total++
			if disc.Notes[0].Resolved {
				resolved++
			}
		}

		if resp.CurrentPage >= resp.TotalPages {
			break
		}
		opts.Page = resp.NextPage
	}

	return &forge.CommentCounts{
		Total:      total,
		Resolved:   resolved,
		Unresolved: total - resolved,
	}, nil
}

// isResolvable checks if a discussion is resolvable.
// Diff discussions (code review comments) are resolvable.
func isResolvable(disc *gitlab.Discussion) bool {
	if len(disc.Notes) == 0 {
		return false
	}
	return disc.Notes[0].Resolvable
}
