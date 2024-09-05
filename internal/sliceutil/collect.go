// Package sliceutil contains utility functions for working with slices.
// It's an extension of the std slices package.
package sliceutil

import "iter"

// CollectErr collects items from a sequence of items and errors,
// stopping at the first error and returning it.
func CollectErr[T any](ents iter.Seq2[T, error]) ([]T, error) {
	var items []T
	for item, err := range ents {
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}
