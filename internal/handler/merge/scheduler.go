package merge

import (
	"context"
	"fmt"
	"time"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/handler/sync"
	"go.abhg.dev/gs/internal/mergequeue"
)

var _ mergequeue.Executor[*mergeItem] = (*mergePlanExecutor)(nil)

// mergePlanExecutor runs the merge loop after preflight checks complete.
//
// It intentionally has no logger.
// Merge-loop status must be reported through progress events
// so terminal and non-terminal output stay in one policy boundary.
type mergePlanExecutor struct {
	RemoteRepository forge.Repository // required
	Repository       GitRepository    // required

	Service Service        // required
	Restack RestackHandler // required
	Submit  SubmitHandler  // required
	Sync    SyncHandler    // required

	Progress mergeProgress // required

	Trunk                 string            // required
	MergeReadinessTimeout time.Duration     // required
	Method                forge.MergeMethod // required
	NoWait                bool
	FailFast              bool
}

// Execute runs the merge queue over the supplied plan items.
//
// Execute is the boundary between preflight planning
// and the merge-loop scheduler.
// It adapts merge items into queue items,
// then lets mergequeue.Scheduler decide readiness,
// failure propagation,
// and skip propagation.
func (e *mergePlanExecutor) Execute(
	ctx context.Context,
	plan []*mergeItem,
) error {
	items := make([]mergequeue.Item[*mergeItem], 0, len(plan))
	for _, item := range plan {
		items = append(items, mergequeue.Item[*mergeItem]{
			ID:     item.branch,
			BaseID: item.base,
			Value:  item,
		})
	}

	scheduler, err := mergequeue.New(items, &mergequeue.Options[*mergeItem]{
		FailFast: e.FailFast,
		Observer: &mergeQueueObserver{
			progress: e.Progress,
		},
	})
	if err != nil {
		return fmt.Errorf("build merge queue: %w", err)
	}
	return scheduler.Run(ctx, e)
}

// Merge implements mergequeue.Executor.
func (e *mergePlanExecutor) Merge(ctx context.Context, item *mergeItem) error {
	if item.base != e.Trunk {
		// Non-trunk items were based on another item in this queue
		// when the plan was built.
		// After that base merges,
		// the item must be restacked,
		// submitted,
		// and resolved to its new head before the forge merge request.
		e.Progress.Event(mergeProgressEvent{
			Kind: mergeProgressPreparing,
			Item: item,
		})
		if err := e.prepareForMerge(ctx, item); err != nil {
			e.Progress.Event(mergeProgressEvent{
				Kind: mergeProgressPrepareFailed,
				Item: item,
			})
			return fmt.Errorf("prepare: %w", err)
		}
	}
	return e.mergeOne(ctx, item)
}

func (e *mergePlanExecutor) mergeOne(
	ctx context.Context,
	item *mergeItem,
) error {
	// The forge will only merge a change that targets trunk.
	// Re-check immediately before each merge because prior queue items
	// and repo sync may have changed the server-side base.
	change, err := e.ensureTargetsTrunk(ctx, item)
	if err != nil {
		e.Progress.Event(mergeProgressEvent{
			Kind: mergeProgressRetargetFailed,
			Item: item,
		})
		return fmt.Errorf("ensure targets trunk: %w", err)
	}

	// Merge readiness is checked after retargeting and restacking
	// so each merge waits on the exact change being merged.
	if err := e.awaitChangeHead(ctx, item, change); err != nil {
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

	e.Progress.Event(mergeProgressEvent{
		Kind: mergeProgressMerging,
		Item: item,
		URL:  change.URL,
	})
	if err := e.RemoteRepository.MergeChange(
		ctx, item.changeID,
		forge.MergeChangeOptions{
			Method:   e.Method,
			HeadHash: item.headHash,
		},
	); err != nil {
		e.Progress.Event(mergeProgressEvent{
			Kind: mergeProgressMergeFailed,
			Item: item,
		})
		return fmt.Errorf("merge: %w", err)
	}

	if e.NoWait {
		e.Progress.Event(mergeProgressEvent{
			Kind: mergeProgressMergeRequested,
			Item: item,
		})
		return nil
	}

	// Wait until the forge reports the merge.
	e.Progress.Event(mergeProgressEvent{
		Kind: mergeProgressWaitingForMerge,
		Item: item,
	})
	if err := e.awaitMerged(ctx, item); err != nil {
		e.Progress.Event(mergeProgressEvent{
			Kind: mergeProgressMergeIncomplete,
			Item: item,
		})
		return fmt.Errorf("await merge: %w", err)
	}
	e.Progress.Event(mergeProgressEvent{
		Kind: mergeProgressMerged,
		Item: item,
	})

	// SyncTrunk updates trunk,
	// deletes merged branches,
	// and retargets their upstacks.
	if err := e.Sync.SyncTrunk(ctx, &sync.TrunkOptions{
		ClosedChanges: sync.ClosedChangesIgnore,
	}); err != nil {
		e.Progress.Event(mergeProgressEvent{
			Kind: mergeProgressSyncFailed,
			Item: item,
		})
		return fmt.Errorf("sync trunk: %w", err)
	}
	return nil
}

// mergeQueueObserver adapts scheduler decisions
// back into merge progress events.
type mergeQueueObserver struct {
	progress mergeProgress
}

func (o *mergeQueueObserver) Failed(item *mergeItem, _ error) {
	o.progress.Event(mergeProgressEvent{
		Kind: mergeProgressFailed,
		Item: item,
	})
}

func (o *mergeQueueObserver) Skipped(
	item *mergeItem,
	_ mergequeue.SkipReason,
) {
	o.progress.Event(mergeProgressEvent{
		Kind: mergeProgressSkipped,
		Item: item,
	})
}
