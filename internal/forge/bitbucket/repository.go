package bitbucket

import (
	"context"

	"go.abhg.dev/gs/internal/forge"
	gw "go.abhg.dev/gs/internal/gateway/bitbucket"
	"go.abhg.dev/gs/internal/silog"
)

//go:generate mockgen -destination=mocks_test.go -package=bitbucket -write_package_comment=false -typed=true go.abhg.dev/gs/internal/gateway/bitbucket Gateway

// Repository is a Bitbucket repository.
type Repository struct {
	forge *Forge
	log   *silog.Logger
	gw    gw.Gateway
}

var (
	_ forge.Repository    = (*Repository)(nil)
	_ forge.WithChangeURL = (*Repository)(nil)
)

func newRepository(forge *Forge, log *silog.Logger, gw gw.Gateway) *Repository {
	return &Repository{
		forge: forge,
		log:   log,
		gw:    gw,
	}
}

// Forge returns the forge this repository belongs to.
func (r *Repository) Forge() forge.Forge { return r.forge }

// ChangeURL returns the web URL for viewing the given pull request.
func (r *Repository) ChangeURL(id forge.ChangeID) string {
	return r.gw.ChangeURL(mustPR(id).Number)
}

// NewChangeMetadata returns the metadata for a pull request.
func (r *Repository) NewChangeMetadata(
	_ context.Context,
	id forge.ChangeID,
) (forge.ChangeMetadata, error) {
	return &PRMetadata{PR: mustPR(id)}, nil
}
