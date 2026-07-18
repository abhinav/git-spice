package spice

import (
	"context"
	"encoding/json"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/spice/state"
)

// BranchOntoMode specifies how BranchOnto moves one branch to its new base.
type BranchOntoMode int

const (
	// BranchOntoRebase updates state and rebases the branch's own commits.
	BranchOntoRebase BranchOntoMode = iota

	// BranchOntoRetargetOnly updates state
	// without rebasing the branch's commits.
	//
	// The resolved upstream boundary is preserved
	// so a future restack can replay the branch correctly.
	BranchOntoRetargetOnly
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

	// Mode controls whether Branch's commits are rebased immediately
	// or only retargeted in git-spice state.
	Mode BranchOntoMode
}

// BranchOnto moves the commits of a branch onto a different base branch,
// updating internal state to reflect the new branch stack.
// It DOES NOT modify the upstack branches of the branch being moved.
// When Mode is [BranchOntoRebase],
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

	replayRange, err := s.resolveBranchReplayRange(ctx, req.Branch, branch)
	if err != nil {
		return fmt.Errorf("resolve branch replay range: %w", err)
	}
	fromHash := replayRange.Upstream

	// The destination can already contain the resolved replay boundary.
	// This is expected when two branches share a downstack
	// and when a branch-onto operation resumes after a conflict.
	// For example, before the first attempt:
	//
	//           C--D (Current)  (git-spice: base=OriginalBase)
	//          /
	//     o---X (OriginalBase)
	//          \
	//           A--B (NewBase)  (git-spice: base=OriginalBase)
	//
	// After Git has replayed Current but before git-spice state is updated:
	//
	//     o---X (OriginalBase)
	//          \
	//           A--B (NewBase)       (git-spice: base=OriginalBase)
	//               \
	//                C--D (Current)  (git-spice: base=OriginalBase)
	//
	// Using the destination as the exclusive boundary selects
	// NewBase..Current. The first attempt excludes NewBase's commits,
	// and a resumed attempt becomes a no-op before state is updated.
	if s.repo.IsAncestor(ctx, fromHash, ontoHash) {
		fromHash = ontoHash
	}

	s.log.Debug(
		"Moving commits onto new base",
		"branch", req.Branch,
		"oldBase", branch.Base,
		"newBase", req.Onto,
		"commits", fromHash.Short()+".."+branch.Head.Short(),
	)

	branchTx := s.store.BeginBranchTx()

	baseHash := ontoHash
	rebaseBranch := true
	switch req.Mode {
	case BranchOntoRebase:
	case BranchOntoRetargetOnly:
		baseHash = fromHash
		rebaseBranch = false
	default:
		must.Failf("unknown branch onto mode: %v", req.Mode)
	}

	if err := branchTx.Upsert(ctx, state.UpsertRequest{
		Name:            req.Branch,
		Base:            req.Onto,
		BaseHash:        baseHash,
		MergedDownstack: req.MergedDownstack,
	}); err != nil {
		return fmt.Errorf("set base of branch %s to %s: %w", req.Branch, req.Onto, err)
	}

	if rebaseBranch {
		if err := s.wt.Rebase(ctx, git.RebaseRequest{
			Branch:    req.Branch,
			Upstream:  string(fromHash),
			Onto:      ontoHash.String(),
			Autostash: true,
			Quiet:     true, // TODO: if verbose, disable this
		}); err != nil {
			return fmt.Errorf("rebase: %w", err)
		}
	}

	if err := branchTx.Commit(ctx, fmt.Sprintf("%v: onto %v", req.Branch, req.Onto)); err != nil {
		return fmt.Errorf("update state: %w", err)
	}

	return nil
}
