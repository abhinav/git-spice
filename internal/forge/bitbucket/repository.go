package bitbucket

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/silog"
)

// Repository is a Bitbucket repository.
type Repository struct {
	client *client

	url             string // base URL (e.g., "https://bitbucket.org")
	workspace, repo string
	log             *silog.Logger
	forge           *Forge
}

var (
	_ forge.Repository    = (*Repository)(nil)
	_ forge.WithChangeURL = (*Repository)(nil)
)

func newRepository(
	forge *Forge,
	url, workspace, repo string,
	log *silog.Logger,
	client *client,
) *Repository {
	return &Repository{
		client:    client,
		url:       url,
		workspace: workspace,
		repo:      repo,
		forge:     forge,
		log:       log,
	}
}

// Forge returns the forge this repository belongs to.
func (r *Repository) Forge() forge.Forge { return r.forge }

// ChangeURL returns the web URL for viewing the given pull request.
func (r *Repository) ChangeURL(id forge.ChangeID) string {
	prNum := mustPR(id).Number
	return fmt.Sprintf("%s/%s/%s/pull-requests/%d", r.url, r.workspace, r.repo, prNum)
}

// DeleteChangeComment deletes an existing comment.
// Bitbucket API supports comment deletion but it's rarely needed.
func (r *Repository) DeleteChangeComment(
	_ context.Context,
	_ forge.ChangeCommentID,
) error {
	// Not implemented - comment deletion is rarely needed
	// and can be done manually if required.
	return nil
}

// NewChangeMetadata returns the metadata for a pull request.
func (r *Repository) NewChangeMetadata(
	_ context.Context,
	id forge.ChangeID,
) (forge.ChangeMetadata, error) {
	pr := mustPR(id)
	return &PRMetadata{PR: pr}, nil
}

// ListChangeTemplates lists pull request templates in the repository.
// Bitbucket has limited template support, so this returns an empty list.
func (r *Repository) ListChangeTemplates(
	_ context.Context,
) ([]*forge.ChangeTemplate, error) {
	return nil, nil
}
