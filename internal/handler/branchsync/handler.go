// Package branchsync implements the per-branch sync operation used by
// 'gs branch sync' and friends. It pulls remote-side commits added to a
// tracked branch since the last push -- typically by a CI bot like
// autofix-ci, license-headers, or codeowners.
//
// The v1 surface is fast-forward only: a branch is updated if its remote
// is strictly ahead of the last-pushed hash AND the local branch hasn't
// moved past that hash. Diverged branches are reported and skipped; the
// rebase strategy is reserved for a follow-up commit.
package branchsync

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
)

// GitRepository is the subset of git.Repository this handler uses.
type GitRepository interface {
	PeelToCommit(ctx context.Context, ref string) (git.Hash, error)
	IsAncestor(ctx context.Context, a, b git.Hash) bool
	Fetch(ctx context.Context, opts git.FetchOptions) error
	SetRef(ctx context.Context, req git.SetRefRequest) error
}

var _ GitRepository = (*git.Repository)(nil)

// GitWorktree is the subset of git.Worktree this handler uses.
type GitWorktree interface {
	CurrentBranch(ctx context.Context) (string, error)
	CheckoutBranch(ctx context.Context, branch string) error
	Pull(ctx context.Context, opts git.PullOptions) error
	Reset(ctx context.Context, commit string, opts git.ResetOptions) error
	Rebase(ctx context.Context, req git.RebaseRequest) error
}

var _ GitWorktree = (*git.Worktree)(nil)

// Store is the subset of state.Store this handler uses.
type Store interface {
	LookupBranch(ctx context.Context, name string) (*state.LookupResponse, error)
	BeginBranchTx() *state.BranchTx
	Trunk() string
}

// Handler implements per-branch sync.
type Handler struct {
	Log        *silog.Logger // required
	Repository GitRepository // required
	Worktree   GitWorktree   // required
	Store      Store         // required

	// Remote is the name of the remote to fetch from (e.g., "origin").
	Remote string // required
}

// Action reports what 'gs branch sync' did to a branch.
type Action int

const (
	// ActionClean means the branch was already in sync.
	ActionClean Action = iota

	// ActionFastForward means the branch was fast-forwarded to its remote.
	ActionFastForward

	// ActionRebased means local commits were replayed on top of the
	// remote-side commits brought in from the upstream.
	ActionRebased

	// ActionBehind means the remote has no commits to integrate: it sits
	// at (or behind) our last push. The local branch is ahead of, or has
	// only moved away from, a stale remote and simply needs to be pushed.
	ActionBehind

	// ActionDiverged means both local and remote have new commits since
	// the last push. Skipped under fast-forward policy.
	ActionDiverged

	// ActionSkipped means the branch was skipped for a reason that
	// merits a warning (no upstream, no LastPushedHash recorded, etc.).
	ActionSkipped
)

// SyncResult reports the outcome of syncing a single branch.
type SyncResult struct {
	Branch string
	Action Action

	// FromHash is the local tip before the sync.
	FromHash git.Hash

	// ToHash is the local tip after the sync. Equal to FromHash when no
	// update was applied.
	ToHash git.Hash

	// SkipReason is set when Action is ActionSkipped.
	SkipReason string
}

// ErrNoUpstream is returned when the branch has no upstream configured
// and therefore cannot be synced.
var ErrNoUpstream = errors.New("branch has no upstream configured")

// Mode controls how diverged branches are handled.
type Mode int

const (
	// ModeFastForward is the default: fast-forward when safe; skip when
	// the branch has diverged from its remote.
	ModeFastForward Mode = iota

	// ModeRebase replays remote-side commits on top of the local branch
	// when both sides have new commits since the last push. Conflicts
	// surface as an interrupted rebase, recoverable with
	// 'gs rebase --continue'.
	ModeRebase
)

// SyncRequest configures a single-branch sync.
type SyncRequest struct {
	Branch string
	Mode   Mode

	// RecordPushed, when set, makes Sync skip all fetching and rebasing and
	// only record the given hash as the branch's last-pushed baseline.
	//
	// It is used to finish a '--rebase' sync that was interrupted by a
	// conflict: once the user resolves it and runs 'gs rebase continue', the
	// remote commits are integrated, but the baseline still needs to be
	// recorded. Re-running the full sync would try to rebase again, so the
	// continuation records the baseline directly instead.
	RecordPushed git.Hash
}

// Sync syncs a single tracked branch according to the request. Returns
// the result of the sync. The caller is responsible for restacking any
// children of the branch when ToHash != FromHash.
func (h *Handler) Sync(ctx context.Context, req SyncRequest) (*SyncResult, error) {
	branch := req.Branch
	log := h.Log.With("branch", branch)

	if branch == h.Store.Trunk() {
		return &SyncResult{Branch: branch, Action: ActionSkipped, SkipReason: "trunk is synced by 'gs repo sync'"}, nil
	}

	// Finish an interrupted '--rebase' sync: just record the baseline.
	if !req.RecordPushed.IsZero() {
		lookup, err := h.Store.LookupBranch(ctx, branch)
		if err != nil {
			return nil, fmt.Errorf("lookup branch: %w", err)
		}
		if lookup.UpstreamBranch == "" {
			return nil, ErrNoUpstream
		}
		if err := h.recordPushedHash(ctx, branch, lookup.UpstreamBranch, req.RecordPushed); err != nil {
			return nil, err
		}
		return &SyncResult{
			Branch:   branch,
			Action:   ActionRebased,
			FromHash: req.RecordPushed,
			ToHash:   req.RecordPushed,
		}, nil
	}

	lookup, err := h.Store.LookupBranch(ctx, branch)
	if err != nil {
		return nil, fmt.Errorf("lookup branch: %w", err)
	}

	if lookup.UpstreamBranch == "" {
		return nil, ErrNoUpstream
	}

	// Fetch the remote ref so origin/<X> is up to date.
	refspec := git.Refspec(h.Remote + "/" + lookup.UpstreamBranch)
	if err := h.Repository.Fetch(ctx, git.FetchOptions{
		Remote:   h.Remote,
		Refspecs: []git.Refspec{git.Refspec(lookup.UpstreamBranch)},
	}); err != nil {
		return nil, fmt.Errorf("fetch %v: %w", refspec, err)
	}

	localHash, err := h.Repository.PeelToCommit(ctx, branch)
	if err != nil {
		return nil, fmt.Errorf("resolve local %v: %w", branch, err)
	}

	remoteRef := h.Remote + "/" + lookup.UpstreamBranch
	remoteHash, err := h.Repository.PeelToCommit(ctx, remoteRef)
	if err != nil {
		return nil, fmt.Errorf("resolve %v: %w", remoteRef, err)
	}

	res := &SyncResult{Branch: branch, FromHash: localHash, ToHash: localHash}

	if localHash == remoteHash {
		res.Action = ActionClean
		return res, nil
	}

	// Local is an ancestor of remote -> can fast-forward.
	if h.Repository.IsAncestor(ctx, localHash, remoteHash) {
		if err := h.fastForward(ctx, branch, lookup.UpstreamBranch, localHash, remoteHash); err != nil {
			return nil, err
		}
		if err := h.recordPushedHash(ctx, branch, lookup.UpstreamBranch, remoteHash); err != nil {
			log.Warn("Could not record pushed hash", "error", err)
		}
		res.Action = ActionFastForward
		res.ToHash = remoteHash
		return res, nil
	}

	// Remote is an ancestor of local -> nothing to pull; we owe a push.
	if h.Repository.IsAncestor(ctx, remoteHash, localHash) {
		res.Action = ActionBehind
		return res, nil
	}

	// Local and remote have diverged in raw Git terms: neither tip is an
	// ancestor of the other. Use the last-pushed hash as a baseline to
	// tell apart two very different situations that look identical here:
	//
	//   - The remote is stale and only our local branch moved (e.g. it
	//     was restacked onto a newer trunk). There is nothing to pull;
	//     the branch owes a push.
	//   - The remote genuinely gained commits since our last push (e.g. a
	//     CI bot). Those are the commits we want to integrate.
	//
	// The remote-side commits are the range pHash..remote. If that range
	// is empty, the remote has gained nothing and the divergence is purely
	// local.
	pHash := lookup.UpstreamLastPushedHash
	remoteAhead := pHash != "" && pHash != remoteHash &&
		h.Repository.IsAncestor(ctx, pHash, remoteHash)

	if !remoteAhead {
		switch {
		case pHash != "" && pHash == remoteHash:
			// Remote sits exactly at our last push: it gained nothing.
			// The local branch moved on its own and just owes a push.
			res.Action = ActionBehind
		case pHash == "":
			// No baseline recorded, so we can't identify what to pull.
			res.Action = ActionSkipped
			res.SkipReason = "no last-pushed hash recorded; push the branch once to establish a baseline"
		default:
			// Remote was rewritten away from our baseline.
			res.Action = ActionDiverged
		}
		return res, nil
	}

	// pHash..remote is a non-empty, well-formed range: the remote has real
	// commits to integrate. Bringing them in means replaying them onto the
	// local tip, which is a rebase, not a fast-forward.
	if req.Mode != ModeRebase {
		res.Action = ActionDiverged
		return res, nil
	}

	// Replay pHash..remote onto the local tip via 'git rebase --onto'.
	//
	// We deliberately do NOT require pHash to be an ancestor of local: a
	// restacked branch drops pHash from its history, yet rebase --onto
	// only needs pHash..remote to be a valid range, which remoteAhead
	// already guarantees. This is what lets remote-side commits survive
	// even after the branch has been restacked locally.
	if err := h.rebase(ctx, branch, lookup.UpstreamBranch, localHash, remoteHash, pHash); err != nil {
		// The rebase was interrupted (typically a conflict). Surface the
		// integration target so the caller can record it as the baseline
		// once the user resolves the conflict and resumes.
		res.ToHash = remoteHash
		return res, err
	}
	if err := h.recordPushedHash(ctx, branch, lookup.UpstreamBranch, remoteHash); err != nil {
		log.Warn("Could not record pushed hash", "error", err)
	}
	// Recompute the post-rebase tip.
	newHash, err := h.Repository.PeelToCommit(ctx, branch)
	if err != nil {
		return nil, fmt.Errorf("resolve post-rebase %v: %w", branch, err)
	}
	res.Action = ActionRebased
	res.ToHash = newHash
	return res, nil
}

// rebase replays the remote-only commits (p..R) onto the local tip (L)
// by checking out the branch, resetting it to R, then running
// git rebase --onto L p. Conflicts surface as a normal interrupted
// rebase; the caller can resume with 'gs rebase --continue'.
func (h *Handler) rebase(ctx context.Context, branch, upstream string, localHash, remoteHash, lastPushed git.Hash) error {
	_ = upstream // currently unused; reserved for future logging

	currentBranch, err := h.Worktree.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("current branch: %w", err)
	}
	if currentBranch != branch {
		if err := h.Worktree.CheckoutBranch(ctx, branch); err != nil {
			return fmt.Errorf("checkout %v: %w", branch, err)
		}
	}

	// Move HEAD to R so the rebase sees p..R as the commit range.
	if err := h.Worktree.Reset(ctx, remoteHash.String(), git.ResetOptions{Mode: git.ResetHard}); err != nil {
		return fmt.Errorf("reset %v to remote: %w", branch, err)
	}

	// Replay p..R (which is now HEAD since HEAD = R) onto L.
	if err := h.Worktree.Rebase(ctx, git.RebaseRequest{
		Branch:    branch,
		Upstream:  lastPushed.String(),
		Onto:      localHash.String(),
		Autostash: true,
	}); err != nil {
		return fmt.Errorf("rebase remote commits onto local: %w", err)
	}

	return nil
}

// fastForward updates a branch ref to a new (descendant) hash. For a
// non-current branch this is a plain ref update; for the currently
// checked-out branch we use 'git pull --rebase --autostash' to bring
// HEAD and the working tree along (a no-commits-to-replay rebase
// degenerates into a fast-forward).
func (h *Handler) fastForward(ctx context.Context, branch, upstream string, oldHash, newHash git.Hash) error {
	currentBranch, err := h.Worktree.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("current branch: %w", err)
	}
	if currentBranch == branch {
		return h.Worktree.Pull(ctx, git.PullOptions{
			Remote:    h.Remote,
			Rebase:    true,
			Autostash: true,
			Refspec:   git.Refspec(upstream),
		})
	}

	return h.Repository.SetRef(ctx, git.SetRefRequest{
		Ref:     "refs/heads/" + branch,
		Hash:    newHash,
		OldHash: oldHash,
	})
}

func (h *Handler) recordPushedHash(ctx context.Context, branch, upstream string, hash git.Hash) error {
	tx := h.Store.BeginBranchTx()
	if err := tx.Upsert(ctx, state.UpsertRequest{
		Name:                   branch,
		UpstreamBranch:         &upstream,
		UpstreamLastPushedHash: &hash,
	}); err != nil {
		return fmt.Errorf("upsert: %w", err)
	}
	return tx.Commit(ctx, "branch sync "+branch+": record fast-forward")
}
