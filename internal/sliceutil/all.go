package sliceutil

import "iter"

// All2 returns an iterator over the items in the slice,
// using the zero value for the second type B.
func All2[B, A any](items []A) iter.Seq2[A, B] {
	return func(yield func(A, B) bool) {
		var zeroB B
		for _, item := range items {
			if !yield(item, zeroB) {
				return
			}
		}
	}
}
