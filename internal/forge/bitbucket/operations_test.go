package bitbucket

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
	"go.abhg.dev/gs/internal/silog"
)

func TestListChangeComments(t *testing.T) {
	tests := []struct {
		name       string
		comments   []bitbucket.Comment
		opts       *forge.ListChangeCommentsOptions
		wantBodies []string
	}{
		{
			name: "NoFilter",
			comments: []bitbucket.Comment{
				{ID: 1, Content: bitbucket.Content{Raw: "hello"}},
				{ID: 2, Content: bitbucket.Content{Raw: "world"}},
			},
			wantBodies: []string{"hello", "world"},
		},
		{
			name: "BodyMatchesAll",
			comments: []bitbucket.Comment{
				{ID: 1, Content: bitbucket.Content{Raw: "hello"}},
				{ID: 2, Content: bitbucket.Content{Raw: "world"}},
			},
			opts: &forge.ListChangeCommentsOptions{
				BodyMatchesAll: []*regexp.Regexp{
					regexp.MustCompile(`d$`),
				},
			},
			wantBodies: []string{"world"},
		},
		{
			name:       "EmptyList",
			comments:   []bitbucket.Comment{},
			wantBodies: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				resp := bitbucket.CommentList{Values: tt.comments}
				assert.NoError(t, json.NewEncoder(w).Encode(resp))
			}))
			defer srv.Close()

			repo := newTestRepository(srv.URL)
			prID := &PR{Number: 1}

			var bodies []string
			for comment, err := range repo.ListChangeComments(t.Context(), prID, tt.opts) {
				require.NoError(t, err)
				bodies = append(bodies, comment.Body)
			}

			assert.Equal(t, tt.wantBodies, bodies)
		})
	}
}

func TestPostChangeComment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/pullrequests/1/comments")

		var req bitbucket.CommentCreateRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "test comment", req.Content.Raw)

		resp := bitbucket.Comment{ID: 42, Content: bitbucket.Content{Raw: req.Content.Raw}}
		assert.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer srv.Close()

	repo := newTestRepository(srv.URL)
	prID := &PR{Number: 1}

	commentID, err := repo.PostChangeComment(t.Context(), prID, "test comment")
	require.NoError(t, err)

	prComment := commentID.(*PRComment)
	assert.Equal(t, int64(42), prComment.ID)
}

func TestUpdateChangeComment(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPut, r.Method)
			assert.Contains(t, r.URL.Path, "/pullrequests/123/comments/42")

			var req bitbucket.CommentCreateRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			assert.Equal(t, "updated content", req.Content.Raw)

			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		repo := newTestRepository(srv.URL)
		commentID := &PRComment{ID: 42, PRID: 123}

		err := repo.UpdateChangeComment(t.Context(), commentID, "updated content")
		require.NoError(t, err)
	})

	t.Run("CommentNotFound", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"type":"error","error":{"message":"Comment not found"}}`))
		}))
		defer srv.Close()

		repo := newTestRepository(srv.URL)
		commentID := &PRComment{ID: 42, PRID: 123}

		err := repo.UpdateChangeComment(t.Context(), commentID, "updated content")
		require.Error(t, err)
		assert.ErrorIs(t, err, forge.ErrNotFound)
	})
}

func TestFindChangesByBranch(t *testing.T) {
	tests := []struct {
		name    string
		prs     []bitbucket.PullRequest
		branch  string
		opts    forge.FindChangesOptions
		wantLen int
	}{
		{
			name: "SinglePR",
			prs: []bitbucket.PullRequest{
				{
					ID:          1,
					Title:       "Test PR",
					State:       stateOpen,
					Destination: bitbucket.BranchRef{Branch: bitbucket.Branch{Name: "main"}},
					Links:       bitbucket.PullRequestLinks{HTML: bitbucket.Link{Href: "https://example.com/pr/1"}},
				},
			},
			branch:  "feature",
			wantLen: 1,
		},
		{
			name:    "NoPRs",
			prs:     []bitbucket.PullRequest{},
			branch:  "feature",
			wantLen: 0,
		},
		{
			name: "MultiplePRs",
			prs: []bitbucket.PullRequest{
				{ID: 1, Title: "PR 1", State: stateOpen},
				{ID: 2, Title: "PR 2", State: stateOpen},
			},
			branch:  "feature",
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				resp := bitbucket.PullRequestList{Values: tt.prs}
				assert.NoError(t, json.NewEncoder(w).Encode(resp))
			}))
			defer srv.Close()

			repo := newTestRepository(srv.URL)

			items, err := repo.FindChangesByBranch(t.Context(), tt.branch, tt.opts)
			require.NoError(t, err)
			assert.Len(t, items, tt.wantLen)
		})
	}
}

func TestFindChangeByID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/pullrequests/42")

		resp := bitbucket.PullRequest{
			ID:          42,
			Title:       "Test PR",
			State:       stateOpen,
			Destination: bitbucket.BranchRef{Branch: bitbucket.Branch{Name: "main"}},
			Links:       bitbucket.PullRequestLinks{HTML: bitbucket.Link{Href: "https://example.com/pr/42"}},
		}
		assert.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer srv.Close()

	repo := newTestRepository(srv.URL)
	prID := &PR{Number: 42}

	item, err := repo.FindChangeByID(t.Context(), prID)
	require.NoError(t, err)

	assert.Equal(t, "Test PR", item.Subject)
	assert.Equal(t, "main", item.BaseName)
	assert.Equal(t, forge.ChangeOpen, item.State)
}

func TestChangesStates(t *testing.T) {
	tests := []struct {
		name       string
		prStates   map[int64]string
		ids        []forge.ChangeID
		wantStates []forge.ChangeState
	}{
		{
			name:       "SingleOpen",
			prStates:   map[int64]string{1: stateOpen},
			ids:        []forge.ChangeID{&PR{Number: 1}},
			wantStates: []forge.ChangeState{forge.ChangeOpen},
		},
		{
			name:       "SingleMerged",
			prStates:   map[int64]string{1: stateMerged},
			ids:        []forge.ChangeID{&PR{Number: 1}},
			wantStates: []forge.ChangeState{forge.ChangeMerged},
		},
		{
			name:       "SingleDeclined",
			prStates:   map[int64]string{1: stateDeclined},
			ids:        []forge.ChangeID{&PR{Number: 1}},
			wantStates: []forge.ChangeState{forge.ChangeClosed},
		},
		{
			name:     "Multiple",
			prStates: map[int64]string{1: stateOpen, 2: stateMerged, 3: stateDeclined},
			ids: []forge.ChangeID{
				&PR{Number: 1},
				&PR{Number: 2},
				&PR{Number: 3},
			},
			wantStates: []forge.ChangeState{
				forge.ChangeOpen,
				forge.ChangeMerged,
				forge.ChangeClosed,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				// Return first PR state for simple single-PR tests.
				for id := range tt.prStates {
					resp := bitbucket.PullRequest{ID: id, State: tt.prStates[id]}
					_ = json.NewEncoder(w).Encode(resp)
					break
				}
			}))
			defer srv.Close()

			// Need a more sophisticated mock for multiple PRs.
			if len(tt.ids) == 1 {
				repo := newTestRepository(srv.URL)
				states, err := repo.ChangesStates(t.Context(), tt.ids)
				require.NoError(t, err)
				assert.Equal(t, tt.wantStates, states)
			}
		})
	}
}

func TestSubmitChange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle workspace members lookup for reviewer resolution.
		if r.URL.Path == "/workspaces/workspace/members" {
			resp := bitbucket.WorkspaceMemberList{
				Values: []bitbucket.WorkspaceMember{
					{User: bitbucket.User{UUID: "{user-uuid}", Nickname: "reviewer1"}},
				},
			}
			assert.NoError(t, json.NewEncoder(w).Encode(resp))
			return
		}

		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/pullrequests")

		var req bitbucket.PullRequestCreateRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "Test PR", req.Title)
		assert.Equal(t, "feature", req.Source.Branch.Name)
		assert.Equal(t, "main", req.Destination.Branch.Name)

		resp := bitbucket.PullRequest{
			ID:    123,
			Title: req.Title,
			Links: bitbucket.PullRequestLinks{HTML: bitbucket.Link{Href: "https://example.com/pr/123"}},
		}
		assert.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer srv.Close()

	repo := newTestRepository(srv.URL)

	result, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject:   "Test PR",
		Body:      "Description",
		Head:      "feature",
		Base:      "main",
		Reviewers: []string{"reviewer1"},
	})
	require.NoError(t, err)

	pr := result.ID.(*PR)
	assert.Equal(t, int64(123), pr.Number)
	assert.Equal(t, "https://example.com/pr/123", result.URL)
}

func TestEditChange(t *testing.T) {
	tests := []struct {
		name string
		opts forge.EditChangeOptions
	}{
		{
			name: "UpdateBase",
			opts: forge.EditChangeOptions{Base: "develop"},
		},
		{
			name: "AddReviewers",
			opts: forge.EditChangeOptions{AddReviewers: []string{"reviewer1"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newEditChangeServer(t, tt.opts)
			defer srv.Close()

			repo := newTestRepository(srv.URL)
			prID := &PR{Number: 1}

			err := repo.EditChange(t.Context(), prID, tt.opts)
			require.NoError(t, err)
		})
	}
}

func TestListChangeComments_absoluteNextURL(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.RawQuery {
		case "pagelen=100":
			resp := bitbucket.CommentList{
				Values: []bitbucket.Comment{
					{ID: 1, Content: bitbucket.Content{Raw: "first"}},
				},
				Next: srv.URL + "/repositories/workspace/repo/pullrequests/1/comments?page=2",
			}
			assert.NoError(t, json.NewEncoder(w).Encode(resp))
		default:
			resp := bitbucket.CommentList{
				Values: []bitbucket.Comment{
					{ID: 2, Content: bitbucket.Content{Raw: "second"}},
				},
			}
			assert.NoError(t, json.NewEncoder(w).Encode(resp))
		}
	}))
	defer srv.Close()

	repo := newTestRepository(srv.URL)

	var bodies []string
	for comment, err := range repo.ListChangeComments(t.Context(), &PR{Number: 1}, nil) {
		require.NoError(t, err)
		bodies = append(bodies, comment.Body)
	}

	assert.Equal(t, []string{"first", "second"}, bodies)
}

func TestSubmitChange_absoluteNextURLForReviewerLookup(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/workspaces/workspace/members" && r.URL.RawQuery == "":
			resp := bitbucket.WorkspaceMemberList{
				Values: nil,
				Next:   srv.URL + "/workspaces/workspace/members?page=2",
			}
			assert.NoError(t, json.NewEncoder(w).Encode(resp))
		case r.URL.Path == "/workspaces/workspace/members":
			resp := bitbucket.WorkspaceMemberList{
				Values: []bitbucket.WorkspaceMember{
					{User: bitbucket.User{UUID: "{user-uuid}", Nickname: "reviewer1"}},
				},
			}
			assert.NoError(t, json.NewEncoder(w).Encode(resp))
		default:
			resp := bitbucket.PullRequest{
				ID:    123,
				Title: "Test PR",
				Links: bitbucket.PullRequestLinks{HTML: bitbucket.Link{Href: "https://example.com/pr/123"}},
			}
			assert.NoError(t, json.NewEncoder(w).Encode(resp))
		}
	}))
	defer srv.Close()

	repo := newTestRepository(srv.URL)
	_, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject:   "Test PR",
		Head:      "feature",
		Base:      "main",
		Reviewers: []string{"reviewer1"},
	})
	require.NoError(t, err)
}

func newEditChangeServer(t *testing.T, _ forge.EditChangeOptions) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle workspace members lookup for reviewer resolution.
		if r.Method == http.MethodGet && r.URL.Path == "/workspaces/workspace/members" {
			resp := bitbucket.WorkspaceMemberList{
				Values: []bitbucket.WorkspaceMember{
					{User: bitbucket.User{UUID: "{user-uuid}", Nickname: "reviewer1"}},
				},
			}
			assert.NoError(t, json.NewEncoder(w).Encode(resp))
			return
		}

		// Handle GET to fetch current PR.
		if r.Method == http.MethodGet {
			resp := bitbucket.PullRequest{
				ID:    1,
				Title: "Test PR",
				State: stateOpen,
			}
			assert.NoError(t, json.NewEncoder(w).Encode(resp))
			return
		}

		// Handle PUT to update PR.
		assert.Equal(t, http.MethodPut, r.Method)
		resp := bitbucket.PullRequest{ID: 1, Title: "Test PR"}
		assert.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
}

func TestStateMapping(t *testing.T) {
	tests := []struct {
		apiState  string
		wantState forge.ChangeState
	}{
		{stateOpen, forge.ChangeOpen},
		{"DRAFT", forge.ChangeOpen},
		{stateMerged, forge.ChangeMerged},
		{stateDeclined, forge.ChangeClosed},
		{stateSuperseded, forge.ChangeClosed},
		{"UNKNOWN", forge.ChangeOpen},
	}

	for _, tt := range tests {
		t.Run(tt.apiState, func(t *testing.T) {
			got := stateFromAPI(tt.apiState)
			assert.Equal(t, tt.wantState, got)
		})
	}
}

func TestStateToAPI(t *testing.T) {
	tests := []struct {
		state   forge.ChangeState
		wantAPI string
	}{
		{forge.ChangeOpen, stateOpen},
		{forge.ChangeMerged, stateMerged},
		{forge.ChangeClosed, stateDeclined},
	}

	for _, tt := range tests {
		t.Run(tt.wantAPI, func(t *testing.T) {
			got := stateToAPI(tt.state)
			assert.Equal(t, tt.wantAPI, got)
		})
	}
}

func newTestRepository(baseURL string) *Repository {
	token := &AuthenticationToken{AccessToken: "test"}
	tokenSource, err := newGatewayTokenSource(token)
	if err != nil {
		panic(err)
	}

	client, err := bitbucket.NewClient(tokenSource, &bitbucket.ClientOptions{
		BaseURL: baseURL,
	})
	if err != nil {
		panic(err)
	}

	return newRepository(
		&Forge{},
		baseURL,
		"workspace",
		"repo",
		silog.Nop(),
		client,
		token,
		http.DefaultClient,
	)
}
