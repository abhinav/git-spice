package merge

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/handler/submit"
	"go.abhg.dev/gs/internal/handler/sync"
	"go.abhg.dev/gs/internal/mergequeue"
	"go.abhg.dev/gs/internal/spice"
)

// mergeWorkspace owns local repository updates for one merge command.
// All queue items and the synchronization barrier share this workspace.
// Remote merge requests and readiness/completion waits do not hold its gate.
type mergeWorkspace struct {
	Repository GitRepository  // required
	Service    Service        // required
	Restack    RestackHandler // required
	Submit     SubmitHandler  // required
	Sync       SyncHandler    // required
	Progress   mergeProgress  // required
	Trunk      string         // required

	// The scheduler serializes Prepare and barriers, but runs merges concurrently.
	// Native fallback also synchronizes and restacks from Run,
	// so it must share admission with ordinary preparation and barriers.
	// gate admits one local operation at a time across all three paths.
	// The executor initializes it with capacity one before scheduling starts.
	gate chan struct{} // required
}

// PrepareChange updates an ordinary item after its downstack has merged.
// Items originally based on trunk already carry their preflight head hash.
func (w *mergeWorkspace) PrepareChange(ctx context.Context, item *mergeItem) error {
	if item.base == w.Trunk {
		return nil
	}
	release, err := w.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	if err := w.prepare(ctx, item); err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	return nil
}

// PrepareRange aligns every member and captures its resulting head commit.
// Unlike an ordinary item, a range must also verify its bottom member:
// the provider requires the entire range to be restacked before merging it.
func (w *mergeWorkspace) PrepareRange(ctx context.Context, items []*mergeItem) ([]forge.ChangeHead, error) {
	release, err := w.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	heads := make([]forge.ChangeHead, len(items))
	for idx, item := range items {
		if err := w.prepare(ctx, item); err != nil {
			return nil, fmt.Errorf("prepare %q: %w", item.branch, err)
		}
		heads[idx] = forge.ChangeHead{Change: item.changeID, HeadHash: item.headHash}
	}
	return heads, nil
}

// SyncTrunk reconciles merges with the local branch forest.
func (w *mergeWorkspace) SyncTrunk(ctx context.Context) error {
	release, err := w.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	return w.syncTrunk(ctx)
}

// SyncAndPrepare advances to the next individual merge during range fallback.
// It holds the gate across both operations so a concurrent barrier or item
// cannot change the local forest between synchronization and preparation.
func (w *mergeWorkspace) SyncAndPrepare(ctx context.Context, item *mergeItem) error {
	release, err := w.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	if err := w.syncTrunk(ctx); err != nil {
		// This is the same queue-wide synchronization as the scheduler barrier,
		// even though fallback performs it before its queue item finishes.
		return &mergequeue.BarrierError{Err: err}
	}
	if err := w.prepare(ctx, item); err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	return nil
}

// acquire serializes local effects across Prepare, concurrent Run fallbacks,
// and the scheduler barrier. Waiting for admission is cancelable;
// a canceled waiter can leave without starting another local mutation.
func (w *mergeWorkspace) acquire(ctx context.Context) (release func(), err error) {
	select {
	case w.gate <- struct{}{}:
		return func() { <-w.gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// syncTrunk requires ownership of the workspace gate.
func (w *mergeWorkspace) syncTrunk(ctx context.Context) error {
	if err := w.Sync.SyncTrunk(ctx, &sync.TrunkOptions{
		ClosedChanges: sync.ClosedChangesIgnore,
	}); err != nil {
		return fmt.Errorf("sync trunk: %w", err)
	}
	return nil
}

// prepare requires ownership of the workspace gate.
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
