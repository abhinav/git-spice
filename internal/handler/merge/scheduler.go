package merge

import (
	"context"
	"fmt"
	"time"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/handler/sync"
	"go.abhg.dev/gs/internal/mergequeue"
)

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

	Progress         mergeProgress    // required
	MergeRequester   mergeRequester   // required
	ReadinessChecker readinessChecker // required

	Trunk        string            // required
	ReadyTimeout time.Duration     // required
	MergeTimeout time.Duration     // required
	Method       forge.MergeMethod // required
	FailFast     bool
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
	inQueue := make(map[string]struct{}, len(plan))
	for _, item := range plan {
		inQueue[item.branch] = struct{}{}
	}

	items := make([]mergequeue.Item, 0, len(plan))
	for _, item := range plan {
		var parent string
		if _, ok := inQueue[item.base]; item.base != e.Trunk && ok {
			parent = item.base
		}
		items = append(items, &mergeQueueItem{
			mergeItem: item,
			executor:  e,
			parent:    parent,
		})
	}

	barrier := func(ctx context.Context) error {
		// SyncTrunk updates trunk,
		// deletes merged branches,
		// and retargets their upstacks.
		if err := e.Sync.SyncTrunk(ctx, &sync.TrunkOptions{
			ClosedChanges: sync.ClosedChangesIgnore,
		}); err != nil {
			return fmt.Errorf("sync trunk: %w", err)
		}
		return nil
	}

	scheduler, err := mergequeue.New(items, &mergequeue.Options{
		FailFast: e.FailFast,
		Barrier:  barrier,
		Observer: &mergeQueueObserver{
			progress: e.Progress,
		},
	})
	if err != nil {
		return fmt.Errorf("build merge queue: %w", err)
	}
	return scheduler.Run(ctx)
}

var _ mergequeue.Item = (*mergeQueueItem)(nil)

type mergeQueueItem struct {
	*mergeItem

	executor *mergePlanExecutor

	// parent is the queue-local branch dependency.
	// Empty means the dependency is already satisfied outside this queue.
	parent string
}

func (i *mergeQueueItem) ID() string {
	return i.branch
}

func (i *mergeQueueItem) Parent() string {
	return i.parent
}

func (i *mergeQueueItem) Prepare(ctx context.Context) error {
	return i.executor.prepareItem(ctx, i.mergeItem)
}

func (i *mergeQueueItem) Run(ctx context.Context) error {
	return i.executor.mergeItem(ctx, i.mergeItem)
}

func (e *mergePlanExecutor) prepareItem(
	ctx context.Context,
	item *mergeItem,
) error {
	if item.base != e.Trunk {
		// Non-trunk items must be restacked and submitted
		// before the forge merge request.
		// Queue parentage determines when this happens;
		// the original local base only tells us whether preparation is needed.
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
	return nil
}

func (e *mergePlanExecutor) mergeItem(
	ctx context.Context,
	item *mergeItem,
) error {
	// The forge may lag a branch update sent before the merge loop
	// or during item preparation.
	// Merge readiness is meaningful only after the forge reports
	// the head this run will pass to the merge request.
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

	e.Progress.Event(mergeProgressEvent{
		Kind: mergeProgressMerging,
		Item: item,
		URL:  item.mergeURL,
	})
	if err := e.MergeRequester.RequestMerge(ctx, item); err != nil {
		e.Progress.Event(mergeProgressEvent{
			Kind: mergeProgressMergeFailed,
			Item: item,
		})
		return fmt.Errorf("merge: %w", err)
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
	return nil
}

// mergeQueueObserver adapts scheduler decisions
// back into merge progress events.
type mergeQueueObserver struct {
	progress mergeProgress
}

func (o *mergeQueueObserver) Prepared(mergequeue.Item) {}

func (o *mergeQueueObserver) Done(mergequeue.Item) {}

func (o *mergeQueueObserver) Failed(queueItem mergequeue.Item, _ error) {
	item := queueItem.(*mergeQueueItem).mergeItem
	o.progress.Event(mergeProgressEvent{
		Kind: mergeProgressFailed,
		Item: item,
	})
}

func (o *mergeQueueObserver) Skipped(
	queueItem mergequeue.Item,
	_ mergequeue.SkipReason,
) {
	item := queueItem.(*mergeQueueItem).mergeItem
	o.progress.Event(mergeProgressEvent{
		Kind: mergeProgressSkipped,
		Item: item,
	})
}
