package gitlab

import (
	"context"
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
)

func toFindChangeItem(mr *gitlab.MergeRequest) *forge.FindChangeItem {
	return &forge.FindChangeItem{
		ID: &MR{
			Number: mr.IID,
		},
		URL:      mr.WebURL,
		State:    forgeChangeState(mr.State),
		Subject:  mr.Title,
		BaseName: mr.TargetBranch,
		HeadHash: git.Hash(mr.SHA),
		Draft:    mr.Draft,
	}
}

func mergeRequestState(s forge.ChangeState) string {
	switch s {
	case forge.ChangeOpen:
		return "opened"
	case forge.ChangeClosed:
		return "closed"
	case forge.ChangeMerged:
		return "merged"
	default:
		return ""
	}
}

func forgeChangeState(s string) forge.ChangeState {
	switch s {
	case "opened":
		return forge.ChangeOpen
	case "closed":
		return forge.ChangeClosed
	case "merged":
		return forge.ChangeMerged
	default:
		return 0
	}
}

// FindChangesByBranch searches for changes with the given branch name.
// It returns both open and closed changes.
// Only recent changes are returned, limited by the given limit.
func (r *Repository) FindChangesByBranch(ctx context.Context, branch string, opts forge.FindChangesOptions) ([]*forge.FindChangeItem, error) {
	if opts.Limit == 0 {
		opts.Limit = 10
	}

	opt := &gitlab.ListProjectMergeRequestsOptions{
		OrderBy:      gitlab.Ptr("updated_at"),
		SourceBranch: gitlab.Ptr(branch),
		ListOptions: gitlab.ListOptions{
			PerPage: opts.Limit,
		},
	}

	if opts.State != 0 {
		opt.State = gitlab.Ptr(mergeRequestState(opts.State))
	}
	requests, _, err := r.client.MergeRequests.ListProjectMergeRequests(
		r.repoID, opt,
		gitlab.WithContext(ctx),
	)
	if err != nil {
		return nil, fmt.Errorf("find changes by branch: %w", err)
	}

	changes := make([]*forge.FindChangeItem, len(requests))
	for i, mr := range requests {
		changes[i] = toFindChangeItem(mr)
	}

	return changes, nil
}

// FindChangeByID searches for a change with the given ID.
func (r *Repository) FindChangeByID(ctx context.Context, id forge.ChangeID) (*forge.FindChangeItem, error) {
	mr, _, err := r.client.MergeRequests.GetMergeRequest(
		r.repoID, mustMR(id).Number, nil,
		gitlab.WithContext(ctx),
	)
	if err != nil {
		return nil, fmt.Errorf("find change by ID: %w", err)
	}

	return toFindChangeItem(mr), nil
}
