package gitspice

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"sync"

	"github.com/charmbracelet/log"
	"go.abhg.dev/git-spice/internal/git"
	"go.abhg.dev/git-spice/internal/spice"
	"go.abhg.dev/git-spice/internal/text"
	"golang.org/x/oauth2"
)

type repoSyncCmd struct {
	// TODO: flag to not delete merged branches?
	// TODO: flag to auto-restack current stack
}

func (*repoSyncCmd) Help() string {
	return text.Dedent(`
		Pulls the latest changes from the remote repository.
		Deletes branches that have were merged into trunk,
		and updates the base branches of branches upstack from those.
	`)
}

func (*repoSyncCmd) Run(
	ctx context.Context,
	log *log.Logger,
	opts *globalOptions,
	tokenSource oauth2.TokenSource,
) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	store, err := ensureStore(ctx, repo, log, opts)
	if err != nil {
		return err
	}

	svc := spice.NewService(repo, store, log)

	remote, err := ensureRemote(ctx, repo, store, log, opts)
	// TODO: move ensure remote to Service
	if err != nil {
		return err
	}

	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
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
			Remote:  remote,
			Rebase:  true,
			Refspec: git.Refspec(trunk),
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

			if err := repo.Checkout(ctx, currentBranch); err != nil {
				return fmt.Errorf("checkout current branch: %w", err)
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
		return nil
	}

	if repo.IsAncestor(ctx, trunkStartHash, trunkEndHash) {
		count, err := repo.CountCommits(ctx,
			git.CommitRangeFrom(trunkEndHash).ExcludeFrom(trunkStartHash))
		if err != nil {
			log.Warn("Failed to count commits", "error", err)
		} else {
			log.Infof("%v: pulled %v new commit(s)", trunk, count)
		}
	}

	ghrepo, err := ensureGitHubRepo(ctx, log, repo, remote)
	if err != nil {
		return err
	}

	gh, err := newGitHubClient(ctx, tokenSource, opts)
	if err != nil {
		return fmt.Errorf("create GitHub client: %w", err)
	}

	// There are two options for detecting merged branches:
	//
	// 1. Query the PR status for each submitted branch.
	//    This is more accurate, but requires a lot of API calls.
	// 2. List recently merged PRs and match against tracked branches.
	//    The number of API calls here is smaller,
	//    but the matching is less accurate because the PR may have been
	//    submitted by someone else.
	//
	// For now, we'll go for (1) with the assumption that the number of
	// submitted branches is small enough that we can afford the API calls.
	// In the future, we may need a hybrid approach that switches to (2).

	var (
		branches []string
		prs      []int // prs[i] = PR for branches[i]
	)
	{
		tracked, err := svc.LoadBranches(ctx)
		if err != nil {
			return fmt.Errorf("list tracked branches: %w", err)
		}

		for _, b := range tracked {
			if b.PR != 0 {
				branches = append(branches, b.Name)
				prs = append(prs, b.PR)
			}
		}
	}

	if len(branches) == 0 {
		log.Debug("No PRs submitted from tracked branches")
		return nil
	}

	prMerged := make([]bool, len(branches)) // whether prs[i] is merged
	{
		idxc := make(chan int) // PRs to query

		// Spawn up to GOMAXPROCS workers to query PR status.
		var wg sync.WaitGroup
		for range min(runtime.GOMAXPROCS(0), len(branches)) {
			wg.Add(1)
			go func() {
				defer wg.Done()

				for idx := range idxc {
					merged, _, err := gh.PullRequests.IsMerged(ctx, ghrepo.Owner, ghrepo.Name, prs[idx])
					if err != nil {
						log.Error("Failed to query PR status", "pr", prs[idx], "error", err)
						continue
					}

					prMerged[idx] = merged
				}
			}()
		}

		// Feed PRs to workers.
		for i := range branches {
			idxc <- i
		}
		close(idxc) // signal workers to exit

		wg.Wait()
	}

	// TODO:
	// Should the branches be deleted in any particular order?
	// (e.g. from the bottom of the stack up)
	for i, branch := range branches {
		if !prMerged[i] {
			continue
		}

		log.Infof("%v: #%v was merged: deleting...", branch, prs[i])
		err := (&branchDeleteCmd{
			Name:  branch,
			Force: true,
		}).Run(ctx, log, opts)
		if err != nil {
			return fmt.Errorf("delete branch %v: %w", branch, err)
		}
	}

	// TODO:
	// If --restack is set, restack the affected branches
	// (or restack just the branches in this stack?)
	// For this, we need the Delete operation to report the affected
	// branches, which means it has to be refactored into a spice-level
	// operation first.
	return nil
}
