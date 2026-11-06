package state

import (
	"context"
	"errors"
	"fmt"
	"path"

	"go.abhg.dev/gs/internal/spice/state/storage"
)

// _preparedDir holds saved change titles and bodies for submission retries.
// These records recover user input; they do not record publication progress.
const _preparedDir = "prepared"

type preparedBranchState struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func (s *Store) preparedBranchJSON(name string) string {
	return path.Join(_preparedDir, name)
}

// PreparedBranch holds the title and body entered for a branch submission.
// Its presence does not establish whether a push or remote creation succeeded.
// Submission saves it before pushing and clears it after remote creation,
// independently of recording the created change's local association.
type PreparedBranch struct {
	// Name is the name of the branch.
	Name string

	// Subject is the subject of the change that was recorded.
	Subject string

	// Body is the body of the change that was recorded.
	Body string
}

// SavePreparedBranch saves submission input for recovery with LoadPreparedBranch
// if pushing or creating the change fails.
// If the branch is already saved, it will be overwritten.
// Use ClearPreparedBranch to remove the saved information.
func (s *Store) SavePreparedBranch(ctx context.Context, b *PreparedBranch) error {
	state := preparedBranchState{
		Subject: b.Subject,
		Body:    b.Body,
	}

	err := s.db.Set(ctx, s.preparedBranchJSON(b.Name), state,
		fmt.Sprintf("%v: save prepared branch", b.Name))
	if err != nil {
		return fmt.Errorf("set prepared branch state: %w", err)
	}

	return nil
}

// LoadPreparedBranch retrieves metadata about a branch submission
// that was previously saved with SavePreparedBranch.
// If there's no information saved, it returns nil.
//
// It may be used to recover from past failures in submitting a branch
// so users do not have to re-enter the change metadata.
func (s *Store) LoadPreparedBranch(ctx context.Context, name string) (*PreparedBranch, error) {
	var state preparedBranchState
	if err := s.db.Get(ctx, s.preparedBranchJSON(name), &state); err != nil {
		if errors.Is(err, storage.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("get prepared branch state: %w", err)
	}

	return &PreparedBranch{
		Name:    name,
		Subject: state.Subject,
		Body:    state.Body,
	}, nil
}

// ClearPreparedBranch removes the information saved about a branch
// that was previously saved with SavePreparedBranch.
// This is a no-op if the branch information isn't saved anymore.
func (s *Store) ClearPreparedBranch(ctx context.Context, name string) error {
	err := s.db.Delete(ctx, s.preparedBranchJSON(name),
		fmt.Sprintf("%v: clear prepared branch", name))
	if err != nil {
		return fmt.Errorf("delete prepared branch state: %w", err)
	}

	return nil
}
