package gitea

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/gateway/gitea"
)

func TestRepository_ListChangeTemplates(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		switch r.URL.Path {
		case "/api/v1/repos/captain/warp-core/contents/.github/PULL_REQUEST_TEMPLATE.md":
			writeJSON(t, w, http.StatusOK, gitea.FileContentResponse{
				Encoding: "base64",
				Content:  "Q2hlY2sgd2FycCBjb3JlIGFsaWdubWVudC4=\n",
			})
		default:
			writeJSON(t, w, http.StatusNotFound, map[string]string{
				"message": "not found",
			})
		}
	})
	defer srv.Close()

	templates, err := newTestRepo(t, srv).ListChangeTemplates(t.Context())
	require.NoError(t, err)
	require.Len(t, templates, 1)
	assert.Equal(t, "PULL_REQUEST_TEMPLATE.md", templates[0].Filename)
	assert.Equal(t, "Check warp core alignment.", templates[0].Body)
}

func TestRepository_ListChangeTemplates_fetchError(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t,
			"/api/v1/repos/captain/warp-core/contents/.gitea/PULL_REQUEST_TEMPLATE.md",
			r.URL.Path)
		writeJSON(t, w, http.StatusInternalServerError, map[string]string{
			"message": "warp core offline",
		})
	})
	defer srv.Close()

	_, err := newTestRepo(t, srv).ListChangeTemplates(t.Context())
	require.Error(t, err)
	assert.ErrorContains(t, err,
		`fetch template ".gitea/PULL_REQUEST_TEMPLATE.md"`)
}
