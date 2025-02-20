package spice

import (
	"context"
	"encoding/json"
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

	// MergedDownstack for [Branch], if any.
	MergedDownstack *[]json.RawMessage
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

	// We're trying to move commits BaseHash..HEAD onto commit OntoHash.
	//
	// However, there's a possibility that BaseHash is reachable from OntoHash
	// because the old base is also the base of onto,
	// and we've already partially rebased and handled a conflict.
	//
	// For example, suppose we have:
	//
	//           C--D (Current)  (gs: base=OriginalBase)
	//          /
	//     o---X (OriginalBase)
	//          \
	//           A--B (NewBase)  (gs: base=OriginalBase)
	//
	// If we run 'gs branch onto NewBase' from Current,
	// and there's a conflict, the user will resolve the rebase conflict,
	// but the gs state will not yet be updated.
	//
	//     o---X (OriginalBase)
	//          \
	//           A--B (NewBase)       (gs: base=OriginalBase)
	//               \
	//                C--D (Current)  (gs: base=OriginalBase)
	//
	// At that point, 'gs rebase continue' will re-run the original command
	// 'gs branch onto NewBase' from Current,
	// except the commits it wants (OriginalBase..Current)
	// now includes commits OriginalBase..NewBase,
	// which will fail for obvious reasons.
	//
	// To catch this, if OriginalBase is reachable from NewBase,
	// we'll change the commit range to NewBase..Current.
	// This will turn the rebase into a no-op, but it'll correctly update state.
	fromHash := branch.BaseHash
	if s.repo.IsAncestor(ctx, fromHash, ontoHash) {
		fromHash = ontoHash
	}

	s.log.Debug("Moving commits onto new base",
		"branch", req.Branch,
		"oldBase", branch.Base,
		"newBase", req.Onto,
		"commits", fromHash.Short()+".."+branch.Head.Short(),
	)

	branchTx := s.store.BeginBranchTx()
	if err := branchTx.Upsert(ctx, state.UpsertRequest{
		Name:            req.Branch,
		Base:            req.Onto,
		BaseHash:        ontoHash,
		MergedDownstack: req.MergedDownstack,
	}); err != nil {
		return fmt.Errorf("set base of branch %s to %s: %w", req.Branch, req.Onto, err)
	}

	if err := s.repo.Rebase(ctx, git.RebaseRequest{
		Branch:    req.Branch,
		Upstream:  string(fromHash),
		Onto:      ontoHash.String(),
		Autostash: true,
		Quiet:     true, // TODO: if verbose, disable this
	}); err != nil {
		return fmt.Errorf("rebase: %w", err)
	}

	if err := branchTx.Commit(ctx, fmt.Sprintf("%v: onto %v", req.Branch, req.Onto)); err != nil {
		return fmt.Errorf("update state: %w", err)
	}

	return nil
}
