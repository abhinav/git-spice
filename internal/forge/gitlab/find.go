package gitlab

import (
	"context"
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
)

func basicMergeRequestToFindChangeItem(mr *gitlab.BasicMergeRequest) *forge.FindChangeItem {
	labels := []string(mr.Labels)
	if len(labels) == 0 {
		labels = nil
	}

	var reviewers []string
	if len(mr.Reviewers) > 0 {
		reviewers = make([]string, len(mr.Reviewers))
		for i, reviewer := range mr.Reviewers {
			reviewers[i] = reviewer.Username
		}
	}

	var assignees []string
	if len(mr.Assignees) > 0 {
		assignees = make([]string, len(mr.Assignees))
		for i, assignee := range mr.Assignees {
			assignees[i] = assignee.Username
		}
	}

	return &forge.FindChangeItem{
		ID: &MR{
			Number: mr.IID,
		},
		URL:       mr.WebURL,
		State:     forgeChangeState(mr.State),
		Subject:   mr.Title,
		BaseName:  mr.TargetBranch,
		HeadHash:  git.Hash(mr.SHA),
		Draft:     mr.Draft,
		Labels:    labels,
		Reviewers: reviewers,
		Assignees: assignees,
	}
}

func mergeRequestToFindChangeItem(mr *gitlab.MergeRequest) *forge.FindChangeItem {
	labels := []string(mr.Labels)
	if len(labels) == 0 {
		labels = nil
	}

	var reviewers []string
	if len(mr.Reviewers) > 0 {
		reviewers = make([]string, len(mr.Reviewers))
		for i, reviewer := range mr.Reviewers {
			reviewers[i] = reviewer.Username
		}
	}

	var assignees []string
	if len(mr.Assignees) > 0 {
		assignees = make([]string, len(mr.Assignees))
		for i, assignee := range mr.Assignees {
			assignees[i] = assignee.Username
		}
	}

	return &forge.FindChangeItem{
		ID: &MR{
			Number: mr.IID,
		},
		URL:       mr.WebURL,
		State:     forgeChangeState(mr.State),
		Subject:   mr.Title,
		BaseName:  mr.TargetBranch,
		HeadHash:  git.Hash(mr.SHA),
		Draft:     mr.Draft,
		Labels:    labels,
		Reviewers: reviewers,
		Assignees: assignees,
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
		OrderBy:      new("updated_at"),
		SourceBranch: new(branch),
		ListOptions: gitlab.ListOptions{
			PerPage: int64(opts.Limit),
		},
	}

	if opts.State != 0 {
		opt.State = new(mergeRequestState(opts.State))
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
		changes[i] = basicMergeRequestToFindChangeItem(mr)
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

	return mergeRequestToFindChangeItem(mr), nil
}
