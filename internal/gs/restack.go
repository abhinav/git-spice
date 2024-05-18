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

// ErrAlreadyRestacked indicates that a branch is already restacked
// on top of its base.
var ErrAlreadyRestacked = errors.New("branch is already restacked")

// RestackResponse is the response to a restack operation.
type RestackResponse struct {
	Base string
}

// TODO: Should we be using --is-ancestor to check if a branch is on top of another?

// Restack restacks the given branch on top of its base branch,
// handling movement of the base branch if necessary.
//
// Returns [ErrAlreadyRestacked] if the branch does not need to be restacked.
func (s *Service) Restack(ctx context.Context, name string) (*RestackResponse, error) {
	b, err := s.store.Lookup(ctx, name)
	if err != nil {
		return nil, err // includes ErrNotExist
	}

	baseHash, err := s.repo.PeelToCommit(ctx, b.Base)
	if err != nil {
		// TODO:
		// Base branch has been deleted.
		// Suggest a means of repairing this:
		// possibly by prompting to select a different base branch.
		if errors.Is(err, git.ErrNotExist) {
			return nil, fmt.Errorf("base branch %v does not exist", b.Base)
		}
		return nil, fmt.Errorf("peel to commit: %w", err)
	}

	// Case:
	// The branch is already on top of its base branch.
	mergeBase, err := s.repo.MergeBase(ctx, name, b.Base)
	if err == nil && mergeBase == baseHash {
		if mergeBase != b.BaseHash {
			// If our information is stale,
			// update the base hash stored in state.
			err := s.store.Update(ctx, &state.UpdateRequest{
				Upserts: []state.UpsertRequest{
					{
						Name:     name,
						BaseHash: mergeBase,
					},
				},
				Message: fmt.Sprintf("branch %v was restacked externally", name),
			})
			if err != nil {
				s.log.Warnf("failed to update state with new base hash: %v", err)
			}
		}

		return nil, ErrAlreadyRestacked
	}

	upstream := b.BaseHash
	// Case:
	// Current branch has diverged from what the target branch
	// was forked from. That is:
	//
	//  ---X---A'---o current
	//      \
	//       A
	//        \
	//         B---o---o target
	//
	// Upstack was forked from our branch when the child of X was A.
	// Since then, we have amended A to get A',
	// but the target branch still points to A.
	//
	// In this case, merge-base --fork-point will give us A,
	// and that should be the base of the target branch.
	forkPoint, err := s.repo.ForkPoint(ctx, b.Base, name)
	if err == nil {
		upstream = forkPoint
		s.log.Debugf("Using fork point %v as rebase base", upstream)
	}

	if err := s.repo.Rebase(ctx, git.RebaseRequest{
		Onto:      baseHash.String(),
		Upstream:  upstream.String(),
		Branch:    name,
		Autostash: true,
		Quiet:     true,
	}); err != nil {
		return nil, fmt.Errorf("rebase: %w", err)
		// TODO: detect conflicts in rebase,
		// print message about "gs continue"
	}

	err = s.store.Update(ctx, &state.UpdateRequest{
		Upserts: []state.UpsertRequest{
			{
				Name:     name,
				BaseHash: baseHash,
			},
		},
		Message: fmt.Sprintf("%s: restacked on %s", name, b.Base),
	})
	if err != nil {
		return nil, fmt.Errorf("update branch information: %w", err)
	}

	return &RestackResponse{
		Base: b.Base,
	}, nil
}

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

	// TODO: a lot of the logic is shared with Restack.
	// See if we can implement this via a DryRun option in Restack.

	baseHash, err := s.repo.PeelToCommit(ctx, b.Base)
	if err != nil {
		if errors.Is(err, git.ErrNotExist) {
			return fmt.Errorf("base branch %v does not exist", b.Base)
		}
		return fmt.Errorf("find commit for %v: %w", b.Base, err)
	}

	mergeBase, err := s.repo.MergeBase(ctx, name, b.Base)
	if err != nil {
		return fmt.Errorf("merge-base(%v, %v): %w", name, b.Base, err)
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
