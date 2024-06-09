// Package maputil provides utilities for working with maps.
// It should be considered an extension to the std maps package.
package maputil

// Keys returns a slice of all keys in the given map.
// The order of the keys is unspecified.
func Keys[K comparable, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
