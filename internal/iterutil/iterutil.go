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

// Uniq returns an iterator that yields unique values
// from all provided slices in order,
// skipping duplicates.
func Uniq[T comparable](slices ...[]T) iter.Seq[T] {
	return func(yield func(T) bool) {
		seen := make(map[T]struct{})
		for _, slice := range slices {
			for _, item := range slice {
				if _, ok := seen[item]; ok {
					continue
				}
				seen[item] = struct{}{}
				if !yield(item) {
					return
				}
			}
		}
	}
}
