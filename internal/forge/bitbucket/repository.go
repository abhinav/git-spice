package bitbucket

import (
	"context"
	"fmt"

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
	_ forge.Repository              = (*Repository)(nil)
	_ forge.WithChangeURL           = (*Repository)(nil)
	_ forge.WithNavigationReference = (*Repository)(nil)
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

// NavigationReference returns the markdown referencing a pull request in
// stack navigation, tagged so that Bitbucket Cloud renders it as an inline
// card: the pull request's title and state, resolved when the comment is
// rendered.
//
// The tag is attr_list syntax, which Bitbucket Data Center's CommonMark
// renderer does not support, so Data Center gets a plain link.
func (r *Repository) NavigationReference(id forge.ChangeID) string {
	link := fmt.Sprintf("[%v](%v)", id, r.ChangeURL(id))
	if r.forge.kind != KindCloud {
		return link
	}

	return link + "{: data-inline-card='' }"
}

// NewChangeMetadata returns the metadata for a pull request.
func (r *Repository) NewChangeMetadata(
	_ context.Context,
	id forge.ChangeID,
) (forge.ChangeMetadata, error) {
	return &PRMetadata{PR: mustPR(id)}, nil
}
