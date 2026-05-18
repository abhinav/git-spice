package state

import (
	"context"
	"errors"
	"fmt"
	"path"

	"go.abhg.dev/gs/internal/spice/state/storage"
)

// _stagedCommentsDir is the directory holding staged comments
// for branches that have not yet been submitted as reviews.
const _stagedCommentsDir = "staged-comments"

// StagedComment is a draft inline comment
// waiting to be batch-submitted as part of a review.
type StagedComment struct {
	// ID is a local auto-increment identifier
	// unique within the branch's staged comments.
	ID int `json:"id"`

	// File is the file path relative to the repository root.
	File string `json:"file"`

	// Line is the line number in the new version of the file.
	Line int `json:"line"`

	// Body is the markdown body of the comment.
	Body string `json:"body"`

	// ThreadID is set when replying to an existing thread.
	// The format is forge-specific.
	ThreadID string `json:"threadID,omitempty"`
}

// StagedComments is the collection of staged comments
// for a branch.
type StagedComments struct {
	// NextID is the next ID to assign
	// to a new staged comment.
	NextID int `json:"nextID"`

	// Comments are the staged comments.
	Comments []StagedComment `json:"comments"`
}

func (s *Store) stagedCommentsJSON(branch string) string {
	return path.Join(_stagedCommentsDir, branch)
}

// SaveStagedComments saves the staged comments
// for the given branch.
// If staged comments already exist for the branch,
// they will be overwritten.
func (s *Store) SaveStagedComments(
	ctx context.Context,
	branch string,
	comments *StagedComments,
) error {
	err := s.db.Set(
		ctx,
		s.stagedCommentsJSON(branch),
		comments,
		fmt.Sprintf(
			"%v: save staged comments", branch,
		),
	)
	if err != nil {
		return fmt.Errorf(
			"set staged comments: %w", err,
		)
	}
	return nil
}

// LoadStagedComments retrieves staged comments
// for the given branch.
// Returns nil if no staged comments exist.
func (s *Store) LoadStagedComments(
	ctx context.Context,
	branch string,
) (*StagedComments, error) {
	var comments StagedComments
	err := s.db.Get(
		ctx,
		s.stagedCommentsJSON(branch),
		&comments,
	)
	if err != nil {
		if errors.Is(err, storage.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf(
			"get staged comments: %w", err,
		)
	}
	return &comments, nil
}

// ClearStagedComments removes staged comments
// for the given branch.
// This is a no-op if no staged comments exist.
func (s *Store) ClearStagedComments(
	ctx context.Context,
	branch string,
) error {
	err := s.db.Delete(
		ctx,
		s.stagedCommentsJSON(branch),
		fmt.Sprintf(
			"%v: clear staged comments", branch,
		),
	)
	if err != nil {
		return fmt.Errorf(
			"delete staged comments: %w", err,
		)
	}
	return nil
}
