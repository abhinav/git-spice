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

func TestRepository_ChangeChecks(t *testing.T) {
	tests := []struct {
		name     string
		statuses []*gateway.CommitStatus
		want     []forge.ChangeCheck
	}{
		{"NoStatuses", nil, nil},
		{
			name: "Success",
			statuses: []*gateway.CommitStatus{{
				State:   gateway.CommitStatusSuccess,
				Context: "ci/test",
			}},
			want: []forge.ChangeCheck{{
				Name:  "ci/test",
				State: forge.ChangeCheckPassed,
			}},
		},
		{
			name: "Warning",
			statuses: []*gateway.CommitStatus{{
				State:   gateway.CommitStatusWarning,
				Context: "ci/test",
			}},
			want: []forge.ChangeCheck{{
				Name:  "ci/test",
				State: forge.ChangeCheckPassed,
			}},
		},
		{
			name: "Pending",
			statuses: []*gateway.CommitStatus{{
				State:   gateway.CommitStatusPending,
				Context: "ci/test",
			}},
			want: []forge.ChangeCheck{{
				Name:  "ci/test",
				State: forge.ChangeCheckPending,
			}},
		},
		{
			name: "Failure",
			statuses: []*gateway.CommitStatus{{
				State:   gateway.CommitStatusFailure,
				Context: "ci/test",
			}},
			want: []forge.ChangeCheck{{
				Name:  "ci/test",
				State: forge.ChangeCheckFailed,
			}},
		},
		{
			name: "Error",
			statuses: []*gateway.CommitStatus{{
				State:   gateway.CommitStatusError,
				Context: "ci/test",
			}},
			want: []forge.ChangeCheck{{
				Name:  "ci/test",
				State: forge.ChangeCheckFailed,
			}},
		},
		{
			name: "MissingContext",
			statuses: []*gateway.CommitStatus{{
				State: gateway.CommitStatusSuccess,
			}},
			want: []forge.ChangeCheck{{
				Name:  "Forgejo commit status 1",
				State: forge.ChangeCheckPassed,
			}},
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
							Index: 42,
							Head:  &gateway.PRBranchInfo{SHA: "abc123"},
						})
					case "/api/v1/repos/owner/repo/statuses/abc123":
						writeGatewayJSON(t, w, http.StatusOK, tt.statuses)
					default:
						t.Fatalf("unexpected request path: %s", r.URL.Path)
					}
				}))
			defer srv.Close()

			repo := newTestRepository(t, srv)
			checks, err := repo.ChangeChecks(t.Context(), &PR{Number: 42})
			require.NoError(t, err)
			assert.Equal(t, tt.want, checks)
		})
	}
}

func TestCommitStatusState(t *testing.T) {
	tests := []struct {
		input gateway.CommitStatusState
		want  forge.ChangeCheckState
	}{
		{gateway.CommitStatusSuccess, forge.ChangeCheckPassed},
		{gateway.CommitStatusWarning, forge.ChangeCheckPassed},
		{gateway.CommitStatusPending, forge.ChangeCheckPending},
		{gateway.CommitStatusFailure, forge.ChangeCheckFailed},
		{gateway.CommitStatusError, forge.ChangeCheckFailed},
		{"unknown", forge.ChangeCheckPending},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, commitStatusState(tt.input), "state=%q", tt.input)
	}
}
