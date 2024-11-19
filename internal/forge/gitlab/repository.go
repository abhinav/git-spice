package gitlab

import (
	"context"
	"fmt"
	"iter"

	"github.com/charmbracelet/log"
	"github.com/xanzy/go-gitlab"
	"go.abhg.dev/gs/internal/forge"
)

// Repository is a GitLab repository.
type Repository struct {
	owner, repo string
	repoID      int
	log         *log.Logger
	client      *gitlab.Client
	forge       *Forge
}

var _ forge.Repository = (*Repository)(nil)

func newRepository(
	forge *Forge,
	owner, repo string,
	log *log.Logger,
	client *gitlab.Client,
	repoID *int,
) (*Repository, error) {
	if repoID == nil {
		project, _, err := client.Projects.GetProject(owner+"/"+repo, nil)
		if err != nil {
			return nil, fmt.Errorf("get repository ID: %w", err)
		}
		repoID = &project.ID
	}

	return &Repository{
		owner:  owner,
		repo:   repo,
		forge:  forge,
		log:    log,
		client: client,
		repoID: *repoID,
	}, nil
}

// Forge returns the forge this repository belongs to.
func (r *Repository) Forge() forge.Forge { return r.forge }

func (r *Repository) SubmitChange(ctx context.Context, req forge.SubmitChangeRequest) (forge.SubmitChangeResult, error) {
	// TODO implement me
	panic("implement me")
}

func (r *Repository) EditChange(ctx context.Context, id forge.ChangeID, opts forge.EditChangeOptions) error {
	// TODO implement me
	panic("implement me")
}

func (r *Repository) FindChangesByBranch(ctx context.Context, branch string, opts forge.FindChangesOptions) ([]*forge.FindChangeItem, error) {
	// TODO implement me
	panic("implement me")
}

func (r *Repository) FindChangeByID(ctx context.Context, id forge.ChangeID) (*forge.FindChangeItem, error) {
	// TODO implement me
	panic("implement me")
}

func (r *Repository) ChangesAreMerged(ctx context.Context, ids []forge.ChangeID) ([]bool, error) {
	// TODO implement me
	panic("implement me")
}

func (r *Repository) PostChangeComment(ctx context.Context, id forge.ChangeID, s string) (forge.ChangeCommentID, error) {
	// TODO implement me
	panic("implement me")
}

func (r *Repository) UpdateChangeComment(ctx context.Context, id forge.ChangeCommentID, s string) error {
	// TODO implement me
	panic("implement me")
}

func (r *Repository) ListChangeComments(ctx context.Context, id forge.ChangeID, options *forge.ListChangeCommentsOptions) iter.Seq2[*forge.ListChangeCommentItem, error] {
	// TODO implement me
	panic("implement me")
}

func (r *Repository) NewChangeMetadata(ctx context.Context, id forge.ChangeID) (forge.ChangeMetadata, error) {
	// TODO implement me
	panic("implement me")
}

func (r *Repository) ListChangeTemplates(ctx context.Context) ([]*forge.ChangeTemplate, error) {
	// TODO implement me
	panic("implement me")
}
