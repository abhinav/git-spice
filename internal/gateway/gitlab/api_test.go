package gitlab

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_ProjectGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v4/projects/captain%2Fwarp-core", r.URL.EscapedPath())
		assert.Empty(t, r.URL.RawQuery)
		assert.Equal(t, "git-spice", r.Header.Get("User-Agent"))
		writeJSON(t, w, http.StatusOK, Project{
			ID: 42,
			Permissions: &Permissions{
				ProjectAccess: &ProjectAccess{
					AccessLevel: MaintainerPermissions,
				},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	project, _, err := client.ProjectGet(t.Context(), "captain/warp-core", nil)
	require.NoError(t, err)
	assert.Equal(t, int64(42), project.ID)
	require.NotNil(t, project.Permissions)
	require.NotNil(t, project.Permissions.ProjectAccess)
	assert.Equal(t, MaintainerPermissions, project.Permissions.ProjectAccess.AccessLevel)
}

func TestClient_UserCurrent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v4/user", r.URL.Path)
		writeJSON(t, w, http.StatusOK, User{
			ID:       7,
			Username: "spock",
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	user, _, err := client.UserCurrent(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int64(7), user.ID)
	assert.Equal(t, "spock", user.Username)
}

func TestClient_UserList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v4/users", r.URL.Path)
		assert.Equal(t, "spock", r.URL.Query().Get("username"))
		assert.Equal(t, "git-spice", r.Header.Get("User-Agent"))
		writeJSON(t, w, http.StatusOK, []*User{
			{ID: 9, Username: "spock"},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	users, _, err := client.UserList(t.Context(), &ListUsersOptions{
		Username: new("spock"),
	})
	require.NoError(t, err)
	require.Len(t, users, 1)
	assert.Equal(t, int64(9), users[0].ID)
}

func TestClient_MergeRequestCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v4/projects/42/merge_requests", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "git-spice", r.Header.Get("User-Agent"))
		assertJSONBody(t, r, `{
			"title":"Stabilize nacelles",
			"description":"Replace failing plasma injector.",
			"source_branch":"scotty/fix",
			"target_branch":"main",
			"labels":"engineering,priority-1",
			"assignee_ids":[11],
			"reviewer_ids":[12,13],
			"remove_source_branch":true
		}`)
		writeJSON(t, w, http.StatusCreated, MergeRequest{
			BasicMergeRequest: BasicMergeRequest{
				IID:    55,
				WebURL: "https://gitlab.example.com/captain/warp-core/-/merge_requests/55",
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	mr, _, err := client.MergeRequestCreate(t.Context(), int64(42), &CreateMergeRequestOptions{
		Title:              new("Stabilize nacelles"),
		Description:        new("Replace failing plasma injector."),
		SourceBranch:       new("scotty/fix"),
		TargetBranch:       new("main"),
		Labels:             (*LabelOptions)(&[]string{"engineering", "priority-1"}),
		AssigneeIDs:        &[]int64{11},
		ReviewerIDs:        &[]int64{12, 13},
		RemoveSourceBranch: new(true),
	})
	require.NoError(t, err)
	assert.Equal(t, int64(55), mr.IID)
	assert.Equal(
		t,
		"https://gitlab.example.com/captain/warp-core/-/merge_requests/55",
		mr.WebURL,
	)
}

func TestClient_MergeRequestGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v4/projects/42/merge_requests/55", r.URL.Path)
		writeJSON(t, w, http.StatusOK, MergeRequest{
			BasicMergeRequest: BasicMergeRequest{
				IID:          55,
				Title:        "Stabilize nacelles",
				TargetBranch: "main",
				Labels:       []string{"engineering"},
				Reviewers: []*BasicUser{
					{ID: 12, Username: "spock"},
				},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	mr, _, err := client.MergeRequestGet(t.Context(), int64(42), 55, nil)
	require.NoError(t, err)
	assert.Equal(t, "Stabilize nacelles", mr.Title)
	require.Len(t, mr.Reviewers, 1)
	assert.Equal(t, "spock", mr.Reviewers[0].Username)
}

func TestClient_MergeRequestUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/api/v4/projects/42/merge_requests/55", r.URL.Path)
		assertJSONBody(t, r, `{
			"title":"Draft: Stabilize nacelles",
			"target_branch":"release",
			"assignee_ids":[11],
			"reviewer_ids":[12],
			"add_labels":"draft,engineering",
			"state_event":"close"
		}`)
		writeJSON(t, w, http.StatusOK, MergeRequest{
			BasicMergeRequest: BasicMergeRequest{
				IID:          55,
				Title:        "Draft: Stabilize nacelles",
				TargetBranch: "release",
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	mr, _, err := client.MergeRequestUpdate(t.Context(), int64(42), 55, &UpdateMergeRequestOptions{
		Title:        new("Draft: Stabilize nacelles"),
		TargetBranch: new("release"),
		AssigneeIDs:  &[]int64{11},
		ReviewerIDs:  &[]int64{12},
		AddLabels:    (*LabelOptions)(&[]string{"draft", "engineering"}),
		StateEvent:   new("close"),
	})
	require.NoError(t, err)
	assert.Equal(t, "release", mr.TargetBranch)
}

func TestClient_MergeRequestList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v4/projects/42/merge_requests", r.URL.Path)
		assert.Equal(t, "updated_at", r.URL.Query().Get("order_by"))
		assert.Equal(t, "opened", r.URL.Query().Get("state"))
		assert.Equal(t, "feature/refit", r.URL.Query().Get("source_branch"))
		assert.Equal(t, "20", r.URL.Query().Get("per_page"))
		assert.ElementsMatch(t, []string{"55", "56"}, r.URL.Query()["iids[]"])
		writeJSON(t, w, http.StatusOK, []*BasicMergeRequest{
			{IID: 55, Title: "One"},
			{IID: 56, Title: "Two"},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	mergeRequests, _, err := client.MergeRequestList(
		t.Context(),
		int64(42),
		&ListProjectMergeRequestsOptions{
			ListOptions:  ListOptions{PerPage: 20},
			IIDs:         &[]int64{55, 56},
			OrderBy:      new("updated_at"),
			State:        new("opened"),
			SourceBranch: new("feature/refit"),
		},
	)
	require.NoError(t, err)
	require.Len(t, mergeRequests, 2)
	assert.Equal(t, int64(56), mergeRequests[1].IID)
}

func TestClient_MergeRequestList_paginated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		switch r.URL.Query().Get("page") {
		case "", "1":
			w.Header().Set("X-Page", "1")
			w.Header().Set("X-Next-Page", "2")
			w.Header().Set("X-Total-Pages", "2")
			writeJSON(t, w, http.StatusOK, []*BasicMergeRequest{
				{IID: 1, Title: "One"},
				{IID: 2, Title: "Two"},
			})
		case "2":
			w.Header().Set("X-Page", "2")
			w.Header().Set("X-Next-Page", "")
			w.Header().Set("X-Total-Pages", "2")
			writeJSON(t, w, http.StatusOK, []*BasicMergeRequest{
				{IID: 3, Title: "Three"},
			})
		default:
			t.Fatalf("unexpected page: %q", r.URL.Query().Get("page"))
		}
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	var all []*BasicMergeRequest
	opts := &ListProjectMergeRequestsOptions{
		ListOptions: ListOptions{PerPage: 2},
	}
	for {
		page, resp, err := client.MergeRequestList(t.Context(), int64(42), opts)
		require.NoError(t, err)
		all = append(all, page...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = int64(resp.NextPage)
	}

	require.Len(t, all, 3)
	assert.Equal(t, int64(1), all[0].IID)
	assert.Equal(t, int64(3), all[2].IID)
}

func TestClient_MergeRequestAccept(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/api/v4/projects/42/merge_requests/55/merge", r.URL.Path)
		assertJSONBody(t, r, `{"should_remove_source_branch":true}`)
		writeJSON(t, w, http.StatusOK, MergeRequest{
			BasicMergeRequest: BasicMergeRequest{
				IID:   55,
				State: "merged",
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	mr, _, err := client.MergeRequestAccept(
		t.Context(),
		int64(42),
		55,
		&AcceptMergeRequestOptions{
			ShouldRemoveSourceBranch: new(true),
		},
	)
	require.NoError(t, err)
	assert.Equal(t, "merged", mr.State)
}

func TestClient_MergeRequestNoteCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v4/projects/42/merge_requests/55/notes", r.URL.Path)
		assertJSONBody(t, r, `{"body":"Recalibrated deflector array."}`)
		writeJSON(t, w, http.StatusCreated, Note{
			ID:   88,
			Body: "Recalibrated deflector array.",
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	note, _, err := client.MergeRequestNoteCreate(
		t.Context(),
		int64(42),
		55,
		&CreateMergeRequestNoteOptions{
			Body: new("Recalibrated deflector array."),
		},
	)
	require.NoError(t, err)
	assert.Equal(t, int64(88), note.ID)
}

func TestClient_MergeRequestNoteUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/api/v4/projects/42/merge_requests/55/notes/88", r.URL.Path)
		assertJSONBody(t, r, `{"body":"Updated calibration."}`)
		writeJSON(t, w, http.StatusOK, Note{
			ID:   88,
			Body: "Updated calibration.",
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	note, _, err := client.MergeRequestNoteUpdate(
		t.Context(),
		int64(42),
		55,
		88,
		&UpdateMergeRequestNoteOptions{
			Body: new("Updated calibration."),
		},
	)
	require.NoError(t, err)
	assert.Equal(t, "Updated calibration.", note.Body)
}

func TestClient_MergeRequestNoteList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v4/projects/42/merge_requests/55/notes", r.URL.Path)
		assert.Equal(t, "asc", r.URL.Query().Get("sort"))
		assert.Equal(t, "20", r.URL.Query().Get("per_page"))
		assert.Equal(t, "2", r.URL.Query().Get("page"))
		w.Header().Set("X-Per-Page", "20")
		w.Header().Set("X-Page", "2")
		w.Header().Set("X-Next-Page", "3")
		w.Header().Set("X-Total-Pages", "4")
		writeJSON(t, w, http.StatusOK, []*Note{
			{ID: 88, Body: "alpha"},
			{ID: 89, Body: "beta"},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	notes, resp, err := client.MergeRequestNoteList(
		t.Context(),
		int64(42),
		55,
		&ListMergeRequestNotesOptions{
			ListOptions: ListOptions{
				PerPage: 20,
				Page:    2,
			},
			Sort: new("asc"),
		},
	)
	require.NoError(t, err)
	require.Len(t, notes, 2)
	assert.Equal(t, "beta", notes[1].Body)
	assert.Equal(t, 2, resp.CurrentPage)
	assert.Equal(t, 20, resp.ItemsPerPage)
	assert.Equal(t, 3, resp.NextPage)
	assert.Equal(t, 4, resp.TotalPages)
}

func TestClient_MergeRequestNoteDelete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/api/v4/projects/42/merge_requests/55/notes/88", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	resp, err := client.MergeRequestNoteDelete(t.Context(), int64(42), 55, 88)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestClient_MergeRequestDiscussionList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v4/projects/42/merge_requests/55/discussions", r.URL.Path)
		assert.Equal(t, "100", r.URL.Query().Get("per_page"))
		assert.Equal(t, "2", r.URL.Query().Get("page"))
		w.Header().Set("X-Per-Page", "100")
		w.Header().Set("X-Page", "2")
		w.Header().Set("X-Next-Page", "3")
		w.Header().Set("X-Total-Pages", "4")
		writeJSON(t, w, http.StatusOK, []*Discussion{
			{
				ID: "discussion-1",
				Notes: []*DiscussionNote{
					{Resolvable: true, Resolved: false},
				},
			},
			{
				ID: "discussion-2",
				Notes: []*DiscussionNote{
					{Resolvable: true, Resolved: true},
				},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	discussions, resp, err := client.MergeRequestDiscussionList(
		t.Context(),
		int64(42),
		55,
		&ListMergeRequestDiscussionsOptions{
			ListOptions: ListOptions{
				PerPage: 100,
				Page:    2,
			},
		},
	)
	require.NoError(t, err)
	require.Len(t, discussions, 2)
	assert.Equal(t, "discussion-1", discussions[0].ID)
	require.Len(t, discussions[0].Notes, 1)
	assert.True(t, discussions[0].Notes[0].Resolvable)
	assert.False(t, discussions[0].Notes[0].Resolved)
	assert.Equal(t, 100, resp.ItemsPerPage)
	assert.Equal(t, 2, resp.CurrentPage)
	assert.Equal(t, 3, resp.NextPage)
	assert.Equal(t, 4, resp.TotalPages)
}

func TestClient_ProjectTemplateList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v4/projects/42/templates/merge_requests", r.URL.Path)
		writeJSON(t, w, http.StatusOK, []*ProjectTemplate{
			{Name: "Bridge.md"},
			{Name: "Engineering.md"},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	templates, _, err := client.ProjectTemplateList(
		t.Context(),
		int64(42),
		"merge_requests",
		nil,
	)
	require.NoError(t, err)
	require.Len(t, templates, 2)
	assert.Equal(t, "Engineering.md", templates[1].Name)
}

func TestClient_ProjectTemplateGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v4/projects/42/templates/merge_requests/Bridge.md", r.URL.Path)
		writeJSON(t, w, http.StatusOK, ProjectTemplate{
			Name:    "Bridge.md",
			Content: "Checklist",
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	template, _, err := client.ProjectTemplateGet(
		t.Context(),
		int64(42),
		"merge_requests",
		"Bridge.md",
	)
	require.NoError(t, err)
	assert.Equal(t, "Checklist", template.Content)
}
