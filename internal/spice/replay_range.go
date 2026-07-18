package spice

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
)

// branchReplayRange relates a tracked branch to the current head of its base.
// BaseHead is the rebase destination, MergeBase describes the current graph,
// and Upstream is the exclusive boundary of the commits to replay.
//
// A branch already restacked on its base has all three hashes at B:
//
//	A---B base (BaseHead=B, MergeBase=B, Upstream=B)
//	     \
//	      P branch
//
// After the base advances, or after an external rebase followed by a base
// advance, BaseHead moves beyond the branch's current merge base:
//
//	A---B---C base (BaseHead=C)
//	     \
//	      P branch (MergeBase=B, Upstream=B)
//
// Restack detects this state because MergeBase differs from BaseHead,
// then replays Upstream..branch-head onto BaseHead.
//
// A retarget without rebase is the exception to that equality check:
//
//	A new-base (BaseHead=A, MergeBase=A)
//	 \
//	  B---P branch (Upstream=B, UpstreamDescendsFromBase=true)
//
// Although BaseHead is the merge base, a later restack must still replay
// Upstream..branch-head to remove the old downstack commit B.
type branchReplayRange struct {
	// BaseHead is the current head of the tracked base branch
	// and the destination of a rebase.
	BaseHead git.Hash

	// MergeBase is the current common ancestor of BaseHead and the branch head.
	// A different BaseHead means the branch is not currently restacked.
	MergeBase git.Hash

	// Upstream is the exclusive lower boundary of the commits to replay.
	// It begins at the recorded base hash and may move to the merge base
	// or fork point when the recorded hash is stale.
	Upstream git.Hash

	// UpstreamDescendsFromBase reports that BaseHead is an ancestor of Upstream.
	// A retarget without rebase uses this relationship to leave replay work
	// for a later restack.
	UpstreamDescendsFromBase bool
}

func (r branchReplayRange) isRestacked() bool {
	return r.MergeBase == r.BaseHead && !r.UpstreamDescendsFromBase
}

// resolveBranchReplayRange finds the commits owned by a tracked branch.
// It reconciles the recorded boundary with the current Git graph
// without changing git-spice state.
func (s *Service) resolveBranchReplayRange(
	ctx context.Context,
	name string,
	branch *LookupBranchResponse,
) (branchReplayRange, error) {
	baseHead, err := s.repo.PeelToCommit(ctx, branch.Base)
	if err != nil {
		if errors.Is(err, git.ErrNotExist) {
			return branchReplayRange{}, fmt.Errorf("base branch %v does not exist", branch.Base)
		}
		return branchReplayRange{}, fmt.Errorf("find commit for %v: %w", branch.Base, err)
	}

	mergeBase, err := s.repo.MergeBase(ctx, baseHead.String(), branch.Head.String())
	if err != nil {
		return branchReplayRange{}, fmt.Errorf(
			"find merge base of %q and %q: %w",
			branch.Base,
			name,
			err,
		)
	}

	upstream := branch.BaseHash

	// An ordinary base advance keeps the recorded boundary
	// because the branch still diverges there:
	//
	//	A---B base (BaseHead=B)
	//	 \
	//	  P branch (branch.Head=P)
	//
	// branch.BaseHash and mergeBase are both A, so Upstream remains A
	// and the branch needs to be restacked onto BaseHead B.
	//
	// A branch rebased outside git-spice can instead have a merge base newer
	// than its recorded boundary:
	//
	//	A---B---C base (BaseHead=C)
	//	     \
	//	      P branch (branch.Head=P)
	//
	// If branch.BaseHash is A and mergeBase is B, advancing Upstream to B
	// prevents base commit B from being treated as a branch commit.
	if upstream != mergeBase && s.repo.IsAncestor(ctx, upstream, mergeBase) {
		s.log.Debug("Recorded base hash is out of date. Using merge base as replay boundary.",
			"base", branch.Base,
			"branch", name,
			"mergeBase", mergeBase)
		upstream = mergeBase
	}

	// A retarget without rebase deliberately records a reachable boundary
	// newer than the merge base with the new base branch:
	//
	//	A new-base (BaseHead=A)
	//	 \
	//	  B---P branch (branch.Head=P)
	//
	// If mergeBase is A and branch.BaseHash is B, preserving Upstream B makes
	// a later restack replay only P onto A and remove old downstack commit B.
	//
	// A rewritten history can instead leave branch.BaseHash disconnected from
	// branch.Head even when Git's reflogs retain the former fork point:
	//
	//	R                 branch.BaseHash, no longer in branch history
	//
	//	F---P---H branch (branch.Head=H)
	//	 \
	//	  ...---B base (BaseHead=B)
	//
	// In that case, forkPoint F is the best available Upstream.
	upstreamDescendsFromBase := upstream != baseHead &&
		!upstream.IsZero() &&
		s.repo.IsAncestor(ctx, baseHead, upstream) &&
		s.repo.IsAncestor(ctx, upstream, branch.Head)

	if !s.repo.IsAncestor(ctx, upstream, branch.Head) {
		forkPoint, err := s.repo.ForkPoint(ctx, branch.Base, name)
		if err == nil {
			if upstream != forkPoint {
				s.log.Debug("Recorded base hash is out of date. Restacking from fork point.",
					"base", branch.Base,
					"branch", name,
					"forkPoint", forkPoint)
			}
			upstream = forkPoint
		}
	}

	return branchReplayRange{
		BaseHead:                 baseHead,
		MergeBase:                mergeBase,
		Upstream:                 upstream,
		UpstreamDescendsFromBase: upstreamDescendsFromBase,
	}, nil
}
