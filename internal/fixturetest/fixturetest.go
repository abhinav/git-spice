// Package fixturetest allows generating values using a possibly-random source
// on the first run of a test, and stores it in a file for subsequent runs.
//
// # Usage
//
// In your test, add a flag or other means of requesting the update mode:
//
//	var _update = flag.Bool("update", false, "update golden files")
//
// Configure the fixtures with the update flag:
//
//	var _fixtures = fixturetest.Config{Update: _update}
//
// Set up one or more fixtures:
//
//	branchNameFixture := fixturetest.New(_fixtures, "branchName", func() string {
//		return randomBranchName()
//	})
//
// Generate or fetch values using the Get method:
//
//	branchNameFixture.Get(t)
//
// If the test is in update mode, the provided function will be called,
// and the value persisted to disk.
// Otherwise, the value will be read from disk and passed to the function.
package fixturetest

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/stretchr/testify/require"
)

// TestingT is a subset of the testing.TB interface.
type TestingT interface {
	Helper()
	Name() string
	Errorf(format string, args ...interface{})
	FailNow()
}

// Config configures the behavior of the fixture system.
type Config struct {
	// Update is a pointer to a value speciftying
	// whether we're in update mode or read mode.
	//
	// This must not be nil.
	Update *bool // required
}

// Fixture is a value that is sourced from a function in update mode,
// and from a file in read mode.
type Fixture[T any] struct {
	cfg  Config
	name string
	gen  func() T
}

// New creates a new fixture with the given configuration.
func New[T any](cfg Config, name string, gen func() T) Fixture[T] {
	return Fixture[T]{cfg, name, gen}
}

// Get returns the value of the fixture.
// If in update mode, the value is generated using the provided function.
// Otherwise, the value is read from disk.
func (f Fixture[T]) Get(t TestingT) T {
	t.Helper()

	fpath := filepath.Join("testdata", t.Name(), f.name)
	if *f.cfg.Update {
		v := f.gen()

		require.NoError(t, os.MkdirAll(filepath.Dir(fpath), 0o755))
		bs, err := json.MarshalIndent(v, "", "  ")
		require.NoError(t, err)

		require.NoError(t, os.WriteFile(fpath, bs, 0o644))
		return v
	}

	bs, err := os.ReadFile(fpath)
	require.NoError(t, err)

	var v T
	require.NoError(t, json.Unmarshal(bs, &v))
	return v
}
