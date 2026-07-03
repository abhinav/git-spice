package gitlab

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/gitlab"
)

// ChangeChecks reports CI/checks for the given merge request.
func (r *Repository) ChangeChecks(
	ctx context.Context, fid forge.ChangeID,
) ([]forge.ChangeCheck, error) {
	id := mustMR(fid)
	mr, _, err := r.client.MergeRequestGet(
		ctx, r.repoID, id.Number, nil,
	)
	if err != nil {
		return nil, fmt.Errorf("get merge request: %w", err)
	}

	if mr.SHA == "" {
		return nil, nil
	}

	var checks []forge.ChangeCheck
	opt := &gitlab.ListCommitStatusesOptions{
		ListOptions: gitlab.ListOptions{PerPage: 100},
	}
	if mr.SourceBranch != "" {
		opt.Ref = &mr.SourceBranch
	}

	for {
		statuses, resp, err := r.client.CommitStatusList(
			ctx,
			r.repoID,
			mr.SHA,
			opt,
		)
		if err != nil {
			return nil, fmt.Errorf("list commit statuses: %w", err)
		}

		for _, status := range statuses {
			checks = append(checks, commitStatusCheck(status))
		}

		if resp.NextPage == 0 {
			break
		}
		opt.Page = int64(resp.NextPage)
	}

	return checks, nil
}

func commitStatusCheck(status *gitlab.CommitStatus) forge.ChangeCheck {
	check := forge.ChangeCheck{Name: status.Name}
	switch status.Status {
	case gitlab.PipelineStatusSuccess, gitlab.PipelineStatusSkipped:
		check.State = forge.ChangeCheckPassed
	case gitlab.PipelineStatusFailed, gitlab.PipelineStatusCanceled:
		check.State = forge.ChangeCheckFailed
	default:
		check.State = forge.ChangeCheckPending
	}
	return check
}
