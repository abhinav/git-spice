package forgejo

import (
	"cmp"
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/forgejo"
)

// FindChangesByBranch searches for changes with the given branch name.
func (r *Repository) FindChangesByBranch(
	ctx context.Context,
	branch string,
	opts forge.FindChangesOptions,
) ([]*forge.FindChangeItem, error) {
	opts.Limit = cmp.Or(opts.Limit, 10)

	sourceRepo := r.owner + "/" + r.repo
	if opts.PushRepository != nil {
		sourceRepo = mustRepositoryID(opts.PushRepository).String()
	}

	listOptions := &forgejo.PullRequestListOptions{
		State: pullRequestListState(opts.State),
		Sort:  "recentupdate",
		ListOptions: forgejo.ListOptions{
			Page:  1,
			Limit: 50,
		},
	}

	var changes []*forge.FindChangeItem
	for {
		prs, response, err := r.client.PullRequestList(
			ctx, r.owner, r.repo, listOptions,
		)
		if err != nil {
			return nil, fmt.Errorf("find changes by branch: %w", err)
		}

		for _, pr := range prs {
			// Forgejo lists pull requests for all source branches and
			// repositories, so skip anything outside the exact source
			// repository and branch git-spice is tracking.
			if pr.Head == nil ||
				pr.Head.Ref != branch ||
				pr.Head.Repository == nil ||
				pr.Head.Repository.FullName != sourceRepo {
				continue
			}
			item := pullRequestToFindChangeItem(pr)
			if opts.State != 0 && item.State != opts.State {
				continue
			}
			changes = append(changes, item)
			if len(changes) == opts.Limit {
				return changes, nil
			}
		}

		if response.NextPage == 0 {
			return changes, nil
		}
		listOptions.Page = int64(response.NextPage)
	}
}

// FindChangeByID searches for a change with the given ID.
func (r *Repository) FindChangeByID(
	ctx context.Context,
	id forge.ChangeID,
) (*forge.FindChangeItem, error) {
	pr, _, err := r.client.PullRequestGet(
		ctx, r.owner, r.repo, mustPR(id).Number,
	)
	if err != nil {
		return nil, fmt.Errorf("find change by ID: %w", err)
	}

	return pullRequestToFindChangeItem(pr), nil
}

func pullRequestToFindChangeItem(pr *forgejo.PullRequest) *forge.FindChangeItem {
	return &forge.FindChangeItem{
		ID:        &PR{Number: pr.Index},
		URL:       pr.HTMLURL,
		State:     forgeChangeState(pr.State, pr.Merged),
		Subject:   pr.Title,
		BaseName:  pullRequestBaseRef(pr),
		HeadHash:  pullRequestHeadHash(pr),
		Draft:     pr.Draft,
		Labels:    labelNames(pr.Labels),
		Reviewers: userLogins(pr.RequestedReviewers),
		Assignees: userLogins(pr.Assignees),
	}
}

func pullRequestListState(s forge.ChangeState) string {
	switch s {
	case forge.ChangeOpen:
		return "open"
	case forge.ChangeClosed:
		return "closed"
	case forge.ChangeMerged:
		return "closed"
	default:
		return "all"
	}
}

func pullRequestBaseRef(pr *forgejo.PullRequest) string {
	if pr.Base == nil {
		return ""
	}
	return pr.Base.Ref
}
