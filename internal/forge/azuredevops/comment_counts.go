package azuredevops

import (
	"context"
	"fmt"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"go.abhg.dev/gs/internal/forge"
)

// CommentCountsByChange retrieves comment resolution counts for multiple PRs.
func (r *Repository) CommentCountsByChange(
	ctx context.Context,
	ids []forge.ChangeID,
) ([]*forge.CommentCounts, error) {
	counts := make([]*forge.CommentCounts, len(ids))
	for i, id := range ids {
		prID := mustPR(id).Number

		count, err := r.commentCounts(ctx, prID)
		if err != nil {
			return nil, fmt.Errorf("get counts for PR #%d: %w", prID, err)
		}
		counts[i] = count
	}
	return counts, nil
}

func (r *Repository) commentCounts(
	ctx context.Context,
	prID int,
) (*forge.CommentCounts, error) {
	threads, err := r.client.gitClient.GetThreads(ctx, git.GetThreadsArgs{
		Project:       strPtr(r.project()),
		RepositoryId:  strPtr(r.repositoryID()),
		PullRequestId: &prID,
	})
	if err != nil {
		return nil, fmt.Errorf("get threads: %w", err)
	}

	var total, resolved int
	for _, thread := range *threads {
		if thread.IsDeleted != nil && *thread.IsDeleted {
			continue
		}
		if thread.Comments == nil || len(*thread.Comments) == 0 {
			continue
		}

		total++
		if thread.Status != nil && isResolvedThreadStatus(*thread.Status) {
			resolved++
		}
	}

	return &forge.CommentCounts{
		Total:      total,
		Resolved:   resolved,
		Unresolved: total - resolved,
	}, nil
}

func isResolvedThreadStatus(status git.CommentThreadStatus) bool {
	switch status {
	case git.CommentThreadStatusValues.Fixed,
		git.CommentThreadStatusValues.WontFix,
		git.CommentThreadStatusValues.Closed,
		git.CommentThreadStatusValues.ByDesign:
		return true
	default:
		return false
	}
}
