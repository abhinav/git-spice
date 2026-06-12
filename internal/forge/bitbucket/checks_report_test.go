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
		want     forge.ChecksRollupState
	}{
		{
			name: "Empty",
			want: forge.ChecksRollupNone,
		},
		{
			name: "AllSuccessful",
			statuses: []bitbucket.CommitStatus{
				{State: bitbucket.CommitStatusSuccessful},
				{State: bitbucket.CommitStatusSuccessful},
			},
			want: forge.ChecksRollupPassed,
		},
		{
			name: "Failed",
			statuses: []bitbucket.CommitStatus{
				{State: bitbucket.CommitStatusSuccessful},
				{State: bitbucket.CommitStatusFailed},
			},
			want: forge.ChecksRollupFailed,
		},
		{
			name: "InProgress",
			statuses: []bitbucket.CommitStatus{
				{State: bitbucket.CommitStatusSuccessful},
				{State: bitbucket.CommitStatusInProgress},
			},
			want: forge.ChecksRollupPending,
		},
		{
			name: "FailedTrumpsInProgress",
			statuses: []bitbucket.CommitStatus{
				{State: bitbucket.CommitStatusInProgress},
				{State: bitbucket.CommitStatusFailed},
			},
			want: forge.ChecksRollupFailed,
		},
		{
			name: "StoppedIsPassed",
			statuses: []bitbucket.CommitStatus{
				{State: bitbucket.CommitStatusSuccessful},
				{State: bitbucket.CommitStatusStopped},
			},
			want: forge.ChecksRollupPassed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, rollupFromCommitStatuses(tt.statuses))
		})
	}
}
