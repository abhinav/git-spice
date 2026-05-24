package gitlab

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/gitlab"
)

// ChangeChecksState reports the aggregate CI pipeline state
// for the given merge request.
func (r *Repository) ChangeChecksState(
	ctx context.Context, fid forge.ChangeID,
) (forge.ChecksState, error) {
	id := mustMR(fid)
	mr, _, err := r.client.MergeRequestGet(
		ctx, r.repoID, id.Number, nil,
	)
	if err != nil {
		return 0, fmt.Errorf("get merge request: %w", err)
	}

	return pipelineState(mr.HeadPipeline), nil
}

func pipelineState(
	pipeline *gitlab.Pipeline,
) forge.ChecksState {
	if pipeline == nil {
		return forge.ChecksPassed // no CI configured
	}

	switch pipeline.Status {
	case gitlab.PipelineStatusSuccess, gitlab.PipelineStatusSkipped:
		return forge.ChecksPassed
	case gitlab.PipelineStatusFailed, gitlab.PipelineStatusCanceled:
		return forge.ChecksFailed
	default:
		return forge.ChecksPending
	}
}
