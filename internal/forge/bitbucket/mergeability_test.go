package bitbucket

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
)

func TestChangeMergeability(t *testing.T) {
	tests := []struct {
		name string
		pr   bitbucket.PullRequest
		want forge.ChangeMergeability
	}{
		{
			name: "Mergeable",
			pr: bitbucket.PullRequest{
				ID:        1,
				Mergeable: new(true),
			},
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityReady,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name: "Queued",
			pr: bitbucket.PullRequest{
				ID:        1,
				Mergeable: new(false),
				Queued:    true,
			},
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityWaiting,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name: "QueuedMergeable",
			pr: bitbucket.PullRequest{
				ID:        1,
				Mergeable: new(true),
				Queued:    true,
			},
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityWaiting,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name: "QueuedWithoutMergeable",
			pr: bitbucket.PullRequest{
				ID:     1,
				Queued: true,
			},
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityWaiting,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name: "NotMergeable",
			pr: bitbucket.PullRequest{
				ID:        1,
				Mergeable: new(false),
			},
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityBlocked,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name: "OmittedMergeable",
			pr: bitbucket.PullRequest{
				ID: 1,
			},
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityUnknown,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/repositories/workspace/repo/pullrequests/1", r.URL.Path)
				require.NoError(t, json.NewEncoder(w).Encode(tt.pr))
			}))
			defer srv.Close()

			repo := newTestRepository(srv.URL)
			got, err := repo.ChangeMergeability(t.Context(), &PR{Number: 1})
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
