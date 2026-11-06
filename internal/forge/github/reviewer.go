package github

import (
	"strings"

	"go.abhg.dev/gs/internal/gateway/github"
)

func reviewerNames(reviewers []string) (users []string, teams []github.TeamName) {
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
	return users, teams
}
