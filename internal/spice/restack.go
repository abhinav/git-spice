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

	commitRange, err := s.branchBaseInfo(ctx, name, b)
	if err != nil {
		return nil, err
	}
	if commitRange.IsRestacked() {
		s.reconcileRecordedBaseHash(ctx, name, b, commitRange.BaseHead)
		return nil, ErrAlreadyRestacked
	}

	// The base can already contain some or all branch-owned commits after a
	// Git operation outside git-spice:
	//
	//	A---B---M base (BaseHead=M)
	//	 \     /
	//	  P---Q branch (MergeBase=Q, Upstream=A)
	//
	// Using the base as the exclusive replay boundary selects BaseHead..Q,
	// which excludes every commit reachable from the base. In this example,
	// the range is empty, so Git moves the branch to M without replaying P or Q.
	//
	// When the base does not contain Upstream, retaining it preserves
	// retarget-without-rebase behavior:
	//
	//	A base (BaseHead=A)
	//	 \
	//	  B---P branch (MergeBase=A, Upstream=B,
	//	                UpstreamDescendsFromBase=true)
	//
	// Replaying B..P onto A drops the old downstack commit B.
	replayBoundary := commitRange.ReplayBoundary(ctx, commitRange.BaseHead)
	if err := s.wt.Rebase(ctx, git.RebaseRequest{
		Onto:      commitRange.BaseHead.String(),
		Upstream:  replayBoundary.String(),
		Branch:    name,
		Autostash: true,
		Quiet:     true,
	}); err != nil {
		return nil, fmt.Errorf("rebase: %w", err)
	}

	tx := s.store.BeginBranchTx()
	if err := tx.Upsert(ctx, state.UpsertRequest{
		Name:     name,
		BaseHash: commitRange.BaseHead,
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

	// Upstream is the exclusive boundary of the branch's commits
	// after reconciling the recorded state with the Git graph.
	Upstream git.Hash
}

func (e *BranchNeedsRestackError) Error() string {
	return fmt.Sprintf("branch needs to be restacked on top of %v", e.Base)
}

// VerifyRestacked is a version of CheckRestacked
// that ignores the returned base branch hash.
func (s *Service) VerifyRestacked(ctx context.Context, name string) error {
	_, err := s.CheckRestacked(ctx, name)
	return err
}

// CheckRestacked verifies that the given branch is on top of its base branch.
// It reconciles the recorded base hash with the Git graph,
// including when the branch still needs to be restacked.
//
// It returns the actual hash of the base branch on success,
// [BranchNeedsRestackError] if the branch needs to be restacked,
// [state.ErrNotExist] if the branch is not tracked.
// Any other error indicates a problem with checking the branch.
func (s *Service) CheckRestacked(ctx context.Context, name string) (baseHash git.Hash, err error) {
	b, err := s.LookupBranch(ctx, name)
	if err != nil {
		return git.ZeroHash, err
	}

	commitRange, err := s.branchBaseInfo(ctx, name, b)
	if err != nil {
		return git.ZeroHash, err
	}

	if !commitRange.IsRestacked() {
		s.reconcileRecordedBaseHash(ctx, name, b, commitRange.Upstream)
		return git.ZeroHash, &BranchNeedsRestackError{
			Base:     b.Base,
			BaseHash: commitRange.BaseHead,
			Upstream: commitRange.Upstream,
		}
	}

	s.reconcileRecordedBaseHash(ctx, name, b, commitRange.BaseHead)
	return commitRange.BaseHead, nil
}

// reconcileRecordedBaseHash persists an ownership boundary recovered from Git.
// State failures are logged because they do not change the graph-derived
// restack status.
func (s *Service) reconcileRecordedBaseHash(
	ctx context.Context,
	name string,
	b *LookupBranchResponse,
	baseHash git.Hash,
) {
	if b.BaseHash == baseHash {
		return
	}

	s.log.Debug("Updating recorded base hash", "branch", name, "base", b.Base)

	tx := s.store.BeginBranchTx()
	if err := tx.Upsert(ctx, state.UpsertRequest{
		Name:     name,
		BaseHash: baseHash,
	}); err != nil {
		s.log.Warn("Failed to update recorded base hash", "error", err)
		return
	}

	if err := tx.Commit(ctx, fmt.Sprintf("%v: branch was restacked externally", name)); err != nil {
		s.log.Warn("Failed to update state", "error", err)
	}
}
