package bitbucket

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_PullRequestCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/2.0/repositories/engineering/warp-core/pullrequests", r.URL.Path)
		assertJSONBody(t, r, `{
			"title":"Stabilize nacelles",
			"description":"Replace the failing injector.",
			"source":{"branch":{"name":"feature/refit"}},
			"destination":{"branch":{"name":"main"}},
			"reviewers":[{"uuid":"{spock}"}],
			"draft":true
		}`)
		writeJSON(t, w, http.StatusCreated, PullRequest{
			ID:          55,
			Title:       "Stabilize nacelles",
			Description: "Replace the failing injector.",
			State:       "OPEN",
			Draft:       true,
			Source: BranchRef{
				Branch: Branch{Name: "feature/refit"},
				Commit: &Commit{Hash: "abc123"},
			},
			Destination: BranchRef{
				Branch: Branch{Name: "main"},
			},
			Reviewers: []User{
				{UUID: "{spock}", Username: "spock"},
			},
			Links: PullRequestLinks{
				HTML: Link{Href: "https://bitbucket.org/engineering/warp-core/pull-requests/55"},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	pr, _, err := client.PullRequestCreate(t.Context(), "engineering", "warp-core", &PullRequestCreateRequest{
		Title:       "Stabilize nacelles",
		Description: "Replace the failing injector.",
		Source: BranchRef{
			Branch: Branch{Name: "feature/refit"},
		},
		Destination: BranchRef{
			Branch: Branch{Name: "main"},
		},
		Reviewers: []Reviewer{{UUID: "{spock}"}},
		Draft:     true,
	})
	require.NoError(t, err)

	assert.Equal(t, int64(55), pr.ID)
	assert.Equal(t, "feature/refit", pr.Source.Branch.Name)
	require.NotNil(t, pr.Source.Commit)
	assert.Equal(t, "abc123", pr.Source.Commit.Hash)
	require.Len(t, pr.Reviewers, 1)
	assert.Equal(t, "spock", pr.Reviewers[0].Username)
}

func TestClient_PullRequestGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/2.0/repositories/engineering/warp-core/pullrequests/55", r.URL.Path)
		writeJSON(t, w, http.StatusOK, PullRequest{
			ID:    55,
			Title: "Stabilize nacelles",
			Links: PullRequestLinks{
				HTML: Link{Href: "https://example.com/pr/55"},
			},
			Destination: BranchRef{
				Branch: Branch{Name: "main"},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	pr, _, err := client.PullRequestGet(t.Context(), "engineering", "warp-core", 55)
	require.NoError(t, err)
	assert.Equal(t, int64(55), pr.ID)
	assert.Equal(t, "main", pr.Destination.Branch.Name)
}

func TestClient_PullRequestUpdate(t *testing.T) {
	title := "Draft: Stabilize nacelles"
	base := "release"
	draft := true

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/2.0/repositories/engineering/warp-core/pullrequests/55", r.URL.Path)
		assertJSONBody(t, r, `{
			"title":"Draft: Stabilize nacelles",
			"destination":{"branch":{"name":"release"}},
			"reviewers":[{"uuid":"{spock}"}],
			"draft":true
		}`)
		writeJSON(t, w, http.StatusOK, PullRequest{
			ID:    55,
			Title: "Draft: Stabilize nacelles",
			Destination: BranchRef{
				Branch: Branch{Name: "release"},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	pr, _, err := client.PullRequestUpdate(t.Context(), "engineering", "warp-core", 55, &PullRequestUpdateRequest{
		Title: &title,
		Destination: &BranchRef{
			Branch: Branch{Name: base},
		},
		Reviewers: []Reviewer{{UUID: "{spock}"}},
		Draft:     &draft,
	})
	require.NoError(t, err)
	assert.Equal(t, "release", pr.Destination.Branch.Name)
}

func TestClient_PullRequestList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/2.0/repositories/engineering/warp-core/pullrequests", r.URL.Path)
		assert.Equal(t, `source.branch.name="feature/refit"`, r.URL.Query().Get("q"))
		assert.Equal(t, "20", r.URL.Query().Get("pagelen"))
		assert.Equal(t, "+values.reviewers", r.URL.Query().Get("fields"))
		writeJSON(t, w, http.StatusOK, PullRequestList{
			Values: []PullRequest{
				{ID: 55, Title: "One"},
				{ID: 56, Title: "Two"},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	prs, _, err := client.PullRequestList(
		t.Context(),
		"engineering",
		"warp-core",
		&PullRequestListOptions{
			Query:   `source.branch.name="feature/refit"`,
			PageLen: 20,
			Fields:  []string{"+values.reviewers"},
		},
	)
	require.NoError(t, err)
	require.Len(t, prs.Values, 2)
	assert.Equal(t, int64(56), prs.Values[1].ID)
}

func TestClient_CommentMethods(t *testing.T) {
	t.Run("Create", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/2.0/repositories/engineering/warp-core/pullrequests/55/comments", r.URL.Path)
			assertJSONBody(t, r, `{"content":{"raw":"Recalibrated the array."}}`)
			writeJSON(t, w, http.StatusCreated, Comment{
				ID:      88,
				Content: Content{Raw: "Recalibrated the array."},
			})
		}))
		defer srv.Close()

		client := newTestClient(t, srv)
		comment, _, err := client.CommentCreate(t.Context(), "engineering", "warp-core", 55, &CommentCreateRequest{
			Content: Content{Raw: "Recalibrated the array."},
		})
		require.NoError(t, err)
		assert.Equal(t, int64(88), comment.ID)
	})

	t.Run("Update", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPut, r.Method)
			assert.Equal(t, "/2.0/repositories/engineering/warp-core/pullrequests/55/comments/88", r.URL.Path)
			assertJSONBody(t, r, `{"content":{"raw":"Updated note"}}`)
			writeJSON(t, w, http.StatusOK, Comment{
				ID:      88,
				Content: Content{Raw: "Updated note"},
			})
		}))
		defer srv.Close()

		client := newTestClient(t, srv)
		comment, _, err := client.CommentUpdate(t.Context(), "engineering", "warp-core", 55, 88, &CommentCreateRequest{
			Content: Content{Raw: "Updated note"},
		})
		require.NoError(t, err)
		assert.Equal(t, "Updated note", comment.Content.Raw)
	})

	t.Run("Delete", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/2.0/repositories/engineering/warp-core/pullrequests/55/comments/88", r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		client := newTestClient(t, srv)
		resp, err := client.CommentDelete(t.Context(), "engineering", "warp-core", 55, 88)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	})

	t.Run("List", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/2.0/repositories/engineering/warp-core/pullrequests/55/comments", r.URL.Path)
			assert.Equal(t, "100", r.URL.Query().Get("pagelen"))
			writeJSON(t, w, http.StatusOK, CommentList{
				Values: []Comment{
					{
						ID:      88,
						Content: Content{Raw: "Needs review"},
						Inline: &Inline{
							Path: "warp.go",
						},
						Resolution: &Resolution{Type: "resolved"},
					},
				},
			})
		}))
		defer srv.Close()

		client := newTestClient(t, srv)
		comments, _, err := client.CommentList(
			t.Context(),
			"engineering",
			"warp-core",
			55,
			nil,
		)
		require.NoError(t, err)
		require.Len(t, comments.Values, 1)
		assert.Equal(t, "Needs review", comments.Values[0].Content.Raw)
		require.NotNil(t, comments.Values[0].Inline)
		assert.NotNil(t, comments.Values[0].Resolution)
	})
}

func TestClient_WorkspaceMemberList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/2.0/workspaces/engineering/members", r.URL.Path)
		writeJSON(t, w, http.StatusOK, WorkspaceMemberList{
			Values: []WorkspaceMember{
				{
					User: User{
						UUID:      "{spock}",
						Username:  "spock",
						AccountID: "42:{spock}",
						Nickname:  "spock",
					},
				},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	members, _, err := client.WorkspaceMemberList(t.Context(), "engineering", nil)
	require.NoError(t, err)
	require.Len(t, members.Values, 1)
	assert.Equal(t, "spock", members.Values[0].User.Username)
}
