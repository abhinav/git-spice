package mergequeue

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduler_Run_parentUnlocksReadySiblingsInInputOrder(
	t *testing.T,
) {
	rec := &recordingQueue{}
	scheduler, err := New([]Item{
		newTestItem("root", rec),
		newTestItem("first", rec).withParent("root"),
		newTestItem("second", rec).withParent("root"),
	}, nil)
	require.NoError(t, err)

	require.NoError(t, scheduler.Run(t.Context()))

	assert.Equal(t, []string{
		"prepare root",
		"prepare first",
		"prepare second",
	}, filterEvents(rec.events(), "prepare "))
	assert.ElementsMatch(t, []string{"root", "first", "second"}, rec.done())
}

func TestScheduler_Run_topoSortsOutOfOrderInput(t *testing.T) {
	rec := &recordingQueue{}
	scheduler, err := New([]Item{
		newTestItem("feature", rec).withParent("base"),
		newTestItem("base", rec),
	}, nil)
	require.NoError(t, err)

	require.NoError(t, scheduler.Run(t.Context()))

	assert.Equal(t, []string{"base", "feature"}, rec.done())
}

func TestScheduler_Run_preparesSiblingsBeforeEitherRunFinishes(
	t *testing.T,
) {
	rec := &recordingQueue{}
	started := make(chan string, 2)
	release := make(chan struct{})

	scheduler, err := New([]Item{
		newTestItem("root", rec),
		newTestItem("first", rec).withParent("root").withRun(func(context.Context) error {
			started <- "first"
			<-release
			return nil
		}),
		newTestItem("second", rec).withParent("root").withRun(func(context.Context) error {
			started <- "second"
			<-release
			return nil
		}),
	}, nil)
	require.NoError(t, err)

	errc := make(chan error, 1)
	go func() {
		errc <- scheduler.Run(t.Context())
	}()

	require.ElementsMatch(t, []string{
		"first",
		"second",
	}, []string{
		receiveStarted(t, started),
		receiveStarted(t, started),
	})
	close(release)
	require.NoError(t, <-errc)

	events := rec.events()
	assert.Less(
		t,
		indexOf(t, events, "prepare second"),
		indexOf(t, events, "run-end first"),
	)
	assert.Less(
		t,
		indexOf(t, events, "prepare second"),
		indexOf(t, events, "run-end second"),
	)
}

func TestScheduler_Run_cancelSkipsOnlyPendingItems(t *testing.T) {
	observer := &recordingObserver{}
	rec := &recordingQueue{}
	started := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())

	scheduler, err := New([]Item{
		newTestItem("root", rec).withRun(func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		}),
		newTestItem("child", rec).withParent("root"),
	}, &Options{
		Observer: observer,
	})
	require.NoError(t, err)

	errc := make(chan error, 1)
	go func() {
		errc <- scheduler.Run(ctx)
	}()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for root Run to start")
	}
	cancel()

	require.ErrorIs(t, <-errc, context.Canceled)
	assert.Equal(t, []skipRecord{
		{item: "child", reason: SkipBecauseCanceled},
	}, observer.skipped)
	assert.Equal(t, []string{"root"}, observer.failed)
}

func TestScheduler_Run_independentBranchContinuesAfterFailure(
	t *testing.T,
) {
	failErr := errors.New("merge failed")
	observer := &recordingObserver{}
	rec := &recordingQueue{}
	scheduler, err := New([]Item{
		newTestItem("root", rec),
		newTestItem("broken", rec).withParent("root").withRunError(failErr),
		newTestItem("blocked", rec).withParent("broken"),
		newTestItem("independent", rec).withParent("root"),
	}, &Options{
		Observer: observer,
	})
	require.NoError(t, err)

	err = scheduler.Run(t.Context())
	require.Error(t, err)

	var itemErr *ItemError
	require.ErrorAs(t, err, &itemErr)
	assert.Equal(t, "broken", itemErr.ID)
	assert.ErrorIs(t, err, failErr)
	assert.Equal(t, []string{"broken"}, observer.failed)
	assert.Equal(t, []skipRecord{
		{item: "blocked", reason: SkipBecauseBelowFailed},
	}, observer.skipped)
	assert.Contains(t, rec.done(), "independent")
}

func TestScheduler_Run_failFastSkipsRemaining(t *testing.T) {
	observer := &recordingObserver{}
	rec := &recordingQueue{}
	scheduler, err := New([]Item{
		newTestItem("root", rec),
		newTestItem("broken", rec).withParent("root").
			withPrepareError(errors.New("merge failed")),
		newTestItem("blocked", rec).withParent("broken"),
		newTestItem("other", rec).withParent("root"),
	}, &Options{
		FailFast: true,
		Observer: observer,
	})
	require.NoError(t, err)

	err = scheduler.Run(t.Context())
	require.Error(t, err)

	assert.ElementsMatch(t, []skipRecord{
		{item: "blocked", reason: SkipBecauseFailFast},
		{item: "other", reason: SkipBecauseFailFast},
	}, observer.skipped)
	assert.NotContains(t, rec.done(), "other")
}

func TestScheduler_Run_returnsItemError(t *testing.T) {
	failErr := errors.New("merge failed")
	scheduler, err := New([]Item{
		newTestItem("broken", &recordingQueue{}).withRunError(failErr),
	}, nil)
	require.NoError(t, err)

	err = scheduler.Run(t.Context())
	require.Error(t, err)

	var itemErr *ItemError
	require.ErrorAs(t, err, &itemErr)
	assert.Equal(t, "broken", itemErr.ID)
	assert.ErrorIs(t, err, failErr)
}

func TestScheduler_Run_barrierRunsAfterFinalSuccessfulRun(t *testing.T) {
	rec := &recordingQueue{}
	scheduler, err := New([]Item{
		newTestItem("root", rec),
	}, &Options{
		Barrier: func(context.Context) error {
			rec.record("barrier")
			return nil
		},
	})
	require.NoError(t, err)

	require.NoError(t, scheduler.Run(t.Context()))

	assert.Equal(t, []string{
		"prepare root",
		"run-start root",
		"run-end root",
		"barrier",
	}, rec.events())
}

func TestScheduler_Run_barrierRunsBeforeDependentPrepare(t *testing.T) {
	rec := &recordingQueue{}
	scheduler, err := New([]Item{
		newTestItem("root", rec),
		newTestItem("child", rec).withParent("root"),
	}, &Options{
		Barrier: func(context.Context) error {
			rec.record("barrier")
			return nil
		},
	})
	require.NoError(t, err)

	require.NoError(t, scheduler.Run(t.Context()))

	events := rec.events()
	assert.Less(t,
		indexOf(t, events, "barrier"),
		indexOf(t, events, "prepare child"),
	)
}

func TestScheduler_Run_barrierCoalescesReadyRunResults(t *testing.T) {
	rec := &recordingQueue{}
	started := make(chan string, 2)
	prepareStarted := make(chan struct{})
	releaseRuns := make(chan struct{})
	releasePrepare := make(chan struct{})

	scheduler, err := New([]Item{
		newTestItem("first", rec).withRun(func(context.Context) error {
			started <- "first"
			<-releaseRuns
			return nil
		}),
		newTestItem("second", rec).withRun(func(context.Context) error {
			started <- "second"
			<-releaseRuns
			return nil
		}),
		newTestItem("block-prepare", rec).withPrepare(func(context.Context) error {
			close(prepareStarted)
			<-releasePrepare
			return nil
		}).withRunError(errors.New("stop after barrier")),
		newTestItem("child", rec).withParent("first"),
	}, &Options{
		Barrier: func(context.Context) error {
			rec.record("barrier")
			return nil
		},
	})
	require.NoError(t, err)

	errc := make(chan error, 1)
	go func() {
		errc <- scheduler.Run(t.Context())
	}()

	require.ElementsMatch(t, []string{
		"first",
		"second",
	}, []string{
		receiveStarted(t, started),
		receiveStarted(t, started),
	})
	select {
	case <-prepareStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for blocking Prepare to start")
	}
	close(releaseRuns)
	require.Eventually(t, func() bool {
		events := rec.events()
		return containsEvent(events, "run-end first") &&
			containsEvent(events, "run-end second")
	}, 3*time.Second, 10*time.Millisecond)
	close(releasePrepare)

	require.Error(t, <-errc)

	events := rec.events()
	firstBarrier := indexOf(t, events, "barrier")
	assert.Less(t, indexOf(t, events, "run-end first"), firstBarrier)
	assert.Less(t, indexOf(t, events, "run-end second"), firstBarrier)
	assert.Less(t, firstBarrier, indexOf(t, events, "prepare child"))
}

func TestScheduler_Run_barrierFailureIsQueueError(t *testing.T) {
	barrierErr := errors.New("sync trunk failed")
	observer := &recordingObserver{}
	rec := &recordingQueue{}
	scheduler, err := New([]Item{
		newTestItem("root", rec),
		newTestItem("child", rec).withParent("root"),
	}, &Options{
		Observer: observer,
		Barrier: func(context.Context) error {
			return barrierErr
		},
	})
	require.NoError(t, err)

	err = scheduler.Run(t.Context())
	require.Error(t, err)

	var barrierError *BarrierError
	require.ErrorAs(t, err, &barrierError)
	assert.ErrorIs(t, err, barrierErr)
	var itemErr *ItemError
	assert.False(t, errors.As(err, &itemErr))
	assert.Equal(t, []skipRecord{
		{item: "child", reason: SkipBecauseBarrierFailed},
	}, observer.skipped)
	assert.Empty(t, observer.failed)
}

func TestScheduler_Run_barrierFailureDoesNotFailCanceledSibling(
	t *testing.T,
) {
	barrierErr := errors.New("sync trunk failed")
	observer := &recordingObserver{}
	rec := &recordingQueue{}
	siblingStarted := make(chan struct{})
	releaseMerged := make(chan struct{})
	scheduler, err := New([]Item{
		newTestItem("merged", rec).withRun(func(context.Context) error {
			<-releaseMerged
			return nil
		}),
		newTestItem("sibling", rec).withRun(func(ctx context.Context) error {
			close(siblingStarted)
			<-ctx.Done()
			return ctx.Err()
		}),
	}, &Options{
		Observer: observer,
		Barrier: func(context.Context) error {
			return barrierErr
		},
	})
	require.NoError(t, err)

	errc := make(chan error, 1)
	go func() {
		errc <- scheduler.Run(t.Context())
	}()

	select {
	case <-siblingStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for sibling Run to start")
	}
	close(releaseMerged)

	err = <-errc
	require.Error(t, err)

	var barrierError *BarrierError
	require.ErrorAs(t, err, &barrierError)
	assert.ErrorIs(t, err, barrierErr)
	var itemErr *ItemError
	assert.False(t, errors.As(err, &itemErr))
	assert.Empty(t, observer.failed)
}

func TestScheduler_Run_dirtyBarrierRunsBeforeFailFastExit(t *testing.T) {
	rec := &recordingQueue{}
	releasePrepare := make(chan struct{})
	barrierCalls := 0
	scheduler, err := New([]Item{
		newTestItem("merged", rec),
		newTestItem("broken", rec).withPrepare(func(context.Context) error {
			<-releasePrepare
			return errors.New("prepare failed")
		}),
	}, &Options{
		FailFast: true,
		Barrier: func(context.Context) error {
			barrierCalls++
			return nil
		},
	})
	require.NoError(t, err)

	errc := make(chan error, 1)
	go func() {
		errc <- scheduler.Run(t.Context())
	}()

	require.Eventually(t, func() bool {
		return containsEvent(rec.events(), "run-end merged")
	}, 3*time.Second, 10*time.Millisecond)
	close(releasePrepare)

	require.Error(t, <-errc)
	assert.Equal(t, 1, barrierCalls)
}

func TestScheduler_Run_noBarrierWithoutSuccessfulRun(t *testing.T) {
	barrierCalls := 0
	scheduler, err := New([]Item{
		newTestItem("broken", &recordingQueue{}).
			withRunError(errors.New("merge failed")),
	}, &Options{
		Barrier: func(context.Context) error {
			barrierCalls++
			return nil
		},
	})
	require.NoError(t, err)

	require.Error(t, scheduler.Run(t.Context()))

	assert.Zero(t, barrierCalls)
}

func TestNew_rejectsDuplicateID(t *testing.T) {
	_, err := New([]Item{
		newTestItem("same", &recordingQueue{}),
		newTestItem("same", &recordingQueue{}),
	}, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `duplicate item ID "same"`)
}

func TestNew_rejectsMissingParent(t *testing.T) {
	_, err := New([]Item{
		newTestItem("child", &recordingQueue{}).withParent("external"),
	}, nil)

	require.Error(t, err)
	assert.Contains(
		t,
		err.Error(),
		`item "child" depends on unknown parent "external"`,
	)
}

func TestNew_rejectsCycle(t *testing.T) {
	_, err := New([]Item{
		newTestItem("one", &recordingQueue{}).withParent("two"),
		newTestItem("two", &recordingQueue{}).withParent("three"),
		newTestItem("three", &recordingQueue{}).withParent("one"),
	}, nil)

	require.Error(t, err)
	assert.Contains(
		t,
		err.Error(),
		"cycle: one -> two -> three -> one",
	)
}

type testItem struct {
	id         string
	parent     string
	rec        *recordingQueue
	prepare    func(context.Context) error
	run        func(context.Context) error
	prepareErr error
	runErr     error
}

func newTestItem(id string, rec *recordingQueue) *testItem {
	return &testItem{
		id:  id,
		rec: rec,
	}
}

func (i *testItem) withParent(parent string) *testItem {
	i.parent = parent
	return i
}

func (i *testItem) withRun(run func(context.Context) error) *testItem {
	i.run = run
	return i
}

func (i *testItem) withPrepare(prepare func(context.Context) error) *testItem {
	i.prepare = prepare
	return i
}

func (i *testItem) withPrepareError(err error) *testItem {
	i.prepareErr = err
	return i
}

func (i *testItem) withRunError(err error) *testItem {
	i.runErr = err
	return i
}

func (i *testItem) ID() string {
	return i.id
}

func (i *testItem) Parent() string {
	return i.parent
}

func (i *testItem) Prepare(ctx context.Context) error {
	i.rec.record("prepare " + i.id)
	if i.prepare != nil {
		if err := i.prepare(ctx); err != nil {
			return err
		}
	}
	return i.prepareErr
}

func (i *testItem) Run(ctx context.Context) error {
	i.rec.record("run-start " + i.id)
	if i.run != nil {
		if err := i.run(ctx); err != nil {
			return err
		}
	}
	i.rec.record("run-end " + i.id)
	if i.runErr == nil {
		i.rec.markDone(i.id)
	}
	return i.runErr
}

type recordingQueue struct {
	mu      sync.Mutex
	eventsV []string
	doneV   []string
}

func (r *recordingQueue) record(event string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.eventsV = append(r.eventsV, event)
}

func (r *recordingQueue) markDone(item string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.doneV = append(r.doneV, item)
}

func (r *recordingQueue) events() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.eventsV...)
}

func (r *recordingQueue) done() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.doneV...)
}

type recordingObserver struct {
	failed  []string
	skipped []skipRecord
}

func (o *recordingObserver) Prepared(Item) {}

func (o *recordingObserver) Done(Item) {}

func (o *recordingObserver) Failed(item Item, _ error) {
	o.failed = append(o.failed, item.ID())
}

func (o *recordingObserver) Skipped(item Item, reason SkipReason) {
	o.skipped = append(o.skipped, skipRecord{
		item:   item.ID(),
		reason: reason,
	})
}

type skipRecord struct {
	item   string
	reason SkipReason
}

func receiveStarted(t *testing.T, ch <-chan string) string {
	t.Helper()

	select {
	case item := <-ch:
		return item
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for sibling Run to start")
		return ""
	}
}

func indexOf(t *testing.T, items []string, target string) int {
	t.Helper()

	for i, item := range items {
		if item == target {
			return i
		}
	}
	t.Fatalf("event %q not found in %v", target, items)
	return 0
}

func filterEvents(events []string, prefix string) []string {
	var filtered []string
	for _, event := range events {
		if strings.HasPrefix(event, prefix) {
			filtered = append(filtered, event)
		}
	}
	return filtered
}

func containsEvent(events []string, target string) bool {
	return slices.Contains(events, target)
}
