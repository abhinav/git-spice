package iterutil

import (
	"iter"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnumerate(t *testing.T) {
	t.Run("EmptySequence", func(t *testing.T) {
		give := slices.Values([]string{})
		got := collectIndexed(Enumerate(give))
		assert.Empty(t, got)
	})

	t.Run("SingleItem", func(t *testing.T) {
		give := slices.Values([]string{"hello"})
		got := collectIndexed(Enumerate(give))
		want := []indexed[string]{
			{0, "hello"},
		}
		assert.Equal(t, want, got)
	})

	t.Run("MultipleItems", func(t *testing.T) {
		give := slices.Values([]string{"foo", "bar", "baz"})
		got := collectIndexed(Enumerate(give))
		want := []indexed[string]{
			{0, "foo"},
			{1, "bar"},
			{2, "baz"},
		}
		assert.Equal(t, want, got)
	})

	t.Run("EarlyTermination", func(t *testing.T) {
		give := slices.Values([]int{10, 20, 30, 40, 50})

		var got []indexed[int]
		for idx, val := range Enumerate(give) {
			got = append(got, indexed[int]{idx, val})
			if idx == 2 {
				break
			}
		}

		want := []indexed[int]{
			{0, 10},
			{1, 20},
			{2, 30},
		}
		assert.Equal(t, want, got)
	})

	t.Run("DifferentTypes", func(t *testing.T) {
		give := slices.Values([]int{42, 100, 0})
		got := collectIndexed(Enumerate(give))
		want := []indexed[int]{
			{0, 42},
			{1, 100},
			{2, 0},
		}
		assert.Equal(t, want, got)
	})

	t.Run("CustomIterator", func(t *testing.T) {
		give := func(yield func(string) bool) {
			items := []string{"alpha", "beta", "gamma"}
			for _, item := range items {
				if !yield(item) {
					return
				}
			}
		}
		got := collectIndexed(Enumerate(iter.Seq[string](give)))
		want := []indexed[string]{
			{0, "alpha"},
			{1, "beta"},
			{2, "gamma"},
		}
		assert.Equal(t, want, got)
	})
}

type indexed[T any] struct {
	Index int
	Value T
}

func collectIndexed[T any](seq iter.Seq2[int, T]) []indexed[T] {
	var results []indexed[T]
	for idx, val := range seq {
		results = append(results, indexed[T]{idx, val})
	}
	return results
}
