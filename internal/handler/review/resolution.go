package review

import (
	"context"
	"fmt"
)

// SetThreadResolutionRequest identifies a thread and its desired state.
type SetThreadResolutionRequest struct {
	// Branch identifies the reviewed branch.
	// The current branch is used when Branch is empty.
	Branch string

	// ThreadID is the command-line representation of the forge thread ID.
	ThreadID string // required

	// Resolved is the desired thread resolution state.
	Resolved bool
}

// SetThreadResolution changes whether a remote review thread is resolved.
func (h *ThreadHandler) SetThreadResolution(
	ctx context.Context,
	req *SetThreadResolutionRequest,
) error {
	branch, err := resolveBranch(ctx, h.Worktree, req.Branch)
	if err != nil {
		return err
	}
	change, err := lookupReviewChange(ctx, h.Service, branch)
	if err != nil {
		return err
	}
	threadID, err := findReviewThreadID(
		ctx,
		h.Repository,
		change.Change.ChangeID(),
		req.ThreadID,
	)
	if err != nil {
		return err
	}

	if req.Resolved {
		if err := h.Resolver.ResolveReviewThread(ctx, threadID); err != nil {
			return fmt.Errorf("resolve thread: %w", err)
		}
		h.Log.Infof("Resolved thread %s.", req.ThreadID)
		return nil
	}
	if err := h.Resolver.UnresolveReviewThread(ctx, threadID); err != nil {
		return fmt.Errorf("reopen thread: %w", err)
	}
	h.Log.Infof("Reopened thread %s.", req.ThreadID)
	return nil
}
