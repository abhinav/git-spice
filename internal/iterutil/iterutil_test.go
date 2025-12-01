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

func TestUniq(t *testing.T) {
	tests := []struct {
		name string
		give [][]string
		want []string
	}{
		{
			name: "SingleSliceNoDuplicates",
			give: [][]string{{"foo", "bar", "baz"}},
			want: []string{"foo", "bar", "baz"},
		},
		{
			name: "SingleSliceWithDuplicates",
			give: [][]string{{"foo", "bar", "foo", "baz", "bar"}},
			want: []string{"foo", "bar", "baz"},
		},
		{
			name: "MultipleSlicesNoDuplicates",
			give: [][]string{{"a", "b"}, {"c", "d"}},
			want: []string{"a", "b", "c", "d"},
		},
		{
			name: "MultipleSlicesWithDuplicatesAcrossSlices",
			give: [][]string{{"a", "b", "c"}, {"b", "c", "d"}, {"c", "d", "e"}},
			want: []string{"a", "b", "c", "d", "e"},
		},
		{
			name: "MultipleSlicesWithDuplicatesWithinAndAcross",
			give: [][]string{{"a", "b", "b", "c"}, {"b", "c", "d", "d"}, {"c", "d", "e"}},
			want: []string{"a", "b", "c", "d", "e"},
		},
		{
			name: "EmptySlicesMixed",
			give: [][]string{{"a"}, {}, {"b", "a"}, {}},
			want: []string{"a", "b"},
		},
		{
			name: "PreserveOrder",
			give: [][]string{{"z", "a", "m"}, {"a", "b", "z"}},
			want: []string{"z", "a", "m", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slices.Collect(Uniq(tt.give...))
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("EmptySlices", func(t *testing.T) {
		got := slices.Collect(Uniq[string]())
		assert.Empty(t, got)
	})

	t.Run("EarlyTermination", func(t *testing.T) {
		var got []int
		for val := range Uniq([]int{1, 2, 3, 4, 5}, []int{6, 7, 8}) {
			got = append(got, val)
			if val == 3 {
				break
			}
		}
		want := []int{1, 2, 3}
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
