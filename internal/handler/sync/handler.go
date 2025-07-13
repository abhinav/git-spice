// Package sync implements synchronization commands.
package sync

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"runtime"
	"sort"
	"sync"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/graph"
	branchdel "go.abhg.dev/gs/internal/handler/delete"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/ui"
)

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
	Checkout(ctx context.Context, name string) error
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
	LoadBranches(ctx context.Context) ([]spice.LoadBranchItem, error)
	ListAbove(ctx context.Context, name string) ([]string, error)
}

var _ Service = (*spice.Service)(nil)

// DeleteHandler allows deleting branches.
type DeleteHandler interface {
	DeleteBranches(context.Context, *branchdel.Request) error
}

// RestackHandler allows restacking the current stack.
type RestackHandler interface {
	RestackStack(ctx context.Context, branch string) error
}

// Handler implements syncing commands.
type Handler struct {
	Log        *silog.Logger  // required
	View       ui.View        // required
	Repository GitRepository  // required
	Worktree   GitWorktree    // required
	Store      Store          // required
	Service    Service        // required
	Delete     DeleteHandler  // required
	Restack    RestackHandler // required

	Remote string // required
	// RemoteRepository is set only if remote refers to a supported forge.
	RemoteRepository forge.Repository // optional
}

// TrunkOptions are options for the SyncTrunk command.
type TrunkOptions struct {
	// TODO: flag to not delete merged branches?

	Restack bool `help:"Restack the current stack after syncing"`
}

// SyncTrunk syncs the trunk branch with the remote repository,
// updating the local branch if necessary.
//
// It also detects other tracked branches that have been merged upstream
// and deletes them. (TODO: this should not be separated out.)
func (h *Handler) SyncTrunk(ctx context.Context, opts *TrunkOptions) error {
	log := h.Log
	opts = cmp.Or(opts, &TrunkOptions{})
	currentBranch, err := h.Worktree.CurrentBranch(ctx)
	if err != nil {
		if !errors.Is(err, git.ErrDetachedHead) {
			return fmt.Errorf("get current branch: %w", err)
		}
		currentBranch = "" // detached head
	}

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
		var worktreePath string // non-empty if checked out in another worktree
		for branch, err := range h.Repository.LocalBranches(ctx, nil) {
			if err != nil {
				return fmt.Errorf("list branches: %w", err)
			}

			if branch.Name == trunk && branch.Worktree != "" {
				worktreePath = branch.Worktree
				break
			}
		}

		if worktreePath != "" {
			// (1c): Trunk is not the current branch,
			// but it is checked out in another worktree.
			// Re-run this command in that worktree.
			log.Debug("Trunk is checked out in another worktree: syncing that worktree instead", "worktree", worktreePath)
			trunkWT, err := h.Repository.OpenWorktree(ctx, worktreePath)
			if err != nil {
				return fmt.Errorf("open worktree %q: %w", worktreePath, err)
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

				if err := h.Worktree.Checkout(ctx, trunk); err != nil {
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

				if err := h.Worktree.Checkout(ctx, "-"); err != nil {
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

	candidates, err := h.Service.LoadBranches(ctx)
	if err != nil {
		return fmt.Errorf("list tracked branches: %w", err)
	}

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
			log.Info("All merged branches may not have been deleted. Use 'gs branch delete' to delete them.")
		}()

		branchesToDelete, err = h.findLocalMergedBranches(ctx, candidates, trunkEndHash)
		if err != nil {
			return fmt.Errorf("find merged branches: %w", err)
		}
	} else {
		// Supported forge. Check for merged CRs and upstream branches.
		branchesToDelete, err = h.findForgeFinishedBranches(ctx, candidates)
		if err != nil {
			return fmt.Errorf("find finished CRs: %w", err)
		}
	}

	if err := h.deleteBranches(ctx, branchesToDelete); err != nil {
		return err
	}

	if opts.Restack {
		// current branch may have changed after deletion
		// of merged branches.
		currentBranch, err := h.Worktree.CurrentBranch(ctx)
		if err != nil {
			log.Warn("Failed to get current branch, skipping restack", "error", err)
		} else {
			// TODO: if the merged branch leaves us on trunk
			// --restack will end up restacking all known branches.
			return h.Restack.RestackStack(ctx, currentBranch)
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
	knownBranches []spice.LoadBranchItem,
) ([]branchDeletion, error) {
	type submittedBranch struct {
		Name string

		Base            string
		MergedDownstack []json.RawMessage

		Change forge.ChangeID
		State  forge.ChangeState

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
	// 1. Branches that we submitted PRs for with `gs branch submit`.
	// 2. Branches that the user submitted PRs for manually
	//    with 'gh pr create' or similar.
	//
	// For the first, we can perform a cheap API call to check the CR status.
	// For the second, we need to find recently merged PRs with that branch
	// name, and match the remote head SHA to the branch head SHA.
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
		wg.Add(1)
		go func() {
			defer wg.Done()

			changeIDs := make([]forge.ChangeID, len(submittedBranches))
			for i, b := range submittedBranches {
				changeIDs[i] = b.Change
			}

			states, err := h.RemoteRepository.ChangesStates(ctx, changeIDs)
			if err != nil {
				h.Log.Error("Failed to query CR status", "error", err)
				return
			}

			for i, state := range states {
				submittedBranches[i].State = state
			}
		}()
	}

	if len(trackedBranches) > 0 {
		trackedch := make(chan *trackedBranch)
		for range min(runtime.GOMAXPROCS(0), len(trackedBranches)) {
			wg.Add(1)
			go func() {
				defer wg.Done()

				for b := range trackedch {
					changes, err := h.RemoteRepository.FindChangesByBranch(ctx, b.Name, forge.FindChangesOptions{
						Limit: 3,
						State: forge.ChangeMerged,
					})
					if err != nil {
						h.Log.Error("Failed to list changes", "branch", b.Name, "error", err)
						continue
					}

					for _, c := range changes {
						if c.State != forge.ChangeMerged {
							continue
						}

						localSHA, err := h.Repository.PeelToCommit(ctx, b.Name)
						if err != nil {
							h.Log.Error("Failed to resolve local head SHA", "branch", b.Name, "error", err)
							continue
						}

						b.Merged = true
						b.Change = c.ID
						b.RemoteHeadSHA = c.HeadHash
						b.LocalHeadSHA = localSHA
					}

				}
			}()
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
			if !ui.Interactive(h.View) {
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
			h.Log.Infof("%v: %v was merged", branch.Name, branch.Change)
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

		if branch.RemoteHeadSHA == branch.LocalHeadSHA {
			h.Log.Infof("%v: %v was merged", branch.Name, branch.Change)
			finishedBranches[branch.Name] = finished
			continue
		}

		mismatchMsg := fmt.Sprintf("%v was merged but local SHA (%v) does not match remote SHA (%v)",
			branch.Change, branch.LocalHeadSHA.Short(), branch.RemoteHeadSHA.Short())

		// If the remote head SHA doesn't match the local head SHA,
		// there may be local commits that haven't been pushed yet.
		// Prompt for deletion if we have the option of prompting.
		if !ui.Interactive(h.View) {
			h.Log.Warnf("%v: %v. Skipping...", branch.Name, mismatchMsg)
			continue
		}

		var shouldDelete bool
		prompt := ui.NewConfirm().
			WithTitle(fmt.Sprintf("Delete %v?", branch.Name)).
			WithDescription(mismatchMsg).
			WithValue(&shouldDelete)
		if err := ui.Run(h.View, prompt); err != nil {
			h.Log.Warn("Skipping branch", "branch", branch.Name, "error", err)
			continue
		}

		if shouldDelete {
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
	topoBranches := graph.Toposort(mergedBranchNames,
		func(name string) (string, bool) {
			base := finishedBranches[name].Base
			// Ordering matters only if the base was also merged.
			baseBranch, ok := finishedBranches[base]
			return base, ok && baseBranch.Merged
		})

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

		aboves, err := h.Service.ListAbove(ctx, name)
		if err != nil {
			h.Log.Warn("Unable to query merged branch's upstacks. Not propagating to merge history.",
				"branch", name, "error", err)
			continue
		}

		changeIDJSON, err := h.RemoteRepository.Forge().MarshalChangeID(branch.ChangeID)
		if err != nil {
			h.Log.Warn("Unable to serialize ChangeID for merged branch. Not propagating to merge history.",
				"branch", name, "changeID", branch.ChangeID, "error", err)
			continue
		}

		for _, above := range aboves {
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

type branchDeletion struct {
	BranchName   string
	UpstreamName string
}

func (h *Handler) deleteBranches(ctx context.Context, branchesToDelete []branchDeletion) error {
	if len(branchesToDelete) == 0 {
		return nil
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
			h.Log.Warn("Run 'gs branch delete' or run 'gs repo sync' again from that worktree to delete it.")
			continue
		}

		deleteBranchNames = append(deleteBranchNames, branchInfo.Name)
	}

	err := h.Delete.DeleteBranches(ctx, &branchdel.Request{
		Branches: deleteBranchNames,
		Force:    true,
	})
	if err != nil {
		return fmt.Errorf("delete merged branches: %w", err)
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

	return nil
}
