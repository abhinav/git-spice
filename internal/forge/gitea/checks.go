package gitea

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
)

// ChangeChecks reports CI/checks for the given pull request.
func (r *Repository) ChangeChecks(
	ctx context.Context,
	fid forge.ChangeID,
) ([]forge.ChangeCheck, error) {
	id := mustPR(fid)

	pr, _, err := r.client.PullGet(ctx, r.owner, r.repo, id.Number)
	if err != nil {
		return nil, fmt.Errorf("get pull request: %w", err)
	}
	if pr.Head == nil || pr.Head.Sha == "" {
		return nil, nil
	}

	var statuses []*giteagw.CommitStatus
	opt := &giteagw.ListCommitStatusOptions{
		ListOptions: giteagw.ListOptions{Limit: 100},
	}
	for {
		page, resp, err := r.client.CommitStatusList(
			ctx, r.owner, r.repo, pr.Head.Sha, opt,
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

func commitStatusCheck(i int, status *giteagw.CommitStatus) forge.ChangeCheck {
	check := forge.ChangeCheck{Name: status.Context}
	if check.Name == "" {
		check.Name = fmt.Sprintf("Gitea commit status %d", i+1)
	}
	check.State = commitStatusState(status.State)
	return check
}

func commitStatusState(state string) forge.ChangeCheckState {
	switch state {
	case "", giteagw.CommitStatusSuccess, giteagw.CommitStatusWarning:
		return forge.ChangeCheckPassed
	case giteagw.CommitStatusPending:
		return forge.ChangeCheckPending
	case giteagw.CommitStatusFailure, giteagw.CommitStatusError:
		return forge.ChangeCheckFailed
	default:
		return forge.ChangeCheckPending
	}
}
