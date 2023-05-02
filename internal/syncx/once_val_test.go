package syncx

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetOnce(t *testing.T) {
	t.Parallel()

	t.Run("unset get", func(t *testing.T) {
		t.Parallel()

		var x SetOnce[int]
		assert.Equal(t, 42, x.Get(42))
		assert.Equal(t, 42, x.Get(0))

		t.Run("ignored set", func(t *testing.T) {
			t.Parallel()

			x.Set(100)
			assert.Equal(t, 42, x.Get(100))
		})
	})

	t.Run("set and get", func(t *testing.T) {
		t.Parallel()

		var x SetOnce[int]
		x.Set(42)
		assert.Equal(t, 42, x.Get(0))

		t.Run("ignored set", func(t *testing.T) {
			t.Parallel()

			x.Set(100)
			assert.Equal(t, 42, x.Get(100))
		})
	})
}

func TestSetOnce_race(t *testing.T) {
	t.Parallel()

	const N = 100

	var (
		value SetOnce[int]
		ready sync.WaitGroup // synchronize Get/Set operation
		done  sync.WaitGroup // synchronize waiting
	)
	ready.Add(N)
	done.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer done.Done()

			ready.Done() // I'm ready.
			ready.Wait() // Are others?

			if i%2 == 0 {
				value.Get(i)
			} else {
				value.Set(i)
			}
		}(i)
	}
	done.Wait()
}
