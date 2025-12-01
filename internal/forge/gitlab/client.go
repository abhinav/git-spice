package gitlab

import (
	"context"
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.abhg.dev/gs/internal/must"
	"golang.org/x/oauth2"
)

type gitlabClient struct {
	MergeRequests    mergeRequestsService
	Notes            notesService
	Projects         projectsService
	ProjectTemplates projectTemplatesService
	Users            usersService
}

func newGitLabClient(ctx context.Context, baseURL string, tok *AuthenticationToken) (*gitlabClient, error) {
	var authSource gitlab.AuthSource
	switch tok.AuthType {
	case AuthTypePAT, AuthTypeEnvironmentVariable:
		authSource = &patAuthSource{token: tok.AccessToken}

	case AuthTypeGitLabCLI:
		// For GitLab CLI, AccessToken will be empty.
		token, err := newGitLabCLI("").Token(ctx, tok.Hostname)
		if err != nil {
			return nil, fmt.Errorf("get token from GitLab CLI: %w", err)
		}

		authSource = gitlab.OAuthTokenSource{
			TokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}),
		}

	case AuthTypeOAuth2:
		// Needs a different client constructor.
		authSource = gitlab.OAuthTokenSource{
			TokenSource: oauth2.StaticTokenSource(&oauth2.Token{
				AccessToken: tok.AccessToken,
			}),
		}
	}

	must.NotBeNilf(authSource,
		"No source for authentication type: %v", tok.AuthType)

	client, err := gitlab.NewAuthSourceClient(authSource, gitlab.WithBaseURL(baseURL))
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

type patAuthSource struct{ token string }

var _ gitlab.AuthSource = (*patAuthSource)(nil)

func (p *patAuthSource) Init(context.Context, *gitlab.Client) error { return nil }

func (p *patAuthSource) Header(context.Context) (key string, value string, err error) {
	return "PRIVATE-TOKEN", p.token, nil
}

// mergeRequestsService allows creating, listing, and fetching merge requests.
type mergeRequestsService interface {
	CreateMergeRequest(
		pid any,
		opt *gitlab.CreateMergeRequestOptions,
		options ...gitlab.RequestOptionFunc,
	) (*gitlab.MergeRequest, *gitlab.Response, error)

	GetMergeRequest(
		pid any,
		mergeRequest int64,
		opt *gitlab.GetMergeRequestsOptions,
		options ...gitlab.RequestOptionFunc,
	) (*gitlab.MergeRequest, *gitlab.Response, error)

	UpdateMergeRequest(
		pid any,
		mergeRequest int64,
		opt *gitlab.UpdateMergeRequestOptions,
		options ...gitlab.RequestOptionFunc,
	) (*gitlab.MergeRequest, *gitlab.Response, error)

	ListProjectMergeRequests(
		pid any,
		opt *gitlab.ListProjectMergeRequestsOptions,
		options ...gitlab.RequestOptionFunc,
	) ([]*gitlab.BasicMergeRequest, *gitlab.Response, error)
}

// notesService allows posting, listing, and fetching notes (comments)
// on merge requests.
type notesService interface {
	CreateMergeRequestNote(
		pid any,
		mergeRequest int64,
		opt *gitlab.CreateMergeRequestNoteOptions,
		options ...gitlab.RequestOptionFunc,
	) (*gitlab.Note, *gitlab.Response, error)

	UpdateMergeRequestNote(
		pid any,
		mergeRequest int64,
		note int64,
		opt *gitlab.UpdateMergeRequestNoteOptions,
		options ...gitlab.RequestOptionFunc,
	) (*gitlab.Note, *gitlab.Response, error)

	ListMergeRequestNotes(
		pid any,
		mergeRequest int64,
		opt *gitlab.ListMergeRequestNotesOptions,
		options ...gitlab.RequestOptionFunc,
	) ([]*gitlab.Note, *gitlab.Response, error)

	DeleteMergeRequestNote(
		pid any,
		mergeRequest, note int64,
		options ...gitlab.RequestOptionFunc,
	) (*gitlab.Response, error)
}

// projectsService allows listing and accessing projects.
type projectsService interface {
	GetProject(
		pid any,
		opt *gitlab.GetProjectOptions,
		options ...gitlab.RequestOptionFunc,
	) (*gitlab.Project, *gitlab.Response, error)
}

// projectTemplatesService allows listing and accessing project templates.
type projectTemplatesService interface {
	ListTemplates(
		pid any,
		templateType string,
		opt *gitlab.ListProjectTemplatesOptions,
		options ...gitlab.RequestOptionFunc,
	) ([]*gitlab.ProjectTemplate, *gitlab.Response, error)

	GetProjectTemplate(
		pid any,
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

	ListUsers(
		opt *gitlab.ListUsersOptions,
		options ...gitlab.RequestOptionFunc,
	) ([]*gitlab.User, *gitlab.Response, error)
}
