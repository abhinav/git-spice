package github

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/must"
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

		rType, name := parseReviewer(reviewer)
		switch rType {
		case reviewerTypeUser:
			id, err := r.userID(ctx, name)
			if err != nil {
				errs = append(errs, fmt.Errorf("lookup user %q: %w", name, err))
				continue
			}
			userIDs = append(userIDs, id)
			r.log.Debug("Resolved user reviewer ID", "username", name, "id", id)

		case reviewerTypeTeam:
			org, teamSlug, _ := strings.Cut(name, "/")
			id, err := r.teamID(ctx, org, teamSlug)
			if err != nil {
				errs = append(errs, fmt.Errorf("lookup team %v/%q: %w", org, name, err))
				continue
			}
			teamIDs = append(teamIDs, id)
			r.log.Debug("Resolved team reviewer ID", "team", name, "id", id)

		default:
			must.Failf("unknown reviewer type %#v for %q", rType, reviewer)
		}
	}

	return userIDs, teamIDs, errors.Join(errs...)
}

type reviewerType int

const (
	reviewerTypeUser reviewerType = iota
	reviewerTypeTeam
)

// parseReviewer determines if a reviewer is a user or team.
// Format: "username" for users, "org/teamname" for teams.
func parseReviewer(reviewer string) (reviewerType, string) {
	if strings.Contains(reviewer, "/") {
		return reviewerTypeTeam, reviewer
	}
	return reviewerTypeUser, reviewer
}

// userID looks up a user's GraphQL ID by login.
func (r *Repository) userID(ctx context.Context, login string) (githubv4.ID, error) {
	var query struct {
		User struct {
			ID githubv4.ID `graphql:"id"`
		} `graphql:"user(login: $login)"`
	}

	variables := map[string]any{
		"login": githubv4.String(login),
	}

	if err := r.client.Query(ctx, &query, variables); err != nil {
		return "", fmt.Errorf("query user: %w", err)
	}

	id := query.User.ID
	if id == "" || id == nil {
		return "", fmt.Errorf("user not found: %q", login)
	}

	return id, nil
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
