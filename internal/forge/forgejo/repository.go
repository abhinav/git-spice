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

var _ forge.Repository = (*Repository)(nil)

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
