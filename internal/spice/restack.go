package spice

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice/state"
)

// ErrAlreadyRestacked indicates that a branch is already restacked
// on top of its base.
var ErrAlreadyRestacked = errors.New("branch is already restacked")

// RestackResponse is the response to a restack operation.
type RestackResponse struct {
	Base string
}

// Restack restacks the given branch on top of its base branch,
// handling movement of the base branch if necessary.
//
// Returns [ErrAlreadyRestacked] if the branch does not need to be restacked.
func (s *Service) Restack(ctx context.Context, name string) (*RestackResponse, error) {
	b, err := s.LookupBranch(ctx, name)
	if err != nil {
		return nil, err // includes ErrNotExist
	}

	err = s.VerifyRestacked(ctx, name)
	if err == nil {
		// Case:
		// The branch is already on top of its base branch
		return nil, ErrAlreadyRestacked
	}
	var restackErr *BranchNeedsRestackError
	if !errors.As(err, &restackErr) {
		return nil, fmt.Errorf("verify restacked: %w", err)
	}

	// The branch needs to be restacked on top of its base branch.
	// We will proceed with the restack.

	baseHash := restackErr.BaseHash
	upstream := b.BaseHash

	// Case:
	// Recorded base hash is super out of date,
	// and is not an ancestor of the current branch.
	// In that case, use fork point as a hail mary
	// to guess the upstream start point.
	//
	// For context, fork point attempts to find the point
	// where the current branch diverged from the branch it
	// was originally forked from.
	// For example, given:
	//
	//  ---X---A'---o foo
	//      \
	//       A
	//        \
	//         B---o---o bar
	//
	// If bar branched from foo, when foo was at A,
	// and then we amended foo to get A',
	// bar will still refer to A.
	//
	// In this case, merge-base --fork-point will give us A,
	// and that should be the upstream (commit to start rebasing from)
	// if the recorded base hash is out of date
	// because the user changed something externally.
	if !s.repo.IsAncestor(ctx, baseHash, b.Head) {
		forkPoint, err := s.repo.ForkPoint(ctx, b.Base, name)
		if err == nil {
			if upstream != forkPoint {
				s.log.Debug("Recorded base hash is out of date. Restacking from fork point.",
					"base", b.Base,
					"branch", name,
					"forkPoint", forkPoint)
			}
			upstream = forkPoint
		}
	}

	if err := s.repo.Rebase(ctx, git.RebaseRequest{
		Onto:      baseHash.String(),
		Upstream:  upstream.String(),
		Branch:    name,
		Autostash: true,
		Quiet:     true,
	}); err != nil {
		return nil, fmt.Errorf("rebase: %w", err)
	}

	tx := s.store.BeginBranchTx()
	if err := tx.Upsert(ctx, state.UpsertRequest{
		Name:     name,
		BaseHash: baseHash,
	}); err != nil {
		return nil, fmt.Errorf("update base hash of %v: %w", name, err)
	}

	if err := tx.Commit(ctx, fmt.Sprintf("%v: restacked on %v", name, b.Base)); err != nil {
		return nil, fmt.Errorf("update state: %w", err)
	}

	return &RestackResponse{
		Base: b.Base,
	}, nil
}

// BranchNeedsRestackError is returned by [Service.VerifyRestacked]
// when a branch needs to be restacked.
type BranchNeedsRestackError struct {
	// Base is the name of the base branch for the branch.
	Base string

	// BaseHash is the hash of the base branch.
	// Note that this is the actual hash, not the hash stored in state.
	BaseHash git.Hash
}

func (e *BranchNeedsRestackError) Error() string {
	return fmt.Sprintf("branch needs to be restacked on top of %v", e.Base)
}

// VerifyRestacked verifies that the branch is on top of its base branch.
// This also updates the base branch hash if the hash is out of date,
// but the branch is restacked properly.
//
// It returns [ErrNeedsRestack] if the branch needs to be restacked,
// [state.ErrNotExist] if the branch is not tracked.
// Any other error indicates a problem with checking the branch.
func (s *Service) VerifyRestacked(ctx context.Context, name string) error {
	// A branch needs to be restacked if
	// its merge base with its base branch
	// is not its base branch's head.
	//
	// That is, the branch is not on top of its base branch's current head.
	b, err := s.LookupBranch(ctx, name)
	if err != nil {
		return err
	}

	baseHash, err := s.repo.PeelToCommit(ctx, b.Base)
	if err != nil {
		if errors.Is(err, git.ErrNotExist) {
			return fmt.Errorf("base branch %v does not exist", b.Base)
		}
		return fmt.Errorf("find commit for %v: %w", b.Base, err)
	}

	if !s.repo.IsAncestor(ctx, baseHash, b.Head) {
		return &BranchNeedsRestackError{
			Base:     b.Base,
			BaseHash: baseHash,
		}
	}

	// Branch does not need to be restacked
	// but the base hash stored in state may be out of date.
	if b.BaseHash != baseHash {
		s.log.Debug("Updating recorded base hash", "branch", name, "base", b.Base)

		tx := s.store.BeginBranchTx()
		if err := tx.Upsert(ctx, state.UpsertRequest{
			Name:     name,
			BaseHash: baseHash,
		}); err != nil {
			s.log.Warn("Failed to update recorded base hash", "error", err)
			return nil
		}

		if err := tx.Commit(ctx, fmt.Sprintf("%v: branch was restacked externally", name)); err != nil {
			// This isn't a critical error. Just log it.
			s.log.Warn("Failed to update state", "error", err)
		}
	}

	return nil
}
