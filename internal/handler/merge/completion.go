package merge

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.abhg.dev/gs/internal/forge"
)

// awaitMerged polls until the given change shows as merged.
// Uses exponential backoff starting at 500ms, capped at 8s.
func (e *mergePlanExecutor) awaitMerged(
	ctx context.Context, item *mergeItem,
) error {
	const (
		_initialDelay = 500 * time.Millisecond
		_maxDelay     = 8 * time.Second
	)

	ctx, cancel := context.WithTimeout(ctx, e.MergeTimeout)
	defer cancel()

	// TODO: This only waits for the immediate change to reach
	// the merged state.
	// Server-side merge queues and richer merge workflows
	// need a more expressive wait state.
	delay := _initialDelay
	for {
		statuses, err := e.RemoteRepository.ChangeStatuses(
			ctx, []forge.ChangeID{item.changeID},
		)
		if err != nil {
			return fmt.Errorf("poll state: %w", err)
		}

		if statuses[0].State == forge.ChangeMerged {
			return nil
		}

		e.Progress.Event(mergeProgressEvent{
			Kind: mergeProgressWaitingForMerge,
			Item: item,
		})
		if err := sleep(ctx, delay); err != nil {
			return errors.New("timed out waiting for merge")
		}

		delay = min(delay*2, _maxDelay)
	}
}
