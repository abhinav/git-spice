package merge

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/graph"
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

	// MergeRangePlans identifies the disjoint selected paths the forge can
	// merge atomically. Selected changes omitted from the plans remain ordinary
	// queue items.
	MergeRangePlans []forge.MergeRangePlan

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
	items, err := e.mergeQueueItems(plan)
	if err != nil {
		return fmt.Errorf("build merge queue items: %w", err)
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

	// Contract each provider plan into one scheduler item. ownerByBranch lets
	// later ordinary changes depend on the whole atomic range containing their
	// immediate base rather than on a branch ID that no longer exists in the
	// queue.
	ownerByBranch := make(map[string]mergeQueueProgressItem, len(plan))
	for planIndex, nativePlan := range e.MergeRangePlans {
		if nativePlan == nil {
			return nil, fmt.Errorf("native merge plan %d is nil", planIndex)
		}

		changeIDs := nativePlan.Changes()
		if len(changeIDs) == 0 {
			return nil, fmt.Errorf("native merge plan %d is empty", planIndex)
		}

		items := make([]*mergeItem, len(changeIDs))
		for i, changeID := range changeIDs {
			item, ok := byChange[changeID.String()]
			if !ok {
				return nil, fmt.Errorf(
					"native merge plan %d contains unselected change %v",
					planIndex,
					changeID,
				)
			}
			if _, claimed := ownerByBranch[item.branch]; claimed {
				return nil, fmt.Errorf(
					"native merge plan %d overlaps at change %v",
					planIndex,
					changeID,
				)
			}
			if i > 0 && item.base != items[i-1].branch {
				return nil, fmt.Errorf(
					"native merge plan %d change %v has base branch %q, want %q",
					planIndex,
					changeID,
					item.base,
					items[i-1].branch,
				)
			}
			items[i] = item
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

	for _, item := range ordered {
		if _, planned := ownerByBranch[item.branch]; planned {
			continue
		}
		ownerByBranch[item.branch] = &changeMergeQueueItem{
			item:     item,
			executor: e,
		}
	}

	queueItems := make([]mergequeue.Item, 0, len(plan))
	added := make(map[mergeQueueProgressItem]struct{}, len(plan))
	for _, item := range ordered {
		queueItem := ownerByBranch[item.branch]
		if _, ok := added[queueItem]; ok {
			continue
		}
		added[queueItem] = struct{}{}

		bottom := queueItem.changes()[0]
		if baseOwner, ok := ownerByBranch[bottom.base]; ok {
			queueItem.setParent(baseOwner.ID())
		}
		queueItems = append(queueItems, queueItem)
	}
	return queueItems, nil
}

// mergeQueueProgressItem exposes the change-level outcome represented by one
// scheduler item. The observer uses it to translate a range failure or skip
// back into progress for each affected change.
type mergeQueueProgressItem interface {
	mergequeue.Item

	setParent(string)
	changes() []*mergeItem
	unmergedChanges() []*mergeItem
}

var (
	_ mergeQueueProgressItem = (*changeMergeQueueItem)(nil)
	_ mergeQueueProgressItem = (*rangeMergeQueueItem)(nil)
)

// changeMergeQueueItem preserves the ordinary per-change merge path for a
// selected change that the forge omitted from its native plans.
type changeMergeQueueItem struct {
	item *mergeItem

	executor *mergePlanExecutor
	parent   string
}

func (i *changeMergeQueueItem) ID() string {
	return i.item.branch
}

func (i *changeMergeQueueItem) Parent() string {
	return i.parent
}

func (i *changeMergeQueueItem) setParent(parent string) {
	i.parent = parent
}

func (i *changeMergeQueueItem) Prepare(ctx context.Context) error {
	return i.executor.prepareItem(ctx, i.item)
}

func (i *changeMergeQueueItem) Run(ctx context.Context) error {
	return i.executor.mergeItem(ctx, i.item)
}

func (i *changeMergeQueueItem) changes() []*mergeItem {
	return []*mergeItem{i.item}
}

func (i *changeMergeQueueItem) unmergedChanges() []*mergeItem {
	return []*mergeItem{i.item}
}

// rangeMergeQueueItem owns one linear range merge,
// including the prepared range request
// and partial completion if the repository requires ordinary fallback.
type rangeMergeQueueItem struct {
	items []*mergeItem         // bottom-to-top
	plan  forge.MergeRangePlan // required

	executor *mergePlanExecutor
	parent   string

	// Prepare captures the request after aligning every item; Run consumes it.
	request forge.MergeRangeRequest

	// completed counts bottom-most items merged before Run returned.
	completed int
}

func (i *rangeMergeQueueItem) ID() string {
	return i.items[len(i.items)-1].branch
}

func (i *rangeMergeQueueItem) Parent() string {
	return i.parent
}

func (i *rangeMergeQueueItem) setParent(parent string) {
	i.parent = parent
}

func (i *rangeMergeQueueItem) Prepare(ctx context.Context) error {
	request, err := i.executor.prepareMergeRange(ctx, i.items)
	if err != nil {
		return err
	}
	i.request = request
	return nil
}

func (i *rangeMergeQueueItem) Run(ctx context.Context) error {
	completed, err := i.executor.mergePreparedRange(
		ctx,
		i.plan,
		i.items,
		i.request,
	)
	i.completed = completed
	return err
}

func (i *rangeMergeQueueItem) changes() []*mergeItem {
	return i.items
}

func (i *rangeMergeQueueItem) unmergedChanges() []*mergeItem {
	return i.items[i.completed:]
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

// prepareMergeRange aligns every member with its current base, then snapshots
// the provider-facing branch path after any restack and submit operations.
func (e *mergePlanExecutor) prepareMergeRange(
	ctx context.Context,
	items []*mergeItem,
) (forge.MergeRangeRequest, error) {
	for _, item := range items {
		e.Progress.Event(mergeProgressEvent{
			Kind: mergeProgressPreparing,
			Item: item,
		})
		if err := e.prepareForMerge(ctx, item); err != nil {
			e.Progress.Event(mergeProgressEvent{
				Kind: mergeProgressPrepareFailed,
				Item: item,
			})
			return forge.MergeRangeRequest{}, fmt.Errorf(
				"prepare %q: %w",
				item.branch,
				err,
			)
		}
	}

	// Preparation may restack branches or change their published metadata.
	// Reload the graph before capturing the provider-facing path.
	branchGraph, err := e.Service.BranchGraph(ctx, nil)
	if err != nil {
		return forge.MergeRangeRequest{}, fmt.Errorf(
			"refresh branch graph: %w",
			err,
		)
	}

	changes := make([]forge.MergeRangeChange, 0, len(items))
	for idx, item := range items {
		branch, ok := branchGraph.Lookup(item.branch)
		if !ok {
			return forge.MergeRangeRequest{}, fmt.Errorf(
				"branch %q is no longer tracked",
				item.branch,
			)
		}
		if branch.Change == nil ||
			branch.Change.ChangeID().String() != item.changeID.String() {
			return forge.MergeRangeRequest{}, fmt.Errorf(
				"branch %q no longer tracks change %v",
				item.branch,
				item.changeID,
			)
		}
		if idx > 0 && branch.Base != items[idx-1].branch {
			return forge.MergeRangeRequest{}, fmt.Errorf(
				"branch %q now has base %q, want %q",
				item.branch,
				branch.Base,
				items[idx-1].branch,
			)
		}
		if branch.Head != item.headHash {
			return forge.MergeRangeRequest{}, fmt.Errorf(
				"branch %q head changed to %s after preparation, expected %s",
				item.branch,
				branch.Head,
				item.headHash,
			)
		}

		base := branch.Base
		if baseBranch, ok := branchGraph.Lookup(base); ok {
			base = cmp.Or(baseBranch.UpstreamBranch, baseBranch.Name)
		}
		changes = append(changes, forge.MergeRangeChange{
			Change:   item.changeID,
			Base:     base,
			Head:     cmp.Or(branch.UpstreamBranch, branch.Name),
			HeadHash: item.headHash,
		})
	}

	return forge.MergeRangeRequest{
		Changes: changes,
		Method:  e.Method,
	}, nil
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
	return e.requestMergeItem(ctx, item)
}

func (e *mergePlanExecutor) requestMergeItem(
	ctx context.Context,
	item *mergeItem,
) error {
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
	items := []*mergeItem{item}
	if err := e.awaitMerged(
		ctx,
		items,
		newChangeCompletionChecker(e.RemoteRepository, items),
	); err != nil {
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

// mergePreparedRange waits for every prepared member and requests one range
// merge. ErrUnsupported resumes the ordinary bottom-up sequence.
func (e *mergePlanExecutor) mergePreparedRange(
	ctx context.Context,
	plan forge.MergeRangePlan,
	items []*mergeItem,
	request forge.MergeRangeRequest,
) (int, error) {
	// A range can merge only when every member has reached the same remote
	// head that was captured during preparation and is independently ready.
	for _, item := range items {
		if err := e.awaitChangeHead(ctx, item); err != nil {
			e.Progress.Event(mergeProgressEvent{
				Kind: mergeProgressForgeHeadFailed,
				Item: item,
			})
			return 0, fmt.Errorf(
				"%s: wait for pushed head: %w",
				item.branch,
				err,
			)
		}
		if err := e.awaitMergeability(ctx, item); err != nil {
			e.Progress.Event(mergeProgressEvent{
				Kind: mergeProgressMergeabilityFailed,
				Item: item,
			})
			return 0, fmt.Errorf(
				"%s: wait for merge readiness: %w",
				item.branch,
				err,
			)
		}
	}

	operation, err := plan.Merge(ctx, request)
	if errors.Is(err, forge.ErrUnsupported) {
		// [forge.ErrUnsupported] guarantees that no range merge started.
		return e.mergeRangeIndividually(ctx, items)
	}
	if err != nil {
		for _, item := range items {
			e.Progress.Event(mergeProgressEvent{
				Kind: mergeProgressMergeFailed,
				Item: item,
			})
		}
		return 0, fmt.Errorf("merge range: %w", err)
	}

	for _, item := range items {
		e.Progress.Event(mergeProgressEvent{
			Kind: mergeProgressMerging,
			Item: item,
			URL:  item.mergeURL,
		})
		e.Progress.Event(mergeProgressEvent{
			Kind: mergeProgressWaitingForMerge,
			Item: item,
		})
	}
	completion := mergeCompletionChecker(
		newChangeCompletionChecker(e.RemoteRepository, items),
	)
	if operation != nil {
		completion = &operationCompletionChecker{
			operation:  operation,
			finalState: completion,
		}
	}
	if err := e.awaitMerged(ctx, items, completion); err != nil {
		for _, item := range items {
			e.Progress.Event(mergeProgressEvent{
				Kind: mergeProgressMergeIncomplete,
				Item: item,
			})
		}
		return 0, fmt.Errorf("await merge range: %w", err)
	}
	for _, item := range items {
		e.Progress.Event(mergeProgressEvent{
			Kind: mergeProgressMerged,
			Item: item,
		})
	}
	return len(items), nil
}

// mergeRangeIndividually resumes the ordinary bottom-up workflow after the
// forge declines a native range without starting it. Range preflight already
// established readiness for the first item. Each later item is synchronized,
// prepared against its newly merged base, and checked again before merging.
func (e *mergePlanExecutor) mergeRangeIndividually(
	ctx context.Context,
	items []*mergeItem,
) (int, error) {
	completed := 0
	for idx, item := range items {
		if idx > 0 {
			if err := e.Sync.SyncTrunk(ctx, &sync.TrunkOptions{
				ClosedChanges: sync.ClosedChangesIgnore,
			}); err != nil {
				return completed, fmt.Errorf("sync trunk: %w", err)
			}
			if err := e.prepareItem(ctx, item); err != nil {
				return completed, err
			}
		}

		var err error
		if idx == 0 {
			err = e.requestMergeItem(ctx, item)
		} else {
			err = e.mergeItem(ctx, item)
		}
		if err != nil {
			return completed, fmt.Errorf(
				"fallback merge %q: %w",
				item.branch,
				err,
			)
		}
		completed++
	}
	return completed, nil
}

// mergeCompletionChecker reports whether a requested merge has completed.
// Implementations hide the provider-specific probes from the polling loop.
type mergeCompletionChecker interface {
	CheckMergeComplete(context.Context) (bool, error)
}

// changeCompletionChecker observes final change state for one merge request.
// ChangeStatuses returns results in request order, so each status maps back to
// the same merge item without a second positional representation.
type changeCompletionChecker struct {
	repository forge.Repository // required
	items      []*mergeItem     // changes whose merged state is observed
}

func newChangeCompletionChecker(
	repository forge.Repository,
	items []*mergeItem,
) *changeCompletionChecker {
	return &changeCompletionChecker{
		repository: repository,
		items:      items,
	}
}

func (c *changeCompletionChecker) CheckMergeComplete(
	ctx context.Context,
) (bool, error) {
	changeIDs := make([]forge.ChangeID, len(c.items))
	for i, item := range c.items {
		changeIDs[i] = item.changeID
	}
	statuses, err := c.repository.ChangeStatuses(ctx, changeIDs)
	if err != nil {
		return false, fmt.Errorf("poll state: %w", err)
	}
	if len(statuses) != len(c.items) {
		return false, fmt.Errorf(
			"poll state: forge returned %d change statuses, want %d",
			len(statuses),
			len(c.items),
		)
	}

	allMerged := true
	for i, status := range statuses {
		switch status.State {
		case forge.ChangeMerged:
		case forge.ChangeOpen:
			allMerged = false
		case forge.ChangeClosed:
			return false, fmt.Errorf(
				"%s: change closed without merging",
				c.items[i].branch,
			)
		default:
			return false, fmt.Errorf(
				"%s: unknown change state %v",
				c.items[i].branch,
				status.State,
			)
		}
	}
	return allMerged, nil
}

// operationCompletionChecker waits for provider acceptance before observing
// final change state. Clearing operation records the phase transition so later
// polls never repeat a completed provider operation.
type operationCompletionChecker struct {
	operation  forge.MergeOperation   // required until accepted
	finalState mergeCompletionChecker // required
}

func (c *operationCompletionChecker) CheckMergeComplete(
	ctx context.Context,
) (bool, error) {
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
			return false, fmt.Errorf(
				"poll merge operation: unknown status %v",
				status,
			)
		}
	}
	return c.finalState.CheckMergeComplete(ctx)
}

// awaitMerged gives an ordinary change and an atomic range the same timeout,
// progress, and polling policy while their checkers own provider-specific
// completion phases.
func (e *mergePlanExecutor) awaitMerged(
	ctx context.Context,
	items []*mergeItem,
	completion mergeCompletionChecker,
) error {
	const (
		_initialDelay = 500 * time.Millisecond
		_maxDelay     = 8 * time.Second
	)

	ctx, cancel := context.WithTimeout(ctx, e.MergeTimeout)
	defer cancel()

	delay := _initialDelay
	for {
		merged, err := completion.CheckMergeComplete(ctx)
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
			e.Progress.Event(mergeProgressEvent{
				Kind: mergeProgressWaitingForMerge,
				Item: item,
			})
		}
		if err := sleep(ctx, delay); err != nil {
			return errors.New("timed out waiting for merge")
		}

		delay = min(delay*2, _maxDelay)
	}
}

// mergeQueueObserver adapts scheduler decisions
// back into merge progress events.
type mergeQueueObserver struct {
	progress mergeProgress
}

func (o *mergeQueueObserver) Prepared(mergequeue.Item) {}

func (o *mergeQueueObserver) Done(mergequeue.Item) {}

func (o *mergeQueueObserver) Failed(queueItem mergequeue.Item, _ error) {
	item := queueItem.(mergeQueueProgressItem)
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
	for _, item := range queueItem.(mergeQueueProgressItem).changes() {
		o.progress.Event(mergeProgressEvent{
			Kind: mergeProgressSkipped,
			Item: item,
		})
	}
}
