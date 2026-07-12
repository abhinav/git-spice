package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/gateway/github"
	"go.abhg.dev/gs/internal/silog"
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

	var logs strings.Builder
	repo, err := newRepository(
		t.Context(), new(Forge),
		"acme", "warp-drive",
		silog.New(&logs, &silog.Options{Level: silog.LevelDebug}),
		newTestGateway(t, server.URL),
		"R_1",
	)
	require.NoError(t, err)

	userIDs, _, err := repo.identityIDs(t.Context(), []string{"alice", "missing"}, nil)
	require.EqualError(t, err, `user not found: "missing"`)
	assert.Equal(t, []github.ID{"U_1", ""}, userIDs)

	cachedUserIDs, _, err := repo.identityIDs(t.Context(), []string{"alice"}, nil)
	require.NoError(t, err)
	assert.Equal(t, []github.ID{"U_1"}, cachedUserIDs)
	assert.Equal(t, 1, requests)
	assert.Equal(t, 1, strings.Count(logs.String(), "Resolved user ID"))
}
