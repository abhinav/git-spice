package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/forge"
)

func TestRollupFromStatusCheckRollupState(t *testing.T) {
	tests := []struct {
		state string
		want  forge.ChecksState
	}{
		{"SUCCESS", forge.ChecksPassed},
		{"PENDING", forge.ChecksPending},
		{"EXPECTED", forge.ChecksPending},
		{"FAILURE", forge.ChecksFailed},
		{"ERROR", forge.ChecksFailed},
		{"", forge.ChecksNone},
		{"unknown", forge.ChecksFailed},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			assert.Equal(t, tt.want, rollupFromStatusCheckRollupState(tt.state))
		})
	}
}

func TestNativeCheckRunState(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		conclusion string
		want       string
	}{
		{"CompletedSuccess", "COMPLETED", "SUCCESS", "success"},
		{"CompletedFailure", "COMPLETED", "FAILURE", "failure"},
		{"CompletedNeutral", "COMPLETED", "NEUTRAL", "neutral"},
		{"CompletedTimedOut", "COMPLETED", "TIMED_OUT", "timed_out"},
		{"InProgress", "IN_PROGRESS", "", "in_progress"},
		{"Queued", "QUEUED", "", "queued"},
		{"CompletedWithoutConclusion", "COMPLETED", "", "completed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, nativeCheckRunState(tt.status, tt.conclusion))
		})
	}
}

func TestNativeStatusContextState(t *testing.T) {
	tests := []struct {
		state string
		want  string
	}{
		{"SUCCESS", "success"},
		{"FAILURE", "failure"},
		{"ERROR", "error"},
		{"PENDING", "pending"},
		{"EXPECTED", "expected"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			assert.Equal(t, tt.want, nativeStatusContextState(tt.state))
		})
	}
}
