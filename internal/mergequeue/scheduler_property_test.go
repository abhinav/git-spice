package mergequeue

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func TestScheduler_Run_accountsForEveryItem(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		count := rapid.IntRange(0, 30).Draw(t, "count")
		ids := make([]string, count)
		items := make([]Item, count)
		record := new(recordingQueue)
		failure := errors.New("injected run failure")
		for idx := range items {
			id := fmt.Sprintf("item-%d", idx)
			ids[idx] = id
			item := newTestItem(id, record)
			// Earlier items are the only possible bases, so every generated
			// input is a forest without needing the scheduler to validate it.
			if base := rapid.IntRange(-1, idx-1).Draw(t, "base"); base >= 0 {
				item.withParent(ids[base])
			}
			if rapid.Bool().Draw(t, "fails") {
				item.withRunError(failure)
			}
			items[idx] = item
		}
		items = rapid.Permutation(items).Draw(t, "order")
		observer := new(recordingObserver)
		scheduler, err := New(items, &Options{
			Observer: observer,
			Barrier:  func(context.Context) error { return nil },
		})
		require.NoError(t, err)

		// Immediate Prepare and Run calls can make their results available
		// together. Every item must still finish or receive a failure/skip
		// outcome before Run returns, including work unlocked by other items.
		err = scheduler.Run(t.Context())
		t.Logf("events: %v", record.events())
		terminal := append(record.done(), observer.failed...)
		for _, skipped := range observer.skipped {
			terminal = append(terminal, skipped.item)
		}
		assert.ElementsMatch(t, ids, terminal)
		if len(observer.failed) == 0 {
			assert.NoError(t, err)
		} else {
			assert.ErrorIs(t, err, failure)
		}
	})
}
