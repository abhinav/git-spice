package gitlab

import (
	"context"
	"fmt"
	"regexp"

	"github.com/xanzy/go-gitlab"
	"go.abhg.dev/gs/internal/cmputil"
	"go.abhg.dev/gs/internal/forge"
)

const draftRegex = `(?i)^\s*(\[Draft]|Draft:|\(Draft\))\s*`

// EditChange edits an existing change in a repository.
func (r *Repository) EditChange(ctx context.Context, id forge.ChangeID, opts forge.EditChangeOptions) error {
	if cmputil.Zero(opts) {
		return nil // nothing to do
	}

	updateOptions := gitlab.UpdateMergeRequestOptions{}

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
				updateOptions.Title = gitlab.Ptr(fmt.Sprintf("%s %s", DRAFT, mr.Title))
			}
		} else {
			if mr.Draft {
				compile := regexp.MustCompile(draftRegex)
				updateOptions.Title = gitlab.Ptr(compile.ReplaceAllString(mr.Title, ""))
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
