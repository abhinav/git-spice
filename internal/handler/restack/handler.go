// Package restack implements business logic for high-level restack operations.
package restack

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/iterutil"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
)

//go:generate mockgen -package restack -destination mocks_test.go . GitWorktree,Service

// GitWorktree is a subet of the git.Worktree interface.
type GitWorktree interface {
	CurrentBranch(ctx context.Context) (string, error)
	Checkout(ctx context.Context, branch string) error
}

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
func (h *Handler) Restack(ctx context.Context, req *Request) (int, error) {
	must.NotBeBlankf(req.Branch, "branch must not be blank")
	must.NotBeEmptyf(req.ContinueCommand, "continue command must not be set")

	req.Scope = cmp.Or(req.Scope, ScopeBranch) // 0 = ScopeBranch

	loadBranchGraph := sync.OnceValues(func() (*spice.BranchGraph, error) {
		return h.Service.BranchGraph(ctx, nil)
	})

	var branchesToRestack []string // branches in restack order

	if req.Scope&scopeDownstackExclusive != 0 {
		branchGraph, err := loadBranchGraph()
		if err != nil {
			return 0, fmt.Errorf("load branch graph: %w", err)
		}

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
		if req.Branch != h.Store.Trunk() {
			branchesToRestack = append(branchesToRestack, req.Branch)
		}
	}

	if req.Scope&ScopeUpstackExclusive != 0 {
		branchGraph, err := loadBranchGraph()
		if err != nil {
			return 0, fmt.Errorf("load branch graph: %w", err)
		}

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

	var restackCount int
loop:
	for _, branch := range branchesToRestack {
		if branch == h.Store.Trunk() {
			continue loop // skip restacking trunk branch
		}

		res, err := h.Service.Restack(ctx, branch)
		if err != nil {
			var rebaseErr *git.RebaseInterruptError
			switch {
			case errors.As(err, &rebaseErr):
				// If the rebase is interrupted by a conflict,
				// we'll resume by re-running this command.
				return 0, h.Service.RebaseRescue(ctx, spice.RebaseRescueRequest{
					Err:     rebaseErr,
					Command: req.ContinueCommand,
					Branch:  req.Branch,
					Message: fmt.Sprintf("interrupted: restack branch %q", branch),
				})

			case errors.Is(err, state.ErrNotExist):
				h.Log.Errorf("%v: branch not tracked: run 'gs branch track %v' to track it", branch, branch)
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

	if err := h.Worktree.Checkout(ctx, req.Branch); err != nil {
		return 0, fmt.Errorf("checkout branch %v: %w", req.Branch, err)
	}

	return restackCount, nil
}
