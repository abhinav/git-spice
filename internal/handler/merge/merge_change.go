package merge

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/mergequeue"
)

// changeMergeQueueItem owns preparation and execution of one selected change.
type changeMergeQueueItem struct {
	item *mergeItem

	executor *mergePlanExecutor
	parent   string
}

var _ mergequeue.Item = (*changeMergeQueueItem)(nil)

func (i *changeMergeQueueItem) ID() string {
	return i.item.branch
}

func (i *changeMergeQueueItem) Parent() string {
	return i.parent
}

func (i *changeMergeQueueItem) Prepare(ctx context.Context) error {
	return i.executor.workspace.PrepareChange(ctx, i.item)
}

func (i *changeMergeQueueItem) Run(ctx context.Context) error {
	return i.executor.mergeItem(ctx, i.item)
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
