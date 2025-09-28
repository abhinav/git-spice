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

//go:generate mockgen -destination mocks_test.go -package checkout -typed . GitRepository,GitWorktree,TrackHandler,Service,Store

// Options defines options for checking out a branch.
// These turn into command line flags, so be mindful of what you add here.
type Options struct {
	DryRun bool `short:"n" xor:"detach-or-dry-run" help:"Print the target branch without checking it out"`
	Detach bool `xor:"detach-or-dry-run" help:"Detach HEAD after checking out"`

	Verbose bool `name:"checkout-verbose" hidden:"" default:"true" config:"checkout.verbose"`
}

// Store provides access to the git-spice state.
type Store interface {
	// Trunk returns the name of the trunk branch.
	Trunk() string
	Remote() (string, error)
}

// GitWorktree allows changing which branch or commit
// is checked out in the current working tree.
type GitWorktree interface {
	DetachHead(ctx context.Context, commitish string) error
	CheckoutBranch(ctx context.Context, branch string) error
}

// GitRepository provides access to the Git repository methods
// that do not require a worktree.
type GitRepository interface {
	CreateBranch(ctx context.Context, req git.CreateBranchRequest) error
	PeelToCommit(ctx context.Context, ref string) (git.Hash, error)
	SetBranchUpstream(ctx context.Context, branch, upstream string) error
}

// TrackHandler allows tracking new branches with git-spice.
type TrackHandler interface {
	TrackBranch(ctx context.Context, req *track.BranchRequest) error
}

// Service provides access to the spice service methods
type Service interface {
	// VerifyRestacked checks if the branch is restacked.
	VerifyRestacked(ctx context.Context, branch string) error
}

// Handler provides a central place for handling checkout operations.
type Handler struct {
	Stdout     io.Writer     // required
	Log        *silog.Logger // required
	Store      Store         // required
	Repository GitRepository // required
	Worktree   GitWorktree   // required
	Track      TrackHandler  // required
	Service    Service       // required
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
			case errors.As(err, &restackErr): // needs to be restacked
				log.Warnf("%v: needs to be restacked: run 'gs branch restack --branch=%v'", branch, branch)

			case errors.Is(err, git.ErrNotExist): // does not exist
				// Branch name might be a reference to a remote branch.
				// Try to recover by checking if the branch exists in the remote.
				var recovered bool
				if remote, err := h.Store.Remote(); err == nil {
					upstreamBranch := fmt.Sprintf("%s/%s", remote, branch)
					if upstreamHead, err := h.Repository.PeelToCommit(ctx, upstreamBranch); err == nil {
						h.Log.Infof("%v: found remote branch %v, checking out", branch, upstreamBranch)

						createReq := git.CreateBranchRequest{
							Name: branch,
							Head: string(upstreamHead),
						}
						if err := h.Repository.CreateBranch(ctx, createReq); err != nil {
							return fmt.Errorf("create branch from remote %q: %w", upstreamBranch, err)
						}

						if err := h.Repository.SetBranchUpstream(ctx, branch, upstreamBranch); err != nil {
							// Non-fatal error; just log it.
							log.Error("Error setting upstream for branch",
								"name", branch, "upstream", upstreamBranch, "error", err)
						}

						recovered = true
					}
				}

				if !recovered {
					return fmt.Errorf("branch %q does not exist", branch)
				}
				fallthrough // we just created the branch so it's not tracked

			case errors.Is(err, state.ErrNotExist): // exists but not tracked
				shouldTrack, err := req.ShouldTrack(branch)
				if err != nil {
					return fmt.Errorf("check if branch should be tracked: %w", err)
				}

				if shouldTrack {
					err := h.Track.TrackBranch(ctx, &track.BranchRequest{
						Branch: branch,
					})
					if err != nil {
						log.Warn("Error tracking branch", "branch", branch, "error", err)
					}
				}

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

	if err := h.Worktree.CheckoutBranch(ctx, branch); err != nil {
		return fmt.Errorf("checkout branch: %w", err)
	}

	if opts.Verbose {
		log.Infof("switched to branch: %s", branch)
	}

	return nil
}
