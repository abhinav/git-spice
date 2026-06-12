package gitlab

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/gitlab"
)

// rollupFromPipelineStatus collapses a GitLab pipeline status into the
// forge rollup taxonomy. Canceled/manual/skipped pipelines are
// user-initiated non-failures and roll up as passed. Unknown statuses
// default to pending — GitLab adds new pipeline states over time and
// reporting them as pending is safer than reporting them as failed.
func rollupFromPipelineStatus(status string) forge.ChecksRollupState {
	switch status {
	case gitlab.PipelineStatusSuccess,
		gitlab.PipelineStatusSkipped,
		gitlab.PipelineStatusCanceled,
		gitlab.PipelineStatusManual:
		return forge.ChecksRollupPassed
	case gitlab.PipelineStatusFailed:
		return forge.ChecksRollupFailed
	case gitlab.PipelineStatusCreated,
		gitlab.PipelineStatusWaitingForResource,
		gitlab.PipelineStatusPreparing,
		gitlab.PipelineStatusPending,
		gitlab.PipelineStatusRunning,
		gitlab.PipelineStatusScheduled:
		return forge.ChecksRollupPending
	default:
		return forge.ChecksRollupPending
	}
}

// ChecksByChange reports per-change rolled-up and per-job pipeline
// state for each of the given merge requests.
//
// One MergeRequestGet + (when a pipeline exists) one PipelineJobsList
// call per MR. The job list is reported in pipeline order (stages
// first, then job order within stages) which is the most useful order
// for both UI surfaces and tooltips.
func (r *Repository) ChecksByChange(
	ctx context.Context, ids []forge.ChangeID,
) ([]*forge.ChecksReport, error) {
	out := make([]*forge.ChecksReport, len(ids))
	for i, id := range ids {
		checks, err := r.changeChecks(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("change %v: %w", id, err)
		}
		out[i] = checks
	}
	return out, nil
}

func (r *Repository) changeChecks(
	ctx context.Context, id forge.ChangeID,
) (*forge.ChecksReport, error) {
	mrID := mustMR(id)
	mr, _, err := r.client.MergeRequestGet(ctx, r.repoID, mrID.Number, nil)
	if err != nil {
		return nil, fmt.Errorf("get merge request: %w", err)
	}

	if mr.HeadPipeline == nil {
		return &forge.ChecksReport{Rollup: forge.ChecksRollupNone}, nil
	}

	out := &forge.ChecksReport{
		Rollup: rollupFromPipelineStatus(mr.HeadPipeline.Status),
		URL:    mr.HeadPipeline.WebURL,
	}

	if mr.HeadPipeline.ID == 0 {
		return out, nil
	}

	jobs, _, err := r.client.PipelineJobsList(ctx, r.repoID, mr.HeadPipeline.ID)
	if err != nil {
		return nil, fmt.Errorf("list pipeline jobs: %w", err)
	}

	out.Runs = make([]forge.CheckRun, 0, len(jobs))
	for _, j := range jobs {
		out.Runs = append(out.Runs, forge.CheckRun{
			Name:  j.Name,
			State: j.Status, // already lowercase per GitLab
			URL:   j.WebURL,
		})
	}
	return out, nil
}
