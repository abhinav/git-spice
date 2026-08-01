package spice

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
)

// branchBaseInfo relates a tracked branch to the current head of its base.
// Commits in the range Upstream..HEAD are owned by the branch.
//
// # How this affects restacking
//
// During a restack operation, BaseHead is the rebase destination,
// and MergeBase describes the current graph.
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
// then replays the owned commits (Upstream..HEAD) onto BaseHead.
//
// However, if a branch is retargeted without rebasing (e.g. branch onto),
// things differ more.
//
//	A new-base (BaseHead=A, MergeBase=A)
//	 \
//	  B old-base
//	   \
//	    P branch (Upstream=B, UpstreamDescendsFromBase=true)
//
// Although BaseHead is the merge base, a later restack must still replay
// Upstream..HEAD to remove the old downstack commit B.
type branchBaseInfo struct {
	repo interface {
		IsAncestor(ctx context.Context, a, b git.Hash) bool
	} // required

	// BaseHead is the current head of the tracked base branch
	// and the destination of a rebase.
	BaseHead git.Hash

	// MergeBase is the current common ancestor of BaseHead and the branch head.
	// A different BaseHead means the branch is not currently restacked.
	MergeBase git.Hash

	// Upstream is the exclusive lower boundary of the commits owned
	// by the branch.
	// It begins at the recorded base hash and may move to the merge base
	// or fork point when the recorded hash is stale.
	Upstream git.Hash

	// UpstreamDescendsFromBase reports that BaseHead is an ancestor of Upstream.
	// A retarget without rebase uses this relationship
	// to leave replay work for a later restack.
	UpstreamDescendsFromBase bool
}

// branchBaseInfo finds the commits owned by a tracked branch.
// It reconciles the recorded boundary with the current Git graph
// without changing git-spice state.
func (s *Service) branchBaseInfo(
	ctx context.Context,
	name string,
	branch *LookupBranchResponse,
) (branchBaseInfo, error) {
	baseHead, err := s.repo.PeelToCommit(ctx, branch.Base)
	if err != nil {
		if errors.Is(err, git.ErrNotExist) {
			return branchBaseInfo{}, fmt.Errorf("base branch %v does not exist", branch.Base)
		}
		return branchBaseInfo{}, fmt.Errorf("find commit for %v: %w", branch.Base, err)
	}

	mergeBase, err := s.repo.MergeBase(ctx, baseHead.String(), branch.Head.String())
	if err != nil {
		return branchBaseInfo{}, fmt.Errorf(
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
	// If branch.BaseHash is A and mergeBase is B, advancing
	// Upstream to B prevents base commit B from being treated as a
	// branch commit.
	if upstream != mergeBase && s.repo.IsAncestor(ctx, upstream, mergeBase) {
		if mergeBase != branch.Head {
			s.log.Debug("Recorded base hash is out of date. Using merge base as ownership boundary.",
				"base", branch.Base,
				"branch", name,
				"mergeBase", mergeBase)
			upstream = mergeBase
		} else {
			// MergeBase == branch.Head has two different meanings.
			//
			// The branch may have been reset to a commit already on the base:
			//
			//	A---B---C base (BaseHead=C)
			//	    |
			//	    +--- branch (Head=B, MergeBase=B)
			//
			// ForkPoint is B, so Upstream advances to B. The branch is
			// empty but still needs restacking onto C.
			//
			// The base may instead contain the branch through a merge side
			// parent:
			//
			//	A---B---M base (BaseHead=M)
			//	 \     /
			//	  P---Q branch (Head=Q, MergeBase=Q)
			//
			// ForkPoint remains A, so Upstream remains A. Commits P and
			// Q remain owned by the branch until it is restacked.
			forkPoint, err := s.repo.ForkPoint(ctx, branch.Base, name)
			if err == nil && upstream != forkPoint {
				s.log.Debug("Recorded base hash is out of date. Using fork point as ownership boundary.",
					"base", branch.Base,
					"branch", name,
					"forkPoint", forkPoint)
				upstream = forkPoint
			}
		}
	}

	// A retarget without rebase deliberately records a reachable boundary
	// newer than the merge base with the new base branch:
	//
	//	A new-base (BaseHead=A)
	//	 \
	//	  B---P branch (branch.Head=P, Upstream=B,
	//	                UpstreamDescendsFromBase=true)
	//
	// If mergeBase is A and branch.BaseHash is B, preserving
	// Upstream B makes a later restack replay only P onto A and remove
	// old downstack commit B.
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
				s.log.Debug("Recorded base hash is out of date. Using fork point as ownership boundary.",
					"base", branch.Base,
					"branch", name,
					"forkPoint", forkPoint)
			}
			upstream = forkPoint
		}
	}

	return branchBaseInfo{
		repo:                     s.repo,
		BaseHead:                 baseHead,
		MergeBase:                mergeBase,
		Upstream:                 upstream,
		UpstreamDescendsFromBase: upstreamDescendsFromBase,
	}, nil
}

func (r *branchBaseInfo) IsRestacked() bool {
	return r.MergeBase == r.BaseHead && !r.UpstreamDescendsFromBase
}

// ReplayBoundary returns the start of the range of commits
// that should be replayed onto dest.
//
// While branchCommitRange owns Upstream..HEAD,
// when replaying onto dest, not all commits might need to be replayed.
func (r *branchBaseInfo) ReplayBoundary(ctx context.Context, dest git.Hash) git.Hash {
	// Upstream refers to start of range of commits for a branch,
	// and destination to commit we're replaying commits on top of.
	//
	// If upstream is reachable by destination,
	// trying to replay upstream..HEAD will result in conflicts.
	// So replay range is now destination..HEAD.
	if r.repo.IsAncestor(ctx, r.Upstream, dest) {
		return dest
	}
	return r.Upstream
}
