// Package mergequeue schedules dependent items for sequential merging.
//
// The package knows only opaque item IDs and base IDs.
// Callers own domain-specific work such as forge calls,
// local repository updates,
// logging,
// and progress rendering.
package mergequeue

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/container/ring"
	"go.abhg.dev/gs/internal/graph"
	"go.abhg.dev/gs/internal/must"
)

// Item is one mergeable item in a dependency queue.
type Item[T any] struct {
	// ID uniquely identifies this item inside one queue.
	ID string // required

	// BaseID identifies the item that must merge before this item.
	// Empty and unknown bases make the item ready at queue start.
	BaseID string

	// Value is passed unchanged to the Executor.
	Value T
}

// Options configures Scheduler behavior.
type Options[T any] struct {
	// FailFast stops scheduling after the first item failure.
	// By default, independent items continue after failures.
	FailFast bool

	// Observer receives scheduler-level state transitions.
	Observer Observer[T]
}

// Executor merges one queue item.
//
// Scheduler calls Merge sequentially.
// A nil error marks the item complete
// and unlocks items based directly on it.
type Executor[T any] interface {
	Merge(context.Context, T) error
}

// Observer receives scheduler-level events.
//
// The observer is for scheduler decisions only,
// such as items failed by Executor
// or skipped without calling Executor.
// Per-item merge progress belongs inside the caller's Executor.
type Observer[T any] interface {
	Failed(T, error)
	Skipped(T, SkipReason)
}

// SkipReason explains why a pending item became ineligible.
type SkipReason int

const (
	// SkipBecauseBelowFailed means a dependency below this item failed.
	SkipBecauseBelowFailed SkipReason = iota

	// SkipBecauseFailFast means fail-fast stopped the remaining queue.
	SkipBecauseFailFast
)

// ItemError wraps an error returned for a specific queue item.
type ItemError struct {
	ID  string // required
	Err error  // required
}

func (e *ItemError) Error() string {
	return fmt.Sprintf("merge queue item %q: %v", e.ID, e.Err)
}

func (e *ItemError) Unwrap() error {
	return e.Err
}

// Scheduler runs mergeable items in dependency order.
type Scheduler[T any] struct {
	// nodes indexes every queued item by Item.ID.
	nodes map[string]*node[T]

	// order stores nodes in topological order.
	// Scheduler uses it for deterministic ready initialization
	// and fail-fast skip reporting.
	order []*node[T]

	// ready contains pending nodes whose queued base has already merged
	// or whose base is not part of this queue.
	ready ring.Q[*node[T]]

	failFast bool

	observer Observer[T]
}

// New validates items and prepares a Scheduler.
//
// Duplicate IDs,
// empty IDs,
// and cycles among queued items are rejected.
// Items may appear in any order:
// an item may appear before or after its base.
// Items with an empty or unknown BaseID are ready at queue start.
// Among ready siblings,
// input order determines execution order.
func New[T any](
	items []Item[T],
	opts *Options[T],
) (*Scheduler[T], error) {
	if opts == nil {
		opts = &Options[T]{}
	}
	observer := opts.Observer
	if observer == nil {
		observer = &nopObserver[T]{}
	}

	scheduler := &Scheduler[T]{
		nodes:    make(map[string]*node[T], len(items)),
		order:    make([]*node[T], 0, len(items)),
		failFast: opts.FailFast,
		observer: observer,
	}

	ids := make([]string, 0, len(items))
	for i, item := range items {
		must.NotBeBlankf(item.ID, "item at index %d has empty ID", i)
		if _, ok := scheduler.nodes[item.ID]; ok {
			return nil, fmt.Errorf("duplicate item ID %q", item.ID)
		}

		n := &node[T]{item: item}
		scheduler.nodes[item.ID] = n
		ids = append(ids, item.ID)
	}

	topoIDs, err := graph.Toposort(ids, func(id string) (string, bool) {
		baseID := scheduler.nodes[id].item.BaseID
		_, ok := scheduler.nodes[baseID]
		return baseID, ok
	})
	if err != nil {
		var cycleErr *graph.CycleError[string]
		if errors.As(err, &cycleErr) {
			return nil, fmt.Errorf(
				"cycle includes items: %s",
				cycleErr.Format(" -> "),
			)
		}
		return nil, fmt.Errorf("sort items: %w", err)
	}
	for _, id := range topoIDs {
		scheduler.order = append(scheduler.order, scheduler.nodes[id])
	}

	for _, n := range scheduler.order {
		base := scheduler.nodes[n.item.BaseID]
		if base == nil {
			scheduler.ready.Push(n)
			continue
		}
		base.aboves = append(base.aboves, n)
	}

	return scheduler, nil
}

// Run executes ready items until the queue completes or fail-fast stops it.
//
// Run mutates scheduler state,
// so a Scheduler should be used for one run only.
// Item failures are wrapped in ItemError and returned with errors.Join.
func (s *Scheduler[T]) Run(
	ctx context.Context,
	exec Executor[T],
) error {
	must.NotBeNilf(exec, "merge queue executor is required")

	var errs []error
	for !s.ready.Empty() {
		n := s.ready.Pop()
		if n.state != nodePending {
			continue
		}

		if err := exec.Merge(ctx, n.item.Value); err != nil {
			n.state = nodeFailed
			s.observer.Failed(n.item.Value, err)
			errs = append(errs, &ItemError{
				ID:  n.item.ID,
				Err: err,
			})
			s.skipAboves(n)
			if s.failFast {
				s.skipRemaining()
				return errors.Join(errs...)
			}
			continue
		}

		n.state = nodeMerged
		for _, above := range n.aboves {
			s.ready.Push(above)
		}
	}

	return errors.Join(errs...)
}

func (s *Scheduler[T]) skipAboves(n *node[T]) {
	for _, above := range n.aboves {
		if above.state != nodePending {
			continue
		}
		above.state = nodeSkipped
		s.observer.Skipped(
			above.item.Value,
			SkipBecauseBelowFailed,
		)
		s.skipAboves(above)
	}
}

func (s *Scheduler[T]) skipRemaining() {
	for _, n := range s.order {
		if n.state != nodePending {
			continue
		}
		n.state = nodeSkipped
		s.observer.Skipped(
			n.item.Value,
			SkipBecauseFailFast,
		)
	}
}

// node is the scheduler's mutable state for one input item.
type node[T any] struct {
	item Item[T]

	// aboves are items based directly on this item.
	aboves []*node[T]

	state nodeState
}

// nodeState records whether a queued item is still eligible.
type nodeState uint8

const (
	nodePending nodeState = iota
	nodeMerged
	nodeFailed
	nodeSkipped
)

// nopObserver is the default observer used when callers do not need
// scheduler events.
type nopObserver[T any] struct{}

func (*nopObserver[T]) Failed(T, error) {}

func (*nopObserver[T]) Skipped(T, SkipReason) {}
