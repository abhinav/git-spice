package forgejo

import (
	"context"
	"fmt"
	"testing"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/forgejo"
	"go.abhg.dev/testing/stub"
)

// MergeChange merges a pull request for integration tests.
func MergeChange(ctx context.Context, repo *Repository, pr *PR) error {
	return repo.MergeChange(ctx, pr, forge.MergeChangeOptions{})
}

// CloseChange closes a pull request for integration tests.
func CloseChange(ctx context.Context, repo *Repository, pr *PR) error {
	state := "closed"
	if _, _, err := repo.client.PullRequestEdit(
		ctx,
		repo.owner,
		repo.repo,
		pr.Number,
		&forgejo.EditPullRequestOption{State: &state},
	); err != nil {
		return fmt.Errorf("close pull request: %w", err)
	}
	return nil
}

// SetChangeCommentsPageSize changes the comment page size for integration
// tests and restores it after the test finishes.
func SetChangeCommentsPageSize(t testing.TB, size int) {
	t.Cleanup(stub.Value(&_listChangeCommentsPageSize, size))
}
