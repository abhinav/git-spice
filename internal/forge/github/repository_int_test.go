package github

import (
	"context"
	"fmt"
)

// NewRepository re-exports the private NewRepository function
// for testing.
var NewRepository = newRepository

func CloseChange(ctx context.Context, repo *Repository, id *PR) error {
	if err := repo.gateway.ClosePullRequest(ctx, id.GQLID); err != nil {
		return fmt.Errorf("close pull request: %w", err)
	}

	repo.log.Debug("Closed pull request", "pr", id.Number)
	return nil
}
