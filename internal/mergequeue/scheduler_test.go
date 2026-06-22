package mergequeue

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduler_Run_parentUnlocksReadySiblingsInInputOrder(
	t *testing.T,
) {
	scheduler, err := New([]Item[string]{
		{ID: "root", Value: "root"},
		{ID: "first", BaseID: "root", Value: "first"},
		{ID: "second", BaseID: "root", Value: "second"},
	}, nil)
	require.NoError(t, err)

	exec := &recordingExecutor{}
	require.NoError(t, scheduler.Run(t.Context(), exec))

	assert.Equal(t, []string{"root", "first", "second"}, exec.items)
}

func TestScheduler_Run_topoSortsOutOfOrderInput(t *testing.T) {
	scheduler, err := New([]Item[string]{
		{ID: "feature", BaseID: "base", Value: "feature"},
		{ID: "base", Value: "base"},
	}, nil)
	require.NoError(t, err)

	exec := &recordingExecutor{}
	require.NoError(t, scheduler.Run(t.Context(), exec))

	assert.Equal(t, []string{"base", "feature"}, exec.items)
}

func TestScheduler_Run_missingBaseStartsReady(t *testing.T) {
	scheduler, err := New([]Item[string]{
		{ID: "child", BaseID: "external", Value: "child"},
		{ID: "root", Value: "root"},
	}, nil)
	require.NoError(t, err)

	exec := &recordingExecutor{}
	require.NoError(t, scheduler.Run(t.Context(), exec))

	assert.Equal(t, []string{"child", "root"}, exec.items)
}

func TestScheduler_Run_independentBranchContinuesAfterFailure(
	t *testing.T,
) {
	failErr := errors.New("merge failed")
	observer := &recordingObserver[string]{}
	scheduler, err := New([]Item[string]{
		{ID: "root", Value: "root"},
		{ID: "broken", BaseID: "root", Value: "broken"},
		{ID: "blocked", BaseID: "broken", Value: "blocked"},
		{ID: "independent", BaseID: "root", Value: "independent"},
	}, &Options[string]{
		Observer: observer,
	})
	require.NoError(t, err)

	err = scheduler.Run(t.Context(), &recordingExecutor{
		fail: map[string]error{"broken": failErr},
	})
	require.Error(t, err)

	var itemErr *ItemError
	require.ErrorAs(t, err, &itemErr)
	assert.Equal(t, "broken", itemErr.ID)
	assert.ErrorIs(t, err, failErr)
	assert.Equal(t, []string{"broken"}, observer.failed)
	assert.Equal(t, []skipRecord[string]{
		{item: "blocked", reason: SkipBecauseBelowFailed},
	}, observer.skipped)
}

func TestScheduler_Run_failFastSkipsRemainingPending(t *testing.T) {
	observer := &recordingObserver[string]{}
	scheduler, err := New([]Item[string]{
		{ID: "root", Value: "root"},
		{ID: "broken", BaseID: "root", Value: "broken"},
		{ID: "blocked", BaseID: "broken", Value: "blocked"},
		{ID: "other", BaseID: "root", Value: "other"},
	}, &Options[string]{
		FailFast: true,
		Observer: observer,
	})
	require.NoError(t, err)

	err = scheduler.Run(t.Context(), &recordingExecutor{
		fail: map[string]error{"broken": errors.New("merge failed")},
	})
	require.Error(t, err)

	assert.Equal(t, []skipRecord[string]{
		{item: "blocked", reason: SkipBecauseBelowFailed},
		{item: "other", reason: SkipBecauseFailFast},
	}, observer.skipped)
}

func TestScheduler_Run_returnsItemError(t *testing.T) {
	failErr := errors.New("merge failed")
	scheduler, err := New([]Item[string]{
		{ID: "broken", Value: "broken"},
	}, nil)
	require.NoError(t, err)

	err = scheduler.Run(t.Context(), &recordingExecutor{
		fail: map[string]error{"broken": failErr},
	})
	require.Error(t, err)

	var itemErr *ItemError
	require.ErrorAs(t, err, &itemErr)
	assert.Equal(t, "broken", itemErr.ID)
	assert.ErrorIs(t, err, failErr)
}

func TestNew_rejectsDuplicateID(t *testing.T) {
	_, err := New([]Item[string]{
		{ID: "same", Value: "one"},
		{ID: "same", Value: "two"},
	}, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `duplicate item ID "same"`)
}

func TestNew_rejectsCycle(t *testing.T) {
	_, err := New([]Item[string]{
		{ID: "one", BaseID: "two", Value: "one"},
		{ID: "two", BaseID: "three", Value: "two"},
		{ID: "three", BaseID: "one", Value: "three"},
	}, nil)

	require.Error(t, err)
	assert.Contains(
		t,
		err.Error(),
		"cycle includes items: one -> two -> three -> one",
	)
}

type recordingExecutor struct {
	items []string
	fail  map[string]error
}

func (e *recordingExecutor) Merge(
	_ context.Context,
	item string,
) error {
	e.items = append(e.items, item)
	return e.fail[item]
}

type recordingObserver[T comparable] struct {
	failed  []T
	skipped []skipRecord[T]
}

func (o *recordingObserver[T]) Failed(item T, _ error) {
	o.failed = append(o.failed, item)
}

func (o *recordingObserver[T]) Skipped(item T, reason SkipReason) {
	o.skipped = append(o.skipped, skipRecord[T]{
		item:   item,
		reason: reason,
	})
}

type skipRecord[T comparable] struct {
	item   T
	reason SkipReason
}
