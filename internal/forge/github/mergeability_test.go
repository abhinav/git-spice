package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shurcooL/githubv4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestRepository_ChangeMergeability(t *testing.T) {
	var queried bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)

		var body struct {
			Query     string `json:"query"`
			Variables struct {
				ID string `json:"id"`
			} `json:"variables"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "prID", body.Variables.ID)
		assert.Contains(t, body.Query, "mergeable")
		assert.Contains(t, body.Query, "mergeStateStatus")
		assert.Contains(t, body.Query, "isDraft")
		queried = true

		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"node": map[string]any{
					"mergeable":        "MERGEABLE",
					"mergeStateStatus": "CLEAN",
					"isDraft":          false,
				},
			},
		}))
	}))
	defer srv.Close()

	repo, err := newRepository(
		t.Context(), new(Forge),
		"owner", "repo",
		silogtest.New(t),
		githubv4.NewEnterpriseClient(srv.URL, nil),
		"repoID",
	)
	require.NoError(t, err)

	got, err := repo.ChangeMergeability(
		t.Context(),
		&PR{Number: 1, GQLID: "prID"},
	)
	require.NoError(t, err)
	assert.True(t, queried)
	assert.Equal(t, forge.ChangeMergeability{
		State:  forge.ChangeMergeabilityReady,
		Reason: forge.ChangeMergeabilityReasonUnknown,
	}, got)
}

func TestRepository_ChangeMergeability_wrapsQueryError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "offline", http.StatusInternalServerError)
	}))
	defer srv.Close()

	repo, err := newRepository(
		t.Context(), new(Forge),
		"owner", "repo",
		silogtest.New(t),
		githubv4.NewEnterpriseClient(srv.URL, nil),
		"repoID",
	)
	require.NoError(t, err)

	_, err = repo.ChangeMergeability(
		t.Context(),
		&PR{Number: 1, GQLID: "prID"},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query mergeability")
}

func TestChangeMergeabilityFromGitHub(t *testing.T) {
	tests := []struct {
		name           string
		giveMergeable  githubv4.MergeableState
		giveMergeState githubv4.MergeStateStatus
		giveDraft      bool
		want           forge.ChangeMergeability
	}{
		{
			name:           "Clean",
			giveMergeable:  githubv4.MergeableStateMergeable,
			giveMergeState: githubv4.MergeStateStatusClean,
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityReady,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name:           "HasHooks",
			giveMergeable:  githubv4.MergeableStateMergeable,
			giveMergeState: githubv4.MergeStateStatusHasHooks,
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityReady,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name:           "Unstable",
			giveMergeable:  githubv4.MergeableStateMergeable,
			giveMergeState: githubv4.MergeStateStatusUnstable,
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityReady,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name:           "Dirty",
			giveMergeable:  githubv4.MergeableStateConflicting,
			giveMergeState: githubv4.MergeStateStatusDirty,
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityBlocked,
				Reason: forge.ChangeMergeabilityReasonConflicts,
			},
		},
		{
			name:           "Behind",
			giveMergeable:  githubv4.MergeableStateMergeable,
			giveMergeState: githubv4.MergeStateStatusBehind,
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityBlocked,
				Reason: forge.ChangeMergeabilityReasonBehind,
			},
		},
		{
			name:           "DraftStatus",
			giveMergeable:  githubv4.MergeableStateMergeable,
			giveMergeState: githubv4.MergeStateStatusDraft,
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityBlocked,
				Reason: forge.ChangeMergeabilityReasonDraft,
			},
		},
		{
			name:           "DraftFlag",
			giveMergeable:  githubv4.MergeableStateMergeable,
			giveMergeState: githubv4.MergeStateStatusClean,
			giveDraft:      true,
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityBlocked,
				Reason: forge.ChangeMergeabilityReasonDraft,
			},
		},
		{
			name:           "Blocked",
			giveMergeable:  githubv4.MergeableStateMergeable,
			giveMergeState: githubv4.MergeStateStatusBlocked,
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityBlocked,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name:           "MergeableWithUnknownStatus",
			giveMergeable:  githubv4.MergeableStateMergeable,
			giveMergeState: githubv4.MergeStateStatusUnknown,
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityWaiting,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name:           "ConflictingFallback",
			giveMergeable:  githubv4.MergeableStateConflicting,
			giveMergeState: githubv4.MergeStateStatusUnknown,
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityBlocked,
				Reason: forge.ChangeMergeabilityReasonConflicts,
			},
		},
		{
			name:           "Waiting",
			giveMergeable:  githubv4.MergeableStateUnknown,
			giveMergeState: githubv4.MergeStateStatusUnknown,
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityWaiting,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name:           "Unknown",
			giveMergeable:  githubv4.MergeableState("RECALIBRATING"),
			giveMergeState: githubv4.MergeStateStatus("RECALIBRATING"),
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityUnknown,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, changeMergeabilityFromGitHub(
				tt.giveMergeable,
				tt.giveMergeState,
				tt.giveDraft,
			))
		})
	}
}
