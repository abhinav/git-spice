package gitea

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
)

// ChangeChecksState reports the aggregate CI status for the given pull request.
func (r *Repository) ChangeChecksState(
	ctx context.Context,
	fid forge.ChangeID,
) (forge.ChecksState, error) {
	id := mustPR(fid)

	pr, _, err := r.client.PullGet(ctx, r.owner, r.repo, id.Number)
	if err != nil {
		return 0, fmt.Errorf("get pull request: %w", err)
	}
	if pr.Head == nil || pr.Head.Sha == "" {
		return forge.ChecksPassed, nil
	}

	status, _, err := r.client.CommitStatusCombined(ctx, r.owner, r.repo, pr.Head.Sha)
	if err != nil {
		return 0, fmt.Errorf("get commit status: %w", err)
	}

	return commitStatusState(status.State), nil
}

func commitStatusState(state string) forge.ChecksState {
	switch state {
	case "", giteagw.CommitStatusSuccess, giteagw.CommitStatusWarning:
		return forge.ChecksPassed
	case giteagw.CommitStatusPending:
		return forge.ChecksPending
	case giteagw.CommitStatusFailure, giteagw.CommitStatusError:
		return forge.ChecksFailed
	default:
		return forge.ChecksPending
	}
}
