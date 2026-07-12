package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestRepository_MergeChange_method(t *testing.T) {
	tests := []struct {
		name       string
		method     forge.MergeMethod
		wantMethod string
	}{
		{
			name:   "Default",
			method: forge.MergeMethodDefault,
		},
		{
			name:       "Merge",
			method:     forge.MergeMethodMerge,
			wantMethod: "MERGE",
		},
		{
			name:       "Squash",
			method:     forge.MergeMethodSquash,
			wantMethod: "SQUASH",
		},
		{
			name:       "Rebase",
			method:     forge.MergeMethodRebase,
			wantMethod: "REBASE",
		},
		{
			name:   "Unsupported",
			method: forge.MergeMethod(99),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var merged bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)

				var body struct {
					Variables struct {
						Input struct {
							PullRequestID string  `json:"pullRequestId"`
							MergeMethod   *string `json:"mergeMethod,omitempty"`
						} `json:"input"`
					} `json:"variables"`
				}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))

				input := body.Variables.Input
				assert.Equal(t, "prID", input.PullRequestID)
				if tt.wantMethod == "" {
					assert.Nil(t, input.MergeMethod)
				} else {
					require.NotNil(t, input.MergeMethod)
					assert.Equal(t, tt.wantMethod, *input.MergeMethod)
				}
				merged = true

				require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"mergePullRequest": map[string]any{
							"pullRequest": map[string]any{
								"id": "prID",
							},
						},
					},
				}))
			}))
			defer srv.Close()

			gateway, err := github.NewGateway(srv.URL, nil, gatewayTestTokenSource("token"))
			require.NoError(t, err)
			repo, err := newRepository(
				t.Context(), new(Forge),
				"owner", "repo",
				silogtest.New(t),
				gateway,
				"repoID",
			)
			require.NoError(t, err)

			err = repo.mergePullRequest(t.Context(), &PR{Number: 1}, "prID", forge.MergeChangeOptions{
				Method: tt.method,
			})
			require.NoError(t, err)
			assert.True(t, merged)
		})
	}
}
