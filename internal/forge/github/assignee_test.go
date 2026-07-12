package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestRepository_addAssigneesToPullRequest_batchesDistinctUncachedUsers(t *testing.T) {
	var requests int
	var identityRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var request struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		switch {
		case strings.Contains(request.Query, "user0:user"):
			identityRequests++
			assert.Equal(t, "alice", request.Variables["user0"])
			assert.Equal(t, "bob", request.Variables["user1"])
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"user0": map[string]any{"id": "U_2"},
					"user1": map[string]any{"id": "U_1"},
				},
			}))

		case strings.Contains(request.Query, "addAssigneesToAssignable"):
			input, ok := request.Variables["input"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "PR_1", input["assignableId"])
			assert.Equal(t, []any{"U_1", "U_2"}, input["assigneeIds"])
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"addAssigneesToAssignable": map[string]any{},
				},
			}))

		default:
			t.Fatalf("unexpected query: %s", request.Query)
		}
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

	err = repo.addAssigneesToPullRequest(
		t.Context(),
		[]string{"alice", "bob", "alice"},
		"PR_1",
	)
	require.NoError(t, err)
	assert.Equal(t, 1, identityRequests)
	assert.Equal(t, 2, requests)
}
