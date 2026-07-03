package gitea

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/gitea"
)

func TestRepository_EditChange_createsMissingLabels(t *testing.T) {
	var labelCreated bool
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/captain/warp-core/labels":
			switch r.Method {
			case http.MethodGet:
				writeJSON(t, w, http.StatusOK, []*gitea.Label{
					{ID: 10, Name: "engineering"},
				})
			case http.MethodPost:
				assertJSONBody(t, r, `{
					"name":"priority-1",
					"color":"#ededed"
				}`)
				labelCreated = true
				writeJSON(t, w, http.StatusCreated, gitea.Label{
					ID:   11,
					Name: "priority-1",
				})
			default:
				http.NotFound(w, r)
			}
		case "/api/v1/repos/captain/warp-core/pulls/44":
			switch r.Method {
			case http.MethodGet:
				writeJSON(t, w, http.StatusOK, gitea.PullRequest{
					Number: 44,
					Labels: []*gitea.Label{{ID: 10, Name: "engineering"}},
				})
			case http.MethodPatch:
				assertJSONBody(t, r, `{"labels":[10,11]}`)
				writeJSON(t, w, http.StatusOK, gitea.PullRequest{
					Number: 44,
				})
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	err := repo.EditChange(t.Context(), &PR{Number: 44}, forge.EditChangeOptions{
		AddLabels: []string{"priority-1"},
	})
	require.NoError(t, err)
	assert.True(t, labelCreated, "missing label should be created")
}
