package gitlab

import (
	"context"
	"fmt"
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
	if cmputil.Zero(opts) {
		return nil // nothing to do
	}

	var updateOptions gitlab.UpdateMergeRequestOptions
	if opts.Base != "" {
		updateOptions.TargetBranch = &opts.Base
	}

	if opts.Draft != nil {
		mr, _, err := r.client.MergeRequests.GetMergeRequest(
			r.repoID, mustMR(id).Number, nil,
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
	}

	_, _, err := r.client.MergeRequests.UpdateMergeRequest(
		r.repoID, mustMR(id).Number, &updateOptions,
		gitlab.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("update draft status: %w", err)
	}

	return nil
}
