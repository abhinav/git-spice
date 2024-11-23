package gitlab

import (
	"context"
	"fmt"

	"github.com/xanzy/go-gitlab"
	"go.abhg.dev/gs/internal/forge"
)

// ChangesAreMerged reports whether the given changes have been merged.
func (r *Repository) ChangesAreMerged(ctx context.Context, ids []forge.ChangeID) ([]bool, error) {
	mrIDs := make([]int, len(ids))
	for i, id := range ids {
		mrIDs[i] = mustMR(id).Number
	}

	requests, _, err := r.mergeRequests.ListProjectMergeRequests(
		r.repoID, &gitlab.ListProjectMergeRequestsOptions{IIDs: &mrIDs},
		gitlab.WithContext(ctx),
	)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	merged := make([]bool, len(ids))
	for i, mr := range requests {
		merged[i] = mr.State == "merged"
	}

	return merged, nil
}
