package github

import (
	"context"
	"fmt"
)

// ChangeTemplate is one pull request template returned by GitHub.
type ChangeTemplate struct {
	// Filename is the repository-relative template filename reported by GitHub.
	Filename string `json:"filename"`

	// Body is the template text returned by GitHub.
	Body string `json:"body"`
}

// ChangeTemplates lists the pull request templates for a repository.
func (c *Gateway) ChangeTemplates(ctx context.Context, owner, repo string) ([]*ChangeTemplate, error) {
	var result struct {
		Repository struct {
			PullRequestTemplates []*ChangeTemplate `json:"pullRequestTemplates"`
		} `json:"repository"`
	}
	vars := struct {
		Name  string `json:"name"`
		Owner string `json:"owner"`
	}{repo, owner}
	query := compactGraphQL(`
		query($name:String!$owner:String!){
			repository(owner: $owner, name: $name){
				pullRequestTemplates{filename,body}
			}
		}
	`)
	if err := c.execute(ctx, query, vars, &result); err != nil {
		return nil, fmt.Errorf("query templates: %w", err)
	}
	return result.Repository.PullRequestTemplates, nil
}
