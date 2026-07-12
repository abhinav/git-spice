package github

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// TeamName identifies a GitHub team within an organization.
type TeamName struct {
	// Organization is the login of the organization that owns the team.
	Organization string

	// Slug is the team's URL slug within the organization.
	Slug string
}

// IdentityIDs looks up GitHub users and teams in one GraphQL operation.
// The returned slices match the length and order of users and teams.
// If GitHub successfully executes the query,
// an identity that does not exist has an empty ID in its result position,
// other identities remain populated,
// and IdentityIDs returns a nil error.
// A transport or GraphQL error returns nil result slices and a non-nil error.
func (c *Gateway) IdentityIDs(
	ctx context.Context,
	users []string,
	teams []TeamName,
) ([]ID, []ID, error) {
	userIDs := make([]ID, len(users))
	teamIDs := make([]ID, len(teams))
	if len(users) == 0 && len(teams) == 0 {
		return userIDs, teamIDs, nil
	}

	variables := make(map[string]any, len(users)+2*len(teams))

	// Build one root selection per identity:
	//
	// query($user0:String!$teamOrg0:String!$teamSlug0:String!) {
	//   user0: user(login: $user0) { id }
	//   team0: organization(login: $teamOrg0) {
	//     team(slug: $teamSlug0) { id }
	//   }
	// }
	var variableDefinitions strings.Builder
	var selections strings.Builder
	for i, user := range users {
		alias := "user" + strconv.Itoa(i)
		fmt.Fprintf(&variableDefinitions, "$%s:String!", alias)
		if selections.Len() > 0 {
			selections.WriteByte(',')
		}
		fmt.Fprintf(&selections, "%s:user(login: $%s){id}", alias, alias)
		variables[alias] = user
	}
	for i, team := range teams {
		alias := "team" + strconv.Itoa(i)
		organizationVariable := "teamOrg" + strconv.Itoa(i)
		slugVariable := "teamSlug" + strconv.Itoa(i)
		fmt.Fprintf(
			&variableDefinitions,
			"$%s:String!$%s:String!",
			organizationVariable,
			slugVariable,
		)
		if selections.Len() > 0 {
			selections.WriteByte(',')
		}
		fmt.Fprintf(
			&selections,
			"%s:organization(login: $%s){team(slug: $%s){id}}",
			alias,
			organizationVariable,
			slugVariable,
		)
		variables[organizationVariable] = team.Organization
		variables[slugVariable] = team.Slug
	}

	query := compactGraphQL(
		"query(" + variableDefinitions.String() + "){" +
			selections.String() + "}",
	)
	var result map[string]struct {
		ID   ID `json:"id"`
		Team *struct {
			ID ID `json:"id"`
		} `json:"team"`
	}
	if err := c.execute(ctx, query, variables, &result); err != nil {
		return nil, nil, fmt.Errorf("query identities: %w", err)
	}

	for i := range users {
		userIDs[i] = result["user"+strconv.Itoa(i)].ID
	}
	for i := range teams {
		team := result["team"+strconv.Itoa(i)].Team
		if team != nil {
			teamIDs[i] = team.ID
		}
	}
	return userIDs, teamIDs, nil
}
