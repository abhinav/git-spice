package gitlab

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/gitlab"
	"go.abhg.dev/gs/internal/git"
)

// maxMergeRequestsPerPage is GitLab's documented upper bound on per_page
// for list endpoints. It also caps the number of iids[] filters per
// request to keep query strings within reasonable URL length limits.
const maxMergeRequestsPerPage = 100

// ChangeStatuses retrieves compact statuses for the given changes in bulk.
func (r *Repository) ChangeStatuses(ctx context.Context, ids []forge.ChangeID) ([]forge.ChangeStatus, error) {
	mrIDs := make([]int64, len(ids))
	for i, id := range ids {
		mrIDs[i] = mustMR(id).Number
	}

	mrMap := make(map[int64]*gitlab.BasicMergeRequest, len(mrIDs))
	for start := 0; start < len(mrIDs); start += maxMergeRequestsPerPage {
		end := min(start+maxMergeRequestsPerPage, len(mrIDs))
		batch := mrIDs[start:end]

		page := int64(0)
		for {
			opts := &gitlab.ListProjectMergeRequestsOptions{
				ListOptions: gitlab.ListOptions{
					PerPage: maxMergeRequestsPerPage,
					Page:    page,
				},
				IIDs: &batch,
			}
			mergeRequests, resp, err := r.client.MergeRequestList(ctx, r.repoID, opts)
			if err != nil {
				return nil, fmt.Errorf("query failed: %w", err)
			}

			for _, mr := range mergeRequests {
				mrMap[mr.IID] = mr
			}

			if resp.NextPage == 0 {
				break
			}
			page = int64(resp.NextPage)
		}
	}

	statuses := make([]forge.ChangeStatus, len(mrIDs))
	for i, id := range mrIDs {
		mr, ok := mrMap[id]
		if !ok {
			// Missing from response (deleted or inaccessible);
			// treat as open so downstream code skips it.
			statuses[i].State = forge.ChangeOpen
			continue
		}
		switch mr.State {
		case "opened":
			statuses[i].State = forge.ChangeOpen
		case "merged":
			statuses[i].State = forge.ChangeMerged
		case "closed":
			statuses[i].State = forge.ChangeClosed
		default:
			statuses[i].State = forge.ChangeOpen
		}
		statuses[i].HeadHash = git.Hash(mr.SHA)
	}

	return statuses, nil
}
