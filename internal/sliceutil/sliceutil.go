package sliceutil

func RemoveFunc[T any](items []T, remove func(T) bool) []T {
	newItems := items[:0]
	for _, item := range items {
		if !remove(item) {
			newItems = append(newItems, item)
		}
	}
	return newItems
}
