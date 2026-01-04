package github

import (
	"context"
	"fmt"
	"strings"
)

// addReviewersToPullRequest adds reviewers to a pull request.
func (r *Repository) addReviewersToPullRequest(
	ctx context.Context,
	reviewers []string,
	prNumber int,
) error {
	if len(reviewers) == 0 {
		return nil
	}

	var ghReviewers ReviewersRequest
	for _, reviewer := range reviewers {
		reviewer = strings.TrimPrefix(reviewer, "@") // optional '@'

		// Team reviewer in the form "org/team",
		// where "org" must match the repository owner.
		if org, team, ok := strings.Cut(reviewer, "/"); ok {
			if org != r.owner {
				return fmt.Errorf("team reviewer organization (%q) does not match repository owner %q", org, r.owner)
			}

			ghReviewers.TeamReviewers = append(ghReviewers.TeamReviewers, team)
		} else {
			// User reviewer.
			ghReviewers.Reviewers = append(ghReviewers.Reviewers, reviewer)
		}
	}

	if err := r.gh3.PullRequestRequestReviewers(ctx, r.owner, r.repo, prNumber, &ghReviewers); err != nil {
		return fmt.Errorf("request reviews: %w", err)
	}

	return nil
}
