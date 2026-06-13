package gitea

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
)

func TestRepository_ChangeChecksState(t *testing.T) {
	tests := []struct {
		name        string
		statusState string
		want        forge.ChecksState
	}{
		{"NoStatuses", "", forge.ChecksPassed},
		{"Success", giteagw.CommitStatusSuccess, forge.ChecksPassed},
		{"Warning", giteagw.CommitStatusWarning, forge.ChecksPassed},
		{"Pending", giteagw.CommitStatusPending, forge.ChecksPending},
		{"Failure", giteagw.CommitStatusFailure, forge.ChecksFailed},
		{"Error", giteagw.CommitStatusError, forge.ChecksFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/repos/captain/warp-core/pulls/42":
					writeJSON(t, w, http.StatusOK, giteagw.PullRequest{
						Number: 42,
						Head:   &giteagw.PRBranch{Sha: "abc123"},
					})
				case "/api/v1/repos/captain/warp-core/commits/abc123/status":
					writeJSON(t, w, http.StatusOK, giteagw.CombinedStatus{
						State: tt.statusState,
					})
				default:
					http.NotFound(w, r)
				}
			})
			defer srv.Close()

			repo := newTestRepo(t, srv)
			state, err := repo.ChangeChecksState(t.Context(), &PR{Number: 42})
			require.NoError(t, err)
			assert.Equal(t, tt.want, state)
		})
	}
}

func TestCommitStatusState(t *testing.T) {
	tests := []struct {
		input string
		want  forge.ChecksState
	}{
		{"", forge.ChecksPassed},
		{giteagw.CommitStatusSuccess, forge.ChecksPassed},
		{giteagw.CommitStatusWarning, forge.ChecksPassed},
		{giteagw.CommitStatusPending, forge.ChecksPending},
		{giteagw.CommitStatusFailure, forge.ChecksFailed},
		{giteagw.CommitStatusError, forge.ChecksFailed},
		{"unknown", forge.ChecksPending},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, commitStatusState(tt.input), "state=%q", tt.input)
	}
}
