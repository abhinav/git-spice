// Package sync implements synchronization commands.
package sync

import (
	"cmp"
	"context"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"maps"
	"runtime"
	"slices"
	"sort"
	"sync"

	"go.abhg.dev/gs/internal/cli"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/graph"
	"go.abhg.dev/gs/internal/handler/autostash"
	branchdel "go.abhg.dev/gs/internal/handler/delete"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/ui"
)

//go:generate mockgen -package sync -typed -destination mocks_test.go . GitRepository,GitWorktree,Store,Service,DeleteHandler,RestackHandler,AutostashHandler

// GitRepository provides access to tree-less Git operations.
type GitRepository interface {
	PeelToCommit(ctx context.Context, name string) (git.Hash, error)
	LocalBranches(ctx context.Context, opts *git.LocalBranchesOptions) iter.Seq2[git.LocalBranch, error]
	OpenWorktree(ctx context.Context, dir string) (*git.Worktree, error) // TODO: GitWorktree
	IsAncestor(ctx context.Context, ancestor, descendant git.Hash) bool
	Fetch(ctx context.Context, opts git.FetchOptions) error
	CountCommits(ctx context.Context, commitRange git.CommitRange) (int, error)
	DeleteBranch(ctx context.Context, name string, opts git.BranchDeleteOptions) error // TODO:specialize to delete remote branch?
	RemoteURL(ctx context.Context, remote string) (string, error)
}

var _ GitRepository = (*git.Repository)(nil)

// GitWorktree provides access to Git operations specific to a worktree.
type GitWorktree interface {
	CurrentBranch(ctx context.Context) (string, error)
	Pull(ctx context.Context, opts git.PullOptions) error
	CheckoutBranch(ctx context.Context, name string) error
	RootDir() string
}

var _ GitWorktree = (*git.Worktree)(nil)

// Store provides read/write access to the state h.Store.
type Store interface {
	Trunk() string
	BeginBranchTx() *state.BranchTx
}

var _ Store = (*state.Store)(nil)

// Service is a subset of the spice.Service interface.
type Service interface {
	BranchGraph(ctx context.Context, opts *spice.BranchGraphOptions) (*spice.BranchGraph, error)
}

var _ Service = (*spice.Service)(nil)

// DeleteHandler allows deleting branches.
type DeleteHandler interface {
	DeleteBranches(context.Context, *branchdel.Request) error
}

// RestackHandler allows restacking branches after sync
// has removed their downstack branches.
type RestackHandler interface {
	RestackBranch(ctx context.Context, req *restack.BranchRequest) error
	RestackUpstack(ctx context.Context, req *restack.UpstackRequest) error
}

// AutostashHandler is a subset of the autostash.Handler interface.
type AutostashHandler interface {
	BeginAutostash(ctx context.Context, opts *autostash.Options) (func(*error, *autostash.CleanupOptions), error)
}

var _ AutostashHandler = (*autostash.Handler)(nil)

// Handler implements syncing commands.
type Handler struct {
	Log        *silog.Logger    // required
	View       ui.View          // required
	Repository GitRepository    // required
	Worktree   GitWorktree      // required
	Store      Store            // required
	Service    Service          // required
	Delete     DeleteHandler    // required
	Restack    RestackHandler   // required
	Autostash  AutostashHandler // required

	Remote string // required
	// RemoteRepository is set only if remote refers to a supported forge.
	RemoteRepository forge.Repository // optional
	// PushRepository identifies the repository that owns pushed branches.
	// If nil, pushed branches are expected to live in RemoteRepository.
	PushRepository forge.RepositoryID // optional
}

// ClosedChanges specifies how to handle closed Change Requests.
type ClosedChanges int

const (
	// ClosedChangesAsk prompts the user whether to delete the branch.
	// This is the default.
	ClosedChangesAsk ClosedChanges = iota

	// ClosedChangesIgnore ignores closed CRs without prompting
	// and leaves the branch intact.
	ClosedChangesIgnore
)

var (
	_ encoding.TextUnmarshaler = (*ClosedChanges)(nil)
	_ encoding.TextMarshaler   = (*ClosedChanges)(nil)
)

// UnmarshalText decodes a ClosedChanges from text.
// It supports "ask" and "ignore" values.
func (c *ClosedChanges) UnmarshalText(bs []byte) error {
	switch string(bs) {
	case "ask":
		*c = ClosedChangesAsk
	case "ignore":
		*c = ClosedChangesIgnore
	default:
		return fmt.Errorf("invalid value %q: expected 'ask' or 'ignore'", bs)
	}
	return nil
}

// MarshalText encodes a ClosedChanges to text.
func (c ClosedChanges) MarshalText() ([]byte, error) {
	switch c {
	case ClosedChangesAsk:
		return []byte("ask"), nil
	case ClosedChangesIgnore:
		return []byte("ignore"), nil
	default:
		return nil, fmt.Errorf("invalid value: %d", int(c))
	}
}

func (c ClosedChanges) String() string {
	switch c {
	case ClosedChangesAsk:
		return "ask"
	case ClosedChangesIgnore:
		return "ignore"
	default:
		return fmt.Sprintf("ClosedChanges(%d)", int(c))
	}
}

// TrunkOptions are options for the SyncTrunk command.
type TrunkOptions struct {
	// TODO: flag to not delete merged branches?

	Restack       spice.RestackMode `default:"none" config:"repoSync.restack" enum:"none,aboves,upstack" help:"How to restack branches above deleted branches. One of 'none', 'aboves', and 'upstack'."`
	ClosedChanges ClosedChanges     `default:"ask" config:"repoSync.closedChanges" enum:"ask,ignore" help:"How to handle closed change requests. One of 'ask' and 'ignore'." hidden:""`
}

// SyncTrunk syncs the trunk branch with the remote repository,
// updating the local branch if necessary.
//
// It also detects other tracked branches that have been merged upstream
// and deletes them. (TODO: this should not be separated out.)
func (h *Handler) SyncTrunk(ctx context.Context, opts *TrunkOptions) (retErr error) {
	log := h.Log
	opts = cmp.Or(opts, &TrunkOptions{})
	currentBranch, err := h.Worktree.CurrentBranch(ctx)
	if err != nil {
		if !errors.Is(err, git.ErrDetachedHead) {
			return fmt.Errorf("get current branch: %w", err)
		}
		currentBranch = "" // detached head
	}

	autostashRescueBranch := currentBranch
	var autostashCleanup func(*error, *autostash.CleanupOptions)
	defer func() {
		if autostashCleanup != nil {
			autostashCleanup(&retErr, &autostash.CleanupOptions{
				RescueBranch: autostashRescueBranch,
			})
		}
	}()

	// Begin autostash only when sync is about to disturb
	// the current worktree.
	//
	// Call this immediately before operations
	// that may fail because of dirty changes,
	// or that may rebase, delete, or check out branches
	// in this worktree.
	//
	// Once started, the deferred cleanup restores
	// the stashed changes on success,
	// or schedules them for rescue on failure.
	beginAutostash := sync.OnceValue(func() error {
		var err error
		autostashCleanup, err = h.Autostash.BeginAutostash(ctx, &autostash.Options{
			Message:   "git-spice: autostash before sync",
			ResetMode: autostash.ResetHard,
		})
		return err
	})

	var trunkCheckedOutElsewhere bool
	trunk := h.Store.Trunk()
	trunkStartHash, err := h.Repository.PeelToCommit(ctx, trunk)
	if err != nil {
		return fmt.Errorf("peel to trunk: %w", err)
	}

	// TODO: This is pretty messy. Refactor.

	// Runs 'git pull' to update the trunk branch.
	// Used if the repository's current branch is trunk.
	pullTrunk := func(wt GitWorktree) error {
		opts := git.PullOptions{
			Remote:    h.Remote,
			Rebase:    true,
			Autostash: true,
			Refspec:   git.Refspec(trunk),
		}
		if err := wt.Pull(ctx, opts); err != nil {
			return fmt.Errorf("pull: %w", err)
		}
		return nil
	}

	// There's a mix of scenarios here:
	//
	// 1. Check out status:
	//    a. trunk is the current branch; or
	//    b. trunk is not the current branch; or
	//    c. trunk is not the current branch,
	//       but is checked out in another worktree
	// 2. Sync status:
	//    a. trunk is at or behind the remote; or
	//    b. trunk has unpushed local commits
	if currentBranch == trunk {
		// (1a): Trunk is the current branch.
		// Sync status doesn't matter,
		// git pull --rebase will handle everything.
		log.Debug("trunk is checked out: pulling changes")
		if err := pullTrunk(h.Worktree); err != nil {
			return fmt.Errorf("update trunk: %w", err)
		}
	} else {
		var trunkWorktreePath string // non-empty if checked out in another worktree
		for branch, err := range h.Repository.LocalBranches(ctx, nil) {
			if err != nil {
				return fmt.Errorf("list branches: %w", err)
			}

			if branch.Name == trunk && branch.Worktree != "" {
				trunkWorktreePath = branch.Worktree
				break
			}
		}

		if trunkWorktreePath != "" {
			trunkCheckedOutElsewhere = true
			// (1c): Trunk is not the current branch,
			// but it is checked out in another worktree.
			// Re-run this command in that worktree.
			log.Debug("Trunk is checked out in another worktree: syncing that worktree instead", "worktree", trunkWorktreePath)
			trunkWT, err := h.Repository.OpenWorktree(ctx, trunkWorktreePath)
			if err != nil {
				return fmt.Errorf("open worktree %q: %w", trunkWorktreePath, err)
			}

			if err := pullTrunk(trunkWT); err != nil {
				return fmt.Errorf("update trunk in worktree: %w", err)
			}
		} else {
			// (1b): Trunk is not the current branch,
			// and it is not checked out in another worktree.

			trunkHash, err := h.Repository.PeelToCommit(ctx, trunk)
			if err != nil {
				return fmt.Errorf("peel to trunk: %w", err)
			}

			remoteHash, err := h.Repository.PeelToCommit(ctx, h.Remote+"/"+trunk)
			if err != nil {
				return fmt.Errorf("resolve remote trunk: %w", err)
			}

			if h.Repository.IsAncestor(ctx, trunkHash, remoteHash) {
				// (2a): Trunk is at or behind the remote.
				// Fetch and upate the local trunk ref.
				log.Debug("trunk is at or behind remote: fetching changes")
				opts := git.FetchOptions{
					Remote: h.Remote,
					Refspecs: []git.Refspec{
						git.Refspec(trunk + ":" + trunk),
					},
				}
				if err := h.Repository.Fetch(ctx, opts); err != nil {
					return fmt.Errorf("fetch: %w", err)
				}
			} else {
				// (2b): Trunk has unpushed local commits
				// but also (1b) trunk is not checked out anywhere,
				// so we can check out trunk and rebase.
				log.Debug("trunk has unpushed commits: pulling from remote")

				if err := beginAutostash(); err != nil {
					return err
				}

				if err := h.Worktree.CheckoutBranch(ctx, trunk); err != nil {
					return fmt.Errorf("checkout trunk: %w", err)
				}

				opts := git.PullOptions{
					Remote:  h.Remote,
					Rebase:  true,
					Refspec: git.Refspec(trunk),
				}
				if err := h.Worktree.Pull(ctx, opts); err != nil {
					return fmt.Errorf("pull: %w", err)
				}

				if err := h.Worktree.CheckoutBranch(ctx, "-"); err != nil {
					return fmt.Errorf("checkout old branch: %w", err)
				}

				// TODO: With a recent enough git,
				// we can attempt to replay those commits
				// without checking out trunk.
				// https://git-scm.com/docs/git-replay/2.44.0
			}
		}

	}

	trunkEndHash, err := h.Repository.PeelToCommit(ctx, trunk)
	if err != nil {
		return fmt.Errorf("peel to trunk: %w", err)
	}

	if trunkStartHash == trunkEndHash {
		log.Infof("%v: already up-to-date", trunk)
	} else if h.Repository.IsAncestor(ctx, trunkStartHash, trunkEndHash) {
		// CountCommits only if IsAncestor is true
		// because there may have been a force push.
		count, err := h.Repository.CountCommits(ctx,
			git.CommitRangeFrom(trunkEndHash).ExcludeFrom(trunkStartHash))
		if err != nil {
			log.Warn("Failed to count commits", "error", err)
		} else {
			log.Infof("%v: pulled %v new commit(s)", trunk, count)
		}
	}

	branchGraph, err := h.Service.BranchGraph(ctx, nil)
	if err != nil {
		return fmt.Errorf("get branch graph: %w", err)
	}
	candidates := slices.Collect(branchGraph.All())

	var branchesToDelete []branchDeletion
	if h.RemoteRepository == nil {
		// Unsupported forge.
		// Find merged branches by checking what's reachable from trunk.
		defer func() {
			// Less log noise if all known branches were merged.
			if len(branchesToDelete) == len(candidates) {
				return
			}

			remoteURL, err := h.Repository.RemoteURL(ctx, h.Remote)
			if err != nil {
				remoteURL = "unknown"
			}

			log.Infof("Unsupported remote %q (%v)", h.Remote, remoteURL)
			log.Infof("All merged branches may not have been deleted. Use '%s branch delete' to delete them.", cli.Name())
		}()

		branchesToDelete, err = h.findLocalMergedBranches(ctx, candidates, trunkEndHash)
		if err != nil {
			return fmt.Errorf("find merged branches: %w", err)
		}
	} else {
		// Supported forge. Check for merged CRs and upstream branches.
		branchesToDelete, err = h.findForgeFinishedBranches(ctx, branchGraph, candidates, opts.ClosedChanges)
		if err != nil {
			return fmt.Errorf("find finished CRs: %w", err)
		}
	}

	if len(branchesToDelete) == 0 {
		return nil
	}

	// Build the restack plan before deletion,
	// while the branch graph still contains the soon-to-be-deleted bases.
	// The plan is filtered after deletion
	// because branches checked out in other worktrees are skipped.
	restackPlan := planDeletedBranchRestacks(opts.Restack, branchesToDelete, branchGraph)

	if err := beginAutostash(); err != nil {
		return err
	}

	// If sync deletes the branch we started on,
	// a later rescued operation cannot resume there.
	// Rescue onto the branch sync intends to leave checked out instead:
	// trunk in the usual case,
	// or no branch if trunk is checked out elsewhere.
	// Otherwise, keep rescuing onto the original current branch.
	branchAfterDelete := currentBranch
	deletingCurrentBranch := slices.ContainsFunc(branchesToDelete, func(b branchDeletion) bool {
		return b.BranchName == currentBranch
	})
	if deletingCurrentBranch {
		if trunkCheckedOutElsewhere {
			branchAfterDelete = ""
		} else {
			branchAfterDelete = trunk
		}
	}
	autostashRescueBranch = branchAfterDelete

	deletedBranchNames, err := h.deleteBranches(ctx, branchesToDelete)
	if err != nil {
		return err
	}

	// Retarget surviving upstack PRs on the forge
	// around branches that are finished there.
	if h.RemoteRepository != nil {
		h.retargetUpstackChanges(ctx,
			collectRetargetCandidates(branchesToDelete, branchGraph))
	}

	if targets := restackPlan.targets(deletedBranchNames); len(targets) > 0 {
		for _, target := range targets {
			// Autostash cleanup records the rescue branch
			// if this restack is interrupted.
			// Keep it aligned with the restack target currently in flight,
			// not merely the first target in the plan.
			autostashRescueBranch = target
			if err := h.restackDeletedBranchUpstack(ctx, opts.Restack, target); err != nil {
				return err
			}
		}
		// Successful restacks may leave us on the final target.
		// Return to the branch that sync would have selected after deletion.
		if branchAfterDelete != "" && branchAfterDelete != targets[len(targets)-1] {
			autostashRescueBranch = branchAfterDelete
			if err := h.Worktree.CheckoutBranch(ctx, branchAfterDelete); err != nil {
				return fmt.Errorf("checkout branch %v: %w", branchAfterDelete, err)
			}
		}
	}

	return nil
}

// findLocalMergedBranches finds branches that have been merged
// by inspecting what's reachable from the trunk.
//
// This will only work for merges and fast-forwards.
// Squash or rebase merges will need to be handled manually by the user.
func (h *Handler) findLocalMergedBranches(
	ctx context.Context,
	knownBranches []spice.LoadBranchItem,
	trunkHash git.Hash,
) ([]branchDeletion, error) {
	// Find branches that have been merged by checking
	// if they are reachable from the trunk.
	var branchesToDelete []branchDeletion
	for _, b := range knownBranches {
		if h.Repository.IsAncestor(ctx, b.Head, trunkHash) {
			h.Log.Infof("%v was merged", b.Name)
			branchesToDelete = append(branchesToDelete, branchDeletion{
				BranchName:   b.Name,
				UpstreamName: b.UpstreamBranch,
			})
		}
	}

	return branchesToDelete, nil
}

func (h *Handler) findForgeFinishedBranches(
	ctx context.Context,
	branchGraph *spice.BranchGraph,
	knownBranches []spice.LoadBranchItem,
	closedChangeHandling ClosedChanges,
) ([]branchDeletion, error) {
	type submittedBranch struct {
		Name string

		Base            string
		MergedDownstack []json.RawMessage

		Change forge.ChangeID
		State  forge.ChangeState
		// Head SHA reported by the forge for the change.
		RemoteHeadSHA git.Hash
		LocalHeadSHA  git.Hash

		// Branch name pushed to the remote.
		UpstreamBranch string
	}

	type trackedBranch struct {
		Name string

		Base            string
		MergedDownstack []json.RawMessage

		Change        forge.ChangeID
		Merged        bool
		RemoteHeadSHA git.Hash
		LocalHeadSHA  git.Hash

		// Branch name pushed to the remote.
		UpstreamBranch string
	}

	// There are two kinds of branches under consideration:
	//
	// 1. Branches that we submitted PRs for with `git-spice branch submit`.
	// 2. Branches that the user submitted PRs for manually
	//    with 'gh pr create' or similar.
	//
	// For the first, we can perform a cheap API call to check the CR status.
	// For the second, we need to find open and merged PRs with that branch
	// name. An open PR normally means the branch is still live,
	// but a source branch name may be reused after an older PR was merged.
	// A merged PR whose remote head contains the local head proves that the
	// local branch is safe to delete,
	// so it wins over an open PR for the same branch name.
	//
	// We'll try to do these checks concurrently.

	var (
		submittedBranches []*submittedBranch
		trackedBranches   []*trackedBranch
	)
	for _, b := range knownBranches {
		upstreamBranch := b.UpstreamBranch
		if upstreamBranch == "" {
			upstreamBranch = b.Name
		}

		if b.Change != nil {
			b := &submittedBranch{
				Name:            b.Name,
				Base:            b.Base,
				Change:          b.Change.ChangeID(),
				LocalHeadSHA:    b.Head,
				UpstreamBranch:  upstreamBranch,
				MergedDownstack: b.MergedDownstack,
			}
			submittedBranches = append(submittedBranches, b)
		} else {
			// TODO:
			// Filter down to only branches that have
			// a remote tracking branch:
			// either $remote/$UpstreamBranch or $remote/$branch exists.
			b := &trackedBranch{
				Name:            b.Name,
				Base:            b.Base,
				UpstreamBranch:  upstreamBranch,
				MergedDownstack: b.MergedDownstack,
			}
			trackedBranches = append(trackedBranches, b)
		}
	}

	var wg sync.WaitGroup
	if len(submittedBranches) > 0 {
		wg.Go(func() {
			changeIDs := make([]forge.ChangeID, len(submittedBranches))
			for i, b := range submittedBranches {
				changeIDs[i] = b.Change
			}

			statuses, err := h.RemoteRepository.ChangeStatuses(ctx, changeIDs)
			if err != nil {
				h.Log.Error("Failed to query CR status", "error", err)
				return
			}

			for i, status := range statuses {
				submittedBranches[i].State = status.State
				submittedBranches[i].RemoteHeadSHA = status.HeadHash
			}
		})
	}

	if len(trackedBranches) > 0 {
		trackedch := make(chan *trackedBranch)
		for range min(runtime.GOMAXPROCS(0), len(trackedBranches)) {
			wg.Go(func() {
				for b := range trackedch {
					changes, err := h.RemoteRepository.FindChangesByBranch(ctx, b.Name, forge.FindChangesOptions{
						PushRepository: h.PushRepository,
						Limit:          10,
					})
					if err != nil {
						h.Log.Error("Failed to list changes", "branch", b.Name, "error", err)
						continue
					}

					// Closed-unmerged changes cannot prove that this branch is
					// safe to delete,
					// so avoid resolving local head state unless a live or
					// merged change could affect the sync decision.
					var hasOpenOrMergedChange bool
					for _, c := range changes {
						if c.State == forge.ChangeOpen || c.State == forge.ChangeMerged {
							hasOpenOrMergedChange = true
							break
						}
					}
					if !hasOpenOrMergedChange {
						continue
					}

					// A reused source branch name is ambiguous until we compare
					// the forge's head hashes with the local branch head.
					localSHA, err := h.Repository.PeelToCommit(ctx, b.Name)
					if err != nil {
						h.Log.Error("Failed to resolve local head SHA", "branch", b.Name, "error", err)
						continue
					}

					// Track the best candidates in forge order,
					// but let a proven-safe merged change outrank an open
					// change for the same reused source branch name.
					var (
						openChange       *forge.FindChangeItem
						mergedChange     *forge.FindChangeItem
						safeMergedChange *forge.FindChangeItem
					)
				findSafeMergedChange:
					for _, c := range changes {
						switch c.State {
						case forge.ChangeOpen:
							openChange = cmp.Or(openChange, c)
						case forge.ChangeMerged:
							mergedChange = cmp.Or(mergedChange, c)
							if mergedChangeHeadCheck(ctx, h.Repository,
								localSHA, c.HeadHash) != mergedChangeHeadMismatch {
								safeMergedChange = c
								break findSafeMergedChange
							}
						}
					}

					// Prefer an open change unless a merged change proves
					// the local head was included,
					// preserving the conservative behavior for ambiguous
					// branch-name matches.
					change := openChange
					if safeMergedChange != nil {
						change = safeMergedChange
					} else if openChange == nil {
						change = mergedChange
					}

					if change != nil {
						b.Merged = change.State == forge.ChangeMerged
						b.Change = change.ID
						b.RemoteHeadSHA = change.HeadHash
						b.LocalHeadSHA = localSHA
					}

				}
			})
		}

		for _, b := range trackedBranches {
			trackedch <- b
		}
		close(trackedch)
	}
	wg.Wait()

	type finishedBranch struct {
		Name           string
		Base           string
		UpstreamBranch string
		ChangeID       forge.ChangeID
		Merged         bool // true if merged, false if closed
	}

	finishedBranches := make(map[string]finishedBranch) // name -> branch
	mergedDownstacks := make(map[string][]json.RawMessage)
	for _, branch := range submittedBranches {
		switch branch.State {
		case forge.ChangeOpen:
			continue // not merged yet

		case forge.ChangeClosed:
			if closedChangeHandling == ClosedChangesIgnore {
				h.Log.Infof("%v: %v was closed but not merged, ignoring", branch.Name, branch.Change)
				continue
			} else if !ui.Interactive(h.View) {
				h.Log.Warnf("%v: %v was closed but not merged.", branch.Name, branch.Change)
				continue
			}

			var shouldDelete bool
			prompt := ui.NewConfirm().
				WithTitle(fmt.Sprintf("Delete %v?", branch.Name)).
				WithDescription(fmt.Sprintf("%v was closed but not merged.", branch.Change)).
				WithValue(&shouldDelete)
			if err := ui.Run(h.View, prompt); err != nil {
				h.Log.Warn("Skipping branch", "branch", branch.Name, "error", err)
				continue
			}

			if shouldDelete {
				finishedBranches[branch.Name] = finishedBranch{
					Name:           branch.Name,
					Base:           branch.Base,
					UpstreamBranch: branch.UpstreamBranch,
					ChangeID:       branch.Change,
					Merged:         false, // closed, not merged
				}
				// Note: Don't propagate mergedDownstacks for closed changes
			}

		case forge.ChangeMerged:
			if !h.shouldDeleteMergedChange(ctx,
				branch.Name, branch.Change,
				branch.LocalHeadSHA, branch.RemoteHeadSHA) {
				continue
			}

			finishedBranches[branch.Name] = finishedBranch{
				Name:           branch.Name,
				Base:           branch.Base,
				UpstreamBranch: branch.UpstreamBranch,
				ChangeID:       branch.Change,
				Merged:         true, // merged
			}
			mergedDownstacks[branch.Name] = branch.MergedDownstack

		}
	}

	for _, branch := range trackedBranches {
		if !branch.Merged {
			continue
		}

		finished := finishedBranch{
			Name:           branch.Name,
			Base:           branch.Base,
			UpstreamBranch: branch.UpstreamBranch,
			ChangeID:       branch.Change,
			Merged:         true, // merged
		}
		mergedDownstacks[branch.Name] = branch.MergedDownstack

		if h.shouldDeleteMergedChange(ctx,
			branch.Name, branch.Change,
			branch.LocalHeadSHA, branch.RemoteHeadSHA) {
			finishedBranches[branch.Name] = finished
		}
	}

	if len(finishedBranches) == 0 {
		return nil, nil
	}

	// Sort the merged branches in topological order: trunk to upstacks.
	// This will be used to propagate merged branch information.
	mergedBranchNames := make([]string, 0, len(finishedBranches))
	for name, branch := range finishedBranches {
		// Only consider merged branches for propagation, not closed ones.
		if branch.Merged {
			mergedBranchNames = append(mergedBranchNames, name)
		}
	}
	sort.Strings(mergedBranchNames)
	topoBranches, err := graph.Toposort(mergedBranchNames,
		func(name string) (string, bool) {
			base := finishedBranches[name].Base
			// Ordering matters only if the base was also merged.
			baseBranch, ok := finishedBranches[base]
			return base, ok && baseBranch.Merged
		})
	if err != nil {
		must.Failf("sort merged branches: %v", err)
	}

	// For each merged branch, bubble up merged downstacks
	// to their direct upstacks.
	//
	// This is done in topological order (branches closer to trunk first)
	// so that if two consecutive branches were merged,
	// both changes are bubbled up.
	for _, name := range topoBranches {
		branch, ok := finishedBranches[name]
		must.Bef(ok, "topologically sorted branch %q must be finished", name)
		must.Bef(branch.Merged, "topologically sorted branch %q must be merged", name)

		changeIDJSON, err := h.RemoteRepository.Forge().MarshalChangeID(branch.ChangeID)
		if err != nil {
			h.Log.Warn("Unable to serialize ChangeID for merged branch. Not propagating to merge history.",
				"branch", name, "changeID", branch.ChangeID, "error", err)
			continue
		}

		for above := range branchGraph.Aboves(name) {
			// MergedDownstack for the upstack of the branch being merged
			// is the branch's own merged downstack and the branch itself.
			var newHistory []json.RawMessage
			newHistory = append(newHistory, mergedDownstacks[name]...)
			newHistory = append(newHistory, changeIDJSON)
			// Combine with anything else already in the merged downstack.
			// (Normally this will be empty.)
			newHistory = append(newHistory, mergedDownstacks[above]...)
			mergedDownstacks[above] = newHistory
		}
	}

	// mergedDownstacks now contains the final merged downstack list
	// for each of the upstack branches. Commit this information.
	branchTx := h.Store.BeginBranchTx()
	for branch, history := range mergedDownstacks {
		// Note: Even branches that are getting merged
		// (and will be deleted) are getting their history updated.
		// This way, if [feat1 -> feat2] are both merged,
		// but feat2 fails to be deleted because of any reason,
		// it still remembers feat1.
		err := branchTx.Upsert(ctx, state.UpsertRequest{
			Name:            branch,
			MergedDownstack: &history,
		})
		if err != nil {
			h.Log.Warnf("%v: unable to update merged downstacks: %v", branch, err)
		}
		delete(mergedDownstacks, branch)
	}
	if err := branchTx.Commit(ctx, "sync: propagate merged branches"); err != nil {
		h.Log.Warn("Unable to propagated merged downstacks", "error", err)
	}

	branchesToDelete := make([]branchDeletion, 0, len(finishedBranches))
	for _, branch := range finishedBranches {
		branchesToDelete = append(branchesToDelete, branchDeletion{
			BranchName:   branch.Name,
			UpstreamName: branch.UpstreamBranch,
		})
	}

	return branchesToDelete, nil
}

func (h *Handler) shouldDeleteMergedChange(
	ctx context.Context,
	branchName string,
	changeID forge.ChangeID,
	localHead, remoteHead git.Hash,
) bool {
	switch mergedChangeHeadCheck(ctx, h.Repository, localHead, remoteHead) {
	case mergedChangeHeadExact:
		h.Log.Infof("%v: %v was merged", branchName, changeID)
		return true

	case mergedChangeHeadRemoteContainsLocal:
		h.Log.Infof("%v: %v was merged; local SHA %v is included in remote SHA %v",
			branchName, changeID, localHead.Short(), remoteHead.Short())
		return true

	case mergedChangeHeadMismatch:
		mismatchMsg := fmt.Sprintf("%v was merged but local SHA (%v) does not match remote SHA (%v)",
			changeID, localHead.Short(), remoteHead.Short())

		// If the remote head SHA doesn't contain the local head SHA,
		// there may be local commits that haven't been pushed yet.
		// Prompt for deletion if we have the option of prompting.
		if !ui.Interactive(h.View) {
			h.Log.Warnf("%v: %v. Skipping...", branchName, mismatchMsg)
			return false
		}

		var shouldDelete bool
		prompt := ui.NewConfirm().
			WithTitle(fmt.Sprintf("Delete %v?", branchName)).
			WithDescription(mismatchMsg).
			WithValue(&shouldDelete)
		if err := ui.Run(h.View, prompt); err != nil {
			h.Log.Warn("Skipping branch", "branch", branchName, "error", err)
			return false
		}

		return shouldDelete

	default:
		must.Bef(false, "unknown merged change head status")
		return false
	}
}

type mergedChangeHeadStatus int

const (
	// The local branch head is the same commit as the forge-reported head.
	mergedChangeHeadExact mergedChangeHeadStatus = iota + 1

	// The forge-reported head contains the local branch head.
	mergedChangeHeadRemoteContainsLocal

	// The forge-reported head does not prove that local commits are safe.
	mergedChangeHeadMismatch
)

func mergedChangeHeadCheck(
	ctx context.Context,
	repo GitRepository,
	localHead, remoteHead git.Hash,
) mergedChangeHeadStatus {
	if localHead == "" || remoteHead == "" {
		return mergedChangeHeadMismatch
	}

	if localHead == remoteHead {
		return mergedChangeHeadExact
	}

	if repo.IsAncestor(ctx, localHead, remoteHead) {
		return mergedChangeHeadRemoteContainsLocal
	}

	return mergedChangeHeadMismatch
}

// branchDeletion describes a local branch that repo sync may delete
// because its upstream change is finished
// or its commits are already reachable from trunk.
type branchDeletion struct {
	// BranchName is the local branch to delete.
	BranchName string

	// UpstreamName is the remote-tracking branch to delete with it,
	// when one exists.
	UpstreamName string
}

// deletedBranchRestackPlan records direct upstacks that survive deletion.
//
// The map is keyed by deletion candidate,
// not by branches that were actually deleted.
// This keeps planning separate from worktree-safety filtering:
// targets applies the delete handler's result before restacking.
type deletedBranchRestackPlan map[string][]string // branch => surviving aboves

func planDeletedBranchRestacks(
	mode spice.RestackMode,
	branchesToDelete []branchDeletion,
	branchGraph *spice.BranchGraph,
) deletedBranchRestackPlan {
	if !mode.Includes(spice.RestackAboves) || len(branchesToDelete) == 0 {
		return nil
	}

	deleted := make(map[string]struct{}, len(branchesToDelete))
	for _, branch := range branchesToDelete {
		deleted[branch.BranchName] = struct{}{}
	}

	plan := make(deletedBranchRestackPlan, len(branchesToDelete))
	for _, branch := range branchesToDelete {
		for above := range branchGraph.Aboves(branch.BranchName) {
			if _, ok := deleted[above]; ok {
				continue
			}
			plan[branch.BranchName] = append(plan[branch.BranchName], above)
		}
	}
	return plan
}

func (p deletedBranchRestackPlan) targets(deletedBranches []string) []string {
	if len(p) == 0 || len(deletedBranches) == 0 {
		return nil
	}

	// Multiple adjacent deletions can point at the same surviving branch.
	// For example, deleting both a and b in a -> b -> c
	// should restack only from c.
	targetSet := make(map[string]struct{})
	for _, branch := range deletedBranches {
		for _, above := range p[branch] {
			targetSet[above] = struct{}{}
		}
	}

	return slices.Sorted(maps.Keys(targetSet))
}

func (h *Handler) restackDeletedBranchUpstack(
	ctx context.Context,
	mode spice.RestackMode,
	target string,
) error {
	switch {
	case mode.Includes(spice.RestackUpstack):
		if err := h.Restack.RestackUpstack(ctx, &restack.UpstackRequest{
			Branch: target,
		}); err != nil {
			return fmt.Errorf("restack upstack %q: %w", target, err)
		}
	case mode.Includes(spice.RestackAboves):
		if err := h.Restack.RestackBranch(ctx, &restack.BranchRequest{
			Branch: target,
		}); err != nil {
			return fmt.Errorf("restack branch %q: %w", target, err)
		}
	case mode.Includes(spice.RestackNone):
		return nil
	default:
		return fmt.Errorf("unknown restack mode: %v", mode)
	}
	return nil
}

func (h *Handler) deleteBranches(ctx context.Context, branchesToDelete []branchDeletion) ([]string, error) {
	if len(branchesToDelete) == 0 {
		return nil, nil
	}

	allBranchNames := make([]string, len(branchesToDelete))
	upstreamByName := make(map[string]string, len(branchesToDelete))
	for i, b := range branchesToDelete {
		allBranchNames[i] = b.BranchName
		if b.UpstreamName != "" {
			upstreamByName[b.BranchName] = b.UpstreamName
		}
	}

	deleteBranchNames := make([]string, 0, len(branchesToDelete))
	for branchInfo, err := range h.Repository.LocalBranches(ctx, &git.LocalBranchesOptions{Patterns: allBranchNames}) {
		if err != nil {
			h.Log.Warn("Failed to list branches", "error", err)
			break
		}

		if branchInfo.Worktree != "" && branchInfo.Worktree != h.Worktree.RootDir() {
			h.Log.Warnf("%v: checked out in another worktree (%v), skipping deletion.", branchInfo.Name, branchInfo.Worktree)
			h.Log.Warnf("Run '%[1]s branch delete' or run '%[1]s repo sync' again from that worktree to delete it.", cli.Name())
			continue
		}

		deleteBranchNames = append(deleteBranchNames, branchInfo.Name)
	}

	err := h.Delete.DeleteBranches(ctx, &branchdel.Request{
		Branches: deleteBranchNames,
		Force:    true,
	})
	if err != nil {
		return nil, fmt.Errorf("delete merged branches: %w", err)
	}

	// Also delete the remote tracking branch for this branch
	// if it still exists.
	for _, branchName := range deleteBranchNames {
		upstreamName, ok := upstreamByName[branchName]
		if !ok {
			continue // no upstream branch, nothing to delete
		}

		remoteBranch := h.Remote + "/" + upstreamName
		if _, err := h.Repository.PeelToCommit(ctx, remoteBranch); err == nil {
			if err := h.Repository.DeleteBranch(ctx, remoteBranch, git.BranchDeleteOptions{
				Remote: true,
			}); err != nil {
				h.Log.Warn("Unable to delete remote tracking branch", "branch", remoteBranch, "error", err)
			}
		}
	}

	return deleteBranchNames, nil
}

// retargetCandidate is a branch whose forge change
// needs retargeting after a sync deletion.
type retargetCandidate struct {
	branch   string
	changeID forge.ChangeID
	newBase  string
}

// collectRetargetCandidates identifies branches
// that need forge retargeting after sync deletion.
//
// It examines pre-deletion branch state to find branches
// that survive deletion but have a base being deleted.
// For each, it resolves the nearest surviving ancestor
// as the new base.
func collectRetargetCandidates(
	deletions []branchDeletion,
	branchGraph *spice.BranchGraph,
) iter.Seq[retargetCandidate] {
	return func(yield func(retargetCandidate) bool) {
		deletedNames := make(map[string]struct{}, len(deletions))
		for _, d := range deletions {
			deletedNames[d.BranchName] = struct{}{}
		}

		for c := range branchGraph.All() {
			if _, deleted := deletedNames[c.Name]; deleted {
				continue
			}
			if c.Change == nil {
				continue
			}
			if _, baseDeleted := deletedNames[c.Base]; !baseDeleted {
				continue
			}

			if !yield(retargetCandidate{
				branch:   c.Name,
				changeID: c.Change.ChangeID(),
				newBase: branchGraph.NextBase(c.Name, func(branch string) bool {
					_, deleted := deletedNames[branch]
					return deleted
				}),
			}) {
				return
			}
		}
	}
}

// retargetUpstackChanges retargets forge changes
// for upstack branches surviving deletion.
func (h *Handler) retargetUpstackChanges(ctx context.Context, candidates iter.Seq[retargetCandidate]) {
	for c := range candidates {
		h.Log.Infof("%s: retargeting %v onto %s...",
			c.branch, c.changeID, c.newBase)
		err := h.RemoteRepository.EditChange(
			ctx, c.changeID,
			forge.EditChangeOptions{Base: c.newBase},
		)
		if err != nil {
			h.Log.Warn("Retarget failed",
				"branch", c.branch, "error", err)
		}
	}
}
