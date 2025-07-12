// Package checkout implements a Handler to change branches in a stack.
package checkout

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/track"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
)

//go:generate mockgen -destination mocks_test.go -package checkout -typed . GitWorktree,TrackHandler,Service,Store

// Options defines options for checking out a branch.
// These turn into command line flags, so be mindful of what you add here.
type Options struct {
	DryRun bool `short:"n" xor:"detach-or-dry-run" help:"Print the target branch without checking it out"`
	Detach bool `xor:"detach-or-dry-run" help:"Detach HEAD after checking out"`
}

// Store provides access to the git-spice state.
type Store interface {
	// Trunk returns the name of the trunk branch.
	Trunk() string
}

// GitWorktree allows changing which branch or commit
// is checked out in the current working tree.
type GitWorktree interface {
	DetachHead(ctx context.Context, commitish string) error
	Checkout(ctx context.Context, branch string) error
}

// TrackHandler allows tracking new branches with git-spice.
type TrackHandler interface {
	AddBranch(ctx context.Context, req *track.AddBranchRequest) error
}

// Service provides access to the spice service methods
type Service interface {
	// VerifyRestacked checks if the branch is restacked.
	VerifyRestacked(ctx context.Context, branch string) error
}

// Handler provides a central place for handling checkout operations.
type Handler struct {
	Stdout   io.Writer     // required
	Log      *silog.Logger // required
	Store    Store         // required
	Worktree GitWorktree   // required
	Track    TrackHandler  // required
	Service  Service       // required
}

// Request is a request to checkout a branch.
type Request struct {
	// Branch is the name of the branch to checkout.
	Branch string // required

	// Options are the options for checking out the branch.
	Options *Options // optional

	// ShouldTrack is called if the branch being checked out is untracked,
	// and allows the caller to decide if the branch should be tracked.
	ShouldTrack func(branch string) (bool, error) // optional
}

// CheckoutBranch checks out the specified branch with git-spice,
// offering to track it if it's not already tracked.
func (h *Handler) CheckoutBranch(ctx context.Context, req *Request) error {
	branch := req.Branch
	opts := cmp.Or(req.Options, &Options{})
	if req.ShouldTrack == nil {
		req.ShouldTrack = func(string) (bool, error) {
			return false, nil
		}
	}

	must.NotBeBlankf(branch, "branch name must not be blank")
	must.NotBef(opts.DryRun && opts.Detach, "cannot use both dry-run and detach options")

	log := h.Log
	if branch != h.Store.Trunk() {
		if err := h.Service.VerifyRestacked(ctx, branch); err != nil {
			var restackErr *spice.BranchNeedsRestackError
			switch {
			case errors.As(err, &restackErr):
				log.Warnf("%v: needs to be restacked: run 'gs branch restack --branch=%v'", branch, branch)
			case errors.Is(err, state.ErrNotExist):
				shouldTrack, err := req.ShouldTrack(branch)
				if err != nil {
					return fmt.Errorf("check if branch should be tracked: %w", err)
				}

				if shouldTrack {
					err := h.Track.AddBranch(ctx, &track.AddBranchRequest{
						Branch: branch,
					})
					if err != nil {
						log.Warn("Error tracking branch", "branch", branch, "error", err)
					}
				}
			case errors.Is(err, git.ErrNotExist):
				return fmt.Errorf("branch %q does not exist", branch)
			default:
				log.Warn("Unable to check if branch is restacked",
					"branch", branch, "error", err)
			}
		}
	}

	if opts.DryRun {
		_, _ = fmt.Fprintln(h.Stdout, branch)
		return nil
	}

	if opts.Detach {
		if err := h.Worktree.DetachHead(ctx, branch); err != nil {
			return fmt.Errorf("detach HEAD: %w", err)
		}

		return nil
	}

	if err := h.Worktree.Checkout(ctx, branch); err != nil {
		return fmt.Errorf("checkout branch: %w", err)
	}

	return nil
}
