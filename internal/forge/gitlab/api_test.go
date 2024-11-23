package gitlab

import "github.com/xanzy/go-gitlab"

var (
	_ mergeRequestsService    = (*gitlab.MergeRequestsService)(nil)
	_ notesService            = (*gitlab.NotesService)(nil)
	_ projectTemplatesService = (*gitlab.ProjectTemplatesService)(nil)
)
