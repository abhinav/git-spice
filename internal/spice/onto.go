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
	// The old upstream boundary is preserved
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

	// The recorded base hash may be stale if the old base branch
	// was advanced outside git-spice,
	// and the branch being moved was restacked outside git-spice.
	//
	// For example:
	//
	//     o---A (RecordedBase)
	//          \
	//           B---C (ActualBase)
	//                \
	//                 D (Current)
	//
	// git-spice may still think Current is based on RecordedBase,
	// but using RecordedBase..Current would replay B and C.
	// If ActualBase is reachable from Current,
	// use ActualBase..Current so only Current's own commits are replayed.
	fromHash := branch.BaseHash
	if actualBaseHash, err := s.repo.PeelToCommit(ctx, branch.Base); err == nil {
		if s.repo.IsAncestor(ctx, actualBaseHash, branch.Head) {
			fromHash = actualBaseHash
		}
	}

	// We're trying to move the selected commit range onto OntoHash.
	//
	// However, there's a possibility that BaseHash is reachable from OntoHash
	// because the old base is also the base of onto,
	// and we've already partially rebased and handled a conflict.
	//
	// For example, suppose we have:
	//
	//           C--D (Current)  (git-spice: base=OriginalBase)
	//          /
	//     o---X (OriginalBase)
	//          \
	//           A--B (NewBase)  (git-spice: base=OriginalBase)
	//
	// If we run 'git-spice branch onto NewBase' from Current,
	// and there's a conflict, the user will resolve the rebase conflict,
	// but the git-spice state will not yet be updated.
	//
	//     o---X (OriginalBase)
	//          \
	//           A--B (NewBase)       (git-spice: base=OriginalBase)
	//               \
	//                C--D (Current)  (git-spice: base=OriginalBase)
	//
	// At that point, 'git-spice rebase continue' will re-run the original command
	// 'git-spice branch onto NewBase' from Current,
	// except the commits it wants (OriginalBase..Current)
	// now includes commits OriginalBase..NewBase,
	// which will fail for obvious reasons.
	//
	// To catch this, if OriginalBase is reachable from NewBase,
	// we'll change the commit range to NewBase..Current.
	// This will turn the rebase into a no-op, but it'll correctly update state.
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
		switch s.restackMethod {
		case RestackMethodRebase:
			if err := s.wt.Rebase(ctx, git.RebaseRequest{
				Branch:    req.Branch,
				Upstream:  string(fromHash),
				Onto:      ontoHash.String(),
				Autostash: true,
				Quiet:     true, // TODO: if verbose, disable this
			}); err != nil {
				return fmt.Errorf("rebase: %w", err)
			}

		case RestackMethodMerge:
			if err := s.ensureCheckedOut(ctx, req.Branch); err != nil {
				return err
			}

			// The fromHash narrowing above matters only for rebase:
			// if ontoHash is already reachable from the branch,
			// the merge is a no-op
			// and the state update below still applies.
			if err := s.wt.Merge(ctx, git.MergeRequest{
				Commit:         ontoHash.String(),
				Message:        fmt.Sprintf("Merge branch '%v' into %v", req.Onto, req.Branch),
				NoEdit:         true,
				Autostash:      true,
				Rerere:         true,
				StrategyOption: s.mergeAutoResolve.StrategyOption(),
				Quiet:          true,
			}); err != nil {
				return fmt.Errorf("merge: %w", err)
			}

		default:
			must.Failf("unknown restack method: %v", s.restackMethod)
		}
	}

	if err := branchTx.Commit(ctx, fmt.Sprintf("%v: onto %v", req.Branch, req.Onto)); err != nil {
		return fmt.Errorf("update state: %w", err)
	}

	return nil
}
