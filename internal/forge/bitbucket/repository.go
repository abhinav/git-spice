package bitbucket

import (
	"context"
	"errors"
	"iter"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/silog"
)

// ErrNotImplemented indicates that a feature is not yet implemented.
var ErrNotImplemented = errors.New("not implemented")

// Repository is a Bitbucket repository.
type Repository struct {
	client *client

	workspace, repo string
	log             *silog.Logger
	forge           *Forge
}

var _ forge.Repository = (*Repository)(nil)

func newRepository(
	forge *Forge,
	workspace, repo string,
	log *silog.Logger,
	client *client,
) *Repository {
	return &Repository{
		client:    client,
		workspace: workspace,
		repo:      repo,
		forge:     forge,
		log:       log,
	}
}

// Forge returns the forge this repository belongs to.
func (r *Repository) Forge() forge.Forge { return r.forge }

// SubmitChange creates a new pull request in the repository.
//
// This is a stub that will be implemented in a future PR.
func (r *Repository) SubmitChange(
	_ context.Context,
	_ forge.SubmitChangeRequest,
) (forge.SubmitChangeResult, error) {
	return forge.SubmitChangeResult{}, ErrNotImplemented
}

// EditChange edits an existing pull request.
//
// This is a stub that will be implemented in a future PR.
func (r *Repository) EditChange(
	_ context.Context,
	_ forge.ChangeID,
	_ forge.EditChangeOptions,
) error {
	return ErrNotImplemented
}

// FindChangesByBranch finds pull requests by branch name.
//
// This is a stub that will be implemented in a future PR.
func (r *Repository) FindChangesByBranch(
	_ context.Context,
	_ string,
	_ forge.FindChangesOptions,
) ([]*forge.FindChangeItem, error) {
	return nil, ErrNotImplemented
}

// FindChangeByID finds a pull request by its ID.
//
// This is a stub that will be implemented in a future PR.
func (r *Repository) FindChangeByID(
	_ context.Context,
	_ forge.ChangeID,
) (*forge.FindChangeItem, error) {
	return nil, ErrNotImplemented
}

// ChangesStates retrieves the states of multiple pull requests.
//
// This is a stub that will be implemented in a future PR.
func (r *Repository) ChangesStates(
	_ context.Context,
	_ []forge.ChangeID,
) ([]forge.ChangeState, error) {
	return nil, ErrNotImplemented
}

// PostChangeComment posts a comment on a pull request.
//
// This is a stub that will be implemented in a future PR.
func (r *Repository) PostChangeComment(
	_ context.Context,
	_ forge.ChangeID,
	_ string,
) (forge.ChangeCommentID, error) {
	return nil, ErrNotImplemented
}

// UpdateChangeComment updates an existing comment.
//
// This is a stub that will be implemented in a future PR.
func (r *Repository) UpdateChangeComment(
	_ context.Context,
	_ forge.ChangeCommentID,
	_ string,
) error {
	return ErrNotImplemented
}

// DeleteChangeComment deletes an existing comment.
//
// This is a stub that will be implemented in a future PR.
func (r *Repository) DeleteChangeComment(
	_ context.Context,
	_ forge.ChangeCommentID,
) error {
	return ErrNotImplemented
}

// ListChangeComments lists comments on a pull request.
//
// This is a stub that will be implemented in a future PR.
func (r *Repository) ListChangeComments(
	_ context.Context,
	_ forge.ChangeID,
	_ *forge.ListChangeCommentsOptions,
) iter.Seq2[*forge.ListChangeCommentItem, error] {
	return func(yield func(*forge.ListChangeCommentItem, error) bool) {
		yield(nil, ErrNotImplemented)
	}
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
