package gitlab

import "github.com/xanzy/go-gitlab"

// This file defines subset of the GitLab client API that we use.

// notesService allows posting, listing, and fetching notes (comments)
// on merge requests.
type notesService interface {
	CreateMergeRequestNote(
		pid interface{},
		mergeRequest int,
		opt *gitlab.CreateMergeRequestNoteOptions,
		options ...gitlab.RequestOptionFunc,
	) (*gitlab.Note, *gitlab.Response, error)

	UpdateMergeRequestNote(
		pid interface{},
		mergeRequest int,
		note int,
		opt *gitlab.UpdateMergeRequestNoteOptions,
		options ...gitlab.RequestOptionFunc,
	) (*gitlab.Note, *gitlab.Response, error)

	ListMergeRequestNotes(
		pid interface{},
		mergeRequest int,
		opt *gitlab.ListMergeRequestNotesOptions,
		options ...gitlab.RequestOptionFunc,
	) ([]*gitlab.Note, *gitlab.Response, error)
}

var _ notesService = (*gitlab.NotesService)(nil)
