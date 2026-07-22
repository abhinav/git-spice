package forgejo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/forgejo"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
)

// Forgejo accepts a head commit guard on merge requests.
// This test keeps the JSON body contract visible until recorded integration
// fixtures can exercise stale-head protection against a real server.
func TestRepository_MergeChange_usesHeadHash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/repos/owner/repo":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.Repository{
					FullName:    "owner/repo",
					Permissions: &forgejo.Permission{Push: true},
				})
			case "/api/v1/user":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.User{ID: 1})
			case "/api/v1/repos/owner/repo/pulls/42/merge":
				assert.Equal(t, http.MethodPost, r.Method)
				assertGatewayJSONBody(t, r, `{
					"Do":"squash",
					"head_commit_id":"abc123"
				}`)
				writeGatewayJSON(t, w, http.StatusOK, forgejo.PullRequest{
					Index: 42,
				})
			default:
				t.Fatalf("unexpected request path: %s", r.URL.Path)
			}
		}))
	defer srv.Close()

	repo := newTestRepository(t, srv)
	require.NoError(t, repo.MergeChange(
		t.Context(),
		&PR{Number: 42},
		forge.MergeChangeOptions{
			Method:   forge.MergeMethodSquash,
			HeadHash: git.Hash("abc123"),
		},
	))
}

// Forgejo represents merged pull requests as closed pull requests with a
// separate merged flag, so merged lookups must list closed pull requests and
// map that flag back into the forge state.
func TestRepository_FindChangesByBranch_findsMergedPRs(t *testing.T) {
	var gotState string
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/repos/owner/repo":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.Repository{
					FullName:    "owner/repo",
					Permissions: &forgejo.Permission{Push: true},
				})
			case "/api/v1/user":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.User{ID: 1})
			case "/api/v1/repos/owner/repo/pulls":
				gotState = r.URL.Query().Get("state")
				writeGatewayJSON(t, w, http.StatusOK, []*forgejo.PullRequest{
					{
						Index:   11,
						HTMLURL: "https://forgejo.example.com/owner/repo/pulls/11",
						State:   "closed",
						Merged:  true,
						Title:   "Merged change",
						Head: &forgejo.PRBranchInfo{
							Ref: "feature",
							Repository: &forgejo.Repository{
								FullName: "owner/repo",
							},
						},
					},
				})
			default:
				t.Fatalf("unexpected request path: %s", r.URL.Path)
			}
		}))
	defer srv.Close()

	repo := newTestRepository(t, srv)
	changes, err := repo.FindChangesByBranch(
		t.Context(),
		"feature",
		forge.FindChangesOptions{State: forge.ChangeMerged},
	)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.Equal(t, forge.ChangeMerged, changes[0].State)
	assert.Equal(t, "closed", gotState)
}

// Forgejo branch filtering happens after listing pull requests.
// The repository must keep paging until enough matching pull requests survive
// that local filter.
func TestRepository_FindChangesByBranch_pagesBeforeFiltering(t *testing.T) {
	var pages []string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/repos/owner/repo":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.Repository{
					FullName:    "owner/repo",
					Permissions: &forgejo.Permission{Push: true},
				})
			case "/api/v1/user":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.User{ID: 1})
			case "/api/v1/repos/owner/repo/pulls":
				pages = append(pages, r.URL.Query().Get("page"))
				assert.Equal(t, "all", r.URL.Query().Get("state"))
				assert.Equal(t, "50", r.URL.Query().Get("limit"))

				switch r.URL.Query().Get("page") {
				case "1":
					w.Header().Set(
						"Link",
						`<`+srv.URL+`/api/v1/repos/owner/repo/pulls?page=2>; rel="next"`,
					)
					writeGatewayJSON(t, w, http.StatusOK, []*forgejo.PullRequest{
						{
							Index: 21,
							State: "open",
							Head: &forgejo.PRBranchInfo{
								Ref: "feature",
								Repository: &forgejo.Repository{
									FullName: "fork/repo",
								},
							},
						},
					})
				case "2":
					writeGatewayJSON(t, w, http.StatusOK, []*forgejo.PullRequest{
						{
							Index:   22,
							HTMLURL: "https://forgejo.example.com/owner/repo/pulls/22",
							State:   "open",
							Title:   "Target repository change",
							Head: &forgejo.PRBranchInfo{
								Ref: "feature",
								Repository: &forgejo.Repository{
									FullName: "owner/repo",
								},
							},
						},
					})
				default:
					t.Fatalf("unexpected page: %s", r.URL.Query().Get("page"))
				}
			default:
				t.Fatalf("unexpected request path: %s", r.URL.Path)
			}
		}))
	defer srv.Close()

	repo := newTestRepository(t, srv)
	changes, err := repo.FindChangesByBranch(
		t.Context(),
		"feature",
		forge.FindChangesOptions{Limit: 1},
	)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.Equal(t, int64(22), mustPR(changes[0].ID).Number)
	assert.Equal(t, []string{"1", "2"}, pages)
}

// Forgejo branch names are not globally unique.
// When a caller supplies a push repository, only pull requests from that
// source repository should match.
func TestRepository_FindChangesByBranch_filtersByPushRepository(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/repos/owner/repo":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.Repository{
					FullName:    "owner/repo",
					Permissions: &forgejo.Permission{Push: true},
				})
			case "/api/v1/user":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.User{ID: 1})
			case "/api/v1/repos/owner/repo/pulls":
				writeGatewayJSON(t, w, http.StatusOK, []*forgejo.PullRequest{
					{
						Index: 31,
						State: "open",
						Head: &forgejo.PRBranchInfo{
							Ref: "feature",
							Repository: &forgejo.Repository{
								FullName: "owner/repo",
							},
						},
					},
					{
						Index:   32,
						HTMLURL: "https://forgejo.example.com/owner/repo/pulls/32",
						State:   "open",
						Title:   "Fork repository change",
						Head: &forgejo.PRBranchInfo{
							Ref: "feature",
							Repository: &forgejo.Repository{
								FullName: "fork/repo",
							},
						},
					},
				})
			default:
				t.Fatalf("unexpected request path: %s", r.URL.Path)
			}
		}))
	defer srv.Close()

	repo := newTestRepository(t, srv)
	changes, err := repo.FindChangesByBranch(
		t.Context(),
		"feature",
		forge.FindChangesOptions{
			PushRepository: &RepositoryID{
				url:   "https://forgejo.example.com",
				owner: "fork",
				name:  "repo",
			},
		},
	)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.Equal(t, int64(32), mustPR(changes[0].ID).Number)
}

// Forgejo edit requests need numeric label IDs.
// The repository must page through labels until every requested name is
// resolved, because repositories may have more labels than one page returns.
func TestRepository_labelIDs_pagesUntilLabelsFound(t *testing.T) {
	var pages []string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/repos/owner/repo":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.Repository{
					FullName:    "owner/repo",
					Permissions: &forgejo.Permission{Push: true},
				})
			case "/api/v1/user":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.User{ID: 1})
			case "/api/v1/repos/owner/repo/labels":
				pages = append(pages, r.URL.Query().Get("page"))
				assert.Equal(t, "50", r.URL.Query().Get("limit"))

				switch r.URL.Query().Get("page") {
				case "1":
					w.Header().Set(
						"Link",
						`<`+srv.URL+`/api/v1/repos/owner/repo/labels?page=2>; rel="next"`,
					)
					writeGatewayJSON(t, w, http.StatusOK, []*forgejo.Label{
						{ID: 1, Name: "first"},
					})
				case "2":
					writeGatewayJSON(t, w, http.StatusOK, []*forgejo.Label{
						{ID: 2, Name: "second"},
					})
				default:
					t.Fatalf("unexpected page: %s", r.URL.Query().Get("page"))
				}
			default:
				t.Fatalf("unexpected request path: %s", r.URL.Path)
			}
		}))
	defer srv.Close()

	repo := newTestRepository(t, srv)
	ids, err := repo.labelIDs(t.Context(), []string{"second"})
	require.NoError(t, err)
	assert.Equal(t, []int64{2}, ids)
	assert.Equal(t, []string{"1", "2"}, pages)
}

func TestRepository_ComparisonURL(t *testing.T) {
	r := &Repository{
		owner: "example",
		repo:  "repo",
		forge: &Forge{Options: Options{URL: "https://forgejo.example.com"}},
	}

	t.Run("SameRepository", func(t *testing.T) {
		assert.Equal(t,
			"https://forgejo.example.com/example/repo/compare/main...feat",
			r.ComparisonURL(forge.ComparisonRequest{Base: "main", Head: "feat"}),
		)
	})

	t.Run("Fork", func(t *testing.T) {
		assert.Equal(t,
			"https://forgejo.example.com/example/repo/compare/main...fork/repo:feat%23review",
			r.ComparisonURL(forge.ComparisonRequest{
				Base: "main",
				Head: "feat#review",
				HeadRepository: &RepositoryID{
					url:   "https://forgejo.example.com",
					owner: "fork",
					name:  "repo",
				},
			}),
		)
	})
}

func newTestRepository(t *testing.T, srv *httptest.Server) *Repository {
	client, err := forgejo.NewClient(
		forgejo.StaticTokenSource(forgejo.Token{
			Type:  forgejo.TokenTypeAPIToken,
			Value: "test-token",
		}),
		&forgejo.ClientOptions{BaseURL: srv.URL},
	)
	require.NoError(t, err)

	repo, err := NewRepository(
		t.Context(),
		new(Forge),
		"owner",
		"repo",
		silog.Nop(),
		client,
	)
	require.NoError(t, err)
	return repo
}

func writeGatewayJSON(
	t *testing.T,
	w http.ResponseWriter,
	code int,
	v any,
) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	require.NoError(t, json.NewEncoder(w).Encode(v))
}

func assertGatewayJSONBody(t *testing.T, r *http.Request, want string) {
	t.Helper()
	var got json.RawMessage
	require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
	assert.JSONEq(t, want, string(got))
}
