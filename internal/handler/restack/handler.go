// Package restack implements business logic for high-level restack operations.
package restack

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"go.abhg.dev/gs/internal/cli"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/autostash"
	"go.abhg.dev/gs/internal/iterutil"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
)

//go:generate mockgen -package restack -destination mocks_test.go . GitWorktree,Service,AutostashHandler

// GitWorktree is a subet of the git.Worktree interface.
type GitWorktree interface {
	CurrentBranch(ctx context.Context) (string, error)
	CheckoutBranch(ctx context.Context, branch string) error
	RootDir() string
}

var _ GitWorktree = (*git.Worktree)(nil)

// AutostashHandler is a subset of the autostash.Handler interface.
type AutostashHandler interface {
	BeginAutostash(ctx context.Context, opts *autostash.Options) (func(*error, *autostash.CleanupOptions), error)
}

var _ AutostashHandler = (*autostash.Handler)(nil)

// Store is a subset of the state.Store interface.
type Store interface {
	Trunk() string
}

// Service is a subset of the spice.Service interface.
type Service interface {
	BranchGraph(ctx context.Context, opts *spice.BranchGraphOptions) (*spice.BranchGraph, error)
	Restack(ctx context.Context, name string) (*spice.RestackResponse, error)
	RebaseRescue(ctx context.Context, req spice.RebaseRescueRequest) error
}

// Handler implements various restack operations.
type Handler struct {
	Log      *silog.Logger // required
	Worktree GitWorktree   // required
	Store    Store         // required
	Service  Service       // required

	// RestackMethod selects how branches are replayed onto their bases.
	//
	// The zero value is [spice.RestackMethodRebase].
	RestackMethod spice.RestackMethod

	// Autostash stashes uncommitted changes around the restack loop.
	//
	// It is only used by the merge restack method;
	// rebase relies on Git's per-branch '--autostash'.
	// May be nil; merge restacks then run against the worktree as-is.
	Autostash AutostashHandler // optional
}

// Scope specifies which branches are affected
// by a restack operation.
type Scope int

const (
	// ScopeBranch selects just the branch specified in the request.
	ScopeBranch Scope = 1 << iota

	// ScopeUpstackExclusive selects the upstack of a branch,
	// excluding the branch itself.
	ScopeUpstackExclusive

	scopeDownstackExclusive

	// ScopeUpstack selects the upstack of a branch,
	// including the branch itself.
	ScopeUpstack = ScopeBranch | ScopeUpstackExclusive

	// ScopeDownstack selects the downstack of a branch,
	// including the branch itself.
	ScopeDownstack = ScopeBranch | scopeDownstackExclusive

	// ScopeStack selects the full stack of a branch:
	// the upstack, downstack, and the branch itself.
	ScopeStack = ScopeUpstack | ScopeDownstack
)

// Request is a request to restack one or more branches.
type Request struct {
	// Branch is the starting point for the restack operation.
	// This branch will be checked out at the end of the operation.
	//
	// Scope is relative to this branch.
	Branch string // required

	// ContinueCommand specifies the git-spice command
	// to run from the Branch's context
	// to resume this operation if it is interrupted
	// due to a conflict.
	ContinueCommand []string // required

	// Scope specifies which branches are affected by the restack operation.
	//
	// Defaults to ScopeBranch.
	Scope Scope
}

// Restack restacks one or more branches according to the request.
func (h *Handler) Restack(ctx context.Context, req *Request) (restackCount int, retErr error) {
	must.NotBeBlankf(req.Branch, "branch must not be blank")
	must.NotBeEmptyf(req.ContinueCommand, "continue command must not be set")

	req.Scope = cmp.Or(req.Scope, ScopeBranch) // 0 = ScopeBranch

	// git merge needs a clean worktree to check out each branch in the
	// loop, so stash around the whole loop; the rebase method gets this
	// per branch from 'git rebase --autostash'. On a conflict, cleanup
	// defers stash restoration to the rebase rescue continuation queue.
	if h.RestackMethod == spice.RestackMethodMerge && h.Autostash != nil {
		cleanup, err := h.Autostash.BeginAutostash(ctx, &autostash.Options{
			Message:   "git-spice: autostash before merge restack",
			ResetMode: autostash.ResetHard,
			Branch:    req.Branch,
		})
		if err != nil {
			return 0, err
		}
		defer cleanup(&retErr, &autostash.CleanupOptions{RescueBranch: req.Branch})
	}

	branchGraph, err := h.Service.BranchGraph(ctx, &spice.BranchGraphOptions{
		IncludeWorktrees: true,
	})
	if err != nil {
		return 0, fmt.Errorf("load branch graph: %w", err)
	}

	var branchesToRestack []string // branches in restack order

	if req.Scope&scopeDownstackExclusive != 0 {
		// Downstack returns the branches in the order,
		// [branch, downstack1, downstack2, ...],
		// not including the trunk.
		//
		// Restacking order is the reverse of that.
		downstack := slices.Collect(branchGraph.Downstack(req.Branch))
		if len(downstack) > 0 && downstack[0] == req.Branch {
			downstack = downstack[1:]
		}
		slices.Reverse(downstack)
		branchesToRestack = append(branchesToRestack, downstack...)
	}

	if req.Scope&ScopeBranch != 0 {
		if req.Branch == h.Store.Trunk() {
			// If we're explicitly only trying to restack trunk,
			// fail the operation.
			if req.Scope == ScopeBranch {
				return 0, errors.New("trunk cannot be restacked")
			}
		} else {
			branchesToRestack = append(branchesToRestack, req.Branch)
		}
	}

	if req.Scope&ScopeUpstackExclusive != 0 {
		// Upstacks returns the branches in the order,
		// [branch, upstack1, upstack2, ...].
		// That's restacking order, so we can use it directly
		// once we drop the first item (the branch itself).
		for idx, branch := range iterutil.Enumerate(branchGraph.Upstack(req.Branch)) {
			if idx == 0 && branch == req.Branch {
				continue // skip the branch itself
			}

			branchesToRestack = append(branchesToRestack, branch)
		}
	}

	// If any of the branches to be restacked
	// are checked out in another Git worktree,
	// we cannot restack anything upstack from that branch.
	//
	// And since branchesToRestack is in the restack order,
	// we can check if a prior skipped branch affects the current branch
	// by just checking the base of the skipped branch.
	currentWT := h.Worktree.RootDir()
	skipped := make(map[string]struct{})
	branchesToActuallyRestack := branchesToRestack[:0]
	var requestBranchWT string // worktree of request.Branch
	for _, branch := range branchesToRestack {
		if branch == h.Store.Trunk() {
			continue // skip restacking trunk branch
		}

		if info, ok := branchGraph.Lookup(branch); ok {
			if _, baseSkipped := skipped[info.Base]; baseSkipped {
				// Base branch not being restacked,
				// so skip this as well.
				h.Log.Warnf("%v: base branch %v was not restacked, skipping", branch, info.Base)
				skipped[branch] = struct{}{}
				continue
			}
		}

		branchWT := branchGraph.Worktree(branch)
		if req.Branch == branch {
			requestBranchWT = branchWT
		}
		if branchWT != "" && branchWT != currentWT {
			// Checked out in another worktree.
			h.Log.Warnf("%v: checked out in another worktree (%v), skipping", branch, branchWT)
			skipped[branch] = struct{}{}
			continue
		}

		branchesToActuallyRestack = append(branchesToActuallyRestack, branch)
	}
	branchesToRestack = branchesToActuallyRestack

loop:
	for _, branch := range branchesToRestack {
		res, err := h.Service.Restack(ctx, branch)
		if err != nil {
			_, isInterrupt := errors.AsType[git.InterruptError](err)
			switch {
			case isInterrupt:
				// If the restack is interrupted by a conflict
				// (rebase or merge),
				// we'll resume by re-running this command.
				return 0, h.Service.RebaseRescue(ctx, spice.RebaseRescueRequest{
					Err:     err,
					Command: req.ContinueCommand,
					Branch:  req.Branch,
					Message: fmt.Sprintf("interrupted: restack branch %q", branch),
				})

			case errors.Is(err, state.ErrNotExist):
				h.Log.Errorf("%v: branch not tracked: run '%s branch track %v' to track it", branch, cli.Name(), branch)
				return 0, errors.New("untracked branch")

			case errors.Is(err, spice.ErrAlreadyRestacked):
				h.Log.Infof("%v: branch does not need to be restacked.", branch)
				continue loop

			default:
				return 0, fmt.Errorf("restack branch %q: %w", branch, err)
			}
		}

		h.Log.Infof("%v: restacked on %v", branch, res.Base)
		restackCount++
	}

	if requestBranchWT != "" && requestBranchWT != currentWT {
		h.Log.Warnf("%v: checked out in another worktree (%v), not checking out here", req.Branch, requestBranchWT)
	} else if restackCount > 0 {
		if err := h.Worktree.CheckoutBranch(ctx, req.Branch); err != nil {
			return 0, fmt.Errorf("checkout branch %v: %w", req.Branch, err)
		}
	}

	return restackCount, nil
}
