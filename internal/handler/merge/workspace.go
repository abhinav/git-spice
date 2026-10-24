package merge

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/handler/submit"
	"go.abhg.dev/gs/internal/handler/sync"
	"go.abhg.dev/gs/internal/spice"
)

// mergeWorkspace owns local preparation and trunk synchronization
// for one merge command.
// The scheduler serializes preparation with trunk synchronization.
type mergeWorkspace struct {
	Repository GitRepository  // required
	Service    Service        // required
	Restack    RestackHandler // required
	Submit     SubmitHandler  // required
	Sync       SyncHandler    // required
	Progress   mergeProgress  // required
	Trunk      string         // required
}

// PrepareChange updates an ordinary item after its downstack has merged.
// Items originally based on trunk already carry their preflight head hash.
func (w *mergeWorkspace) PrepareChange(ctx context.Context, item *mergeItem) error {
	if item.base == w.Trunk {
		return nil
	}
	if err := w.prepare(ctx, item); err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	return nil
}

// SyncTrunk reconciles merges with the local branch forest.
func (w *mergeWorkspace) SyncTrunk(ctx context.Context) error {
	if err := w.Sync.SyncTrunk(ctx, &sync.TrunkOptions{
		ClosedChanges: sync.ClosedChangesIgnore,
	}); err != nil {
		return fmt.Errorf("sync trunk: %w", err)
	}
	return nil
}

// Queue dependencies determine when preparation may start;
// resolving the head last captures any commit produced by restacking.
func (w *mergeWorkspace) prepare(ctx context.Context, item *mergeItem) (err error) {
	w.Progress.Event(mergeProgressEvent{Kind: mergeProgressPreparing, Item: item})
	defer func() {
		if err != nil {
			w.Progress.Event(mergeProgressEvent{Kind: mergeProgressPrepareFailed, Item: item})
		}
	}()

	if err := w.Service.VerifyRestacked(ctx, item.branch); err != nil {
		if _, ok := errors.AsType[*spice.BranchNeedsRestackError](err); !ok {
			return fmt.Errorf("verify restacked: %w", err)
		}
		if err := w.Restack.RestackBranch(ctx, &restack.BranchRequest{
			Branch: item.branch,
		}); err != nil {
			return fmt.Errorf("restack branch: %w", err)
		}
		if err := w.Submit.Submit(ctx, &submit.Request{
			Branch: item.branch,
			Options: &submit.Options{
				// Publish follows the normal change-update path;
				// UpdateOnly prevents creating a new change request.
				Publish:    true,
				UpdateOnly: new(true),
			},
		}); err != nil {
			return fmt.Errorf("submit branch update: %w", err)
		}
	}
	head, err := w.Repository.PeelToCommit(ctx, item.branch)
	if err != nil {
		return fmt.Errorf("resolve updated head: %w", err)
	}
	item.headHash = head
	return nil
}
