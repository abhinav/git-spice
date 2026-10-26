package merge

import (
	"context"
	"fmt"
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
	return i.executor.workspace.PrepareChange(ctx, i.item)
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

func (e *mergePlanExecutor) mergeItem(ctx context.Context, item *mergeItem) error {
	if err := e.awaitReady(ctx, item); err != nil {
		return err
	}
	return e.requestMergeItem(ctx, item)
}

// requestMergeItem requests and awaits one merge after readiness is established.
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
	if err := e.awaitMerged(ctx, items, nil); err != nil {
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
