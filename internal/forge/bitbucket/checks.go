package bitbucket

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
)

// ChangeChecks reports build statuses for the given pull request.
func (r *Repository) ChangeChecks(
	ctx context.Context, fid forge.ChangeID,
) ([]forge.ChangeCheck, error) {
	id := mustPR(fid)
	pr, err := r.getPullRequest(ctx, id.Number)
	if err != nil {
		return nil, fmt.Errorf("get pull request: %w", err)
	}

	if pr.Source.Commit == nil {
		return nil, nil
	}
	return r.commitChecks(ctx, pr.Source.Commit.Hash)
}

func (r *Repository) commitChecks(
	ctx context.Context, commitHash string,
) ([]forge.ChangeCheck, error) {
	var statuses []bitbucket.CommitStatus
	opt := &bitbucket.CommitStatusListOptions{}
	for {
		page, resp, err := r.client.CommitStatusList(
			ctx, r.workspace, r.repo, commitHash, opt,
		)
		if err != nil {
			return nil, fmt.Errorf("get commit statuses: %w", err)
		}

		statuses = append(statuses, page.Values...)
		if resp.NextURL == "" {
			break
		}
		opt.PageURL = resp.NextURL
	}

	return statusChecks(statuses), nil
}

func statusChecks(
	statuses []bitbucket.CommitStatus,
) []forge.ChangeCheck {
	if len(statuses) == 0 {
		return nil
	}

	checks := make([]forge.ChangeCheck, 0, len(statuses))
	for i, s := range statuses {
		check := forge.ChangeCheck{Name: s.Key}
		if check.Name == "" {
			check.Name = fmt.Sprintf("Bitbucket build status %d", i+1)
		}
		switch s.State {
		case bitbucket.CommitStatusFailed,
			bitbucket.CommitStatusStopped:
			check.State = forge.ChangeCheckFailed
		case bitbucket.CommitStatusInProgress:
			check.State = forge.ChangeCheckPending
		default:
			check.State = forge.ChangeCheckPassed
		}
		checks = append(checks, check)
	}
	return checks
}
