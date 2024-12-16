package gitlab

import (
	"context"
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.abhg.dev/gs/internal/must"
)

type gitlabClient struct {
	MergeRequests    mergeRequestsService
	Notes            notesService
	Projects         projectsService
	ProjectTemplates projectTemplatesService
	Users            usersService
}

func newGitLabClient(ctx context.Context, baseURL string, tok *AuthenticationToken) (*gitlabClient, error) {
	var newClient func(string, ...gitlab.ClientOptionFunc) (*gitlab.Client, error)
	accessToken := tok.AccessToken
	switch tok.AuthType {
	case AuthTypePAT:
		newClient = gitlab.NewClient
	case AuthTypeGitLabCLI:
		// For GitLab CLI, AccessToken will be empty.
		token, err := newGitLabCLI("").Token(ctx, tok.Hostname)
		if err != nil {
			return nil, fmt.Errorf("get token from GitLab CLI: %w", err)
		}

		accessToken = token
		fallthrough
	case AuthTypeOAuth2:
		newClient = gitlab.NewOAuthClient
	default:
		return nil, fmt.Errorf("unknown auth type: %d", tok.AuthType)
	}

	must.NotBeBlankf(accessToken,
		"access token must be set for auth type: %v", tok.AuthType)

	client, err := newClient(accessToken, gitlab.WithBaseURL(baseURL))
	if err != nil {
		return nil, err
	}
	return &gitlabClient{
		MergeRequests:    client.MergeRequests,
		Notes:            client.Notes,
		ProjectTemplates: client.ProjectTemplates,
		Projects:         client.Projects,
		Users:            client.Users,
	}, nil
}

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

	DeleteMergeRequestNote(
		pid interface{},
		mergeRequest, note int,
		options ...gitlab.RequestOptionFunc,
	) (*gitlab.Response, error)
}

// projectsService allows listing and accessing projects.
type projectsService interface {
	GetProject(
		pid interface{},
		opt *gitlab.GetProjectOptions,
		options ...gitlab.RequestOptionFunc,
	) (*gitlab.Project, *gitlab.Response, error)
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

// usersService allows listing and accessing users.
type usersService interface {
	CurrentUser(
		options ...gitlab.RequestOptionFunc,
	) (*gitlab.User, *gitlab.Response, error)
}
