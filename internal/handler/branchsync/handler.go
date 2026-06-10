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
	Pull(ctx context.Context, opts git.PullOptions) error
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

	// ActionBehind means the remote is behind the local branch; the
	// branch will need to be pushed.
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

// Sync syncs a single tracked branch. Returns the result of the sync.
// The caller is responsible for restacking any children of the branch
// when ToHash != FromHash.
func (h *Handler) Sync(ctx context.Context, branch string) (*SyncResult, error) {
	log := h.Log.With("branch", branch)

	if branch == h.Store.Trunk() {
		return &SyncResult{Branch: branch, Action: ActionSkipped, SkipReason: "trunk is synced by 'gs repo sync'"}, nil
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

	// Truly diverged. v1 doesn't pull in this case.
	res.Action = ActionDiverged
	return res, nil
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
