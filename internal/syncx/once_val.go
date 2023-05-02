// Package syncx provides synchronization utilities.
package syncx

import "sync"

// SetOnce is a variable that can only be set once.
// After the first Set, all following calls are ignored.
type SetOnce[T any] struct {
	value T
	once  sync.Once
}

// TODO: Use sync.OnceValue when Go 1.21 is released.

// Set sets the value for this SetOnce if it wasn't set already.
// If Set was already called before, this call is ignored.
func (o *SetOnce[T]) Set(v T) {
	o.once.Do(func() { o.value = v })
}

// Get returns the value that was previously set,
// or fallback if it wasn't.
func (o *SetOnce[T]) Get(fallback T) T {
	o.Set(fallback)
	return o.value
}
