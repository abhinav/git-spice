package gitlab

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/gitlab"
)

// NewRepository re-exports the private NewRepository function
// for testing.
var NewRepository = newRepository

// RepositoryOptions re-exports the private repositoryOptions type
type RepositoryOptions = repositoryOptions

func RepositoryProjectID(repo *Repository) int64 {
	return repo.repoID
}

// MergeChange merges a merge request using the production method.
func MergeChange(ctx context.Context, repo *Repository, id *MR) error {
	return repo.MergeChange(ctx, id, forge.MergeChangeOptions{})
}

func CloseChange(ctx context.Context, repo *Repository, id *MR) error {
	_, _, err := repo.client.MergeRequestUpdate(
		ctx,
		repo.repoID,
		id.Number,
		&gitlab.UpdateMergeRequestOptions{
			StateEvent: new("close"),
		},
	)
	if err != nil {
		return fmt.Errorf("close merge request: %w", err)
	}
	repo.log.Debug("Closed merge request", "mr", id.Number)
	return nil
}
