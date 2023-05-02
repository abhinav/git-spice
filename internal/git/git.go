// Package git provides access to the Git CLI with a Git library-like
// interface.
//
// All shell-to-Git interactions should be done through this package.
package git

import "context"

type Git interface {
	AddNote(context.Context, *AddNoteRequest) error
}

type AddNoteRequest struct {
	Ref     string // e.g. "refs/notes/commits"
	Force   bool
	Object  string // e.g. "HEAD"
	Message string
}
