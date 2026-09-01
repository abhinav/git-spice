package review

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/spice/state"
)

// SetThreadResolutionRequest identifies a thread and its desired state.
type SetThreadResolutionRequest struct {
	// Branch identifies the reviewed branch.
	Branch string // required

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
	change, err := h.Service.LookupBranch(ctx, req.Branch)
	if err != nil {
		if errors.Is(err, state.ErrNotExist) {
			return fmt.Errorf("branch not tracked: %s", req.Branch)
		}
		return fmt.Errorf("get branch: %w", err)
	}
	if change.Change == nil {
		return fmt.Errorf(
			"no change request for %s; "+
				"submit the branch first with "+
				"'gs branch submit'",
			req.Branch,
		)
	}

	// ReviewThreadID is opaque.
	// Recover the forge-owned value whose String form the command accepted.
	var threadID forge.ReviewThreadID
	for thread, err := range h.Repository.ListReviewThreads(
		ctx,
		change.Change.ChangeID(),
	) {
		if err != nil {
			return fmt.Errorf("list review threads: %w", err)
		}
		if thread.ID.String() == req.ThreadID {
			threadID = thread.ID
			break
		}
	}
	if threadID == nil {
		return fmt.Errorf("review thread %q not found", req.ThreadID)
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
