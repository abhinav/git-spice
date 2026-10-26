package sync

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"go.abhg.dev/gs/internal/cli"
	"go.abhg.dev/gs/internal/git"
	branchdel "go.abhg.dev/gs/internal/handler/delete"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/spice"
)

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

func (h *Handler) deleteBranches(
	ctx context.Context,
	branchesToDelete []branchDeletion,
	detachWorktrees bool,
) ([]string, error) {
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
			if !detachWorktrees {
				h.Log.Warnf("%v: checked out in another worktree (%v), skipping deletion.", branchInfo.Name, branchInfo.Worktree)
				h.Log.Warnf("Run '%[1]s branch delete' or run '%[1]s repo sync' again from that worktree to delete it.", cli.Name())
				continue
			}

			detachedHead, err := func() (git.Hash, error) {
				worktree, err := h.Repository.OpenWorktree(ctx, branchInfo.Worktree)
				if err != nil {
					return "", fmt.Errorf("open worktree: %w", err)
				}
				if err := worktree.DetachHead(ctx, ""); err != nil {
					return "", fmt.Errorf("detach HEAD: %w", err)
				}

				detachedHead, err := worktree.Head(ctx)
				if err != nil {
					return "", fmt.Errorf("read detached HEAD: %w", err)
				}
				return detachedHead, nil
			}()
			if err != nil {
				h.Log.Warn("Unable to detach worktree; skipping branch deletion",
					"branch", branchInfo.Name,
					"worktree", branchInfo.Worktree,
					"error", err)
				continue
			}
			h.Log.Infof("%v: detached worktree %v at %v.",
				branchInfo.Name, branchInfo.Worktree, detachedHead)
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
