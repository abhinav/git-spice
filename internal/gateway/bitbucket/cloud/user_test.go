package cloud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_resolveReviewerUUIDs(t *testing.T) {
	// Shared member directory for all lookup scenarios.
	members := []WorkspaceMember{
		{User: User{UUID: "{uuid-alice}", Nickname: "alice"}},
		{User: User{
			UUID:     "{uuid-bob}",
			Username: "bob",
			Nickname: "bobby",
		}},
		{User: User{
			UUID:      "{uuid-carol}",
			Nickname:  "carol",
			AccountID: "712020:f766d886",
		}},
		{User: User{
			UUID:      "{uuid-dave-1}",
			Nickname:  "dave",
			AccountID: "1:dave",
		}},
		{User: User{
			UUID:      "{uuid-dave-2}",
			Nickname:  "dave",
			AccountID: "2:dave",
		}},
	}

	tests := []struct {
		name        string
		identifiers []string
		want        []string
		wantErr     string
	}{
		{
			name:        "Nickname",
			identifiers: []string{"alice"},
			want:        []string{"{uuid-alice}"},
		},
		{
			name:        "NicknameCaseInsensitive",
			identifiers: []string{"ALICE"},
			want:        []string{"{uuid-alice}"},
		},
		{
			name:        "Username",
			identifiers: []string{"bob"},
			want:        []string{"{uuid-bob}"},
		},
		{
			name:        "AccountID",
			identifiers: []string{"712020:f766d886"},
			want:        []string{"{uuid-carol}"},
		},
		{
			name:        "Multiple",
			identifiers: []string{"alice", "bob"},
			want:        []string{"{uuid-alice}", "{uuid-bob}"},
		},
		{
			name:        "NicknameNotFound",
			identifiers: []string{"nobody"},
			wantErr:     `user "nobody" not found in workspace "workspace"`,
		},
		{
			name:        "AccountIDNotFound",
			identifiers: []string{"999:absent"},
			wantErr:     `account_id "999:absent" not found in workspace "workspace"`,
		},
		{
			name:        "Ambiguous",
			identifiers: []string{"dave"},
			wantErr: `multiple users match "dave": ` +
				`[1:dave 2:dave] (use account_id to disambiguate)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/workspaces/workspace/members", r.URL.Path)
				assert.NoError(t, json.NewEncoder(w).Encode(
					WorkspaceMemberList{Values: members}))
			}))
			defer srv.Close()

			gw := newTestGateway(t, srv.URL)
			got, err := gw.resolveReviewerUUIDs(t.Context(), tt.identifiers)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				assert.ErrorContains(t, err, "lookup user")
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGateway_resolveReviewerUUIDs_empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request: %s %s", r.Method, r.URL)
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	got, err := gw.resolveReviewerUUIDs(t.Context(), nil)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestGateway_resolveReviewerUUIDs_accountIDPaged(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/workspaces/workspace/members", r.URL.Path)

		if r.URL.RawQuery == "" {
			assert.NoError(t, json.NewEncoder(w).Encode(WorkspaceMemberList{
				Values: []WorkspaceMember{
					{User: User{UUID: "{uuid-1}", AccountID: "1:one"}},
				},
				Next: srv.URL + "/workspaces/workspace/members?page=2",
			}))
			return
		}

		assert.NoError(t, json.NewEncoder(w).Encode(WorkspaceMemberList{
			Values: []WorkspaceMember{
				{User: User{UUID: "{uuid-2}", AccountID: "2:two"}},
			},
		}))
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	got, err := gw.resolveReviewerUUIDs(t.Context(), []string{"2:two"})
	require.NoError(t, err)
	assert.Equal(t, []string{"{uuid-2}"}, got)
}

func TestGateway_resolveReviewerUUIDs_error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	_, err := gw.resolveReviewerUUIDs(t.Context(), []string{"alice"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "list workspace members")
}
