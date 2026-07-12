package github

import (
	"context"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/gateway/github"
)

// addReviewersToPullRequest adds reviewers to a pull request.
func (r *Repository) addReviewersToPullRequest(
	ctx context.Context,
	reviewers []string,
	prGraphQLID github.ID,
) error {
	if len(reviewers) == 0 {
		return nil
	}

	userIDs, teamIDs, err := r.reviewersIDs(ctx, reviewers)
	if err != nil {
		return fmt.Errorf("resolve reviewer IDs: %w", err)
	}

	input := github.RequestReviewsInput{
		PullRequestID: prGraphQLID,
		Union:         new(true),
	}
	if len(userIDs) > 0 {
		input.UserIDs = &userIDs
	}
	if len(teamIDs) > 0 {
		input.TeamIDs = &teamIDs
	}

	if err := r.gateway.RequestReviews(ctx, &input); err != nil {
		return fmt.Errorf("request reviews: %w", err)
	}

	return nil
}

// reviewersIDs resolves reviewer names to GraphQL IDs.
// Returns separate slices for user IDs and team IDs.
func (r *Repository) reviewersIDs(
	ctx context.Context,
	reviewers []string,
) (userIDs []github.ID, teamIDs []github.ID, err error) {
	var users []string
	var teams []github.TeamName
	for _, reviewer := range reviewers {
		reviewer = strings.TrimSpace(reviewer)
		if reviewer == "" {
			continue
		}

		// Team reviewer in the form "org/team",
		// where "org" must match the repository owner.
		if org, teamSlug, ok := strings.Cut(reviewer, "/"); ok {
			teams = append(teams, github.TeamName{
				Organization: org,
				Slug:         teamSlug,
			})
		} else {
			users = append(users, reviewer)
		}
	}

	userIDs, teamIDs, err = r.identityIDs(ctx, users, teams)
	if err != nil {
		return nil, nil, err
	}
	for i, user := range users {
		r.log.Debug("Resolved user reviewer ID", "username", user, "id", userIDs[i])
	}
	for i, team := range teams {
		r.log.Debug(
			"Resolved team reviewer ID",
			"team", team.Organization+"/"+team.Slug,
			"id", teamIDs[i],
		)
	}
	return userIDs, teamIDs, nil
}
