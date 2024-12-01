package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"sync"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type repoSyncCmd struct {
	// TODO: flag to not delete merged branches?
	Restack bool `help:"Restack the current stack after syncing"`
}

func (*repoSyncCmd) Help() string {
	return text.Dedent(`
		Branches with merged Change Requests
		will be deleted after syncing.

		The repository must have a remote associated for syncing.
		A prompt will ask for one if the repository
		was not initialized with a remote.
	`)
}

func (cmd *repoSyncCmd) Run(
	ctx context.Context,
	secretStash secret.Stash,
	log *log.Logger,
	view ui.View,
) error {
	repo, store, svc, err := openRepo(ctx, log, view)
	if err != nil {
		return err
	}

	remote, err := ensureRemote(ctx, repo, store, log, view)
	// TODO: move ensure remote to Service
	if err != nil {
		return err
	}

	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		if !errors.Is(err, git.ErrDetachedHead) {
			return fmt.Errorf("get current branch: %w", err)
		}
		currentBranch = "" // detached head
	}

	trunk := store.Trunk()
	trunkStartHash, err := repo.PeelToCommit(ctx, trunk)
	if err != nil {
		return fmt.Errorf("peel to trunk: %w", err)
	}

	// TODO: This is pretty messy. Refactor.

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
		opts := git.PullOptions{
			Remote:    remote,
			Rebase:    true,
			Autostash: true,
			Refspec:   git.Refspec(trunk),
		}
		if err := repo.Pull(ctx, opts); err != nil {
			return fmt.Errorf("pull: %w", err)
		}
	} else {
		localBranches, err := repo.LocalBranches(ctx)
		if err != nil {
			return fmt.Errorf("list branches: %w", err)
		}

		// (1c): Trunk is not the current branch,
		// but it is checked out in another worktree.
		// Reject sync because we don't want to update the ref
		// and mess up the other worktree.
		trunkCheckedOut := slices.ContainsFunc(localBranches,
			func(b git.LocalBranch) bool {
				return b.Name == trunk && b.CheckedOut
			})
		if trunkCheckedOut {
			// TODO:
			// restack should support working off $remote/$trunk
			// so we can still git fetch
			// and continue working on the branch
			// without updating the local trunk ref.
			return errors.New("trunk cannot be updated: " +
				"it is checked out in another worktree")
		}
		// Rest of this block is (1b): Trunk is not the current branch.

		trunkHash, err := repo.PeelToCommit(ctx, trunk)
		if err != nil {
			return fmt.Errorf("peel to trunk: %w", err)
		}

		remoteHash, err := repo.PeelToCommit(ctx, remote+"/"+trunk)
		if err != nil {
			return fmt.Errorf("resolve remote trunk: %w", err)
		}

		if repo.IsAncestor(ctx, trunkHash, remoteHash) {
			// (2a): Trunk is at or behind the remote.
			// Fetch and upate the local trunk ref.
			log.Debug("trunk is at or behind remote: fetching changes")
			opts := git.FetchOptions{
				Remote: remote,
				Refspecs: []git.Refspec{
					git.Refspec(trunk + ":" + trunk),
				},
			}
			if err := repo.Fetch(ctx, opts); err != nil {
				return fmt.Errorf("fetch: %w", err)
			}
		} else {
			// (2b): Trunk has unpushed local commits
			// but also (1b) trunk is not checked out anywhere,
			// so we can check out trunk and rebase.
			log.Debug("trunk has unpushed commits: pulling from remote")

			if err := repo.Checkout(ctx, trunk); err != nil {
				return fmt.Errorf("checkout trunk: %w", err)
			}

			opts := git.PullOptions{
				Remote:  remote,
				Rebase:  true,
				Refspec: git.Refspec(trunk),
			}
			if err := repo.Pull(ctx, opts); err != nil {
				return fmt.Errorf("pull: %w", err)
			}

			if err := repo.Checkout(ctx, "-"); err != nil {
				return fmt.Errorf("checkout old branch: %w", err)
			}

			// TODO: With a recent enough git,
			// we can attempt to replay those commits
			// without checking out trunk.
			// https://git-scm.com/docs/git-replay/2.44.0
		}
	}

	trunkEndHash, err := repo.PeelToCommit(ctx, trunk)
	if err != nil {
		return fmt.Errorf("peel to trunk: %w", err)
	}

	if trunkStartHash == trunkEndHash {
		log.Infof("%v: already up-to-date", trunk)
	} else if repo.IsAncestor(ctx, trunkStartHash, trunkEndHash) {
		// CountCommits only if IsAncestor is true
		// because there may have been a force push.
		count, err := repo.CountCommits(ctx,
			git.CommitRangeFrom(trunkEndHash).ExcludeFrom(trunkStartHash))
		if err != nil {
			log.Warn("Failed to count commits", "error", err)
		} else {
			log.Infof("%v: pulled %v new commit(s)", trunk, count)
		}
	}

	candidates, err := svc.LoadBranches(ctx)
	if err != nil {
		return fmt.Errorf("list tracked branches: %w", err)
	}

	var branchesToDelete []branchDeletion
	if remoteRepo, err := openRemoteRepositorySilent(ctx, secretStash, repo, remote); err != nil {
		var unsupported *unsupportedForgeError
		if !errors.As(err, &unsupported) {
			return err
		}

		// Unsupported forge.
		// Find merged branches by checking what's reachable from trunk.
		defer func() {
			// Less log noise if all known branches were merged.
			if len(branchesToDelete) == len(candidates) {
				return
			}

			log.Infof("Unsupported remote %q (%v)", unsupported.Remote, unsupported.RemoteURL)
			log.Info("All merged branches may not have been deleted. Use 'gs branch delete' to delete them.")
		}()

		branchesToDelete, err = cmd.findLocalMergedBranches(ctx, log, repo, candidates, trunkEndHash)
		if err != nil {
			return fmt.Errorf("find merged branches: %w", err)
		}
	} else {
		// Supported forge. Check for merged CRs and upstream branches.
		branchesToDelete, err = cmd.findForgeMergedBranches(ctx, log, repo, remoteRepo, candidates, view)
		if err != nil {
			return fmt.Errorf("find merged CRs: %w", err)
		}
	}
	if err := cmd.deleteBranches(ctx, view, log, repo, remote, branchesToDelete); err != nil {
		return err
	}

	if cmd.Restack {
		return (&stackRestackCmd{}).Run(ctx, log, view)
	}

	return nil
}

// findLocalMergedBranches finds branches that have been merged
// by inspecting what's reachable from the trunk.
//
// This will only work for merges and fast-forwards.
// Squash or rebase merges will need to be handled manually by the user.
func (cmd *repoSyncCmd) findLocalMergedBranches(
	ctx context.Context,
	log *log.Logger,
	repo *git.Repository,
	knownBranches []spice.LoadBranchItem,
	trunkHash git.Hash,
) ([]branchDeletion, error) {
	// Find branches that have been merged by checking
	// if they are reachable from the trunk.
	var branchesToDelete []branchDeletion
	for _, b := range knownBranches {
		if repo.IsAncestor(ctx, b.Head, trunkHash) {
			log.Infof("%v was merged", b.Name)
			branchesToDelete = append(branchesToDelete, branchDeletion{
				BranchName:   b.Name,
				UpstreamName: b.UpstreamBranch,
			})
		}
	}

	return branchesToDelete, nil
}

func (cmd *repoSyncCmd) findForgeMergedBranches(
	ctx context.Context,
	log *log.Logger,
	repo *git.Repository,
	remoteRepo forge.Repository,
	knownBranches []spice.LoadBranchItem,
	view ui.View,
) ([]branchDeletion, error) {
	type submittedBranch struct {
		Name   string
		Change forge.ChangeID
		Merged bool

		// Branch name pushed to the remote.
		UpstreamBranch string
	}

	type trackedBranch struct {
		Name string

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
				Name:           b.Name,
				Change:         b.Change.ChangeID(),
				UpstreamBranch: upstreamBranch,
			}
			submittedBranches = append(submittedBranches, b)
		} else {
			// TODO:
			// Filter down to only branches that have
			// a remote tracking branch:
			// either $remote/$UpstreamBranch or $remote/$branch exists.
			b := &trackedBranch{
				Name:           b.Name,
				UpstreamBranch: upstreamBranch,
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

			merged, err := remoteRepo.ChangesAreMerged(ctx, changeIDs)
			if err != nil {
				log.Error("Failed to query CR status", "error", err)
				return
			}

			for i, m := range merged {
				submittedBranches[i].Merged = m
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
					changes, err := remoteRepo.FindChangesByBranch(ctx, b.Name, forge.FindChangesOptions{
						Limit: 3,
						State: forge.ChangeMerged,
					})
					if err != nil {
						log.Error("Failed to list changes", "branch", b.Name, "error", err)
						continue
					}

					for _, c := range changes {
						if c.State != forge.ChangeMerged {
							continue
						}

						localSHA, err := repo.PeelToCommit(ctx, b.Name)
						if err != nil {
							log.Error("Failed to resolve local head SHA", "branch", b.Name, "error", err)
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

	var branchesToDelete []branchDeletion
	for _, branch := range submittedBranches {
		if !branch.Merged {
			continue
		}

		log.Infof("%v: %v was merged", branch.Name, branch.Change)
		branchesToDelete = append(branchesToDelete, branchDeletion{
			BranchName:   branch.Name,
			UpstreamName: branch.UpstreamBranch,
		})
	}

	for _, branch := range trackedBranches {
		if !branch.Merged {
			continue
		}

		if branch.RemoteHeadSHA == branch.LocalHeadSHA {
			log.Infof("%v: %v was merged", branch.Name, branch.Change)
			branchesToDelete = append(branchesToDelete, branchDeletion{
				BranchName:   branch.Name,
				UpstreamName: branch.UpstreamBranch,
			})
			continue
		}

		mismatchMsg := fmt.Sprintf("%v was merged but local SHA (%v) does not match remote SHA (%v)",
			branch.Change, branch.LocalHeadSHA.Short(), branch.RemoteHeadSHA.Short())

		// If the remote head SHA doesn't match the local head SHA,
		// there may be local commits that haven't been pushed yet.
		// Prompt for deletion if we have the option of prompting.
		if !ui.Interactive(view) {
			log.Warnf("%v: %v. Skipping...", branch.Name, mismatchMsg)
			continue
		}

		var shouldDelete bool
		prompt := ui.NewConfirm().
			WithTitle(fmt.Sprintf("Delete %v?", branch.Name)).
			WithDescription(mismatchMsg).
			WithValue(&shouldDelete)
		if err := ui.Run(view, prompt); err != nil {
			log.Warn("Skipping branch", "branch", branch.Name, "error", err)
			continue
		}

		if shouldDelete {
			branchesToDelete = append(branchesToDelete, branchDeletion{
				BranchName:   branch.Name,
				UpstreamName: branch.UpstreamBranch,
			})
		}
	}

	return branchesToDelete, nil
}

type branchDeletion struct {
	BranchName   string
	UpstreamName string
}

func (cmd *repoSyncCmd) deleteBranches(
	ctx context.Context,
	view ui.View,
	log *log.Logger,
	repo *git.Repository,
	remote string,
	branchesToDelete []branchDeletion,
) error {
	if len(branchesToDelete) == 0 {
		return nil
	}

	branchNames := make([]string, len(branchesToDelete))
	for i, b := range branchesToDelete {
		branchNames[i] = b.BranchName
	}

	err := (&branchDeleteCmd{
		Branches:              branchNames,
		Force:                 true,
		KeepChangeIDsAsMerged: true,
	}).Run(ctx, log, view)
	if err != nil {
		return fmt.Errorf("delete merged branches: %w", err)
	}

	// Also delete the remote tracking branch for this branch
	// if it still exists.
	for _, branch := range branchesToDelete {
		remoteBranch := remote + "/" + branch.UpstreamName
		if _, err := repo.PeelToCommit(ctx, remoteBranch); err == nil {
			if err := repo.DeleteBranch(ctx, remoteBranch, git.BranchDeleteOptions{
				Remote: true,
			}); err != nil {
				log.Warn("Unable to delete remote tracking branch", "branch", remoteBranch, "error", err)
			}
		}
	}

	return nil
}
