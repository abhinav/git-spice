package bitbucket

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
)

// Bitbucket build status states.
const (
	buildSuccessful = "SUCCESSFUL"
	buildInProgress = "INPROGRESS"
	buildFailed     = "FAILED"
	buildStopped    = "STOPPED"
)

// ChangeChecksStatus reports the aggregate build status
// for the given pull request.
func (r *Repository) ChangeChecksStatus(
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
		return forge.ChecksPassed // no checks configured
	}

	for _, s := range statuses {
		switch s.State {
		case buildFailed, buildStopped:
			return forge.ChecksFailed
		case buildInProgress:
			return forge.ChecksPending
		}
	}

	return forge.ChecksPassed
}
