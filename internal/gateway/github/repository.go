package github

import (
	"context"
	"fmt"
)

// RepositoryID looks up GitHub's GraphQL node ID for a repository.
func (c *Gateway) RepositoryID(ctx context.Context, owner, repo string) (ID, error) {
	query := compactGraphQL(`
		query($owner:String!$repo:String!){
			repository(owner: $owner, name: $repo){
				id
			}
		}
	`)
	var result struct {
		Repository struct {
			ID ID `json:"id"`
		} `json:"repository"`
	}
	if err := c.execute(ctx, query, struct {
		Owner string `json:"owner"`
		Repo  string `json:"repo"`
	}{Owner: owner, Repo: repo}, &result); err != nil {
		return "", fmt.Errorf("query repository: %w", err)
	}
	return result.Repository.ID, nil
}
