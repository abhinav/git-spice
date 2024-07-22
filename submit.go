package main

import (
	"sync"

	"go.abhg.dev/gs/internal/forge"
)

// submitSession is a single session of submitting branches.
// This provides the ability to share state between
// the multiple 'branch submit' invocations made by
// 'stack submit', 'upstack submit', and 'downstack submit'.
//
// The zero value of this type is a valid empty session.
type submitSession struct {
	// Branches that have been submitted (created or updated)
	// in this session.
	branches []string

	// Values that are memoized across multiple branch submits.
	remote     memoizedValues[string, error]
	remoteRepo memoizedValues[forge.Repository, error]
}

type memoizedValues[A, B any] struct {
	once sync.Once

	a A
	b B
}

func (m *memoizedValues[A, B]) Get(f func() (A, B)) (A, B) {
	m.once.Do(func() { m.a, m.b = f() })
	return m.a, m.b
}
