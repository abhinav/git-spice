package gitea

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
)

func TestRepository_FindChangesByBranch_searchesPastLimitBeforeFiltering(
	t *testing.T,
) {
	const targetBranch = "feature"

	var allPRs []*giteagw.PullRequest
	for i := range 55 {
		allPRs = append(allPRs, &giteagw.PullRequest{
			Number: int64(i + 1),
			Title:  "Unrelated",
			State:  "open",
			Head: &giteagw.PRBranch{
				Ref: "unrelated-" + strconv.Itoa(i),
				Repo: &giteagw.Repository{
					FullName: "captain/warp-core",
				},
			},
		})
	}
	allPRs = append(allPRs, &giteagw.PullRequest{
		Number: 56,
		Title:  "Target",
		State:  "open",
		Head: &giteagw.PRBranch{
			Ref: targetBranch,
			Repo: &giteagw.Repository{
				FullName: "captain/warp-core",
			},
		},
	})

	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/repos/captain/warp-core/pulls", r.URL.Path)
		require.Equal(t, "open", r.URL.Query().Get("state"))
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		require.NoError(t, err)
		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		require.NoError(t, err)

		start := (page - 1) * limit
		end := min(start+limit, len(allPRs))
		if end < len(allPRs) {
			w.Header().Set("X-Next-Page", strconv.Itoa(page+1))
		}
		writeJSON(t, w, http.StatusOK, allPRs[start:end])
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	got, err := repo.FindChangesByBranch(
		t.Context(),
		targetBranch,
		forge.FindChangesOptions{State: forge.ChangeOpen, Limit: 10},
	)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, &PR{Number: 56}, got[0].ID)
}

func TestMatchesBranch_forkRepository(t *testing.T) {
	pr := &giteagw.PullRequest{
		Head: &giteagw.PRBranch{
			Label: "feature",
			Ref:   "feature",
			Repo: &giteagw.Repository{
				FullName: "test-reviewer/test-fork-repo",
			},
		},
	}

	assert.True(t, matchesBranch(
		pr,
		"feature",
		&RepositoryID{
			url:   "https://gitea.example.com/test-reviewer/test-fork-repo",
			owner: "test-reviewer",
			name:  "test-fork-repo",
		},
		"test-owner",
		"test-repo",
	))
	assert.False(t, matchesBranch(
		pr,
		"feature",
		nil,
		"test-owner",
		"test-repo",
	))
}
