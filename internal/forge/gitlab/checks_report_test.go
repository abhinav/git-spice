package gitlab

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/gitlab"
)

func TestRollupFromPipelineStatus(t *testing.T) {
	tests := []struct {
		status string
		want   forge.ChecksRollupState
	}{
		{gitlab.PipelineStatusSuccess, forge.ChecksRollupPassed},
		{gitlab.PipelineStatusSkipped, forge.ChecksRollupPassed},
		// Canceled and manual are user-initiated non-failures.
		{gitlab.PipelineStatusCanceled, forge.ChecksRollupPassed},
		{gitlab.PipelineStatusManual, forge.ChecksRollupPassed},
		{gitlab.PipelineStatusFailed, forge.ChecksRollupFailed},
		{gitlab.PipelineStatusPending, forge.ChecksRollupPending},
		{gitlab.PipelineStatusRunning, forge.ChecksRollupPending},
		{gitlab.PipelineStatusCreated, forge.ChecksRollupPending},
		{gitlab.PipelineStatusPreparing, forge.ChecksRollupPending},
		{gitlab.PipelineStatusWaitingForResource, forge.ChecksRollupPending},
		{gitlab.PipelineStatusScheduled, forge.ChecksRollupPending},
		{"unknown", forge.ChecksRollupPending},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			assert.Equal(t, tt.want, rollupFromPipelineStatus(tt.status))
		})
	}
}
