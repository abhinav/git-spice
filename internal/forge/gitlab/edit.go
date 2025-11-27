package gitlab

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.abhg.dev/gs/internal/cmputil"
	"go.abhg.dev/gs/internal/forge"
)

// GitLab tracks draft status in the title of a merge request
// with one of the following prefixes.
//
// https://docs.gitlab.com/ee/user/project/merge_requests/drafts.html#mark-merge-requests-as-drafts
var _draftRegex = regexp.MustCompile(`(?i)^\s*(\[Draft]|Draft:|\(Draft\))\s*`)

// EditChange edits an existing change in a repository.
func (r *Repository) EditChange(ctx context.Context, id forge.ChangeID, opts forge.EditChangeOptions) error {
	if cmputil.Zero(opts.Base) &&
		cmputil.Zero(opts.Draft) &&
		len(opts.Labels) == 0 &&
		len(opts.Reviewers) == 0 &&
		len(opts.Assignees) == 0 {
		return nil // nothing to do
	}

	mrID := mustMR(id)

	var (
		updateOptions gitlab.UpdateMergeRequestOptions
		logUpdates    []slog.Attr

		mergeRequest *gitlab.MergeRequest
	)

	// Used if we need to fetch the current MR status.
	getMergeRequest := func() (*gitlab.MergeRequest, error) {
		if mergeRequest != nil {
			return mergeRequest, nil
		}

		var err error
		mergeRequest, _, err = r.client.MergeRequests.GetMergeRequest(
			r.repoID, mrID.Number, nil,
			gitlab.WithContext(ctx),
		)
		if err != nil {
			return nil, fmt.Errorf("get merge request for update: %w", err)
		}

		return mergeRequest, nil
	}
	if opts.Base != "" {
		updateOptions.TargetBranch = &opts.Base
		logUpdates = append(logUpdates, slog.String("base", opts.Base))
	}

	// TODO:
	// As part of submit, we've likely already fetched this information.
	// Cache it in memory.
	if opts.Draft != nil {
		mr, err := getMergeRequest()
		if err != nil {
			return err
		}

		if *opts.Draft {
			if !mr.Draft {
				updateOptions.Title = gitlab.Ptr(fmt.Sprintf("%s %s", _draftPrefix, mr.Title))
			}
		} else {
			if mr.Draft {
				title := _draftRegex.ReplaceAllString(mr.Title, "")
				updateOptions.Title = &title
			}
		}
		logUpdates = append(logUpdates, slog.Bool("draft", *opts.Draft))
	}

	if len(opts.Labels) > 0 {
		updateOptions.AddLabels = (*gitlab.LabelOptions)(&opts.Labels)
	}

	if len(opts.Reviewers) > 0 {
		// TODO: de-dupe

		reviewerIDs, err := r.resolveReviewerIDs(ctx, opts.Reviewers)
		if err != nil {
			return fmt.Errorf("resolve reviewer IDs: %w", err)
		}
		updateOptions.ReviewerIDs = &reviewerIDs
		logUpdates = append(logUpdates, slog.Any("reviewers", opts.Reviewers))
	}

	if len(opts.Assignees) > 0 {
		assigneeIDs, err := r.assigneeIDs(ctx, opts.Assignees)
		if err != nil {
			return fmt.Errorf("resolve assignees: %w", err)
		}

		mr, err := getMergeRequest()
		if err != nil {
			return err
		}

		// Make this cleaner.
		existing := make([]int, 0, len(mr.Assignees))
		for _, assignee := range mr.Assignees {
			existing = append(existing, assignee.ID)
		}

		merged := mergeAssigneeIDs(existing, assigneeIDs)
		updateOptions.AssigneeIDs = &merged
		logUpdates = append(logUpdates, slog.Any("assignees", merged))
	}

	_, _, err := r.client.MergeRequests.UpdateMergeRequest(
		r.repoID, mrID.Number, &updateOptions,
		gitlab.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("update merge request: %w", err)
	}
	if len(logUpdates) > 0 {
		r.log.Debug("Updated merge request",
			"mr", mrID.Number,
			"new", slog.GroupValue(logUpdates...),
		)
	}

	return nil
}

func mergeAssigneeIDs(existing, assignees []int) []int {
	if len(assignees) == 0 {
		return existing
	}

	merged := make([]int, 0, len(existing)+len(assignees))
	seen := make(map[int]struct{}, len(existing)+len(assignees))
	for _, id := range existing {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		merged = append(merged, id)
	}
	for _, id := range assignees {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		merged = append(merged, id)
	}

	return merged
}
