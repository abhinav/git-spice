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
		// Merge handler relies on "no pipeline -> passed" so projects
		// without CI don't false-fail. Preserved for backwards
		// compatibility; ChecksByChange uses ChecksNone instead.
		return forge.ChecksPassed
	}
	return rollupFromPipelineStatus(pipeline.Status)
}

// rollupFromPipelineStatus collapses a GitLab pipeline status into
// the upstream rollup taxonomy. Canceled/manual/skipped pipelines
// are user-initiated non-failures and roll up as passed.
// Unknown statuses default to pending — GitLab adds new pipeline
// states over time and reporting them as pending is safer than
// reporting them as failed.
func rollupFromPipelineStatus(status string) forge.ChecksState {
	switch status {
	case gitlab.PipelineStatusSuccess,
		gitlab.PipelineStatusSkipped,
		gitlab.PipelineStatusCanceled,
		gitlab.PipelineStatusManual:
		return forge.ChecksPassed
	case gitlab.PipelineStatusFailed:
		return forge.ChecksFailed
	case gitlab.PipelineStatusCreated,
		gitlab.PipelineStatusWaitingForResource,
		gitlab.PipelineStatusPreparing,
		gitlab.PipelineStatusPending,
		gitlab.PipelineStatusRunning,
		gitlab.PipelineStatusScheduled:
		return forge.ChecksPending
	default:
		return forge.ChecksPending
	}
}

// ChecksByChange reports per-change rolled-up and per-job pipeline
// state for each of the given merge requests.
//
// One MergeRequestGet + (when a pipeline exists) one PipelineJobsList
// call per MR. The job list is reported in pipeline order (stages
// first, then job order within stages) which is the most useful
// order for both UI surfaces and tooltips.
func (r *Repository) ChecksByChange(
	ctx context.Context, ids []forge.ChangeID,
) ([]*forge.ChangeChecks, error) {
	out := make([]*forge.ChangeChecks, len(ids))
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
) (*forge.ChangeChecks, error) {
	mrID := mustMR(id)
	mr, _, err := r.client.MergeRequestGet(ctx, r.repoID, mrID.Number, nil)
	if err != nil {
		return nil, fmt.Errorf("get merge request: %w", err)
	}

	if mr.HeadPipeline == nil {
		return &forge.ChangeChecks{Rollup: forge.ChecksNone}, nil
	}

	out := &forge.ChangeChecks{
		Rollup: rollupFromPipelineStatus(mr.HeadPipeline.Status),
		URL:    mr.HeadPipeline.WebURL,
	}

	if mr.HeadPipeline.ID == 0 {
		return out, nil
	}

	jobs, _, err := r.client.PipelineJobsList(
		ctx, r.repoID, mr.HeadPipeline.ID,
	)
	if err != nil {
		// A pipeline existing but its job list being unfetchable
		// (e.g. a transient API failure) shouldn't drop the rollup
		// the user can already see. Log via the caller's normal
		// error path; preserve rollup + URL.
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
