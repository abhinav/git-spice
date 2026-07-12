package github

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGraphQLEnums_JSON(t *testing.T) {
	tests := []struct {
		name string
		give any
		want string
	}{
		{name: "PullRequestState", give: PullRequestStateMerged, want: `"MERGED"`},
		{name: "MergeableState", give: MergeableStateConflicting, want: `"CONFLICTING"`},
		{name: "MergeStateStatus", give: MergeStateStatusHasHooks, want: `"HAS_HOOKS"`},
		{name: "StatusState", give: StatusStateExpected, want: `"EXPECTED"`},
		{name: "CheckStatusState", give: CheckStatusStateInProgress, want: `"IN_PROGRESS"`},
		{name: "CheckConclusionState", give: CheckConclusionStateStartupFailure, want: `"STARTUP_FAILURE"`},
		{name: "MergeMethod", give: MergeMethodSquash, want: `"SQUASH"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.give)
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(got))
		})
	}
}

func TestGraphQLEnums_rejectUnknownInboundValue(t *testing.T) {
	var got PullRequestState
	err := json.Unmarshal([]byte(`"RECALIBRATING"`), &got)
	assert.ErrorContains(t, err, `unknown github.PullRequestState: "RECALIBRATING"`)
}

func TestGraphQLEnums_rejectUnknownOutboundValue(t *testing.T) {
	_, err := json.Marshal(PullRequestStateUnknown)
	require.Error(t, err)

	_, err = json.Marshal(PullRequestState(100))
	require.Error(t, err)
}
