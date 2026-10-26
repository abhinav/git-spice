package forgejo

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/forgejo"
	"go.abhg.dev/gs/internal/silog"
)

// Repository is a Forgejo repository.
type Repository struct {
	client *forgejo.Client

	owner string
	repo  string
	log   *silog.Logger
	forge *Forge

	userID  int64
	canPush bool
}

var (
	_ forge.Repository        = (*Repository)(nil)
	_ forge.WithComparisonURL = (*Repository)(nil)
)

// ComparisonURL returns a URL for a comparison on Forgejo.
// See Forgejo's [compare API documentation].
//
// [compare API documentation]: https://codeberg.org/api/swagger#/repository/repoCompareDiff
func (r *Repository) ComparisonURL(req forge.ComparisonRequest) string {
	head := req.HeadURLEncoded()
	if req.HeadRepository != nil {
		headRepo := mustRepositoryID(req.HeadRepository)
		if headRepo.owner != r.owner || headRepo.name != r.repo {
			// Forgejo qualifies a cross-fork head with its owner and repository.
			head = headRepo.String() + ":" + head
		}
	}
	return fmt.Sprintf("%s/%s/%s/compare/%s...%s",
		r.forge.URL(), r.owner, r.repo, req.BaseURLEncoded(), head)
}

// NewRepository builds a Forgejo repository wrapper.
func NewRepository(
	ctx context.Context,
	forge *Forge,
	owner string,
	repo string,
	log *silog.Logger,
	client *forgejo.Client,
) (*Repository, error) {
	gatewayRepo, _, err := client.RepositoryGet(ctx, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("get repository: %w", err)
	}
	user, _, err := client.UserCurrent(ctx)
	if err != nil {
		return nil, fmt.Errorf("get current user: %w", err)
	}

	return &Repository{
		client: client,
		owner:  owner,
		repo:   repo,
		log:    log,
		forge:  forge,
		userID: user.ID,
		canPush: gatewayRepo.Permissions != nil &&
			gatewayRepo.Permissions.Push,
	}, nil
}

// Forge returns the forge this repository belongs to.
func (r *Repository) Forge() forge.Forge { return r.forge }
