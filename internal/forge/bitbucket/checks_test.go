package bitbucket

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
)

func TestRollupFromCommitStatuses(t *testing.T) {
	tests := []struct {
		name     string
		statuses []bitbucket.CommitStatus
		want     forge.ChecksState
	}{
		{
			name: "Empty",
			want: forge.ChecksNone,
		},
		{
			name: "AllSuccessful",
			statuses: []bitbucket.CommitStatus{
				{State: bitbucket.CommitStatusSuccessful},
				{State: bitbucket.CommitStatusSuccessful},
			},
			want: forge.ChecksPassed,
		},
		{
			name: "Failed",
			statuses: []bitbucket.CommitStatus{
				{State: bitbucket.CommitStatusSuccessful},
				{State: bitbucket.CommitStatusFailed},
			},
			want: forge.ChecksFailed,
		},
		{
			name: "InProgress",
			statuses: []bitbucket.CommitStatus{
				{State: bitbucket.CommitStatusSuccessful},
				{State: bitbucket.CommitStatusInProgress},
			},
			want: forge.ChecksPending,
		},
		{
			name: "FailedTrumpsInProgress",
			statuses: []bitbucket.CommitStatus{
				{State: bitbucket.CommitStatusInProgress},
				{State: bitbucket.CommitStatusFailed},
			},
			want: forge.ChecksFailed,
		},
		{
			name: "StoppedIsPassed",
			statuses: []bitbucket.CommitStatus{
				{State: bitbucket.CommitStatusSuccessful},
				{State: bitbucket.CommitStatusStopped},
			},
			want: forge.ChecksPassed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, rollupFromCommitStatuses(tt.statuses))
		})
	}
}

func TestAggregateStatuses_keepsNoCheckSemantics(t *testing.T) {
	// aggregateStatuses predates the richer API and must keep its
	// "no checks -> passed" semantics so the merge handler doesn't
	// false-fail on repos without CI.
	assert.Equal(t, forge.ChecksPassed, aggregateStatuses(nil))
}
