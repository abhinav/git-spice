// Package mergequeue schedules dependent items.
//
// The package owns graph scheduling only.
// Callers own domain-specific work such as forge calls,
// local repository updates,
// logging,
// readiness polling,
// and progress rendering.
package mergequeue

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"go.abhg.dev/container/ring"
	"go.abhg.dev/gs/internal/graph"
	"go.abhg.dev/gs/internal/must"
)

// Item is one schedulable node in a dependency queue.
type Item interface {
	// ID uniquely identifies this item inside one queue.
	ID() string

	// Parent returns the ID of the item that must finish before this item can
	// run.
	//
	// An empty parent means this item can run as soon as scheduling starts.
	Parent() string

	// Prepare performs setup after the parent finishes and before Run starts.
	//
	// The scheduler never calls Prepare on two items at the same time.
	Prepare(context.Context) error

	// Run blocks until the item is done or fails.
	//
	// The scheduler may call Run on multiple items at the same time.
	Run(context.Context) error
}

// Options configures Scheduler behavior.
type Options struct {
	// FailFast stops scheduling after the first item failure.
	// By default, independent items continue after failures.
	FailFast bool

	// Observer receives scheduler-level state transitions.
	Observer Observer

	// Barrier runs after one or more successful item runs
	// and before the scheduler starts any later Prepare call.
	//
	// Multiple successful runs may coalesce into one Barrier call.
	// Barrier errors are queue-level errors,
	// not errors for any specific item.
	Barrier func(context.Context) error
}

// Observer receives scheduler-level events.
//
// The observer is for scheduler decisions only,
// such as items that finish,
// fail,
// or become skipped without running.
// Per-item progress belongs inside the caller's Item implementation.
//
// Observer calls are serialized within one [Scheduler.Run] call.
// If callers run the same Scheduler concurrently,
// Item,
// Observer,
// and Options.Barrier implementations must be safe for concurrent calls.
type Observer interface {
	Prepared(Item)
	Done(Item)
	Failed(Item, error)
	Skipped(Item, SkipReason)
}

// SkipReason explains why a pending item became ineligible.
type SkipReason int

const (
	// SkipBecauseBelowFailed means a parent or ancestor item failed.
	SkipBecauseBelowFailed SkipReason = iota

	// SkipBecauseFailFast means fail-fast stopped the remaining queue.
	SkipBecauseFailFast

	// SkipBecauseCanceled means context cancellation stopped the queue.
	SkipBecauseCanceled

	// SkipBecauseBarrierFailed means the queue-level barrier failed.
	SkipBecauseBarrierFailed
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

// BarrierError wraps an error returned by Options.Barrier.
type BarrierError struct {
	Err error // required
}

func (e *BarrierError) Error() string {
	return fmt.Sprintf("merge queue barrier: %v", e.Err)
}

func (e *BarrierError) Unwrap() error {
	return e.Err
}

// Scheduler runs items after their parents complete.
type Scheduler struct {
	// nodes indexes every queued item by Item.ID.
	nodes map[string]*node

	// input stores nodes in caller-provided order.
	// Scheduler uses it to keep ready sibling preparation deterministic.
	input []*node

	// order stores nodes in topological order.
	// Scheduler uses it for deterministic ready initialization
	// and fail-fast skip reporting.
	order []*node

	// roots are nodes with no queue-local parent.
	roots []*node

	failFast bool

	observer Observer
	barrier  func(context.Context) error
}

// New validates items and prepares a Scheduler.
//
// Duplicate IDs,
// missing parents,
// and cycles among queued items are rejected.
// Nil items and empty item IDs are invalid.
// Items may appear in any order:
// an item may appear before or after its parent.
// Among ready siblings,
// input order determines preparation order.
// A nil opts uses zero-value options.
func New(items []Item, opts *Options) (*Scheduler, error) {
	if opts == nil {
		opts = &Options{}
	}
	observer := opts.Observer
	if observer == nil {
		observer = nopObserver{}
	}

	scheduler := &Scheduler{
		nodes:    make(map[string]*node, len(items)),
		input:    make([]*node, 0, len(items)),
		order:    make([]*node, 0, len(items)),
		failFast: opts.FailFast,
		observer: observer,
		barrier:  opts.Barrier,
	}

	for i, item := range items {
		must.NotBeNilf(item, "item at index %d is nil", i)
		id := item.ID()
		must.NotBeBlankf(id, "item at index %d has empty ID", i)
		if _, ok := scheduler.nodes[id]; ok {
			return nil, fmt.Errorf("duplicate item ID %q", id)
		}
		n := &node{item: item}
		scheduler.nodes[id] = n
		scheduler.input = append(scheduler.input, n)
	}

	for _, n := range scheduler.input {
		parentID := n.item.Parent()
		if parentID == "" {
			scheduler.roots = append(scheduler.roots, n)
			continue
		}

		parent := scheduler.nodes[parentID]
		if parent == nil {
			return nil, fmt.Errorf(
				"item %q depends on unknown parent %q",
				n.item.ID(),
				parentID,
			)
		}
		n.parent = parent
		parent.aboves = append(parent.aboves, n)
	}

	order, err := graph.Toposort(scheduler.input,
		func(n *node) (*node, bool) {
			return n.parent, n.parent != nil
		})
	if err != nil {
		return nil, err
	}
	scheduler.order = order

	return scheduler, nil
}

// Run executes items until the queue completes,
// fail-fast stops it,
// or ctx is canceled.
//
// If Options.Barrier is configured,
// successful item runs dirty the barrier.
// The scheduler runs the barrier before starting later Prepare calls
// and before returning after the final successful run.
//
// On cancellation,
// Run stops scheduling new work,
// skips pending items,
// and waits for started Prepare and Run work to return.
// Errors returned by already-started Prepare and Run calls
// are still reported as item failures
// and joined into the returned error.
//
// State flow:
//
//	ready queue --prepare worker--> scheduler loop --Run--> resultc
//	    ^                                                        |
//	    |                                                        v
//	    +----------- parent done unlocks direct aboves <---------+
//
//	prepare failure or run failure marks the item failed
//	and recursively skips every direct or indirect above.
//
// Item failures are wrapped in ItemError and returned with errors.Join.
// Barrier failures are wrapped in BarrierError and returned with errors.Join.
func (s *Scheduler) Run(ctx context.Context) (retErr error) {
	// Internal cancellation stops item workers after fail-fast or barrier
	// failure.
	// The barrier still uses the caller's context,
	// so a successful run can flush queue-level state before Run exits.
	barrierCtx := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type workResultKind uint8
	const (
		prepareResult workResultKind = iota
		runResult
	)

	type workResult struct {
		// kind identifies which worker phase reported this result.
		kind workResultKind

		// node is the item whose phase finished.
		node *node

		// err is the phase error, if any.
		err error
	}

	type nodeStatus uint8
	const (
		nodePending nodeStatus = iota
		nodePreparing
		nodeRunning
		nodeDone
		nodeFailed
		nodeSkipped
	)

	// Scheduler runtime state is local to this function.
	// The run loop owns all state transitions and observer calls,
	// so worker goroutines can only report completion over channels.

	// resultc is large enough for each item to report both worker phases.
	// If cancellation makes several workers exit while the run loop is
	// unwinding,
	// no worker should block forever while reporting its final phase result.
	resultc := make(chan workResult, 2*len(s.order))

	var waitGroup sync.WaitGroup

	// preparec feeds one worker that serializes Prepare calls.
	// The scheduler loop only pops a ready item after this send succeeds,
	// so the ready queue still owns items that have not entered Prepare.
	preparec := make(chan *node)
	waitGroup.Go(func() {
		for n := range preparec {
			resultc <- workResult{
				kind: prepareResult,
				node: n,
				err:  n.item.Prepare(ctx),
			}
		}
	})

	// states records the scheduler status for every node.
	// Queue entries are allowed to become stale after a skip,
	// so state is the authority before an item starts or finishes.
	states := make(map[*node]nodeStatus, len(s.order))
	for _, n := range s.order {
		states[n] = nodePending
	}

	// skips all nodes upstack from n that are still pending,
	// not including n itself.
	var skipUpstack func(*node)
	skipUpstack = func(n *node) {
		for _, above := range n.aboves {
			if states[above] != nodePending {
				continue
			}
			states[above] = nodeSkipped
			s.observer.Skipped(above.item, SkipBecauseBelowFailed)
			skipUpstack(above)
		}
	}

	// stopping means the scheduler is leaving the scheduling loop.
	// Started work is drained after workers exit.
	var stopping bool
	var stopReason SkipReason

	stopScheduling := func(reason SkipReason) {
		if stopping {
			return
		}
		stopping = true
		stopReason = reason
		cancel()

		// Skip remaining pending items.
		// Everything that's ongoing will be drained.
		for _, n := range s.order {
			if states[n] != nodePending {
				continue
			}
			states[n] = nodeSkipped
			s.observer.Skipped(n.item, reason)
		}
	}

	// errs accumulates item and context failures for the final joined error.
	var errs []error
	defer func() {
		retErr = errors.Join(retErr, errors.Join(errs...))
	}()

	// failNode marks the given node as failed
	// and recursively skips all its pending upstack.
	failNode := func(n *node, err error) {
		// A barrier failure is already reported as a queue-level error.
		// Cancellation from that shutdown should not turn unrelated
		// in-flight work into item failures.
		if stopping &&
			stopReason == SkipBecauseBarrierFailed &&
			errors.Is(err, context.Canceled) {
			return
		}
		if states[n] == nodeFailed || states[n] == nodeSkipped {
			return
		}
		states[n] = nodeFailed
		s.observer.Failed(n.item, err)
		errs = append(errs, &ItemError{
			ID:  n.item.ID(),
			Err: err,
		})
		if s.failFast {
			stopScheduling(SkipBecauseFailFast)
		} else {
			skipUpstack(n)
		}
	}

	scheduleNodeRun := func(n *node) {
		waitGroup.Go(func() {
			resultc <- workResult{
				kind: runResult,
				node: n,
				err:  n.item.Run(ctx),
			}
		})
	}

	// barrierDirty means at least one successful Run has completed
	// since the last successful queue-level barrier.
	var barrierDirty bool
	runBarrier := func() {
		barrierDirty = false
	}
	if s.barrier != nil {
		runBarrier = func() {
			if !barrierDirty {
				return
			}
			err := s.barrier(barrierCtx)
			barrierDirty = false
			if err != nil {
				errs = append(errs, &BarrierError{Err: err})
				stopScheduling(SkipBecauseBarrierFailed)
			}
		}
	}

	// The shutdown phase waits for workers and drains their buffered results.
	// Drained results preserve errors from work that already started,
	// but they must not unlock more work or emit normal progress events.
	defer func() {
		// Stop the prepare worker before waiting for the worker group.
		// No sender reaches preparec after Run starts returning,
		// so closing preparec lets the prepare worker leave its range loop.
		close(preparec)

		// Workers can still report to resultc while the scheduler loop is no
		// longer receiving from it.
		// resultc is sized for every possible phase result,
		// so waiting for workers here cannot deadlock on a result send.
		waitGroup.Wait()
		close(resultc)

		// Drain results reported after the scheduler decided to stop.
		// The drain records errors from already-started work,
		// but it does not unlock aboves or emit Done/Prepared events
		// because scheduling has ended.
		for res := range resultc {
			switch res.kind {
			case prepareResult:
				if res.err != nil {
					failNode(res.node, res.err)
				}

			case runResult:
				if res.err != nil {
					failNode(res.node, res.err)
				} else if s.barrier != nil {
					barrierDirty = true
				}

			default:
				must.Failf("unknown work result kind %d", res.kind)
			}
		}

		runBarrier()
	}()

	// preparing reports whether a Prepare call has been started
	// and hasn't yet reported on resultc.
	var preparing bool

	// running counts Run workers that have started
	// and have not yet reported on resultc.
	//
	// The count is decremented when a run result is received,
	// even if the node was skipped or failed while Run was running.
	var running int

	var ready ring.Q[*node]
	for _, n := range s.roots {
		ready.Push(n)
	}

	handleResult := func(res workResult) {
		switch res.kind {
		case prepareResult:
			preparing = false
			if states[res.node] != nodePreparing {
				return
			}
			if res.err != nil {
				failNode(res.node, res.err)
				return
			}

			states[res.node] = nodeRunning
			s.observer.Prepared(res.node.item)

			// Eligible to run after preparation.
			running++
			scheduleNodeRun(res.node)

		case runResult:
			running--
			if states[res.node] != nodeRunning {
				return
			}
			if res.err != nil {
				failNode(res.node, res.err)
				return
			}

			// Barrier is always dirtied after a successful run.
			states[res.node] = nodeDone
			barrierDirty = true
			s.observer.Done(res.node.item)
			for _, above := range res.node.aboves {
				if states[above] == nodePending {
					ready.Push(above)
				}
			}

		default:
			must.Failf("unknown work result kind %d", res.kind)
		}
	}

	for !stopping {
		if barrierDirty && !preparing {
			// Process completed runs that are already available
			// before running the barrier.
			// That coalesces sibling completions into one barrier call
			// while still blocking later Prepare calls until the barrier runs.
			for running > 0 && len(resultc) > 0 {
				select {
				case res := <-resultc:
					handleResult(res)
				default:
					// No more results to drain.
				}
			}

			runBarrier()
			if stopping {
				return
			}
		}

		// Offer at most one ready item to the prepare worker.
		var readyNode *node
		if !ready.Empty() {
			readyNode = ready.Peek()
			must.Bef(states[readyNode] == nodePending,
				"ready node %q is not pending", readyNode.item.ID())
		}

		// Nothing left to do.
		if running == 0 && !preparing && readyNode == nil {
			return
		}

		preparec := (chan<- *node)(preparec)
		if readyNode == nil || len(resultc) > 0 || barrierDirty {
			// A nil channel never resolves
			// so if there's nothing to schedule
			// or a result is already waiting,
			// or the barrier must run before the next Prepare,
			// this select arm won't resolve.
			preparec = nil
		}

		select {
		case preparec <- readyNode:
			// Note that we pop from ready
			// only after successfully sending to preparec.
			// If another case is selected,
			// this node will be reconsidered next iteration.
			ready.Pop()
			states[readyNode] = nodePreparing
			preparing = true

		case res := <-resultc:
			handleResult(res)

		case <-ctx.Done():
			errs = append(errs, ctx.Err())
			stopScheduling(SkipBecauseCanceled)
			return
		}

	}

	return nil
}

// node is the scheduler's validated graph view of one input item.
type node struct {
	item Item

	// parent is the item this node waits for before it can prepare.
	parent *node

	// aboves are items that directly depend on this item.
	aboves []*node
}

func (n *node) String() string {
	return n.item.ID()
}

// nopObserver is the default observer used when callers do not need
// scheduler events.
type nopObserver struct{}

func (nopObserver) Prepared(Item) {}

func (nopObserver) Done(Item) {}

func (nopObserver) Failed(Item, error) {}

func (nopObserver) Skipped(Item, SkipReason) {}
