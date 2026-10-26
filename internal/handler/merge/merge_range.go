package merge

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
)

// rangeMergeQueueItem owns the preparation and execution of one native range.
// Unsupported native merges fall back within this same queue item:
// upstack items must wait until every member has merged individually.
type rangeMergeQueueItem struct {
	items []*mergeItem         // bottom-to-top
	plan  forge.MergeRangePlan // required

	executor *mergePlanExecutor
	parent   string

	// Prepare captures commit expectations after aligning every member.
	request forge.MergeRangeRequest

	// completed is the length of the successfully merged bottom-most prefix.
	// The observer reports failure only for the remaining suffix.
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
	heads, err := i.executor.workspace.PrepareRange(ctx, i.items)
	if err != nil {
		return err
	}
	i.request = forge.MergeRangeRequest{Heads: heads, Method: i.executor.Method}
	return nil
}

func (i *rangeMergeQueueItem) Run(ctx context.Context) error {
	e := i.executor
	for _, item := range i.items {
		if err := e.awaitReady(ctx, item); err != nil {
			return fmt.Errorf("%s: %w", item.branch, err)
		}
	}

	operation, err := i.plan.Merge(ctx, i.request)
	if errors.Is(err, forge.ErrUnsupported) {
		// ErrUnsupported guarantees that no atomic merge started.
		// Readiness for the first individual merge is already established.
		return i.mergeIndividually(ctx)
	}
	if err != nil {
		for _, item := range i.items {
			e.Progress.Event(mergeProgressEvent{Kind: mergeProgressMergeFailed, Item: item})
		}
		return fmt.Errorf("merge range: %w", err)
	}

	for _, item := range i.items {
		e.Progress.Event(mergeProgressEvent{Kind: mergeProgressMerging, Item: item, URL: item.mergeURL})
		e.Progress.Event(mergeProgressEvent{Kind: mergeProgressWaitingForMerge, Item: item})
	}
	if err := e.awaitMerged(ctx, i.items, operation); err != nil {
		for _, item := range i.items {
			e.Progress.Event(mergeProgressEvent{Kind: mergeProgressMergeIncomplete, Item: item})
		}
		return fmt.Errorf("await merge range: %w", err)
	}
	i.completed = len(i.items)
	for _, item := range i.items {
		e.Progress.Event(mergeProgressEvent{Kind: mergeProgressMerged, Item: item})
	}
	return nil
}

// mergeIndividually preserves ordinary bottom-up merging after native refusal.
// Each successful merge changes the next member's base, so later members must
// synchronize, prepare, and pass readiness again before requesting their merge.
func (i *rangeMergeQueueItem) mergeIndividually(ctx context.Context) error {
	e := i.executor
	for idx, item := range i.items {
		if idx > 0 {
			if err := e.workspace.SyncAndPrepare(ctx, item); err != nil {
				return err
			}
		}

		var err error
		if idx == 0 {
			err = e.requestMergeItem(ctx, item)
		} else {
			err = e.mergeItem(ctx, item)
		}
		if err != nil {
			return fmt.Errorf("fallback merge %q: %w", item.branch, err)
		}
		i.completed++
	}
	return nil
}

func (i *rangeMergeQueueItem) changes() []*mergeItem {
	return i.items
}

func (i *rangeMergeQueueItem) unmergedChanges() []*mergeItem {
	return i.items[i.completed:]
}
