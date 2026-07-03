package gitea

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
)

func TestRepository_ChangeMergeability(t *testing.T) {
	mergeable := true
	conflicting := false

	tests := []struct {
		name      string
		draft     bool
		mergeable *bool
		want      forge.ChangeMergeability
	}{
		{
			name:      "Ready",
			mergeable: &mergeable,
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityReady,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name:      "Conflicts",
			mergeable: &conflicting,
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
		{
			name: "Unknown",
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityUnknown,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/repos/captain/warp-core/pulls/42":
					writeJSON(t, w, http.StatusOK, giteagw.PullRequest{
						Number:    42,
						Draft:     tt.draft,
						Mergeable: tt.mergeable,
					})
				default:
					http.NotFound(w, r)
				}
			})
			defer srv.Close()

			repo := newTestRepo(t, srv)
			got, err := repo.ChangeMergeability(t.Context(), &PR{Number: 42})
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
