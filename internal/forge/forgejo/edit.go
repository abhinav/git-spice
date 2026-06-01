package forgejo

import (
	"context"
	"fmt"
	"slices"

	"go.abhg.dev/gs/internal/cmputil"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/forgejo"
)

// EditChange edits an existing change in a repository.
func (r *Repository) EditChange(
	ctx context.Context,
	id forge.ChangeID,
	opts forge.EditChangeOptions,
) error {
	if cmputil.Zero(opts.Base) &&
		cmputil.Zero(opts.Draft) &&
		len(opts.AddLabels) == 0 &&
		len(opts.AddReviewers) == 0 &&
		len(opts.AddAssignees) == 0 {
		return nil
	}

	prID := mustPR(id)
	input := &forgejo.EditPullRequestOption{Draft: opts.Draft}
	if opts.Base != "" {
		input.Base = &opts.Base
	}
	if len(opts.AddLabels) > 0 || len(opts.AddAssignees) > 0 {
		pr, _, err := r.client.PullRequestGet(
			ctx, r.owner, r.repo, prID.Number,
		)
		if err != nil {
			return fmt.Errorf("get pull request for update: %w", err)
		}

		if len(opts.AddLabels) > 0 {
			labelIDs, err := r.labelIDs(ctx, opts.AddLabels)
			if err != nil {
				return fmt.Errorf("resolve labels: %w", err)
			}
			labels := mergeLabelIDs(pr.Labels, labelIDs)
			input.Labels = &labels
		}

		if len(opts.AddAssignees) > 0 {
			assignees := mergeUserLogins(pr.Assignees, opts.AddAssignees)
			input.Assignees = &assignees
		}
	}

	if _, _, err := r.client.PullRequestEdit(
		ctx, r.owner, r.repo, prID.Number, input,
	); err != nil {
		return fmt.Errorf("update pull request: %w", err)
	}

	if len(opts.AddReviewers) > 0 {
		_, _, err := r.client.PullReviewRequestCreate(
			ctx,
			r.owner,
			r.repo,
			prID.Number,
			&forgejo.PullReviewRequestOptions{Reviewers: opts.AddReviewers},
		)
		if err != nil {
			return fmt.Errorf("request reviewers: %w", err)
		}
	}

	return nil
}

func mergeLabelIDs(
	existing []*forgejo.Label,
	additional []int64,
) []int64 {
	ids := make([]int64, 0, len(existing)+len(additional))
	for _, label := range existing {
		ids = append(ids, label.ID)
	}
	ids = append(ids, additional...)
	slices.Sort(ids)
	return slices.Compact(ids)
}

func mergeUserLogins(
	existing []*forgejo.User,
	additional []string,
) []string {
	logins := append(userLogins(existing), additional...)
	slices.Sort(logins)
	return slices.Compact(logins)
}
