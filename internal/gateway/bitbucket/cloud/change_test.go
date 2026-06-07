package cloud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
	"go.abhg.dev/gs/internal/git"
)

func TestGateway_CreateChange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle workspace members lookup for reviewer resolution.
		if r.URL.Path == "/workspaces/workspace/members" {
			assert.NoError(t, json.NewEncoder(w).Encode(WorkspaceMemberList{
				Values: []WorkspaceMember{
					{User: User{UUID: "{user-uuid}", Nickname: "reviewer1"}},
				},
			}))
			return
		}

		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/pullrequests")

		var req PullRequestCreateRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "Test PR", req.Title)
		assert.Equal(t, "Description", req.Description)
		assert.Equal(t, "feature", req.Source.Branch.Name)
		assert.Equal(t, "main", req.Destination.Branch.Name)
		assert.Equal(t, []Reviewer{{UUID: "{user-uuid}"}}, req.Reviewers)

		assert.NoError(t, json.NewEncoder(w).Encode(PullRequest{
			ID:    123,
			Title: req.Title,
			State: stateOpen,
			Links: PullRequestLinks{
				HTML: Link{Href: "https://example.com/pr/123"},
			},
		}))
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	pr, err := gw.CreateChange(t.Context(), bitbucket.CreateChangeRequest{
		Subject:   "Test PR",
		Body:      "Description",
		Head:      "feature",
		Base:      "main",
		Reviewers: []string{"reviewer1"},
	})
	require.NoError(t, err)

	assert.Equal(t, int64(123), pr.Number)
	assert.Equal(t, "https://example.com/pr/123", pr.URL)
}

func TestGateway_CreateChange_pushRepository(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)

		var req PullRequestCreateRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.NotNil(t, req.Source.Repository)
		assert.Equal(t, "fork/repo", req.Source.Repository.FullName)

		assert.NoError(t, json.NewEncoder(w).Encode(PullRequest{ID: 1}))
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	_, err := gw.CreateChange(t.Context(), bitbucket.CreateChangeRequest{
		Subject:        "Test PR",
		Head:           "feature",
		Base:           "main",
		PushRepository: stubRepositoryID("fork/repo"),
	})
	require.NoError(t, err)
}

func TestGateway_CreateChange_unsubmittedBase(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(
			`{"type": "error", "error": {"message": "destination branch not found"}}`,
		))
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	_, err := gw.CreateChange(t.Context(), bitbucket.CreateChangeRequest{
		Subject: "Test PR",
		Head:    "feature",
		Base:    "missing",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, forge.ErrUnsubmittedBase)
}

func TestGateway_CreateChange_absoluteNextURLForReviewerLookup(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/workspaces/workspace/members" && r.URL.RawQuery == "":
			assert.NoError(t, json.NewEncoder(w).Encode(WorkspaceMemberList{
				Values: nil,
				Next:   srv.URL + "/workspaces/workspace/members?page=2",
			}))
		case r.URL.Path == "/workspaces/workspace/members":
			assert.NoError(t, json.NewEncoder(w).Encode(WorkspaceMemberList{
				Values: []WorkspaceMember{
					{User: User{UUID: "{user-uuid}", Nickname: "reviewer1"}},
				},
			}))
		default:
			assert.NoError(t, json.NewEncoder(w).Encode(PullRequest{
				ID:    123,
				Title: "Test PR",
				Links: PullRequestLinks{
					HTML: Link{Href: "https://example.com/pr/123"},
				},
			}))
		}
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	_, err := gw.CreateChange(t.Context(), bitbucket.CreateChangeRequest{
		Subject:   "Test PR",
		Head:      "feature",
		Base:      "main",
		Reviewers: []string{"reviewer1"},
	})
	require.NoError(t, err)
}

func TestGateway_GetChange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/pullrequests/42")

		assert.NoError(t, json.NewEncoder(w).Encode(PullRequest{
			ID:    42,
			Title: "Test PR",
			State: stateOpen,
			Source: BranchRef{
				Branch: Branch{Name: "feature"},
				Commit: &Commit{Hash: "abcdef123456"},
			},
			Destination: BranchRef{Branch: Branch{Name: "main"}},
			Reviewers:   []User{{UUID: "{user-uuid}", Nickname: "reviewer1"}},
			Links: PullRequestLinks{
				HTML: Link{Href: "https://example.com/pr/42"},
			},
		}))
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	pr, err := gw.GetChange(t.Context(), 42)
	require.NoError(t, err)

	assert.Equal(t, int64(42), pr.Number)
	assert.Equal(t, "https://example.com/pr/42", pr.URL)
	assert.Equal(t, "Test PR", pr.Subject)
	assert.Equal(t, "main", pr.BaseName)
	assert.Equal(t, forge.ChangeOpen, pr.State)
	assert.Equal(t, git.Hash("abcdef123456"), pr.HeadHash)
	assert.Equal(t, []string{"reviewer1"}, pr.Reviewers)
}

func TestGateway_GetChange_states(t *testing.T) {
	tests := []struct {
		name      string
		pr        PullRequest
		wantState forge.ChangeState
		wantDraft bool
		wantHead  git.Hash
	}{
		{
			name:      "Open",
			pr:        PullRequest{ID: 1, State: stateOpen},
			wantState: forge.ChangeOpen,
		},
		{
			name:      "DraftFlag",
			pr:        PullRequest{ID: 1, State: stateOpen, Draft: true},
			wantState: forge.ChangeOpen,
			wantDraft: true,
		},
		{
			name:      "DraftState",
			pr:        PullRequest{ID: 1, State: "DRAFT"},
			wantState: forge.ChangeOpen,
			wantDraft: true,
		},
		{
			name: "Merged",
			pr: PullRequest{
				ID:          1,
				State:       stateMerged,
				MergeCommit: &Commit{Hash: "mergehash"},
			},
			wantState: forge.ChangeMerged,
			wantHead:  git.Hash("mergehash"),
		},
		{
			name:      "Declined",
			pr:        PullRequest{ID: 1, State: stateDeclined},
			wantState: forge.ChangeClosed,
		},
		{
			name:      "Superseded",
			pr:        PullRequest{ID: 1, State: stateSuperseded},
			wantState: forge.ChangeClosed,
		},
		{
			name:      "Unknown",
			pr:        PullRequest{ID: 1, State: "UNKNOWN"},
			wantState: forge.ChangeOpen,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				assert.NoError(t, json.NewEncoder(w).Encode(tt.pr))
			}))
			defer srv.Close()

			gw := newTestGateway(t, srv.URL)
			pr, err := gw.GetChange(t.Context(), 1)
			require.NoError(t, err)

			assert.Equal(t, tt.wantState, pr.State)
			assert.Equal(t, tt.wantDraft, pr.Draft)
			assert.Equal(t, tt.wantHead, pr.HeadHash)
		})
	}
}

func TestGateway_FindChangesByBranch(t *testing.T) {
	tests := []struct {
		name              string
		prs               []PullRequest
		branch            string
		opts              bitbucket.FindChangesOptions
		wantQueryContains []string
		wantLen           int
	}{
		{
			name: "SinglePR",
			prs: []PullRequest{
				{
					ID:    1,
					Title: "Test PR",
					State: stateOpen,
					Destination: BranchRef{
						Branch: Branch{Name: "main"},
					},
					Links: PullRequestLinks{
						HTML: Link{Href: "https://example.com/pr/1"},
					},
				},
			},
			branch: "feature",
			wantQueryContains: []string{
				`source.branch.name="feature"`,
				`source.repository.full_name="workspace/repo"`,
			},
			wantLen: 1,
		},
		{
			name:    "NoPRs",
			prs:     []PullRequest{},
			branch:  "feature",
			wantLen: 0,
		},
		{
			name: "MultiplePRs",
			prs: []PullRequest{
				{ID: 1, Title: "PR 1", State: stateOpen},
				{ID: 2, Title: "PR 2", State: stateOpen},
			},
			branch:  "feature",
			wantLen: 2,
		},
		{
			name: "PushRepository",
			opts: bitbucket.FindChangesOptions{
				PushRepository: stubRepositoryID("fork/repo"),
			},
			branch: "feature",
			wantQueryContains: []string{
				`source.repository.full_name="fork/repo"`,
			},
			wantLen: 0,
		},
		{
			name:   "StateFilter",
			opts:   bitbucket.FindChangesOptions{State: forge.ChangeOpen},
			branch: "feature",
			wantQueryContains: []string{
				`state="OPEN"`,
			},
			wantLen: 0,
		},
		{
			name:   "StateFilterMerged",
			opts:   bitbucket.FindChangesOptions{State: forge.ChangeMerged},
			branch: "feature",
			wantQueryContains: []string{
				`state="MERGED"`,
			},
			wantLen: 0,
		},
		{
			name:   "StateFilterClosed",
			opts:   bitbucket.FindChangesOptions{State: forge.ChangeClosed},
			branch: "feature",
			wantQueryContains: []string{
				`state="DECLINED"`,
			},
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				query := r.URL.Query().Get("q")
				for _, want := range tt.wantQueryContains {
					assert.Contains(t, query, want)
				}
				assert.Equal(t, "10", r.URL.Query().Get("pagelen"))
				assert.Equal(t, "+values.reviewers", r.URL.Query().Get("fields"))
				assert.NoError(t, json.NewEncoder(w).Encode(
					PullRequestList{Values: tt.prs}))
			}))
			defer srv.Close()

			gw := newTestGateway(t, srv.URL)
			prs, err := gw.FindChangesByBranch(t.Context(), tt.branch, tt.opts)
			require.NoError(t, err)
			assert.Len(t, prs, tt.wantLen)
		})
	}
}

func TestGateway_FindChangesByBranch_limit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "5", r.URL.Query().Get("pagelen"))
		assert.NoError(t, json.NewEncoder(w).Encode(PullRequestList{}))
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	_, err := gw.FindChangesByBranch(
		t.Context(), "feature", bitbucket.FindChangesOptions{Limit: 5},
	)
	require.NoError(t, err)
}

func TestGateway_UpdateChange_empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request: %s %s", r.Method, r.URL)
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	require.NoError(t, gw.UpdateChange(t.Context(), 1, bitbucket.ChangeUpdate{}))
}

func TestGateway_UpdateChange_base(t *testing.T) {
	var puts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Contains(t, r.URL.Path, "/pullrequests/1")
		puts++

		var req PullRequestUpdateRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.NotNil(t, req.Destination)
		assert.Equal(t, "develop", req.Destination.Branch.Name)
		assert.Nil(t, req.Title)
		assert.Nil(t, req.Description)
		assert.Empty(t, req.Reviewers)

		assert.NoError(t, json.NewEncoder(w).Encode(PullRequest{ID: 1}))
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	require.NoError(t, gw.UpdateChange(t.Context(), 1, bitbucket.ChangeUpdate{Base: "develop"}))
	assert.Equal(t, 1, puts)
}

// TestGateway_UpdateChange_addReviewers verifies that
// adding reviewers to a PR with an existing description
// does not clear the description,
// and merges new reviewers with the existing ones.
//
// Bitbucket replaces the entire PR resource on PUT,
// so a PUT that omits "description" will wipe it out.
// The reviewer PUT must therefore re-send the existing description.
func TestGateway_UpdateChange_addReviewers(t *testing.T) {
	// Track the PR description across PUTs.
	// Bitbucket-style: PUT replaces the resource,
	// so an omitted description clears it.
	lastDescription := "Initial description"
	var gotReviewers []Reviewer
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet &&
			r.URL.Path == "/workspaces/workspace/members":
			assert.NoError(t, json.NewEncoder(w).Encode(WorkspaceMemberList{
				Values: []WorkspaceMember{
					{User: User{UUID: "{user-uuid}", Nickname: "reviewer1"}},
				},
			}))
		case r.Method == http.MethodGet:
			assert.Contains(t, r.URL.Path, "/pullrequests/1")
			assert.NoError(t, json.NewEncoder(w).Encode(PullRequest{
				ID:          1,
				Title:       "Test PR",
				Description: lastDescription,
				State:       stateOpen,
				Reviewers:   []User{{UUID: "{existing-uuid}"}},
			}))
		case r.Method == http.MethodPut:
			var req PullRequestUpdateRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			if req.Description == nil {
				lastDescription = ""
			} else {
				lastDescription = *req.Description
			}
			require.NotNil(t, req.Title)
			assert.Equal(t, "Test PR", *req.Title)
			gotReviewers = req.Reviewers
			assert.NoError(t, json.NewEncoder(w).Encode(PullRequest{ID: 1}))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	require.NoError(t, gw.UpdateChange(t.Context(), 1, bitbucket.ChangeUpdate{
		AddReviewers: []string{"reviewer1"},
	}))

	assert.Equal(t, "Initial description", lastDescription,
		"description must survive the reviewer PUT")
	assert.Equal(t, []Reviewer{
		{UUID: "{existing-uuid}"},
		{UUID: "{user-uuid}"},
	}, gotReviewers)
}

func TestGateway_SetChangeDraft(t *testing.T) {
	tests := []struct {
		name  string
		draft bool
	}{
		{name: "Draft", draft: true},
		{name: "Undraft", draft: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPut, r.Method)
				assert.Contains(t, r.URL.Path, "/pullrequests/1")

				// The PUT must carry only the draft flag.
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.Equal(t, map[string]any{"draft": tt.draft}, body)

				assert.NoError(t, json.NewEncoder(w).Encode(PullRequest{ID: 1}))
			}))
			defer srv.Close()

			gw := newTestGateway(t, srv.URL)
			require.NoError(t, gw.SetChangeDraft(t.Context(), 1, tt.draft))
		})
	}
}

func TestGateway_MergeChange(t *testing.T) {
	tests := []struct {
		name         string
		method       forge.MergeMethod
		wantStrategy any
	}{
		{
			name:         "Default",
			method:       forge.MergeMethodDefault,
			wantStrategy: nil,
		},
		{
			name:         "Merge",
			method:       forge.MergeMethodMerge,
			wantStrategy: "merge_commit",
		},
		{
			name:         "Squash",
			method:       forge.MergeMethodSquash,
			wantStrategy: "squash",
		},
		{
			name:         "Rebase",
			method:       forge.MergeMethodRebase,
			wantStrategy: "rebase_merge",
		},
		{
			name:   "Unsupported",
			method: forge.MergeMethod(99),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var merged bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Contains(t, r.URL.Path, "/pullrequests/1/merge")

				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				if tt.wantStrategy == nil {
					assert.NotContains(t, body, "merge_strategy")
				} else {
					assert.Equal(t, tt.wantStrategy, body["merge_strategy"])
				}
				merged = true

				assert.NoError(t, json.NewEncoder(w).Encode(PullRequest{
					ID:    1,
					State: stateMerged,
				}))
			}))
			defer srv.Close()

			gw := newTestGateway(t, srv.URL)
			require.NoError(t, gw.MergeChange(t.Context(), 1, tt.method))
			assert.True(t, merged)
		})
	}
}

func TestGateway_MergeChange_error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	err := gw.MergeChange(t.Context(), 1, forge.MergeMethodDefault)
	require.Error(t, err)
	assert.ErrorContains(t, err, "merge pull request")
}

// stubRepositoryID is a [forge.RepositoryID]
// whose String reports the wrapped "workspace/name" value.
type stubRepositoryID string

var _ forge.RepositoryID = stubRepositoryID("")

func (s stubRepositoryID) String() string { return string(s) }

func (stubRepositoryID) ChangeURL(forge.ChangeID) string {
	panic("unexpected ChangeURL call")
}
