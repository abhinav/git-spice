package gitea

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
	"go.abhg.dev/gs/internal/silog"
)

// Repository is a Gitea repository.
type Repository struct {
	client *giteagw.Client

	owner, repo string
	log         *silog.Logger
	forge       *Forge

	// userID is the ID of the authenticated user.
	// Used to determine whether a comment can be updated.
	userID int64

	// canPush reports whether the authenticated user has push access.
	canPush bool
}

var (
	_ forge.Repository        = (*Repository)(nil)
	_ forge.WithChangeURL     = (*Repository)(nil)
	_ forge.WithComparisonURL = (*Repository)(nil)
)

// ChangeURL returns the web URL for viewing the given pull request.
func (r *Repository) ChangeURL(id forge.ChangeID) string {
	return fmt.Sprintf("%s/%s/%s/pulls/%d",
		r.forge.Options.URL, r.owner, r.repo, mustPR(id).Number)
}

// ComparisonURL returns a URL for a comparison on Gitea.
// See Gitea's [compare API documentation].
//
// [compare API documentation]: https://docs.gitea.com/api/1.24/#tag/repository/operation/repoCompareDiff
func (r *Repository) ComparisonURL(req forge.ComparisonRequest) string {
	head := req.HeadURLEncoded()
	if req.HeadRepository != nil {
		headRepo := mustRepositoryID(req.HeadRepository)
		if headRepo.owner != r.owner || headRepo.name != r.repo {
			// Gitea qualifies a cross-fork head with its owner and repository.
			head = headRepo.String() + ":" + head
		}
	}
	return fmt.Sprintf("%s/%s/%s/compare/%s...%s",
		r.forge.Options.URL, r.owner, r.repo, req.BaseURLEncoded(), head)
}

func newRepository(
	ctx context.Context,
	f *Forge,
	owner, repo string,
	log *silog.Logger,
	client *giteagw.Client,
) (*Repository, error) {
	gatewayRepo, _, err := client.RepoGet(ctx, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("get repository: %w", err)
	}

	user, _, err := client.UserCurrent(ctx)
	if err != nil {
		return nil, fmt.Errorf("get current user: %w", err)
	}

	return &Repository{
		client:  client,
		owner:   owner,
		repo:    repo,
		forge:   f,
		log:     log,
		userID:  user.ID,
		canPush: gatewayRepo.Permissions != nil && gatewayRepo.Permissions.Push,
	}, nil
}

// Forge returns the forge this repository belongs to.
func (r *Repository) Forge() forge.Forge { return r.forge }

// NewRepository re-exports newRepository for integration tests.
var NewRepository = newRepository
