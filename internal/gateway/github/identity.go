package github

import (
	"context"
	"fmt"
)

// UserID looks up a user's GraphQL node ID by login.
func (c *Gateway) UserID(ctx context.Context, login string) (ID, error) {
	var result struct {
		User struct {
			ID ID `json:"id"`
		} `json:"user"`
	}
	query := compactGraphQL(`
		query($login:String!){
			user(login: $login){id}
		}
	`)
	if err := c.execute(ctx, query, struct {
		Login string `json:"login"`
	}{login}, &result); err != nil {
		return "", fmt.Errorf("query user: %w", err)
	}
	return result.User.ID, nil
}

// TeamID looks up a team's GraphQL node ID by organization and slug.
func (c *Gateway) TeamID(ctx context.Context, org, slug string) (ID, error) {
	var result struct {
		Organization struct {
			Team struct {
				ID ID `json:"id"`
			} `json:"team"`
		} `json:"organization"`
	}
	vars := struct {
		Org  string `json:"org"`
		Slug string `json:"slug"`
	}{org, slug}
	query := compactGraphQL(`
		query($org:String!$slug:String!){
			organization(login: $org){
				team(slug: $slug){id}
			}
		}
	`)
	if err := c.execute(ctx, query, vars, &result); err != nil {
		return "", fmt.Errorf("query team: %w", err)
	}
	return result.Organization.Team.ID, nil
}
