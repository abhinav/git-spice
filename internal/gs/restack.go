package gs

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/state"
)

// ErrNeedsRestack indicates that a branch should be restacked
// on top of its base branch.
var ErrNeedsRestack = errors.New("branch needs to be restacked")

// ErrNotExist indicates that a branch is not tracked.
var ErrNotExist = git.ErrNotExist

// VerifyRestacked verifies that the branch is on top of its base branch.
// This also updates the base branch hash if the hash is out of date,
// but the branch is restacked properly.
//
// It returns [ErrNeedsRestack] if the branch needs to be restacked,
// and [ErrNotExist] if the branch is not tracked.
// Any other error indicates a problem with checking the branch.
func (s *Service) VerifyRestacked(ctx context.Context, name string) error {
	// A branch needs to be restacked if
	// its merge base with its base branch
	// is not its base branch's head.
	//
	// That is, the branch is not on top of its base branch's current head.
	b, err := s.store.Lookup(ctx, name)
	if err != nil {
		return err // includes ErrNotExist
	}

	mergeBase, err := s.repo.MergeBase(ctx, name, b.Base)
	if err != nil {
		return fmt.Errorf("merge-base(%v, %v): %w", name, b.Base, err)
	}

	baseHash, err := s.repo.PeelToCommit(ctx, b.Base)
	if err != nil {
		if errors.Is(err, git.ErrNotExist) {
			return fmt.Errorf("base branch %v does not exist", b.Base)
		}
		return fmt.Errorf("find commit for %v: %w", b.Base, err)
	}

	// Branch needs to be restacked.
	if baseHash != mergeBase {
		return ErrNeedsRestack
	}

	// Branch does not need to be restacked
	// but the base hash stored in state is out of date.
	if b.BaseHash != baseHash {
		req := state.UpdateRequest{
			Upserts: []state.UpsertRequest{
				{Name: name, BaseHash: baseHash},
			},
			Message: fmt.Sprintf("branch %v was restacked externally", name),
		}
		if err := s.store.Update(ctx, &req); err != nil {
			// This isn't a critical error. Just log it.
			s.log.Warnf("failed to update state with new base hash: %v", err)
		}
	}

	return nil
}
