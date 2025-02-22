package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/shurcooL/githubv4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/logutil"
	"go.abhg.dev/testing/stub"
)

// SetListChangeCommentsPageSize changes the page size
// used for listing change comments.
//
// It restores the old value after the test finishes.
func SetListChangeCommentsPageSize(t testing.TB, pageSize int) {
	t.Cleanup(stub.Value(&_listChangeCommentsPageSize, pageSize))
}

func TestListChangeComments(t *testing.T) {
	type commentRes struct {
		ID   string `json:"id"`
		Body string `json:"body"`
		URL  string `json:"url"`

		ViewerCanUpdate bool `json:"viewerCanUpdate"`
		ViewerDidAuthor bool `json:"viewerDidAuthor"`

		CreatedAt time.Time `json:"createdAt"`
		UpdatedAt time.Time `json:"updatedAt"`
	}

	tests := []struct {
		name string
		give []commentRes
		opts *forge.ListChangeCommentsOptions

		wantBodies []string
	}{
		{
			name: "NoFilter",
			give: []commentRes{
				{
					ID:   "abc",
					Body: "hello",
					URL:  "https://example.com/comment/abc",
				},
				{
					ID:   "def",
					Body: "world",
					URL:  "https://example.com/comment/def",
				},
			},
			wantBodies: []string{"hello", "world"},
		},
		{
			name: "BodyMatchesAll",
			give: []commentRes{
				{
					ID:   "abc",
					Body: "hello",
					URL:  "https://example.com/comment/abc",
				},
				{
					ID:   "def",
					Body: "world",
					URL:  "https://example.com/comment/def",
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
			name: "CanUpdate",
			give: []commentRes{
				{
					ID:              "abc",
					Body:            "hello",
					URL:             "https://example.com/comment/abc",
					ViewerCanUpdate: true,
				},
				{
					ID:              "def",
					Body:            "world",
					URL:             "https://example.com/comment/def",
					ViewerCanUpdate: false,
				},
			},
			opts: &forge.ListChangeCommentsOptions{
				CanUpdate: true,
			},
			wantBodies: []string{"hello"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := map[string]any{
				"data": map[string]any{
					"node": map[string]any{
						"comments": map[string]any{
							"pageInfo": map[string]any{
								"hasNextPage": false,
							},
							"nodes": tt.give,
						},
					},
				},
			}

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				assert.NoError(t, enc.Encode(response))
			}))
			defer srv.Close()

			repo, err := newRepository(
				t.Context(), new(Forge),
				"owner", "repo",
				logutil.TestLogger(t),
				githubv4.NewEnterpriseClient(srv.URL, nil),
				"repoID",
			)
			require.NoError(t, err)

			prID := PR{Number: 1, GQLID: "prID"}

			ctx := t.Context()
			var bodies []string
			for comment, err := range repo.ListChangeComments(ctx, &prID, tt.opts) {
				require.NoError(t, err)
				bodies = append(bodies, comment.Body)
			}

			assert.Equal(t, tt.wantBodies, bodies)
		})
	}
}
