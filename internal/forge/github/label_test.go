package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/gateway/github"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestRepository_ensureLabels_batchesLookups(t *testing.T) {
	var lookupRequests atomic.Int64
	var createRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query     string          `json:"query"`
			Variables json.RawMessage `json:"variables"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))

		switch {
		case strings.HasPrefix(request.Query, "query("):
			lookupRequests.Add(1)
			assert.Contains(t, request.Query, "label0:label")
			assert.Contains(t, request.Query, "label1:label")
			assert.Contains(t, request.Query, "label2:label")
			assert.NotContains(t, request.Query, "label3:label")
			assert.JSONEq(t, `{
				"label0": "bug",
				"label1": "missing",
				"label2": "other",
				"name": "repo",
				"owner": "acme"
			}`, string(request.Variables))
			_, err := w.Write([]byte(`{
				"data": {"repository": {
					"label0": {"id": "L_1"},
					"label1": null,
					"label2": null
				}}
			}`))
			assert.NoError(t, err)

		case strings.Contains(request.Query, "createLabel"):
			createRequests.Add(1)
			var variables struct {
				Input struct {
					RepositoryID github.ID `json:"repositoryId"`
					Color        string    `json:"color"`
					Name         string    `json:"name"`
				} `json:"input"`
			}
			require.NoError(t, json.Unmarshal(request.Variables, &variables))
			assert.Equal(t, github.ID("R_1"), variables.Input.RepositoryID)
			assert.Equal(t, "EDEDED", variables.Input.Color)

			labelID := "L_2"
			if variables.Input.Name == "other" {
				labelID = "L_3"
			} else {
				assert.Equal(t, "missing", variables.Input.Name)
			}
			_, err := w.Write([]byte(`{
				"data": {"createLabel": {"label": {"id": "` + labelID + `"}}}
			}`))
			assert.NoError(t, err)

		default:
			t.Fatalf("unexpected query: %s", request.Query)
		}
	}))
	t.Cleanup(server.Close)

	repository, err := newRepository(
		t.Context(), new(Forge), "acme", "repo", silogtest.New(t),
		newTestGateway(t, server.URL), "R_1",
	)
	require.NoError(t, err)

	ids, err := repository.ensureLabels(
		t.Context(), []string{"bug", "missing", "missing", "other"},
	)
	require.NoError(t, err)
	assert.Equal(t, []github.ID{"L_1", "L_2", "L_2", "L_3"}, ids)
	assert.Equal(t, int64(1), lookupRequests.Load())
	assert.Equal(t, int64(2), createRequests.Load())
}
