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
	if cmputil.Zero(opts.Base) && cmputil.Zero(opts.Draft) && len(opts.Labels) == 0 && len(opts.Reviewers) == 0 {
		return nil // nothing to do
	}

	mr := mustMR(id)

	var (
		updateOptions gitlab.UpdateMergeRequestOptions
		logUpdates    []slog.Attr
	)
	if opts.Base != "" {
		updateOptions.TargetBranch = &opts.Base
		logUpdates = append(logUpdates, slog.String("base", opts.Base))
	}

	if opts.Draft != nil {
		mr, _, err := r.client.MergeRequests.GetMergeRequest(
			r.repoID, mr.Number, nil,
			gitlab.WithContext(ctx),
		)
		if err != nil {
			return fmt.Errorf("get merge request for update: %w", err)
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
		reviewerIDs, err := r.resolveReviewerIDs(ctx, opts.Reviewers)
		if err != nil {
			return fmt.Errorf("resolve reviewer IDs: %w", err)
		}
		updateOptions.ReviewerIDs = &reviewerIDs
		logUpdates = append(logUpdates, slog.Any("reviewers", opts.Reviewers))
	}

	_, _, err := r.client.MergeRequests.UpdateMergeRequest(
		r.repoID, mr.Number, &updateOptions,
		gitlab.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("update merge request: %w", err)
	}
	if len(logUpdates) > 0 {
		r.log.Debug("Updated merge request",
			"mr", mr.Number,
			"new", slog.GroupValue(logUpdates...),
		)
	}

	return nil
}
