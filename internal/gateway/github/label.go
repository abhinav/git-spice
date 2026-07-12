package github

import (
	"context"
	"fmt"
)

// LabelID looks up a repository label's GraphQL node ID by name.
func (c *Gateway) LabelID(ctx context.Context, owner, repo, label string) (ID, error) {
	var result struct {
		Repository struct {
			Label struct {
				ID ID `json:"id"`
			} `json:"label"`
		} `json:"repository"`
	}
	vars := struct {
		Label string `json:"label"`
		Name  string `json:"name"`
		Owner string `json:"owner"`
	}{label, repo, owner}
	query := compactGraphQL(`
		query($label:String!$name:String!$owner:String!){
			repository(owner: $owner, name: $name){
				label(name: $label){id}
			}
		}
	`)
	if err := c.execute(ctx, query, vars, &result); err != nil {
		return "", fmt.Errorf("query label: %w", err)
	}
	return result.Repository.Label.ID, nil
}

// AddLabelsToLabelable adds labels to a labelable node.
func (c *Gateway) AddLabelsToLabelable(ctx context.Context, labelableID ID, labelIDs []ID) error {
	mutation := compactGraphQL(`
		mutation($input:AddLabelsToLabelableInput!){
			addLabelsToLabelable(input: $input){clientMutationId}
		}
	`)
	return c.mutate(ctx, mutation, struct {
		LabelableID ID   `json:"labelableId"`
		LabelIDs    []ID `json:"labelIds"`
	}{labelableID, labelIDs}, &struct{}{})
}

// CreateLabel creates a repository label and returns its node ID.
func (c *Gateway) CreateLabel(ctx context.Context, repositoryID ID, color, name string) (ID, error) {
	var result struct {
		CreateLabel struct {
			Label struct {
				ID ID `json:"id"`
			} `json:"label"`
		} `json:"createLabel"`
	}
	mutation := compactGraphQL(`
		mutation($input:CreateLabelInput!){
			createLabel(input: $input){label{id}}
		}
	`)
	if err := c.mutate(ctx, mutation, struct {
		RepositoryID ID     `json:"repositoryId"`
		Color        string `json:"color"`
		Name         string `json:"name"`
	}{repositoryID, color, name}, &result); err != nil {
		return "", err
	}
	return result.CreateLabel.Label.ID, nil
}

// DeleteLabel deletes a label node.
func (c *Gateway) DeleteLabel(ctx context.Context, id ID) error {
	mutation := compactGraphQL(`
		mutation($input:DeleteLabelInput!){
			deleteLabel(input: $input){clientMutationId}
		}
	`)
	return c.mutate(ctx, mutation, struct {
		ID ID `json:"id"`
	}{id}, &struct{}{})
}
