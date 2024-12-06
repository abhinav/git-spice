package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type branchDeleteCmd struct {
	Force    bool     `help:"Force deletion of the branch"`
	Branches []string `arg:"" optional:"" help:"Names of the branches to delete" predictor:"branches"`
}

func (*branchDeleteCmd) Help() string {
	return text.Dedent(`
		The deleted branches and their commits are removed from the stack.
		Branches above the deleted branches are rebased onto
		the next branches available downstack.

		A prompt will allow selecting the target branch if none are provided.
	`)
}

func (cmd *branchDeleteCmd) Run(ctx context.Context, log *log.Logger, view ui.View) error {
	repo, store, svc, err := openRepo(ctx, log, view)
	if err != nil {
		return err
	}

	if len(cmd.Branches) == 0 {
		// If a branch name is not given, prompt for one;
		// assuming we're in interactive mode.
		if !ui.Interactive(view) {
			return fmt.Errorf("cannot proceed without branch name: %w", errNoPrompt)
		}

		currentBranch, err := repo.CurrentBranch(ctx)
		if err != nil {
			currentBranch = ""
		}

		branch, err := (&branchPrompt{
			Disabled: func(b git.LocalBranch) bool {
				return b.Name == store.Trunk()
			},
			Default: currentBranch,
			Title:   "Select a branch to delete",
		}).Run(ctx, view, repo, store)
		if err != nil {
			return fmt.Errorf("select branch: %w", err)
		}

		cmd.Branches = []string{branch}
	}

	type branchInfo struct {
		Name string

		Tracked bool
		Base    string // base branch (may be unset if untracked)

		Head            git.Hash // head hash (set only if exists)
		Exists          bool
		ChangeID        string
		MergedDownstack []string
	}

	// name to branch info
	branchesToDelete := make(map[string]*branchInfo, len(cmd.Branches))
	for _, branch := range cmd.Branches {
		base := store.Trunk()
		tracked, exists := true, true
		var mergedDownstack []string
		var changeID string

		var head git.Hash
		if b, err := svc.LookupBranch(ctx, branch); err != nil {
			if delErr := new(spice.DeletedBranchError); errors.As(err, &delErr) {
				exists = false
				base = delErr.Base
				log.Info("branch has already been deleted", "branch", branch)
			} else if errors.Is(err, state.ErrNotExist) {
				tracked = false
				log.Debug("branch is not tracked", "error", err)
				log.Info("branch is not tracked: deleting anyway", "branch", branch)
			} else {
				return fmt.Errorf("lookup branch %v: %w", branch, err)
			}
		} else {
			head = b.Head
			base = b.Base
			mergedDownstack = b.MergedDownstack
			// TODO
			if change := b.Change; change != nil {
				if branchChangeID := change.ChangeID(); branchChangeID != nil {
					changeID = branchChangeID.String()
				}
			}
			must.NotBeBlankf(base, "base branch for %v must be set", branch)
			must.NotBeBlankf(head.String(), "head commit for %v must be set", branch)
		}

		// Branch is untracked, but exists.
		if exists && head == "" {
			hash, err := repo.PeelToCommit(ctx, branch)
			if err != nil {
				return fmt.Errorf("peel to commit: %w", err)
			}
			head = hash
		}

		branchesToDelete[branch] = &branchInfo{
			Name:            branch,
			Head:            head,
			Base:            base,
			Tracked:         tracked,
			Exists:          exists,
			ChangeID:        changeID,
			MergedDownstack: mergedDownstack,
		}
	}

	// upstack restack changes the current branch.
	// checkoutTarget specifiest he branch we'll check out after deletion.
	// The logic for this is as follows:
	//
	//  - if in detached HEAD state, use the current commit
	//  - if the current branch is not being deleted, use that
	//  - if the current branch is being deleted,
	//     - if there are multiple branches, use trunk
	//     - if there is only one branch, use its base
	//
	// TODO: Make an 'upstack restack' spice.Service method
	// that won't leave us on the wrong branch.
	var checkoutTarget string
	if currentBranch, err := repo.CurrentBranch(ctx); err != nil {
		if !errors.Is(err, git.ErrDetachedHead) {
			return fmt.Errorf("get current branch: %w", err)
		}

		head, err := repo.PeelToCommit(ctx, "HEAD")
		if err != nil {
			return fmt.Errorf("peel to commit: %w", err)
		}

		// In detached HEAD state, use the current commit.
		checkoutTarget = head.String()
	} else {
		checkoutTarget = currentBranch

		// Current branch is being deleted.
		// If there are multiple branches, use trunk.
		if slices.Contains(cmd.Branches, currentBranch) {
			// If current branch is being deleted,
			// pick a different branch to check out.
			if len(branchesToDelete) == 1 {
				info, ok := branchesToDelete[currentBranch]
				must.Bef(ok, "current branch %v not found in branches to delete", currentBranch)
				checkoutTarget = info.Base
			} else {
				// Multiple branches are being deleted.
				// Use trunk.
				checkoutTarget = store.Trunk()
			}
		}
	}

	// Mapping of branch name to list of change IDs that it depends on
	// that have already been merged.
	allBranchHistory := make(map[string][]string)

	// For each branch under consideration,
	// if it's a tracked branch, update the upstacks from it
	// to point to its base, or the next branch downstack
	// if the base is also being deleted.
	for branch, info := range branchesToDelete {
		if !info.Tracked {
			continue
		}

		// Search through the bases to find one
		// that isn't being deleted.
		base := info.Base
		baseInfo, deletingBase := branchesToDelete[base]
		for base != store.Trunk() && deletingBase {
			base = baseInfo.Base
			baseInfo, deletingBase = branchesToDelete[base]
		}

		aboves, err := svc.ListAbove(ctx, branch)
		if err != nil {
			return fmt.Errorf("list above %v: %w", branch, err)
		}

		for _, above := range aboves {
			// Propagate the merged branches from the current branch to all branches above it.
			var newHistory []string
			newHistory = append(newHistory, info.MergedDownstack...) // merged downstack of the current branch
			newHistory = append(newHistory, info.ChangeID)           // current branch
			newHistory = append(newHistory, allBranchHistory[above]...)
			allBranchHistory[above] = newHistory
			if _, ok := branchesToDelete[above]; ok {
				// This upstack is also being deleted. Skip.
				continue
			}

			if err := svc.BranchOnto(ctx, &spice.BranchOntoRequest{
				Branch: above,
				Onto:   base,
			}); err != nil {
				contCmd := []string{"branch", "delete"}
				if cmd.Force {
					contCmd = append(contCmd, "--force")
				}
				contCmd = append(contCmd, cmd.Branches...)
				return svc.RebaseRescue(ctx, spice.RebaseRescueRequest{
					Err:     err,
					Command: contCmd,
					Branch:  checkoutTarget,
					Message: fmt.Sprintf("interrupted: %v: deleting branch", branch),
				})
			}
			log.Infof("%v: moved upstack onto %v", above, base)
		}

		delete(allBranchHistory, branch)
	}

	if err := repo.Checkout(ctx, checkoutTarget); err != nil {
		return fmt.Errorf("checkout %v: %w", checkoutTarget, err)
	}

	// Remaining branches may have relationships with each other.
	// We'll need to delete them in topological order: leaf to root.
	var (
		deleteOrder []*branchInfo
		visit       func(string)
	)
	visit = func(branch string) {
		info := branchesToDelete[branch]
		if info == nil {
			return // already visited or not in the list
		}

		visit(info.Base)
		deleteOrder = append(deleteOrder, info)
		delete(branchesToDelete, branch)
	}
	for branch := range branchesToDelete {
		visit(branch)
	}

	branchTx := store.BeginBranchTx()

	for branchToUpdate, merged := range allBranchHistory {
		if len(merged) == 0 {
			continue
		}
		_, err := svc.LookupBranch(ctx, branchToUpdate)
		if err != nil {
			if errors.Is(err, state.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("lookup branch: %w", err)
		}

		if err := branchTx.Upsert(ctx, state.UpsertRequest{
			Name:            branchToUpdate,
			MergedDownstack: merged,
		}); err != nil {
			return fmt.Errorf("update merged branches for %v: %w", branchToUpdate, err)
		}
	}

	// deleteOrder is now in [base, ..., leaf] order. Reverse it.
	slices.Reverse(deleteOrder)

	var untrackedNames []string
	for _, b := range deleteOrder {
		branch, head := b.Name, b.Head
		exists, tracked, force := b.Exists, b.Tracked, cmd.Force

		// If the branch exists, and is not reachable from HEAD,
		// git will refuse to delete it.
		// If we can prompt, ask the user to upgrade to a forceful deletion.
		if exists && !force && ui.Interactive(view) && !repo.IsAncestor(ctx, head, "HEAD") {
			log.Warnf("%v (%v) is not reachable from HEAD", branch, head.Short())
			prompt := ui.NewConfirm().
				WithTitlef("Delete %v anyway?", branch).
				WithDescriptionf("%v has not been merged into HEAD. This may result in data loss.", branch).
				WithValue(&force)
			if err := ui.Run(view, prompt); err != nil {
				return fmt.Errorf("run prompt: %w", err)
			}
		}

		if exists {
			opts := git.BranchDeleteOptions{Force: force}
			if err := repo.DeleteBranch(ctx, branch, opts); err != nil {
				// If the branch still exists,
				// it's likely because it's not merged.
				if _, peelErr := repo.PeelToCommit(ctx, branch); peelErr == nil {
					log.Error("git refused to delete the branch", "err", err)
					log.Error("try re-running with --force")
					return errors.New("branch not deleted")
				}

				// If the branch doesn't exist,
				// it may already have been deleted.
				log.Warn("branch may already have been deleted", "err", err)
			}

			log.Infof("%v: deleted (was %v)", branch, head.Short())
		}

		if tracked {
			if err := branchTx.Delete(ctx, branch); err != nil {
				log.Warn("Unable to untrack branch", "branch", branch, "error", err)
				log.Warn("Try manually untracking the branch with 'gs branch untrack'")
			} else {
				untrackedNames = append(untrackedNames, branch)
			}
		}
	}

	msg := fmt.Sprintf("delete: %v", strings.Join(untrackedNames, ", "))
	if err := branchTx.Commit(ctx, msg); err != nil {
		return fmt.Errorf("update state: %w", err)
	}

	return nil
}
