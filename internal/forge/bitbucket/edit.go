package bitbucket

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
)

// EditChange edits an existing pull request.
func (r *Repository) EditChange(
	ctx context.Context,
	id forge.ChangeID,
	opts forge.EditChangeOptions,
) error {
	prID := mustPR(id).Number

	if err := r.updatePRBase(ctx, prID, opts.Base); err != nil {
		return err
	}

	if err := r.updatePRDraft(ctx, prID, opts.Draft); err != nil {
		return err
	}

	if err := r.addPRReviewers(ctx, prID, opts.AddReviewers); err != nil {
		return err
	}

	r.warnUnsupportedEditOptions(opts)
	return nil
}

func (r *Repository) updatePRBase(ctx context.Context, prID int64, base string) error {
	if base == "" {
		return nil
	}

	return r.updatePullRequest(ctx, prID, &bitbucket.PullRequestUpdateRequest{
		Destination: &bitbucket.BranchRef{
			Branch: bitbucket.Branch{Name: base},
		},
	})
}

func (r *Repository) updatePRDraft(ctx context.Context, prID int64, draft *bool) error {
	if draft == nil {
		return nil
	}
	return r.updatePullRequest(ctx, prID, &bitbucket.PullRequestUpdateRequest{
		Draft: draft,
	})
}

func (r *Repository) addPRReviewers(
	ctx context.Context,
	prID int64,
	reviewers []string,
) error {
	if len(reviewers) == 0 {
		return nil
	}

	// First get current PR to preserve existing reviewers.
	pr, err := r.getPullRequest(ctx, prID)
	if err != nil {
		return fmt.Errorf("get current PR: %w", err)
	}

	// Resolve new reviewer UUIDs.
	newReviewers, err := r.resolveReviewerUUIDs(ctx, reviewers)
	if err != nil {
		return fmt.Errorf("resolve reviewers: %w", err)
	}

	// Merge existing and new reviewers.
	allReviewers := mergeReviewers(pr.Reviewers, newReviewers)

	return r.updatePullRequest(ctx, prID, &bitbucket.PullRequestUpdateRequest{
		Title:     &pr.Title,
		Reviewers: allReviewers,
	})
}

func mergeReviewers(existing []bitbucket.User, added []string) []bitbucket.Reviewer {
	seen := make(map[string]bool)
	result := make([]bitbucket.Reviewer, 0, len(existing)+len(added))

	for _, u := range existing {
		if !seen[u.UUID] {
			seen[u.UUID] = true
			result = append(result, bitbucket.Reviewer{UUID: u.UUID})
		}
	}
	for _, rev := range added {
		if !seen[rev] {
			seen[rev] = true
			result = append(result, bitbucket.Reviewer{UUID: rev})
		}
	}
	return result
}

func (r *Repository) updatePullRequest(
	ctx context.Context,
	prID int64,
	req *bitbucket.PullRequestUpdateRequest,
) error {
	_, _, err := r.client.PullRequestUpdate(ctx, r.workspace, r.repo, prID, req)
	if err != nil {
		return fmt.Errorf("update pull request: %w", err)
	}
	r.log.Debug("Updated pull request", "pr", prID)
	return nil
}

func (r *Repository) warnUnsupportedEditOptions(opts forge.EditChangeOptions) {
	if len(opts.AddLabels) > 0 {
		r.log.Warn("Bitbucket does not support PR labels; ignoring --label flags")
	}
	if len(opts.AddAssignees) > 0 {
		r.log.Warn("Bitbucket does not support PR assignees; ignoring --assign flags")
	}
}
