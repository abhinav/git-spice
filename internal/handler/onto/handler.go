// Package onto coordinates branch and upstack base changes.
package onto

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
)

// RestackHandler restacks branches after their downstack base changes.
type RestackHandler interface {
	RestackUpstack(ctx context.Context, req *restack.UpstackRequest) error
}

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
func (h *Handler) BranchOnto(ctx context.Context, req *BranchRequest) error {
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

		if err := h.Restack.RestackUpstack(ctx, &restack.UpstackRequest{
			Branch: above,
			Options: &restack.UpstackOptions{
				SkipStart: true,
			},
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
func (h *Handler) UpstackOnto(ctx context.Context, req *UpstackRequest) error {
	err := h.Service.BranchOnto(ctx, &spice.BranchOntoRequest{
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

	return h.Restack.RestackUpstack(ctx, &restack.UpstackRequest{
		Branch: req.Branch,
		Options: &restack.UpstackOptions{
			SkipStart: true,
		},
	})
}
