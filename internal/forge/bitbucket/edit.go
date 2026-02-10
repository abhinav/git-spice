package bitbucket

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
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

	req := &apiUpdatePRRequest{
		Destination: &apiBranchRef{Branch: apiBranch{Name: base}},
	}
	return r.updatePullRequest(ctx, prID, req)
}

func (r *Repository) updatePRDraft(ctx context.Context, prID int64, draft *bool) error {
	if draft == nil {
		return nil
	}
	return r.updatePullRequest(ctx, prID, &apiUpdatePRRequest{Draft: draft})
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

	req := &apiUpdatePRRequest{
		Title:     pr.Title, // Required by Bitbucket PUT
		Reviewers: allReviewers,
	}
	return r.updatePullRequest(ctx, prID, req)
}

func mergeReviewers(existing []apiUser, added []apiReviewer) []apiReviewer {
	seen := make(map[string]bool)
	result := make([]apiReviewer, 0, len(existing)+len(added))

	for _, u := range existing {
		if !seen[u.UUID] {
			seen[u.UUID] = true
			result = append(result, apiReviewer{UUID: u.UUID})
		}
	}
	for _, rev := range added {
		if !seen[rev.UUID] {
			seen[rev.UUID] = true
			result = append(result, rev)
		}
	}
	return result
}

func (r *Repository) updatePullRequest(
	ctx context.Context,
	prID int64,
	req *apiUpdatePRRequest,
) error {
	path := fmt.Sprintf("/repositories/%s/%s/pullrequests/%d", r.workspace, r.repo, prID)

	var resp apiPullRequest
	if err := r.client.put(ctx, path, req, &resp); err != nil {
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
