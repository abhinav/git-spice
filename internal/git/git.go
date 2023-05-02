// Package git provides access to the Git CLI with a Git library-like
// interface.
//
// All shell-to-Git interactions should be done through this package.
package git

import "context"

// Git provides access to the Git CLI.
type Git interface {
	// AddNote adds a Git note to an object.
	AddNote(context.Context, AddNoteRequest) error

	// ListCommits lists the commits matching the given criteria.
	ListCommits(context.Context, ListCommitsRequest) ([]string, error)
}

// AddNoteRequest is a request to add a Git note to an object.
type AddNoteRequest struct {
	// Ref is the notes reference.
	// For example, "refs/notes/commits".
	// If empty, the default notes reference will be used.
	Ref string

	// Force indicates whether to overwrite an existing note.
	// If false, an error will be returned if a note already exists.
	Force bool

	// Object is the object to add the note to.
	// For example, "HEAD".
	Object string

	// Message holds the contents of the note.
	Message string
}

// ListCommitsRequest holds the parameters for listing commits.
type ListCommitsRequest struct {
	// Start is the starting commit, inclusive.
	Start string

	// Stop is the stopping commit, exclusive.
	Stop string
}
