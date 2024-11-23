package gitlab

import "github.com/xanzy/go-gitlab"

// This file defines subset of the GitLab client API that we use.

// mergeRequestsService allows creating, listing, and fetching merge requests.
type mergeRequestsService interface {
	CreateMergeRequest(
		pid interface{},
		opt *gitlab.CreateMergeRequestOptions,
		options ...gitlab.RequestOptionFunc,
	) (*gitlab.MergeRequest, *gitlab.Response, error)

	GetMergeRequest(
		pid interface{},
		mergeRequest int,
		opt *gitlab.GetMergeRequestsOptions,
		options ...gitlab.RequestOptionFunc,
	) (*gitlab.MergeRequest, *gitlab.Response, error)

	UpdateMergeRequest(
		pid interface{},
		mergeRequest int,
		opt *gitlab.UpdateMergeRequestOptions,
		options ...gitlab.RequestOptionFunc,
	) (*gitlab.MergeRequest, *gitlab.Response, error)

	ListProjectMergeRequests(
		pid interface{},
		opt *gitlab.ListProjectMergeRequestsOptions,
		options ...gitlab.RequestOptionFunc,
	) ([]*gitlab.MergeRequest, *gitlab.Response, error)
}

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

// projectTemplatesService allows listing and accessing project templates.
type projectTemplatesService interface {
	ListTemplates(
		pid interface{},
		templateType string,
		opt *gitlab.ListProjectTemplatesOptions,
		options ...gitlab.RequestOptionFunc,
	) ([]*gitlab.ProjectTemplate, *gitlab.Response, error)

	GetProjectTemplate(
		pid interface{},
		templateType string,
		templateName string,
		options ...gitlab.RequestOptionFunc,
	) (*gitlab.ProjectTemplate, *gitlab.Response, error)
}
