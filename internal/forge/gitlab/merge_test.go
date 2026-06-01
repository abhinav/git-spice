package gitlab

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	gatewaygitlab "go.abhg.dev/gs/internal/gateway/gitlab"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestRepository_MergeChange_method(t *testing.T) {
	tests := []struct {
		name      string
		method    forge.MergeMethod
		wantField bool
	}{
		{
			name: "Default",
		},
		{
			name:   "Merge",
			method: forge.MergeMethodMerge,
		},
		{
			name:   "Rebase",
			method: forge.MergeMethodRebase,
		},
		{
			name:      "Squash",
			method:    forge.MergeMethodSquash,
			wantField: true,
		},
		{
			name:   "Unsupported",
			method: forge.MergeMethod(99),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPut, r.Method)
				assert.Equal(t, "/api/v4/projects/100/merge_requests/55/merge", r.URL.Path)

				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				if tt.wantField {
					assert.Equal(t, true, body["squash"])
				} else {
					assert.NotContains(t, body, "squash")
				}

				writeGitLabJSON(t, w, http.StatusOK, gatewaygitlab.MergeRequest{
					BasicMergeRequest: gatewaygitlab.BasicMergeRequest{
						IID:   55,
						State: "merged",
					},
				})
			}))
			defer srv.Close()

			client, err := gatewaygitlab.NewClient(
				gatewaygitlab.StaticTokenSource(gatewaygitlab.Token{
					Type:  gatewaygitlab.TokenTypePrivateToken,
					Value: "test-token",
				}),
				&gatewaygitlab.ClientOptions{BaseURL: srv.URL},
			)
			require.NoError(t, err)

			repo := &Repository{
				client: client,
				repoID: 100,
				log:    silogtest.New(t),
			}
			require.NoError(t, repo.MergeChange(
				t.Context(),
				&MR{Number: 55},
				forge.MergeChangeOptions{Method: tt.method},
			))
		})
	}
}
