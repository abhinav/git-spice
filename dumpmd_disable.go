//go:build !dumpmd

package main

// When building without the "dumpmd" build tag,
// the dumpmd does not do anything.

type dumpMarkdownCmd struct{}
