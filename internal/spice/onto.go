package spice

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/spice/state"
)

// BranchOntoRequest is a request to move a branch onto another branch.
type BranchOntoRequest struct {
	// Branch is the branch to move.
	// This must not be the trunk branch.
	Branch string

	// Onto is the target branch to move the branch onto.
	// Onto may be the trunk branch.
	Onto string
}

// BranchOnto moves the commits of a branch onto a different base branch,
// updating internal state to reflect the new branch stack.
// It DOES NOT modify the upstack branches of the branch being moved.
// As this involves a rebase operation,
// the caller should be prepared to rescue the operation if it fails.
func (s *Service) BranchOnto(ctx context.Context, req *BranchOntoRequest) error {
	must.NotBeEqualf(req.Branch, s.store.Trunk(), "cannot move trunk")

	branch, err := s.LookupBranch(ctx, req.Branch)
	if err != nil {
		return fmt.Errorf("lookup branch: %w", err)
	}

	var ontoHash git.Hash
	if req.Onto == s.store.Trunk() {
		ontoHash, err = s.repo.PeelToCommit(ctx, req.Onto)
		if err != nil {
			return fmt.Errorf("resolve trunk: %w", err)
		}
	} else {
		// Non-trunk branches must be tracked.
		onto, err := s.LookupBranch(ctx, req.Onto)
		if err != nil {
			return fmt.Errorf("lookup onto: %w", err)
		}
		ontoHash = onto.Head
	}

	if err := s.repo.Rebase(ctx, git.RebaseRequest{
		Branch:    req.Branch,
		Upstream:  branch.BaseHash.String(),
		Onto:      ontoHash.String(),
		Autostash: true,
		Quiet:     true,
	}); err != nil {
		return fmt.Errorf("rebase: %w", err)
	}

	// If the operation succeeded, update internal state.
	err = s.store.UpdateBranch(ctx, &state.UpdateRequest{
		Upserts: []state.UpsertRequest{
			{
				Name:     req.Branch,
				Base:     req.Onto,
				BaseHash: ontoHash,
			},
		},
		Message: fmt.Sprintf("%v: onto %v", req.Branch, req.Onto),
	})
	if err != nil {
		return fmt.Errorf("update state: %w", err)
	}

	return nil
}
