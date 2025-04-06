//go:build !profile

package main

// ProfileFlags is a placeholder for the profile flags.
// The corresponding _enable Go file contains the actual implementation.
//
// To build gs with profiling enabled, use:
//
//	go build -tags profile
type ProfileFlags struct{}

// Start is a no-op for the profile disabled build.
func (*ProfileFlags) Start() error { return nil }

// Stop is a no-op for the profile disabled build.
func (*ProfileFlags) Stop() error { return nil }
