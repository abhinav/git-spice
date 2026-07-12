package github

import (
	"context"
	"fmt"
)

// RefExists reports whether a fully qualified repository ref exists.
func (c *Gateway) RefExists(ctx context.Context, owner, repo, ref string) (bool, error) {
	var result struct {
		Repository struct {
			Ref *struct {
				Name string `json:"name"`
			} `json:"ref"`
		} `json:"repository"`
	}
	vars := struct {
		Owner string `json:"owner"`
		Ref   string `json:"ref"`
		Repo  string `json:"repo"`
	}{owner, ref, repo}
	query := compactGraphQL(`
		query($owner:String!$ref:String!$repo:String!){
			repository(owner: $owner, name: $repo){
				ref(qualifiedName: $ref){name}
			}
		}
	`)
	if err := c.execute(ctx, query, vars, &result); err != nil {
		return false, fmt.Errorf("query ref: %w", err)
	}
	return result.Repository.Ref != nil, nil
}
