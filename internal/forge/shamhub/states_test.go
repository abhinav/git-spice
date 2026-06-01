package shamhub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/silog"
)

func TestForgeRepository_MergeChange_mergeMethod(t *testing.T) {
	tests := []struct {
		name string
		give forge.MergeMethod
		want string
	}{
		{
			name: "Default",
		},
		{
			name: "Squash",
			give: forge.MergeMethodSquash,
			want: "squash",
		},
		{
			name: "Rebase",
			give: forge.MergeMethodRebase,
		},
		{
			name: "Unsupported",
			give: forge.MergeMethod(99),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got map[string]string
			srv := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(
						t,
						"/owner/repo/change/42/merge",
						r.URL.Path,
					)
					require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
					_, _ = w.Write([]byte(`{}`))
				},
			))
			t.Cleanup(srv.Close)

			repo, err := newRepository(
				&Forge{
					Options: Options{
						URL:    "https://example.com",
						APIURL: srv.URL,
					},
					Log: silog.Nop(),
				},
				&AuthenticationToken{tok: "token"},
				&RepositoryID{
					url:   "https://example.com",
					owner: "owner",
					repo:  "repo",
				},
				srv.Client(),
			)
			require.NoError(t, err)

			err = repo.MergeChange(t.Context(), ChangeID(42), forge.MergeChangeOptions{
				Method: tt.give,
			})
			require.NoError(t, err)

			if tt.want == "" {
				assert.NotContains(t, got, "mergeMethod")
			} else {
				assert.Equal(t, tt.want, got["mergeMethod"])
			}
		})
	}
}
