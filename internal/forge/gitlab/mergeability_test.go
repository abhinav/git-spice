package gitlab

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/gitlab"
)

func TestRepository_ChangeMergeability(t *testing.T) {
	tests := []struct {
		name         string
		status       string
		hasConflicts bool
		want         forge.ChangeMergeability
	}{
		{
			name:   "Ready",
			status: gitlab.DetailedMergeStatusMergeable,
			want: forge.ChangeMergeability{
				State: forge.ChangeMergeabilityReady,
			},
		},
		{
			name:   "BlockedByDraft",
			status: gitlab.DetailedMergeStatusDraftStatus,
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityBlocked,
				Reason: forge.ChangeMergeabilityReasonDraft,
			},
		},
		{
			name:         "NeedRebaseWithConflicts",
			status:       gitlab.DetailedMergeStatusNeedRebase,
			hasConflicts: true,
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityBlocked,
				Reason: forge.ChangeMergeabilityReasonConflicts,
			},
		},
		{
			name:   "Unknown",
			status: "new_gitlab_status",
			want: forge.ChangeMergeability{
				State: forge.ChangeMergeabilityUnknown,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, http.MethodGet, r.Method)
					assert.Equal(
						t,
						"/api/v4/projects/42/merge_requests/55",
						r.URL.Path,
					)
					assert.Empty(t, r.URL.RawQuery)

					writeJSON(t, w, gitlab.MergeRequest{
						BasicMergeRequest: gitlab.BasicMergeRequest{
							DetailedMergeStatus: tt.status,
							HasConflicts:        tt.hasConflicts,
						},
					})
				},
			))
			defer srv.Close()

			client, err := gitlab.NewClient(
				gitlab.StaticTokenSource(gitlab.Token{
					Type:  gitlab.TokenTypePrivateToken,
					Value: "token",
				}),
				&gitlab.ClientOptions{
					BaseURL:    srv.URL,
					HTTPClient: srv.Client(),
				},
			)
			require.NoError(t, err)

			got, err := (&Repository{
				client: client,
				repoID: 42,
			}).ChangeMergeability(t.Context(), &MR{Number: 55})
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
