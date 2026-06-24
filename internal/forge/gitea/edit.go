package gitea

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"go.abhg.dev/gs/internal/cmputil"
	"go.abhg.dev/gs/internal/forge"
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
)

// EditChange edits an existing pull request.
func (r *Repository) EditChange(ctx context.Context, id forge.ChangeID, opts forge.EditChangeOptions) error {
	if cmputil.Zero(opts.Base) &&
		cmputil.Zero(opts.Draft) &&
		len(opts.AddLabels) == 0 &&
		len(opts.AddReviewers) == 0 &&
		len(opts.AddAssignees) == 0 {
		return nil // nothing to do
	}

	prID := mustPR(id)

	var (
		updateOptions giteagw.EditPullRequestOption
		logUpdates    []slog.Attr

		// Lazy-fetched current PR state.
		currentPR *giteagw.PullRequest
	)

	getPR := func() (*giteagw.PullRequest, error) {
		if currentPR != nil {
			return currentPR, nil
		}
		var err error
		currentPR, _, err = r.client.PullGet(ctx, r.owner, r.repo, prID.Number)
		if err != nil {
			return nil, fmt.Errorf("get pull request for update: %w", err)
		}
		return currentPR, nil
	}

	if opts.Base != "" {
		updateOptions.Base = &opts.Base
		logUpdates = append(logUpdates, slog.String("base", opts.Base))
	}

	if opts.Draft != nil {
		// Gitea uses the "WIP:" title prefix to mark draft PRs, not an API field.
		// We need to fetch the current PR title to add/remove the prefix.
		pr, err := getPR()
		if err != nil {
			return err
		}
		if *opts.Draft {
			if !pr.Draft {
				updateOptions.Title = new(fmt.Sprintf("%s %s", _draftPrefix, pr.Title))
			}
		} else {
			if pr.Draft {
				title := _draftRegex.ReplaceAllString(pr.Title, "")
				updateOptions.Title = &title
			}
		}
		logUpdates = append(logUpdates, slog.Bool("draft", *opts.Draft))
	}

	if len(opts.AddLabels) > 0 {
		newLabelIDs, err := r.ensureLabels(ctx, opts.AddLabels)
		if err != nil {
			return fmt.Errorf("ensure labels: %w", err)
		}

		existing, err := r.currentLabelIDs(ctx, prID.Number)
		if err != nil {
			return err
		}
		updateOptions.Labels = mergeLabelIDs(existing, newLabelIDs)
		logUpdates = append(logUpdates, slog.Any("labels", updateOptions.Labels))
	}

	// Reviewers are handled separately via the dedicated endpoint below;
	// don't include them in the PATCH request (Gitea ignores them in PATCH).
	var addReviewers []string
	if len(opts.AddReviewers) > 0 {
		addReviewers = opts.AddReviewers
		logUpdates = append(logUpdates, slog.Any("reviewers", addReviewers))
	}

	if len(opts.AddAssignees) > 0 {
		pr, err := getPR()
		if err != nil {
			return err
		}
		existing := usernamesFrom(pr.Assignees)
		updateOptions.Assignees = mergeUsernames(existing, opts.AddAssignees)
		logUpdates = append(logUpdates, slog.Any("assignees", updateOptions.Assignees))
	}

	_, _, err := r.client.PullEdit(ctx, r.owner, r.repo, prID.Number, &updateOptions)
	if err != nil {
		return fmt.Errorf("update pull request: %w", err)
	}

	// Add reviewers via the dedicated endpoint.
	// Gitea's PATCH /pulls/{index} ignores the reviewers field.
	if len(addReviewers) > 0 {
		if _, err := r.client.ReviewRequestCreate(ctx, r.owner, r.repo, prID.Number, addReviewers); err != nil {
			return fmt.Errorf("add reviewers: %w", err)
		}
	}

	if len(logUpdates) > 0 {
		r.log.Debug("Updated pull request",
			"pr", prID.Number,
			"new", slog.GroupValue(logUpdates...),
		)
	}

	return nil
}

func mergeLabelIDs(existing, add []int64) []int64 {
	seen := make(map[int64]struct{}, len(existing)+len(add))
	for _, id := range existing {
		seen[id] = struct{}{}
	}
	for _, id := range add {
		seen[id] = struct{}{}
	}
	return slices.Sorted(maps.Keys(seen))
}

func usernamesFrom(users []*giteagw.User) []string {
	if len(users) == 0 {
		return nil
	}
	names := make([]string, len(users))
	for i, u := range users {
		names[i] = u.Login
	}
	return names
}

func mergeUsernames(existing, add []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(add))
	for _, u := range existing {
		seen[u] = struct{}{}
	}
	for _, u := range add {
		seen[u] = struct{}{}
	}
	return slices.Sorted(maps.Keys(seen))
}
