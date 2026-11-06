package merge

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.abhg.dev/gs/internal/forge"
)

// awaitMerged waits for every supplied item's change to reach merged state.
// If operation is non-nil, it is the merge request covering all of those items;
// wait for its acceptance before checking their individual change states.
// A nil operation means the merge was requested without an asynchronous handle.
// Acceptance and merged-state checks share one timeout and backoff schedule.
func (e *mergePlanExecutor) awaitMerged(
	ctx context.Context,
	items []*mergeItem,
	operation forge.MergeOperation,
) error {
	const (
		_initialDelay = 500 * time.Millisecond
		_maxDelay     = 8 * time.Second
	)

	ctx, cancel := context.WithTimeout(ctx, e.MergeTimeout)
	defer cancel()

	completion := mergeCompletion{
		repository: e.RemoteRepository,
		items:      items,
		operation:  operation,
	}
	delay := _initialDelay
	for {
		merged, err := completion.check(ctx)
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return errors.New("timed out waiting for merge")
			}
			return err
		}
		if merged {
			return nil
		}

		for _, item := range items {
			e.Progress.Event(mergeProgressEvent{Kind: mergeProgressWaitingForMerge, Item: item})
		}
		if err := sleep(ctx, delay); err != nil {
			return errors.New("timed out waiting for merge")
		}
		delay = min(delay*2, _maxDelay)
	}
}

// mergeCompletion tracks the two phases of one requested merge.
// Provider acceptance can precede visible merged states;
// only the latter permits local synchronization and upstack preparation.
type mergeCompletion struct {
	repository forge.Repository // required
	items      []*mergeItem     // required

	// operation is nil when there is no acceptance phase or it has completed.
	// Once accepted, later polls query only the change states.
	operation forge.MergeOperation
}

func (c *mergeCompletion) check(ctx context.Context) (bool, error) {
	if c.operation != nil {
		status, err := c.operation.Status(ctx)
		if err != nil {
			return false, fmt.Errorf("poll merge operation: %w", err)
		}
		switch status {
		case forge.MergeOperationPending:
			return false, nil
		case forge.MergeOperationAccepted:
			c.operation = nil
		default:
			return false, fmt.Errorf("poll merge operation: unknown status %v", status)
		}
	}

	changeIDs := make([]forge.ChangeID, len(c.items))
	for idx, item := range c.items {
		changeIDs[idx] = item.changeID
	}
	statuses, err := c.repository.ChangeStatuses(ctx, changeIDs)
	if err != nil {
		return false, fmt.Errorf("poll state: %w", err)
	}
	allMerged := true
	for idx, status := range statuses {
		switch status.State {
		case forge.ChangeMerged:
		case forge.ChangeOpen:
			allMerged = false
		case forge.ChangeClosed:
			return false, fmt.Errorf("%s: change closed without merging", c.items[idx].branch)
		default:
			return false, fmt.Errorf("%s: unknown change state %v", c.items[idx].branch, status.State)
		}
	}
	return allMerged, nil
}
