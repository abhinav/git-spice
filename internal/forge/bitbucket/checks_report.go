package bitbucket

import (
	"context"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
)

// rollupFromCommitStatuses collapses a Bitbucket build-status list into
// the forge rollup taxonomy.
//
// Failure wins over pending; pending wins over passed; absence of any
// statuses returns ChecksRollupNone (no CI configured/reported).
// Stopped statuses are operator-cancelled non-failures and roll up as
// passed.
func rollupFromCommitStatuses(
	statuses []bitbucket.CommitStatus,
) forge.ChecksRollupState {
	if len(statuses) == 0 {
		return forge.ChecksRollupNone
	}

	pending := false
	for _, s := range statuses {
		switch s.State {
		case bitbucket.CommitStatusFailed:
			return forge.ChecksRollupFailed
		case bitbucket.CommitStatusInProgress:
			pending = true
		}
	}
	if pending {
		return forge.ChecksRollupPending
	}
	return forge.ChecksRollupPassed
}

// ChecksByChange reports per-change rolled-up and per-run build state
// for each of the given pull requests.
//
// One getPullRequest + (when the PR has a source commit) a paginated
// CommitStatusList walk per PR. Build statuses are reported in
// Bitbucket's natural order.
func (r *Repository) ChecksByChange(
	ctx context.Context, ids []forge.ChangeID,
) ([]*forge.ChecksReport, error) {
	out := make([]*forge.ChecksReport, len(ids))
	for i, id := range ids {
		checks, err := r.changeChecks(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("change %v: %w", id, err)
		}
		out[i] = checks
	}
	return out, nil
}

func (r *Repository) changeChecks(
	ctx context.Context, id forge.ChangeID,
) (*forge.ChecksReport, error) {
	prID := mustPR(id)
	pr, err := r.getPullRequest(ctx, prID.Number)
	if err != nil {
		return nil, fmt.Errorf("get pull request: %w", err)
	}

	if pr.Source.Commit == nil {
		return &forge.ChecksReport{Rollup: forge.ChecksRollupNone}, nil
	}

	var statuses []bitbucket.CommitStatus
	opt := &bitbucket.CommitStatusListOptions{}
	for {
		page, resp, err := r.client.CommitStatusList(
			ctx, r.workspace, r.repo, pr.Source.Commit.Hash, opt,
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

	out := &forge.ChecksReport{Rollup: rollupFromCommitStatuses(statuses)}
	out.Runs = make([]forge.CheckRun, 0, len(statuses))
	for _, s := range statuses {
		name := s.Name
		if name == "" {
			name = s.Key
		}
		out.Runs = append(out.Runs, forge.CheckRun{
			Name:  name,
			State: strings.ToLower(s.State),
			URL:   s.URL,
		})
	}
	return out, nil
}
