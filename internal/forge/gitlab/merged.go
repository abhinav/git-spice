package gitlab

import (
	"context"
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.abhg.dev/gs/internal/forge"
)

// ChangesAreMerged reports whether the given changes have been merged.
func (r *Repository) ChangesAreMerged(ctx context.Context, ids []forge.ChangeID) ([]bool, error) {
	mrIDs := make([]int, len(ids))
	for i, id := range ids {
		mrIDs[i] = mustMR(id).Number
	}

	mergeRequests, _, err := r.client.MergeRequests.ListProjectMergeRequests(
		r.repoID, &gitlab.ListProjectMergeRequestsOptions{IIDs: &mrIDs},
		gitlab.WithContext(ctx),
	)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	// create a map of MR IDs to MRs
	mrMap := make(map[int]*gitlab.BasicMergeRequest)
	for _, mr := range mergeRequests {
		mrMap[mr.IID] = mr
	}

	merged := make([]bool, len(mrIDs))
	for i, id := range mrIDs {
		merged[i] = mrMap[id].State == "merged"
	}

	return merged, nil
}
