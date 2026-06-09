// Package onto coordinates branch and upstack base changes.
package onto

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/autostash"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
)

// RestackHandler restacks branches after their downstack base changes.
type RestackHandler interface {
	RestackUpstack(ctx context.Context, branch string, opts *restack.UpstackOptions) error
}

// AutostashHandler is a subset of the autostash.Handler interface.
type AutostashHandler interface {
	BeginAutostash(ctx context.Context, opts *autostash.Options) (func(*error, *autostash.CleanupOptions), error)
}

var _ AutostashHandler = (*autostash.Handler)(nil)

// Handler coordinates higher-level onto operations.
//
// The lower-level spice service moves one branch at a time.
// Handler owns the command workflows that also retarget or restack
// surrounding branches.
type Handler struct {
	Log      *silog.Logger  // required
	Worktree *git.Worktree  // required
	Service  *spice.Service // required
	Restack  RestackHandler // required

	// RestackMethod selects how branches are replayed onto their bases.
	//
	// The zero value is [spice.RestackMethodRebase].
	RestackMethod spice.RestackMethod

	// Autostash stashes uncommitted changes around an onto operation.
	//
	// It is only used by the merge restack method;
	// rebase relies on Git's per-branch '--autostash'.
	// May be nil; merge onto operations then run against the worktree as-is.
	Autostash AutostashHandler // optional
}

// beginMergeAutostash stashes uncommitted changes around a merge-method
// onto operation, mirroring [restack.Handler]. 'git merge' needs a clean
// worktree to check out branches; the rebase method instead relies on
// 'git rebase --autostash' per branch.
//
// It returns a no-op cleanup when the restack method is not merge
// or when Autostash is nil.
func (h *Handler) beginMergeAutostash(
	ctx context.Context,
	branch string,
) (func(*error), error) {
	if h.RestackMethod != spice.RestackMethodMerge || h.Autostash == nil {
		return func(*error) {}, nil
	}

	cleanup, err := h.Autostash.BeginAutostash(ctx, &autostash.Options{
		Message:   "git-spice: autostash before merge onto",
		ResetMode: autostash.ResetHard,
		Branch:    branch,
	})
	if err != nil {
		return nil, err
	}

	return func(errPtr *error) {
		cleanup(errPtr, &autostash.CleanupOptions{RescueBranch: branch})
	}, nil
}

// BranchRequest describes a branch onto operation.
type BranchRequest struct {
	// Branch is the branch to move.
	Branch string // required

	// Onto is the destination branch.
	Onto string // required

	// Restack decides how branches above Branch are replayed
	// after being retargeted onto Branch's original base.
	Restack spice.RestackMode

	// ContinueCommand resumes the operation after a rebase rescue.
	ContinueCommand []string
}

// BranchOnto moves one branch onto another branch.
//
// Branches directly above the moved branch are first retargeted onto the
// moved branch's original base.
// The request's restack mode decides whether those direct aboves,
// and their own upstacks,
// are also rebased immediately.
func (h *Handler) BranchOnto(ctx context.Context, req *BranchRequest) (retErr error) {
	cleanup, err := h.beginMergeAutostash(ctx, req.Branch)
	if err != nil {
		return err
	}
	defer cleanup(&retErr)

	branch, err := h.Service.LookupBranch(ctx, req.Branch)
	if err != nil {
		if errors.Is(err, state.ErrNotExist) {
			return fmt.Errorf("branch not tracked: %s", req.Branch)
		}
		return fmt.Errorf("get branch: %w", err)
	}

	aboves, err := h.Service.ListAbove(ctx, req.Branch)
	if err != nil {
		return fmt.Errorf("list branches above %s: %w", req.Branch, err)
	}

	ontoMode := spice.BranchOntoRetargetOnly
	if req.Restack.Includes(spice.RestackAboves) {
		ontoMode = spice.BranchOntoRebase
	}

	for _, above := range aboves {
		if err := h.Service.BranchOnto(ctx, &spice.BranchOntoRequest{
			Branch: above,
			Onto:   branch.Base,
			Mode:   ontoMode,
		}); err != nil {
			return h.Service.RebaseRescue(ctx, spice.RebaseRescueRequest{
				Err:     err,
				Command: req.ContinueCommand,
				Branch:  req.Branch,
				Message: fmt.Sprintf("interrupted: %s: branch onto %s", req.Branch, req.Onto),
			})
		}
		if req.Restack.None() {
			h.Log.Infof("%s: retargeted upstack onto %s", above, branch.Base)
			continue
		}

		h.Log.Infof("%s: moved upstack onto %s", above, branch.Base)
		if !req.Restack.Includes(spice.RestackUpstack) {
			continue
		}

		if err := h.Restack.RestackUpstack(ctx, above, &restack.UpstackOptions{
			SkipStart: true,
		}); err != nil {
			return h.Service.RebaseRescue(ctx, spice.RebaseRescueRequest{
				Err:     err,
				Command: req.ContinueCommand,
				Branch:  req.Branch,
				Message: fmt.Sprintf("interrupted: %s: branch onto %s", req.Branch, req.Onto),
			})
		}
	}

	if err := h.Service.BranchOnto(ctx, &spice.BranchOntoRequest{
		Branch: req.Branch,
		Onto:   req.Onto,
	}); err != nil {
		return h.Service.RebaseRescue(ctx, spice.RebaseRescueRequest{
			Err:     err,
			Command: req.ContinueCommand,
			Branch:  req.Branch,
			Message: fmt.Sprintf("interrupted: %s: branch onto %s", req.Branch, req.Onto),
		})
	}

	h.Log.Infof("%s: moved onto %s", req.Branch, req.Onto)
	return h.Worktree.CheckoutBranch(ctx, req.Branch)
}

// UpstackRequest describes an upstack onto operation.
type UpstackRequest struct {
	// Branch is the first branch to move.
	Branch string // required

	// Onto is the destination branch.
	Onto string // required

	// ContinueCommand resumes the operation after a rebase rescue.
	ContinueCommand []string
}

// UpstackOnto moves one branch and restacks the branches above it.
func (h *Handler) UpstackOnto(ctx context.Context, req *UpstackRequest) (retErr error) {
	cleanup, err := h.beginMergeAutostash(ctx, req.Branch)
	if err != nil {
		return err
	}
	defer cleanup(&retErr)

	err = h.Service.BranchOnto(ctx, &spice.BranchOntoRequest{
		Branch: req.Branch,
		Onto:   req.Onto,
	})
	if err != nil {
		return h.Service.RebaseRescue(ctx, spice.RebaseRescueRequest{
			Err:     err,
			Command: req.ContinueCommand,
			Branch:  req.Branch,
			Message: fmt.Sprintf("interrupted: %s: upstack onto %s", req.Branch, req.Onto),
		})
	}
	h.Log.Infof("%v: moved upstack onto %v", req.Branch, req.Onto)

	return h.Restack.RestackUpstack(ctx, req.Branch, &restack.UpstackOptions{
		SkipStart: true,
	})
}
