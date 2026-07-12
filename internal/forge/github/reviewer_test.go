package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/gateway/github"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestRepository_reviewersIDs_batchesUncachedIdentities(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var request struct {
			Variables json.RawMessage `json:"variables"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		assert.JSONEq(t, `{
			"teamOrg0": "acme",
			"teamSlug0": "platform",
			"user0": "alice",
			"user1": "bob"
		}`, string(request.Variables))
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"user0": map[string]any{"id": "U_1"},
				"user1": map[string]any{"id": "U_2"},
				"team0": map[string]any{
					"team": map[string]any{"id": "T_1"},
				},
			},
		}))
	}))
	defer server.Close()

	repo, err := newRepository(
		t.Context(), new(Forge),
		"acme", "warp-drive",
		silogtest.New(t),
		newTestGateway(t, server.URL),
		"R_1",
	)
	require.NoError(t, err)

	userIDs, teamIDs, err := repo.reviewersIDs(
		t.Context(),
		[]string{
			"alice",
			"acme/platform",
			"bob",
			"alice",
			"acme/platform",
		},
	)
	require.NoError(t, err)
	assert.Equal(t, []github.ID{"U_1", "U_2", "U_1"}, userIDs)
	assert.Equal(t, []github.ID{"T_1", "T_1"}, teamIDs)
	assert.Equal(t, 1, requests)
}
