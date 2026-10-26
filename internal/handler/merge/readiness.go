package merge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
)

// awaitReady checks readiness only after the forge observes the expected head.
// Readiness must describe the commit being merged,
// rather than the head from before a branch update.
func (e *mergePlanExecutor) awaitReady(
	ctx context.Context,
	item *mergeItem,
) error {
	if err := e.awaitChangeHead(ctx, item); err != nil {
		e.Progress.Event(mergeProgressEvent{
			Kind: mergeProgressForgeHeadFailed,
			Item: item,
		})
		return fmt.Errorf("wait for pushed head: %w", err)
	}

	if err := e.awaitMergeability(ctx, item); err != nil {
		e.Progress.Event(mergeProgressEvent{
			Kind: mergeProgressMergeabilityFailed,
			Item: item,
		})
		return fmt.Errorf("wait for merge readiness: %w", err)
	}
	return nil
}

// awaitChangeHead waits until the forge reports the same change head
// that the merge loop is about to merge.
func (e *mergePlanExecutor) awaitChangeHead(
	ctx context.Context,
	item *mergeItem,
) error {
	const (
		_baseDelay = 10 * time.Second
		_maxDelay  = 30 * time.Second

		// Head visibility is a forge catch-up wait,
		// not a merge readiness policy wait.
		// Keep this bounded separately from --ready-timeout
		// so repository rules and CI get the configured readiness budget.
		_timeout = time.Minute
	)

	return e.awaitChangeHeadWithDelay(
		ctx, item, _timeout, _baseDelay, _maxDelay,
	)
}

func (e *mergePlanExecutor) awaitChangeHeadWithDelay(
	ctx context.Context,
	item *mergeItem,
	timeout, baseDelay, maxDelay time.Duration,
) error {
	if item.headHash == "" {
		return nil
	}
	hashMatches := func(got git.Hash) bool {
		return got != "" &&
			(item.headHash == got ||
				strings.HasPrefix(item.headHash.String(), got.String()) ||
				strings.HasPrefix(got.String(), item.headHash.String()))
	}

	delay := baseDelay
	for attempt := 0; ; attempt++ {
		statuses, err := e.RemoteRepository.ChangeStatuses(
			ctx, []forge.ChangeID{item.changeID},
		)
		if err != nil {
			return fmt.Errorf("query change head: %w", err)
		}
		if len(statuses) == 0 {
			return errors.New("forge returned no change status")
		}
		if hashMatches(statuses[0].HeadHash) {
			return nil
		}

		if timeout == 0 {
			return fmt.Errorf(
				"change head is still %s, want %s",
				statuses[0].HeadHash, item.headHash,
			)
		}
		if attempt == 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}

		e.Progress.Event(mergeProgressEvent{
			Kind: mergeProgressWaitingForForgeHead,
			Item: item,
		})
		if err := sleep(ctx, delay); err != nil {
			return fmt.Errorf("HEAD did not update after %v", timeout)
		}
		delay = min(delay*2, maxDelay)
	}
}

// awaitMergeability polls until the forge reports that the change is
// ready to merge.
// Uses truncated exponential backoff.
func (e *mergePlanExecutor) awaitMergeability(
	ctx context.Context,
	item *mergeItem,
) error {
	const (
		_baseDelay = 10 * time.Second
		_maxDelay  = 30 * time.Second
	)

	return e.awaitMergeabilityWithDelay(
		ctx, item, e.ReadyTimeout, _baseDelay, _maxDelay,
	)
}

func (e *mergePlanExecutor) awaitMergeabilityWithDelay(
	ctx context.Context,
	item *mergeItem,
	timeout, baseDelay, maxDelay time.Duration,
) error {
	must.NotBeNilf(e.ReadinessChecker, "merge: ReadinessChecker is required")

	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	delay := baseDelay
	for attempt := 0; ; attempt++ {
		mergeability, err := e.ReadinessChecker.CheckMergeItemReady(ctx, item)
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("not ready after %v", timeout)
			}
			return fmt.Errorf("check merge readiness: %w", err)
		}
		switch mergeability.State {
		case forge.ChangeMergeabilityReady:
			e.Progress.Event(mergeProgressEvent{
				Kind: mergeProgressMergeabilityReady,
				Item: item,
			})
			return nil
		case forge.ChangeMergeabilityWaiting:
			if timeout == 0 {
				return fmt.Errorf("not ready after %v", timeout)
			}
		case forge.ChangeMergeabilityBlocked:
			return fmt.Errorf("blocked: %s", mergeability.Reason)
		case forge.ChangeMergeabilityUnknown:
			return errors.New("unknown state")
		case forge.ChangeMergeabilityUnsupported:
			return errors.New("unknown state")
		default:
			return fmt.Errorf("unknown state: %v", mergeability.State)
		}
		e.Progress.Event(mergeProgressEvent{
			Kind: mergeProgressWaitingForMergeability,
			Item: item,
		})
		if err := sleep(ctx, delay); err != nil {
			return fmt.Errorf("not ready after %v", timeout)
		}
		delay = min(delay*2, maxDelay)
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
