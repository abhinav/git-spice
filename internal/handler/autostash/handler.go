// Package autostash implements automatic stashing
// and restoration of uncommitted changes.
//
// Use the autostash handler from commands that want to retain
// dirty changes in the worktree.
package autostash

import (
	"cmp"
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
)

//go:generate mockgen -package autostash -destination mocks_test.go -typed . GitWorktree,Service

// GitWorktree is a subset of the git.Worktree interface.
type GitWorktree interface {
	CurrentBranch(ctx context.Context) (string, error)

	StashCreate(ctx context.Context, message string) (git.Hash, error)
	StashApply(ctx context.Context, stash string) error
	StashStore(ctx context.Context, stash git.Hash, message string) error

	Reset(ctx context.Context, commit string, opts git.ResetOptions) error
	CheckoutFiles(ctx context.Context, req *git.CheckoutFilesRequest) error
}

var _ GitWorktree = (*git.Worktree)(nil)

// Service is a subset of the spice.Service interface.
type Service interface {
	RebaseRescue(ctx context.Context, req spice.RebaseRescueRequest) error
}

var _ Service = (*spice.Service)(nil)

// Handler manages automatic stashing and restoration of uncommitted changes.
type Handler struct {
	Log      *silog.Logger // required
	Worktree GitWorktree   // required
	Service  Service       // required
}

// ResetMode specifies how to reset the working tree after stashing.
type ResetMode int

const (
	// ResetHard performs a hard reset to HEAD,
	// discarding all uncommitted changes in the index and working tree.
	//
	// Use this for operations that need a completely clean working tree.
	//
	// This is the default mode.
	ResetHard ResetMode = iota

	// ResetWorktree restores the working tree to match the index,
	// preserving staged changes.
	//
	// This is similar to 'git stash -k'.
	ResetWorktree

	// ResetNone leaves the working tree and index as-is after stashing.
	//
	// Use this for operations that have their own mechanism
	// for dealing with uncommitted changes,
	ResetNone
)

// Options configures autostash behavior.
type Options struct {
	// Message is the stash message to use.
	//
	// If unset, a default message is used.
	Message string

	// Branch is the branch we're operating on.
	//
	// We will check out this branch if we need to restore stashed changes
	// after a failed operation.
	//
	// If unset, the current branch is used.
	Branch string

	// ResetMode specifies how to reset the working tree after stashing.
	//
	// By default, we do a hard reset to ensure a clean working tree.
	ResetMode ResetMode
}

// BeginAutostash starts an autostash session.
//
// It stashes uncommitted changes (if any),
// resets the tree according to the specified mode,
// and returns a function to restore them later.
//
// The returned function is typically called with a defer statement,
// passing a pointer to the error being returned.
// It will restore stashed changes on success,
// or schedule restoration via RebaseRescue on failure.
//
//	cleanup, err := handler.BeginAutostash(ctx, &autostash.Options{...})
//	if err != nil {
//	    return err
//	}
//	defer cleanup(&err)
//
//	// Perform operations...
//	return someOperation()
//
// The autostash-guarded operation may be any operation
// that requires a clean working tree,
// such as a rebase or commit.
//
// If the operation performs a rebase and is interrupted,
// the stashed changes will be restored after the rebase is resolved.
func (h *Handler) BeginAutostash(
	ctx context.Context,
	opts *Options,
) (cleanup func(*error), err error) {
	opts = cmp.Or(opts, &Options{})
	opts.Message = cmp.Or(opts.Message, "git-spice: autostash before operation")

	if opts.Branch == "" {
		currentBranch, err := h.Worktree.CurrentBranch(ctx)
		if err != nil {
			if errors.Is(err, git.ErrDetachedHead) {
				return nil, errors.New("HEAD is detached; cannot determine branch for autostash")
			}
			return nil, fmt.Errorf("determine current branch: %w", err)
		}
		opts.Branch = currentBranch
	}

	stashHash, err := h.Worktree.StashCreate(ctx, opts.Message)
	if err != nil {
		if !errors.Is(err, git.ErrNoChanges) {
			return nil, fmt.Errorf("stash changes: %w", err)
		}

		// No changes to stash.
		// Return a no-op cleanup function.
		return func(*error) {}, nil
	}

	// We created a stash.
	// Reset the working tree according to the mode.
	switch opts.ResetMode {
	case ResetHard:
		if err := h.Worktree.Reset(ctx, "HEAD", git.ResetOptions{
			Mode: git.ResetHard,
		}); err != nil {
			return nil, fmt.Errorf("reset before operation: %w", err)
		}

	case ResetWorktree:
		if err := h.Worktree.CheckoutFiles(ctx, &git.CheckoutFilesRequest{
			Pathspecs: []string{"."},
		}); err != nil {
			return nil, fmt.Errorf("restore working tree before operation: %w", err)
		}

	case ResetNone:
		// Do nothing.

	default:
		panic(fmt.Sprintf("unknown ResetMode: %d", opts.ResetMode))
	}

	return func(errPtr *error) {
		// Provided context may have been canceled or timed out.
		ctx := context.WithoutCancel(ctx)

		if errPtr == nil {
			errPtr = new(error)
		}

		if *errPtr == nil {
			// Operation succeeded: apply the stash.
			if err := h.RestoreAutostash(ctx, stashHash.String()); err != nil {
				// If we couldn't apply the stash on success,
				// return that error.
				*errPtr = err
			}
			return
		}

		// Failure: schedule stash restoration via RebaseRescue.
		*errPtr = h.Service.RebaseRescue(ctx, spice.RebaseRescueRequest{
			Err:     *errPtr,
			Command: []string{"internal", "autostash-pop", stashHash.String()},
			Branch:  opts.Branch,
			Message: fmt.Sprintf("interrupted: restore stashed changes %q", stashHash),
		})
	}, nil
}

// RestoreAutostash tries to apply the stashed changes to the worktree.
// If the operation fails, it pushes the stashed changes
// so that the user can run 'git stash pop' manually.
func (h *Handler) RestoreAutostash(ctx context.Context, stashHash string) error {
	err := h.Worktree.StashApply(ctx, stashHash)
	if err == nil {
		h.Log.Info("Applied autostash")
		return nil
	}

	// If autostash apply fails,
	// log the error, and save the stash for restoration.
	h.Log.Error("Failed to apply autostashed changes", "error", err)
	if err := h.Worktree.StashStore(ctx, git.Hash(stashHash), "git-spice: autostash failed to apply"); err != nil {
		// If even stash store fails, there's nothing we can do.
		// Tell the user to manually recover the stash.
		h.Log.Error("Failed to save autostashed changes", "error", err)
		h.Log.Errorf("You can try recovering them with 'git stash apply %s'", stashHash)
		return errors.New("stashed changes could not be applied or saved")
	}

	h.Log.Error("Your changes are safe in the stash. You can:")
	h.Log.Error("- apply them with 'git stash pop';")
	h.Log.Error("- or drop them with 'git stash drop'")
	return errors.New("autostashed changes could not be applied")
}
