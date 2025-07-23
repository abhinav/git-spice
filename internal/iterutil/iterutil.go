// Package iterutil contains utilities for working with iterators.
package iterutil

import "iter"

// Enumerate adds 0-indexing to a single value iterator.
func Enumerate[T any](seq iter.Seq[T]) iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		var idx int
		for item := range seq {
			if !yield(idx, item) {
				return
			}
			idx++
		}
	}
}
