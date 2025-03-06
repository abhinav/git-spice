package gitlab

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"go.abhg.dev/testing/stub"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/logutil"
)

// SetListChangeCommentsPageSize changes the page size
// used for listing change comments.
//
// It restores the old value after the test finishes.
func SetListChangeCommentsPageSize(t testing.TB, pageSize int) {
	t.Cleanup(stub.Value(&_listChangeCommentsPageSize, pageSize))
}

func TestListChangeComments(t *testing.T) {
	tests := []struct {
		name    string
		project gitlab.Project
		user    gitlab.User
		notes   []gitlab.Note
		opts    *forge.ListChangeCommentsOptions

		wantBodies []string
	}{
		{
			name:    "NoFilter",
			project: newProject(100, gitlab.Ptr(gitlab.DeveloperPermissions), nil),
			user:    gitlab.User{ID: 1},
			notes: []gitlab.Note{
				{
					ID:   12,
					Body: "hello",
				},
				{
					ID:   13,
					Body: "world",
				},
			},
			wantBodies: []string{"hello", "world"},
		},
		{
			name:    "BodyMatchesAll",
			project: newProject(100, gitlab.Ptr(gitlab.DeveloperPermissions), nil),
			user:    gitlab.User{ID: 1},
			notes: []gitlab.Note{
				{
					ID:   12,
					Body: "hello",
				},
				{
					ID:   13,
					Body: "world",
				},
			},
			opts: &forge.ListChangeCommentsOptions{
				BodyMatchesAll: []*regexp.Regexp{
					regexp.MustCompile(`d$`),
				},
			},
			wantBodies: []string{"world"},
		},
		{
			name:    "CanUpdate",
			project: newProject(100, gitlab.Ptr(gitlab.DeveloperPermissions), nil),
			user:    gitlab.User{ID: 1},
			notes: []gitlab.Note{
				{
					ID:     12,
					Body:   "hello",
					Author: gitlab.NoteAuthor{ID: 2},
				},
				{
					ID:     13,
					Body:   "world",
					Author: gitlab.NoteAuthor{ID: 1},
				},
			},
			opts: &forge.ListChangeCommentsOptions{
				CanUpdate: true,
			},
			wantBodies: []string{"world"},
		},
		{
			name:    "CanUpdateByProjectAccessLevelPermission",
			project: newProject(100, gitlab.Ptr(gitlab.MaintainerPermissions), nil),
			user:    gitlab.User{ID: 1},
			notes: []gitlab.Note{
				{
					ID:     12,
					Body:   "hello",
					Author: gitlab.NoteAuthor{ID: 2},
				},
				{
					ID:     13,
					Body:   "world",
					Author: gitlab.NoteAuthor{ID: 2},
				},
			},
			opts: &forge.ListChangeCommentsOptions{
				CanUpdate: true,
			},
			wantBodies: []string{"hello", "world"},
		},
		{
			name:    "CanUpdateByGroupAccessLevelPermission",
			project: newProject(100, nil, gitlab.Ptr(gitlab.MaintainerPermissions)),
			user:    gitlab.User{ID: 1},
			notes: []gitlab.Note{
				{
					ID:     12,
					Body:   "hello",
					Author: gitlab.NoteAuthor{ID: 2},
				},
				{
					ID:     13,
					Body:   "world",
					Author: gitlab.NoteAuthor{ID: 2},
				},
			},
			opts: &forge.ListChangeCommentsOptions{
				CanUpdate: true,
			},
			wantBodies: []string{"hello", "world"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				switch r.RequestURI {
				case "/api/v4/projects/100":
					assert.NoError(t, enc.Encode(tt.project))
				case "/api/v4/user":
					assert.NoError(t, enc.Encode(tt.user))
				default:
					assert.NoError(t, enc.Encode(tt.notes))
				}
			}))
			defer srv.Close()

			client, _ := newGitLabClient(t.Context(), srv.URL, &AuthenticationToken{
				AuthType:    AuthTypePAT,
				AccessToken: "token",
			})
			repoID := 100
			repo, err := newRepository(
				t.Context(), new(Forge),
				"owner", "repo",
				logutil.TestLogger(t),
				client,
				&repoID,
			)
			require.NoError(t, err)

			mrID := MR{Number: 1}

			ctx := t.Context()
			var bodies []string
			for comment, err := range repo.ListChangeComments(ctx, &mrID, tt.opts) {
				require.NoError(t, err)
				bodies = append(bodies, comment.Body)
			}

			assert.Equal(t, tt.wantBodies, bodies)
		})
	}
}

func newProject(
	id int,
	projectAccessLevel *gitlab.AccessLevelValue,
	groupAccessLevel *gitlab.AccessLevelValue,
) gitlab.Project {
	project := new(gitlab.Project)
	project.ID = id
	project.Permissions = new(gitlab.Permissions)
	if projectAccessLevel != nil {
		project.Permissions.ProjectAccess = &gitlab.ProjectAccess{
			AccessLevel: *projectAccessLevel,
		}
	}
	if groupAccessLevel != nil {
		project.Permissions.GroupAccess = &gitlab.GroupAccess{
			AccessLevel: *groupAccessLevel,
		}
	}
	return *project
}
