package merge

import (
	"context"
	"fmt"
	"time"

	"go.abhg.dev/gs/internal/forge"
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

	workspace *mergeWorkspace
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
	e.workspace = &mergeWorkspace{
		Repository: e.Repository,
		Service:    e.Service,
		Restack:    e.Restack,
		Submit:     e.Submit,
		Sync:       e.Sync,
		Progress:   e.Progress,
		Trunk:      e.Trunk,
	}
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
		items = append(items, &changeMergeQueueItem{
			item:     item,
			executor: e,
			parent:   parent,
		})
	}

	scheduler, err := mergequeue.New(items, &mergequeue.Options{
		FailFast: e.FailFast,
		Barrier:  e.workspace.SyncTrunk,
		Observer: &mergeQueueObserver{
			progress: e.Progress,
		},
	})
	if err != nil {
		return fmt.Errorf("build merge queue: %w", err)
	}
	return scheduler.Run(ctx)
}

// mergeQueueObserver adapts scheduler decisions
// back into merge progress events.
type mergeQueueObserver struct {
	progress mergeProgress
}

func (o *mergeQueueObserver) Prepared(mergequeue.Item) {}

func (o *mergeQueueObserver) Done(mergequeue.Item) {}

func (o *mergeQueueObserver) Failed(queueItem mergequeue.Item, _ error) {
	item := queueItem.(*changeMergeQueueItem).item
	o.progress.Event(mergeProgressEvent{
		Kind: mergeProgressFailed,
		Item: item,
	})
}

func (o *mergeQueueObserver) Skipped(
	queueItem mergequeue.Item,
	_ mergequeue.SkipReason,
) {
	item := queueItem.(*changeMergeQueueItem).item
	o.progress.Event(mergeProgressEvent{
		Kind: mergeProgressSkipped,
		Item: item,
	})
}
