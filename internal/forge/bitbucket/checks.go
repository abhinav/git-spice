package bitbucket

import (
	"context"

	"go.abhg.dev/gs/internal/forge"
)

// ChangeChecksState reports failed > pending > passed for a pull request.
func (r *Repository) ChangeChecksState(
	ctx context.Context,
	id forge.ChangeID,
) (forge.ChecksState, error) {
	pr, err := r.gw.GetChange(ctx, mustPR(id).Number)
	if err != nil {
		return 0, err
	}

	if pr.HeadHash == "" {
		return forge.ChecksPassed, nil
	}

	checks, err := r.gw.ListCommitChecks(ctx, pr.HeadHash)
	if err != nil {
		return 0, err
	}

	result := forge.ChecksPassed
	for _, check := range checks {
		switch check {
		case forge.ChecksFailed:
			return forge.ChecksFailed, nil
		case forge.ChecksPending:
			result = forge.ChecksPending
		}
	}
	return result, nil
}
