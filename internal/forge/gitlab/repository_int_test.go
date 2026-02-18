package gitlab

import (
	"context"
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

// NewRepository re-exports the private NewRepository function
// for testing.
var NewRepository = newRepository

// RepositoryOptions re-exports the private repositoryOptions type
type RepositoryOptions = repositoryOptions

func MergeChange(ctx context.Context, repo *Repository, id *MR) error {
	_, _, err := repo.client.MergeRequests.AcceptMergeRequest(
		repo.repoID,
		id.Number,
		&gitlab.AcceptMergeRequestOptions{},
		gitlab.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("merge merge request: %w", err)
	}
	repo.log.Debug("Merged merge request", "mr", id.Number)
	return nil
}

func CloseChange(ctx context.Context, repo *Repository, id *MR) error {
	_, _, err := repo.client.MergeRequests.UpdateMergeRequest(
		repo.repoID,
		id.Number,
		&gitlab.UpdateMergeRequestOptions{
			StateEvent: new("close"),
		},
		gitlab.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("close merge request: %w", err)
	}
	repo.log.Debug("Closed merge request", "mr", id.Number)
	return nil
}
