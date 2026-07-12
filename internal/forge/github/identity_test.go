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

func TestRepository_identityIDsCachesSuccessesFromPartialLookup(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var request struct {
			Query string `json:"query"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		assert.Contains(t, request.Query, "user0:user(login: $user0){id}")
		assert.Contains(t, request.Query, "user1:user(login: $user1){id}")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"user0": map[string]any{"id": "U_1"},
				"user1": nil,
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

	userIDs, _, err := repo.identityIDs(t.Context(), []string{"alice", "missing"}, nil)
	require.EqualError(t, err, `user not found: "missing"`)
	assert.Equal(t, []github.ID{"U_1", ""}, userIDs)

	id, err := repo.userID(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, github.ID("U_1"), id)
	assert.Equal(t, 1, requests)
}
