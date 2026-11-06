package gitea

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
)

func TestRepository_ChangeChecks(t *testing.T) {
	tests := []struct {
		name     string
		statuses []*giteagw.CommitStatus
		want     []forge.ChangeCheck
	}{
		{"NoStatuses", nil, nil},
		{
			name: "Success",
			statuses: []*giteagw.CommitStatus{{
				State:   giteagw.CommitStatusSuccess,
				Context: "ci/test",
			}},
			want: []forge.ChangeCheck{{
				Name:  "ci/test",
				State: forge.ChangeCheckPassed,
			}},
		},
		{
			name: "Warning",
			statuses: []*giteagw.CommitStatus{{
				State:   giteagw.CommitStatusWarning,
				Context: "ci/test",
			}},
			want: []forge.ChangeCheck{{
				Name:  "ci/test",
				State: forge.ChangeCheckPassed,
			}},
		},
		{
			name: "Pending",
			statuses: []*giteagw.CommitStatus{{
				State:   giteagw.CommitStatusPending,
				Context: "ci/test",
			}},
			want: []forge.ChangeCheck{{
				Name:  "ci/test",
				State: forge.ChangeCheckPending,
			}},
		},
		{
			name: "Failure",
			statuses: []*giteagw.CommitStatus{{
				State:   giteagw.CommitStatusFailure,
				Context: "ci/test",
			}},
			want: []forge.ChangeCheck{{
				Name:  "ci/test",
				State: forge.ChangeCheckFailed,
			}},
		},
		{
			name: "Error",
			statuses: []*giteagw.CommitStatus{{
				State:   giteagw.CommitStatusError,
				Context: "ci/test",
			}},
			want: []forge.ChangeCheck{{
				Name:  "ci/test",
				State: forge.ChangeCheckFailed,
			}},
		},
		{
			name: "MissingContext",
			statuses: []*giteagw.CommitStatus{{
				State: giteagw.CommitStatusSuccess,
			}},
			want: []forge.ChangeCheck{{
				Name:  "Gitea commit status 1",
				State: forge.ChangeCheckPassed,
			}},
		},
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
				case "/api/v1/repos/captain/warp-core/commits/abc123/statuses":
					writeJSON(t, w, http.StatusOK, tt.statuses)
				default:
					http.NotFound(w, r)
				}
			})
			defer srv.Close()

			repo := newTestRepo(t, srv)
			checks, err := repo.ChangeChecks(t.Context(), &PR{Number: 42})
			require.NoError(t, err)
			assert.Equal(t, tt.want, checks)
		})
	}
}

func TestCommitStatusState(t *testing.T) {
	tests := []struct {
		input string
		want  forge.ChangeCheckState
	}{
		{"", forge.ChangeCheckPassed},
		{giteagw.CommitStatusSuccess, forge.ChangeCheckPassed},
		{giteagw.CommitStatusWarning, forge.ChangeCheckPassed},
		{giteagw.CommitStatusPending, forge.ChangeCheckPending},
		{giteagw.CommitStatusFailure, forge.ChangeCheckFailed},
		{giteagw.CommitStatusError, forge.ChangeCheckFailed},
		{"unknown", forge.ChangeCheckPending},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, commitStatusState(tt.input), "state=%q", tt.input)
	}
}
