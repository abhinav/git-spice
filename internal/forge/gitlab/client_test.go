package gitlab

import gitlab "gitlab.com/gitlab-org/api/client-go"

var (
	_ mergeRequestsService    = (*gitlab.MergeRequestsService)(nil)
	_ notesService            = (*gitlab.NotesService)(nil)
	_ projectsService         = (*gitlab.ProjectsService)(nil)
	_ projectTemplatesService = (*gitlab.ProjectTemplatesService)(nil)
)
