package gitea

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/gitea"
)

func TestRepository_CommentCountsByChange_countsReviewComments(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/captain/warp-core/pulls/42/reviews":
			switch r.URL.Query().Get("page") {
			case "", "1":
				w.Header().Set("X-Next-Page", "2")
				writeJSON(t, w, http.StatusOK, []*gitea.PullReview{
					{ID: 100},
				})
			case "2":
				writeJSON(t, w, http.StatusOK, []*gitea.PullReview{
					{ID: 101},
				})
			default:
				t.Fatalf("unexpected review page: %q", r.URL.Query().Get("page"))
			}
		case "/api/v1/repos/captain/warp-core/pulls/42/reviews/100/comments":
			writeJSON(t, w, http.StatusOK, []*gitea.PullReviewComment{
				{ID: 1000},
				{ID: 1001},
			})
		case "/api/v1/repos/captain/warp-core/pulls/42/reviews/101/comments":
			writeJSON(t, w, http.StatusOK, []*gitea.PullReviewComment{
				{ID: 1002},
			})
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	counts, err := repo.CommentCountsByChange(t.Context(), []forge.ChangeID{
		&PR{Number: 42},
	})
	require.NoError(t, err)
	require.Len(t, counts, 1)
	assert.Equal(t, &forge.CommentCounts{
		Total:      3,
		Unresolved: 3,
	}, counts[0])
}
