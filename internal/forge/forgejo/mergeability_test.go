package forgejo

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	gateway "go.abhg.dev/gs/internal/gateway/forgejo"
)

func TestRepository_ChangeMergeability(t *testing.T) {
	tests := []struct {
		name      string
		draft     bool
		mergeable bool
		want      forge.ChangeMergeability
	}{
		{
			name:      "Ready",
			mergeable: true,
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityReady,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name: "NotMergeable",
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityBlocked,
				Reason: forge.ChangeMergeabilityReasonConflicts,
			},
		},
		{
			name:  "Draft",
			draft: true,
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityBlocked,
				Reason: forge.ChangeMergeabilityReasonDraft,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/v1/repos/owner/repo":
						writeGatewayJSON(t, w, http.StatusOK, gateway.Repository{
							FullName:    "owner/repo",
							Permissions: &gateway.Permission{Push: true},
						})
					case "/api/v1/user":
						writeGatewayJSON(t, w, http.StatusOK, gateway.User{ID: 1})
					case "/api/v1/repos/owner/repo/pulls/42":
						writeGatewayJSON(t, w, http.StatusOK, gateway.PullRequest{
							Index:     42,
							Draft:     tt.draft,
							Mergeable: tt.mergeable,
						})
					default:
						t.Fatalf("unexpected request path: %s", r.URL.Path)
					}
				}))
			defer srv.Close()

			repo := newTestRepository(t, srv)
			got, err := repo.ChangeMergeability(t.Context(), &PR{Number: 42})
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
