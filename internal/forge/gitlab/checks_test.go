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
		want   forge.ChecksState
	}{
		{gitlab.PipelineStatusSuccess, forge.ChecksPassed},
		{gitlab.PipelineStatusSkipped, forge.ChecksPassed},
		// Canceled and manual are user-initiated non-failures.
		{gitlab.PipelineStatusCanceled, forge.ChecksPassed},
		{gitlab.PipelineStatusManual, forge.ChecksPassed},
		{gitlab.PipelineStatusFailed, forge.ChecksFailed},
		{gitlab.PipelineStatusPending, forge.ChecksPending},
		{gitlab.PipelineStatusRunning, forge.ChecksPending},
		{gitlab.PipelineStatusCreated, forge.ChecksPending},
		{gitlab.PipelineStatusPreparing, forge.ChecksPending},
		{gitlab.PipelineStatusWaitingForResource, forge.ChecksPending},
		{gitlab.PipelineStatusScheduled, forge.ChecksPending},
		{"unknown", forge.ChecksPending},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			assert.Equal(t, tt.want, rollupFromPipelineStatus(tt.status))
		})
	}
}

func TestPipelineState_noPipelineMeansPassed(t *testing.T) {
	// pipelineState predates the richer API and must keep its
	// "no pipeline -> passed" behavior so the merge handler doesn't
	// false-fail on projects with no CI.
	assert.Equal(t, forge.ChecksPassed, pipelineState(nil))
}
