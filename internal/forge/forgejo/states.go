package forgejo

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/forgejo"
	"go.abhg.dev/gs/internal/git"
)

// ChangeStatuses retrieves compact statuses for the given changes.
func (r *Repository) ChangeStatuses(
	ctx context.Context,
	ids []forge.ChangeID,
) ([]forge.ChangeStatus, error) {
	statuses := make([]forge.ChangeStatus, len(ids))
	for i, id := range ids {
		pr, _, err := r.client.PullRequestGet(
			ctx, r.owner, r.repo, mustPR(id).Number,
		)
		if err != nil {
			return nil, fmt.Errorf("get pull request %v: %w", id, err)
		}
		statuses[i] = forge.ChangeStatus{
			State:    forgeChangeState(pr.State, pr.Merged),
			HeadHash: pullRequestHeadHash(pr),
		}
	}
	return statuses, nil
}

// ChangeChecks reports CI/checks for the given pull request.
func (r *Repository) ChangeChecks(
	ctx context.Context,
	id forge.ChangeID,
) ([]forge.ChangeCheck, error) {
	pr, _, err := r.client.PullRequestGet(
		ctx, r.owner, r.repo, mustPR(id).Number,
	)
	if err != nil {
		return nil, fmt.Errorf("get pull request: %w", err)
	}
	if pr.Head == nil || pr.Head.SHA == "" {
		return nil, nil
	}

	var statuses []*forgejo.CommitStatus
	opt := &forgejo.ListOptions{Limit: 100}
	for {
		page, resp, err := r.client.CommitStatusList(
			ctx, r.owner, r.repo, pr.Head.SHA, opt,
		)
		if err != nil {
			return nil, fmt.Errorf("list commit statuses: %w", err)
		}
		statuses = append(statuses, page...)
		if resp.NextPage == 0 {
			break
		}
		opt.Page = int64(resp.NextPage)
	}

	if len(statuses) == 0 {
		return nil, nil
	}

	checks := make([]forge.ChangeCheck, 0, len(statuses))
	for i, status := range statuses {
		checks = append(checks, commitStatusCheck(i, status))
	}
	return checks, nil
}

func forgeChangeState(state string, merged bool) forge.ChangeState {
	if merged {
		return forge.ChangeMerged
	}
	switch state {
	case "open":
		return forge.ChangeOpen
	case "closed":
		return forge.ChangeClosed
	case "merged":
		return forge.ChangeMerged
	default:
		return 0
	}
}

func commitStatusCheck(i int, status *forgejo.CommitStatus) forge.ChangeCheck {
	check := forge.ChangeCheck{Name: status.Context}
	if check.Name == "" {
		check.Name = fmt.Sprintf("Forgejo commit status %d", i+1)
	}
	check.State = commitStatusState(status.State)
	return check
}

func commitStatusState(state forgejo.CommitStatusState) forge.ChangeCheckState {
	switch state {
	case forgejo.CommitStatusSuccess, forgejo.CommitStatusWarning:
		return forge.ChangeCheckPassed
	case forgejo.CommitStatusFailure, forgejo.CommitStatusError:
		return forge.ChangeCheckFailed
	default:
		return forge.ChangeCheckPending
	}
}

func pullRequestHeadHash(pr *forgejo.PullRequest) git.Hash {
	if pr.Head == nil {
		return ""
	}
	return git.Hash(pr.Head.SHA)
}
