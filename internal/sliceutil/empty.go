package sliceutil

import "iter"

// Empty2 returns an empty iterator for a sequence of two types A and B.
func Empty2[A, B any]() iter.Seq2[A, B] {
	return func(func(A, B) bool) {
	}
}
