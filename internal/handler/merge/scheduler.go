package merge

import (
	"context"
	"fmt"
	"time"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/graph"
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

	// MergeRangePlans identifies the disjoint selected paths the forge can
	// merge atomically. Selected changes omitted from the plans remain ordinary
	// queue items.
	MergeRangePlans []forge.MergeRangePlan

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
		gate:       make(chan struct{}, 1),
	}
	items, err := e.mergeQueueItems(plan)
	if err != nil {
		return fmt.Errorf("build merge queue items: %w", err)
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

// mergeQueueItems overlays provider-selected atomic ranges onto the complete
// user-selected merge forest. Changes omitted from the provider plans remain
// ordinary queue items, so native-stack limitations never remove work from the
// user's merge request.
func (e *mergePlanExecutor) mergeQueueItems(
	plan []*mergeItem,
) ([]mergequeue.Item, error) {
	byBranch := make(map[string]*mergeItem, len(plan))
	byChange := make(map[string]*mergeItem, len(plan))
	for _, item := range plan {
		if _, ok := byBranch[item.branch]; ok {
			return nil, fmt.Errorf("duplicate branch %q", item.branch)
		}
		byBranch[item.branch] = item

		changeKey := item.changeID.String()
		if previous, ok := byChange[changeKey]; ok {
			return nil, fmt.Errorf(
				"branches %q and %q track the same change %v",
				previous.branch,
				item.branch,
				item.changeID,
			)
		}
		byChange[changeKey] = item
	}

	ordered, err := graph.Toposort(plan,
		func(item *mergeItem) (*mergeItem, bool) {
			base, ok := byBranch[item.base]
			return base, ok
		})
	if err != nil {
		return nil, err
	}

	// A native range is one scheduler item, even when an ordinary branch
	// forks from its middle. For branches 1 -> 2 -> {3, 4}, native [1, 2, 4]
	// produces these queue dependencies (arrows point upstack):
	//
	//     [1, 2, 4] --> [3]
	//
	// The range's top branch names the whole item, so branch 3 waits for
	// queue item 4. ownerByBranch maps each base to its owning queue item.
	// The forge contract supplies non-empty, disjoint, bottom-to-top ranges.
	ownerByBranch := make(map[string]mergeQueueItem, len(plan))
	for _, nativePlan := range e.MergeRangePlans {
		changeIDs := nativePlan.Changes()
		items := make([]*mergeItem, len(changeIDs))
		for idx, changeID := range changeIDs {
			items[idx] = byChange[changeID.String()]
		}
		queueItem := &rangeMergeQueueItem{
			items:    items,
			plan:     nativePlan,
			executor: e,
		}
		for _, item := range items {
			ownerByBranch[item.branch] = queueItem
		}
	}

	queueItems := make([]mergequeue.Item, 0, len(plan))
	for _, item := range ordered {
		queueItem, ok := ownerByBranch[item.branch]
		if !ok {
			queueItem = &changeMergeQueueItem{item: item, executor: e}
			ownerByBranch[item.branch] = queueItem
		}

		// Emit a range at its bottom member, preserving bottom-up input order.
		// Later members already belong to that same queue item.
		bottom := queueItem.changes()[0]
		if item != bottom {
			continue
		}
		if baseOwner, ok := ownerByBranch[bottom.base]; ok {
			queueItem.setParent(baseOwner.ID())
		}
		queueItems = append(queueItems, queueItem)
	}
	return queueItems, nil
}

// mergeQueueItem groups selected changes under one scheduler identity.
// Construction assigns the item's dependency; the observer projects its
// outcome back onto the member changes, excluding any completed prefix.
type mergeQueueItem interface {
	mergequeue.Item

	setParent(string)
	changes() []*mergeItem
	unmergedChanges() []*mergeItem
}

var (
	_ mergeQueueItem = (*changeMergeQueueItem)(nil)
	_ mergeQueueItem = (*rangeMergeQueueItem)(nil)
)

// mergeQueueObserver adapts scheduler decisions
// back into merge progress events.
type mergeQueueObserver struct {
	progress mergeProgress
}

func (o *mergeQueueObserver) Prepared(mergequeue.Item) {}

func (o *mergeQueueObserver) Done(mergequeue.Item) {}

func (o *mergeQueueObserver) Failed(queueItem mergequeue.Item, _ error) {
	item := queueItem.(mergeQueueItem)
	for _, change := range item.unmergedChanges() {
		o.progress.Event(mergeProgressEvent{
			Kind: mergeProgressFailed,
			Item: change,
		})
	}
}

func (o *mergeQueueObserver) Skipped(
	queueItem mergequeue.Item,
	_ mergequeue.SkipReason,
) {
	for _, item := range queueItem.(mergeQueueItem).changes() {
		o.progress.Event(mergeProgressEvent{
			Kind: mergeProgressSkipped,
			Item: item,
		})
	}
}
