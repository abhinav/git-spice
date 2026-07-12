package github

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_IdentityIDs(t *testing.T) {
	gateway := newTestGateway(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Query     string          `json:"query"`
			Variables json.RawMessage `json:"variables"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		assert.Equal(t, compactGraphQL(`
			query($user0:String!$user1:String!$teamOrg0:String!$teamSlug0:String!){
				user0:user(login: $user0){id},
				user1:user(login: $user1){id},
				team0:organization(login: $teamOrg0){
					team(slug: $teamSlug0){id}
				}
			}
		`), request.Query)
		assert.JSONEq(t, `{
			"teamOrg0": "acme",
			"teamSlug0": "platform",
			"user0": "alice",
			"user1": "bob"
		}`, string(request.Variables))
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body: io.NopCloser(strings.NewReader(`{
				"data": {
					"user0": {"id": "U_1"},
					"user1": null,
					"team0": {"team": {"id": "T_1"}}
				}
			}`)),
		}, nil
	}))

	userIDs, teamIDs, err := gateway.IdentityIDs(
		t.Context(),
		[]string{"alice", "bob"},
		[]TeamName{{Organization: "acme", Slug: "platform"}},
	)
	require.NoError(t, err)
	assert.Equal(t, []ID{"U_1", ""}, userIDs)
	assert.Equal(t, []ID{"T_1"}, teamIDs)
}

func TestGateway_IdentityIDs_empty(t *testing.T) {
	gateway := newTestGateway(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("unexpected request")
		return nil, nil
	}))

	userIDs, teamIDs, err := gateway.IdentityIDs(t.Context(), nil, nil)
	require.NoError(t, err)
	assert.Empty(t, userIDs)
	assert.Empty(t, teamIDs)
}
