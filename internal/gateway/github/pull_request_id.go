package github

import (
	"context"
	"fmt"
)

// PullRequestID looks up a pull request's GraphQL node ID by number.
func (c *Gateway) PullRequestID(ctx context.Context, owner, repo string, number int) (ID, error) {
	var result struct {
		Repository struct {
			PullRequest struct {
				ID ID `json:"id"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}
	query := compactGraphQL(`
		query($number:Int!$owner:String!$repo:String!){
			repository(owner: $owner, name: $repo){
				pullRequest(number: $number){id}
			}
		}
	`)
	variables := struct {
		Number int    `json:"number"`
		Owner  string `json:"owner"`
		Repo   string `json:"repo"`
	}{number, owner, repo}
	if err := c.execute(ctx, query, variables, &result); err != nil {
		return "", fmt.Errorf("query pull request ID: %w", err)
	}
	return result.Repository.PullRequest.ID, nil
}
