package gitea

import (
	"context"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
)

// FindChangesByBranch searches for pull requests with the given head branch.
//
// Gitea's API `head` query parameter does not work reliably in all versions,
// so we fetch by state and filter client-side by Head.Ref.
func (r *Repository) FindChangesByBranch(ctx context.Context, branch string, opts forge.FindChangesOptions) ([]*forge.FindChangeItem, error) {
	if opts.Limit == 0 {
		opts.Limit = 10
	}

	// Determine which states to fetch.
	var states []string
	switch opts.State {
	case forge.ChangeOpen:
		states = []string{"open"}
	case forge.ChangeClosed, forge.ChangeMerged:
		states = []string{"closed"}
	default:
		// All states: fetch open and closed separately.
		states = []string{"open", "closed"}
	}

	var items []*forge.FindChangeItem
	for _, state := range states {
		prs, err := r.listAllPRsByState(ctx, state, opts.Limit)
		if err != nil {
			return nil, fmt.Errorf("find changes by branch (%s): %w", state, err)
		}

		for _, pr := range prs {
			if !matchesBranch(pr, branch, opts.PushRepository) {
				continue
			}
			// Apply precise state filter (Gitea uses "closed" for both merged
			// and non-merged closed PRs).
			if opts.State == forge.ChangeMerged && !pr.Merged {
				continue
			}
			if opts.State == forge.ChangeClosed && pr.Merged {
				continue
			}
			items = append(items, pullRequestToFindChangeItem(pr))
			if len(items) >= opts.Limit {
				return items, nil
			}
		}
	}
	return items, nil
}

// listAllPRsByState fetches all PRs with the given state, paginating as needed.
func (r *Repository) listAllPRsByState(ctx context.Context, state string, maxItems int) ([]*giteagw.PullRequest, error) {
	var all []*giteagw.PullRequest
	page := int64(1)
	pageSize := int64(50)

	for {
		prs, resp, err := r.client.PullList(ctx, r.owner, r.repo, &giteagw.ListPullRequestsOptions{
			State:       state,
			Limit:       pageSize,
			ListOptions: giteagw.ListOptions{Page: page},
		})
		if err != nil {
			return nil, err
		}
		all = append(all, prs...)
		if resp.NextPage == 0 || len(all) >= maxItems*2 {
			break
		}
		page = int64(resp.NextPage)
	}
	return all, nil
}

// matchesBranch reports whether a PR's head branch matches the given branch
// name, taking fork repositories into account.
func matchesBranch(pr *giteagw.PullRequest, branch string, pushRepo forge.RepositoryID) bool {
	if pr.Head == nil {
		return false
	}

	if pushRepo != nil {
		// Fork PR: match "forkowner:branch" in Head.Label,
		// or just match the branch ref for same-name cases.
		pushRID := mustRepositoryID(pushRepo)
		expectedLabel := pushRID.owner + ":" + branch
		if pr.Head.Label == expectedLabel {
			return true
		}
		// Fall back to matching ref if label doesn't work.
		return pr.Head.Ref == branch && isFromFork(pr)
	}

	// Same-repository PR: match branch ref exactly, exclude forks.
	return pr.Head.Ref == branch && !isFromFork(pr)
}

// isFromFork reports whether a PR's head branch comes from a fork.
// Gitea sets Head.Label to "owner:branch" for fork PRs
// and just "branch" for same-repo PRs.
func isFromFork(pr *giteagw.PullRequest) bool {
	if pr.Head == nil {
		return false
	}
	return strings.Contains(pr.Head.Label, ":")
}

// FindChangeByID searches for a pull request with the given ID.
func (r *Repository) FindChangeByID(ctx context.Context, id forge.ChangeID) (*forge.FindChangeItem, error) {
	pr, _, err := r.client.PullGet(ctx, r.owner, r.repo, mustPR(id).Number)
	if err != nil {
		return nil, fmt.Errorf("find change by ID: %w", err)
	}
	return pullRequestToFindChangeItem(pr), nil
}

// ChangeStatuses retrieves compact statuses for the given changes.
func (r *Repository) ChangeStatuses(ctx context.Context, ids []forge.ChangeID) ([]forge.ChangeStatus, error) {
	statuses := make([]forge.ChangeStatus, len(ids))
	for i, id := range ids {
		pr, _, err := r.client.PullGet(ctx, r.owner, r.repo, mustPR(id).Number)
		if err != nil {
			// Treat inaccessible PRs as open so downstream skips them.
			statuses[i].State = forge.ChangeOpen
			r.log.Warn("Could not fetch PR status; treating as open", "pr", id, "err", err)
			continue
		}
		statuses[i] = pullRequestToChangeStatus(pr)
	}
	return statuses, nil
}
