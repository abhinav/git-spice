package submit

import (
	"context"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/sliceutil"
)

// remoteDecision categorizes the relationship between the remote upstream
// branch and the commit git-spice is about to push to it.
//
// It is the basis for deciding whether a force-push is safe:
// a force-push must never discard commits git-spice did not push itself.
type remoteDecision int

const (
	// decisionSafe means the remote branch is exactly where git-spice last
	// left it, or does not exist yet. Force-pushing cannot destroy commits
	// we did not push.
	decisionSafe remoteDecision = iota

	// decisionFastForward means the local branch already contains every
	// commit on the remote, so pushing is an ordinary fast-forward that
	// needs neither --force nor a lease.
	decisionFastForward

	// decisionUpToDate means the remote already points at the commit we
	// want to push; there is nothing to do.
	decisionUpToDate

	// decisionNoBaseline means git-spice has no record of what it last
	// pushed and the remote differs from the local branch,
	// so it cannot prove the push is safe.
	decisionNoBaseline

	// decisionAdvanced means the remote gained commits on top of our last
	// push: lastPushed..remoteHead is a non-empty, well-formed range.
	// Force-pushing would discard those commits.
	decisionAdvanced

	// decisionDiverged means the remote was rewritten away from our last
	// push: lastPushed is not an ancestor of remoteHead.
	// We cannot identify what a force-push would discard.
	decisionDiverged
)

// classifyRemote decides whether it is safe to force-push local
// over the remote upstream branch.
//
//   - lastPushed is the commit git-spice last recorded pushing to the
//     upstream branch, or the zero hash if it was never recorded.
//   - remoteHead is the upstream branch's current head, or the zero hash
//     if the branch does not exist on the remote.
//   - local is the commit we want to push.
//
// isAncestor reports whether a is an ancestor of b.
//
// The lease baseline is lastPushed, not the local remote-tracking ref:
// a tracking ref advances on every 'git fetch', which would silently make
// a --force-with-lease pass even after someone else pushed. lastPushed only
// changes when git-spice itself pushes, so it survives restacks (which only
// rewrite local hashes) while still catching commits added by others.
func classifyRemote(
	lastPushed, remoteHead, local git.Hash,
	isAncestor func(a, b git.Hash) bool,
) remoteDecision {
	switch {
	case remoteHead.IsZero():
		return decisionSafe
	case remoteHead == local:
		return decisionUpToDate
	case isAncestor(remoteHead, local):
		// Local already contains every remote commit, so pushing only
		// adds commits on top: an ordinary fast-forward. This is safe
		// even without a recorded baseline, and git itself rejects it
		// if the remote moves out from under us before the push lands.
		return decisionFastForward
	case lastPushed.IsZero():
		return decisionNoBaseline
	case remoteHead == lastPushed:
		return decisionSafe
	case isAncestor(lastPushed, remoteHead):
		return decisionAdvanced
	default:
		return decisionDiverged
	}
}

// ensureSafePush verifies that force-pushing local to the remote upstream
// branch will not discard commits git-spice did not push.
//
// It returns the --force-with-lease value to use when pushing
// (empty if no lease is needed, e.g. a first push), or a typed error
// describing why the push must not proceed. On a blocking decision, it logs
// actionable guidance before returning the error.
//
// remoteHead is the upstream branch's current head, or the zero hash if the
// branch does not exist on the remote. lastPushed is the commit git-spice
// last recorded pushing to the upstream branch (zero if never recorded).
func (h *Handler) ensureSafePush(
	ctx context.Context,
	log *silog.Logger,
	branch, remoteName, upstreamBranch string,
	lastPushed, remoteHead, local git.Hash,
) (lease string, err error) {
	// Classifying the push relies on comparing commits with merge-base,
	// so the remote head must exist locally. The forge hands us its hash
	// but not the object, so fetch it when it isn't already present
	// (e.g. someone else pushed since our last fetch).
	if !remoteHead.IsZero() && remoteHead != local {
		if _, err := h.Repository.PeelToCommit(ctx, remoteHead.String()); err != nil {
			if ferr := h.Repository.Fetch(ctx, git.FetchOptions{
				Remote:   remoteName,
				Refspecs: []git.Refspec{git.Refspec(upstreamBranch)},
			}); ferr != nil {
				log.Warn("Could not fetch remote branch", "error", ferr)
			}
		}
	}

	switch classifyRemote(lastPushed, remoteHead, local, func(a, b git.Hash) bool {
		return h.Repository.IsAncestor(ctx, a, b)
	}) {
	case decisionSafe:
		// A first push (remoteHead absent) needs no lease.
		// Otherwise the remote sits exactly at our last push, so lease
		// against it: the push is rejected if anyone moved it since.
		if remoteHead.IsZero() {
			return "", nil
		}
		return upstreamBranch + ":" + lastPushed.String(), nil

	case decisionFastForward, decisionUpToDate:
		return "", nil

	case decisionNoBaseline:
		log.Errorf("%v: Not pushing: no record of what git-spice last pushed to %v/%v, and it differs from your branch.",
			branch, remoteName, upstreamBranch)
		log.Errorf("Run 'gs branch sync' to adopt the remote as a baseline, or use --force to overwrite it.")
		return "", &RemoteNoBaselineError{
			Branch:         branch,
			Remote:         remoteName,
			UpstreamBranch: upstreamBranch,
			RemoteHash:     remoteHead,
			LocalHash:      local,
		}

	case decisionAdvanced:
		commits := h.foreignCommits(ctx, log, lastPushed, remoteHead)
		log.Errorf("%v: Not pushing: %v/%v has commits that are not in your branch:",
			branch, remoteName, upstreamBranch)
		for _, c := range commits {
			log.Errorf("  %v %v (%v)", c.ShortHash, c.Subject, c.AuthorEmail)
		}
		log.Errorf("Someone may have pushed to this branch.")
		log.Errorf("Run 'gs branch sync' to bring those commits into your branch, then submit again.")
		log.Errorf("To overwrite them anyway, use --force.")
		return "", &RemoteAdvancedError{
			Branch:         branch,
			Remote:         remoteName,
			UpstreamBranch: upstreamBranch,
			Commits:        commits,
		}

	default: // decisionDiverged
		log.Errorf("%v: Not pushing: %v/%v was rewritten and no longer contains your last push (%v).",
			branch, remoteName, upstreamBranch, lastPushed.Short())
		log.Errorf("It now points at %v. Inspect the remote branch; use --force to overwrite it.",
			remoteHead.Short())
		return "", &RemoteDivergedError{
			Branch:         branch,
			Remote:         remoteName,
			UpstreamBranch: upstreamBranch,
			LastPushed:     lastPushed,
			RemoteHash:     remoteHead,
		}
	}
}

// foreignCommits lists the commits present on the remote upstream branch
// (lastPushed..remoteHead) that are not in the local branch, newest first.
//
// ensureSafePush has already fetched the remote head, so the commits are
// available locally. Failures are logged and yield a nil slice: the caller
// still blocks the push, it just can't enumerate the offending commits.
func (h *Handler) foreignCommits(
	ctx context.Context,
	log *silog.Logger,
	lastPushed, remoteHead git.Hash,
) []git.CommitDetail {
	commits, err := sliceutil.CollectErr(h.Repository.ListCommitsDetails(ctx,
		git.CommitRangeFrom(remoteHead).ExcludeFrom(lastPushed)))
	if err != nil {
		log.Warn("Could not list new remote commits", "error", err)
		return nil
	}
	return commits
}
