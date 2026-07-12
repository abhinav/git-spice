package github

import (
	"context"
	"fmt"
)

// FindPullRequests finds recent pull requests with the requested head branch.
func (c *Gateway) FindPullRequests(ctx context.Context, owner, repo, branch string, limit int, states []PullRequestState) ([]*PullRequest, error) {
	var result struct {
		Repository struct {
			PullRequests struct {
				Nodes []*PullRequest `json:"nodes"`
			} `json:"pullRequests"`
		} `json:"repository"`
	}
	variables := struct {
		Branch string             `json:"branch"`
		Limit  int                `json:"limit"`
		Owner  string             `json:"owner"`
		Repo   string             `json:"repo"`
		States []PullRequestState `json:"states"`
	}{branch, limit, owner, repo, states}
	query := compactGraphQL(`
		query($branch:String!$limit:Int!$owner:String!$repo:String!$states:[PullRequestState!]!){
			repository(owner: $owner, name: $repo){
				pullRequests(first: $limit, headRefName: $branch, states: $states, orderBy: {field: UPDATED_AT, direction: DESC}){
					nodes{` + pullRequestFields + `}
				}
			}
		}
	`)
	if err := c.execute(ctx, query, variables, &result); err != nil {
		return nil, fmt.Errorf("query pull requests: %w", err)
	}
	return result.Repository.PullRequests.Nodes, nil
}

// PullRequest loads a pull request by repository and number.
func (c *Gateway) PullRequest(ctx context.Context, owner, repo string, number int) (*PullRequest, error) {
	var result struct {
		Repository struct {
			PullRequest *PullRequest `json:"pullRequest"`
		} `json:"repository"`
	}
	variables := struct {
		Number int    `json:"number"`
		Owner  string `json:"owner"`
		Repo   string `json:"repo"`
	}{number, owner, repo}
	query := compactGraphQL(`
		query($number:Int!$owner:String!$repo:String!){
			repository(owner: $owner, name: $repo){
				pullRequest(number: $number){` + pullRequestFields + `}
			}
		}
	`)
	if err := c.execute(ctx, query, variables, &result); err != nil {
		return nil, fmt.Errorf("query pull request: %w", err)
	}
	return result.Repository.PullRequest, nil
}
