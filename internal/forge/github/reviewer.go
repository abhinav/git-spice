package github

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/shurcooL/githubv4"
)

// addReviewersToPullRequest adds reviewers to a pull request.
func (r *Repository) addReviewersToPullRequest(
	ctx context.Context,
	reviewers []string,
	prGraphQLID githubv4.ID,
) error {
	if len(reviewers) == 0 {
		return nil
	}

	userIDs, teamIDs, err := r.reviewersIDs(ctx, reviewers)
	if err != nil {
		return fmt.Errorf("resolve reviewer IDs: %w", err)
	}

	var m struct {
		RequestReviews struct {
			ClientMutationID githubv4.String `graphql:"clientMutationId"`
		} `graphql:"requestReviews(input: $input)"`
	}

	input := githubv4.RequestReviewsInput{
		PullRequestID: prGraphQLID,
		Union:         githubv4.NewBoolean(true),
	}
	if len(userIDs) > 0 {
		input.UserIDs = &userIDs
	}
	if len(teamIDs) > 0 {
		input.TeamIDs = &teamIDs
	}

	if err := r.client.Mutate(ctx, &m, input, nil); err != nil {
		return fmt.Errorf("request reviews: %w", err)
	}

	return nil
}

// reviewersIDs resolves reviewer names to GraphQL IDs.
// Returns separate slices for user IDs and team IDs.
func (r *Repository) reviewersIDs(
	ctx context.Context,
	reviewers []string,
) (userIDs []githubv4.ID, teamIDs []githubv4.ID, err error) {
	var errs []error

	// TODO: parallelize lookups or combine into one GQL query.
	for _, reviewer := range reviewers {
		reviewer = strings.TrimSpace(reviewer)
		if reviewer == "" {
			continue
		}

		// Team reviewer in the form "org/team",
		// where "org" must match the repository owner.
		if org, teamSlug, ok := strings.Cut(reviewer, "/"); ok {
			id, err := r.teamID(ctx, org, teamSlug)
			if err != nil {
				errs = append(errs, fmt.Errorf("lookup team %q: %w", reviewer, err))
				continue
			}
			teamIDs = append(teamIDs, id)
			r.log.Debug("Resolved team reviewer ID", "team", reviewer, "id", id)
		} else {
			id, err := r.userID(ctx, reviewer)
			if err != nil {
				errs = append(errs, fmt.Errorf("lookup user %q: %w", reviewer, err))
				continue
			}
			userIDs = append(userIDs, id)
			r.log.Debug("Resolved user reviewer ID", "username", reviewer, "id", id)
		}
	}

	return userIDs, teamIDs, errors.Join(errs...)
}

// teamID looks up a team's GraphQL ID by organization and team slug.
func (r *Repository) teamID(ctx context.Context, org, teamSlug string) (githubv4.ID, error) {
	var query struct {
		Organization struct {
			Team struct {
				ID githubv4.ID `graphql:"id"`
			} `graphql:"team(slug: $slug)"`
		} `graphql:"organization(login: $org)"`
	}

	variables := map[string]any{
		"org":  githubv4.String(org),
		"slug": githubv4.String(teamSlug),
	}

	if err := r.client.Query(ctx, &query, variables); err != nil {
		return "", fmt.Errorf("query team: %w", err)
	}

	id := query.Organization.Team.ID
	if id == "" || id == nil {
		return "", fmt.Errorf("team not found: %q/%q", org, teamSlug)
	}

	return id, nil
}
