package github

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/cmputil"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
)

// EditChange edits an existing change in a repository.
func (r *Repository) EditChange(ctx context.Context, fid forge.ChangeID, opts forge.EditChangeOptions) error {
	if cmputil.Zero(opts.Base) &&
		cmputil.Zero(opts.Draft) &&
		len(opts.AddLabels) == 0 &&
		len(opts.AddReviewers) == 0 &&
		len(opts.AddAssignees) == 0 {
		return nil // nothing to do
	}
	pr := mustPR(fid)

	// We don't know the GraphQL ID for the PR, so find it.
	graphQLID, err := r.graphQLID(ctx, pr)
	if err != nil {
		return fmt.Errorf("get pull request ID: %w", err)
	}

	if opts.Base != "" {
		input := github.UpdatePullRequestInput{
			PullRequestID: graphQLID,
			BaseRefName:   &opts.Base,
		}

		if err := r.gateway.UpdatePullRequest(ctx, &input); err != nil {
			return fmt.Errorf("edit pull request: %w", err)
		}
		r.log.Debug("Changed base branch for PR", "new.base", opts.Base)
	}

	// Draft status is a separate API call for some reason.
	if opts.Draft != nil {
		// And for some reason, it's a different mutation based on
		// whether it's true or false.
		var logMsg string
		if *opts.Draft {
			err = r.gateway.ConvertPullRequestToDraft(ctx, graphQLID)
			logMsg = "Converted PR to draft"
		} else {
			err = r.gateway.MarkPullRequestReadyForReview(ctx, graphQLID)
			logMsg = "Marked PR as ready for review"
		}

		if err != nil {
			return fmt.Errorf("update draft status: %w", err)
		}

		r.log.Debug(logMsg, "pr", pr.Number)
	}

	// TODO:
	// perform in parallel, share resolved user IDs, etc.
	// maybe even cache and persist resolved IDs in store.

	if err := r.addLabelsToPullRequest(ctx, opts.AddLabels, graphQLID); err != nil {
		return fmt.Errorf("add labels to PR: %w", err)
	}

	if err := r.addReviewersToPullRequest(ctx, opts.AddReviewers, graphQLID); err != nil {
		return fmt.Errorf("add reviewers to PR: %w", err)
	}

	if err := r.addAssigneesToPullRequest(ctx, opts.AddAssignees, graphQLID); err != nil {
		return fmt.Errorf("add assignees to PR: %w", err)
	}

	return nil
}
