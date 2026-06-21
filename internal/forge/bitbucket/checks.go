package bitbucket

import (
	"context"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
)

// ChangeChecksState reports the aggregate build status
// for the given pull request.
func (r *Repository) ChangeChecksState(
	ctx context.Context, fid forge.ChangeID,
) (forge.ChecksState, error) {
	id := mustPR(fid)
	pr, err := r.getPullRequest(ctx, id.Number)
	if err != nil {
		return 0, fmt.Errorf("get pull request: %w", err)
	}

	if pr.Source.Commit == nil {
		return forge.ChecksPassed, nil
	}
	return r.commitChecksState(ctx, pr.Source.Commit.Hash)
}

func (r *Repository) commitChecksState(
	ctx context.Context, commitHash string,
) (forge.ChecksState, error) {
	statuses, _, err := r.client.CommitStatusList(
		ctx, r.workspace, r.repo, commitHash,
	)
	if err != nil {
		return 0, fmt.Errorf("get commit statuses: %w", err)
	}

	return aggregateStatuses(statuses.Values), nil
}

func aggregateStatuses(
	statuses []bitbucket.CommitStatus,
) forge.ChecksState {
	if len(statuses) == 0 {
		// Merge handler relies on "no statuses -> passed" so repos
		// without CI don't false-fail. Preserved; ChecksByChange
		// uses rollupFromCommitStatuses which returns ChecksNone.
		return forge.ChecksPassed
	}

	for _, s := range statuses {
		switch s.State {
		case bitbucket.CommitStatusFailed:
			return forge.ChecksFailed
		case bitbucket.CommitStatusInProgress:
			return forge.ChecksPending
		}
	}

	return forge.ChecksPassed
}

// rollupFromCommitStatuses collapses a Bitbucket build-status list
// into the upstream rollup taxonomy.
//
// Failure wins over pending; pending wins over passed; absence of
// any statuses returns ChecksNone (no CI configured/reported).
// Stopped statuses are operator-cancelled non-failures and roll up
// as passed.
func rollupFromCommitStatuses(
	statuses []bitbucket.CommitStatus,
) forge.ChecksState {
	if len(statuses) == 0 {
		return forge.ChecksNone
	}

	pending := false
	for _, s := range statuses {
		switch s.State {
		case bitbucket.CommitStatusFailed:
			return forge.ChecksFailed
		case bitbucket.CommitStatusInProgress:
			pending = true
		}
	}
	if pending {
		return forge.ChecksPending
	}
	return forge.ChecksPassed
}

// ChecksByChange reports per-change rolled-up and per-run build state
// for each of the given pull requests.
//
// One getPullRequest + (when the PR has a source commit) one
// CommitStatusList call per PR. Build statuses are reported in
// Bitbucket's natural order.
func (r *Repository) ChecksByChange(
	ctx context.Context, ids []forge.ChangeID,
) ([]*forge.ChangeChecks, error) {
	out := make([]*forge.ChangeChecks, len(ids))
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
) (*forge.ChangeChecks, error) {
	prID := mustPR(id)
	pr, err := r.getPullRequest(ctx, prID.Number)
	if err != nil {
		return nil, fmt.Errorf("get pull request: %w", err)
	}

	if pr.Source.Commit == nil {
		return &forge.ChangeChecks{Rollup: forge.ChecksNone}, nil
	}

	statuses, _, err := r.client.CommitStatusList(
		ctx, r.workspace, r.repo, pr.Source.Commit.Hash,
	)
	if err != nil {
		return nil, fmt.Errorf("get commit statuses: %w", err)
	}

	out := &forge.ChangeChecks{
		Rollup: rollupFromCommitStatuses(statuses.Values),
	}
	out.Runs = make([]forge.CheckRun, 0, len(statuses.Values))
	for _, s := range statuses.Values {
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
