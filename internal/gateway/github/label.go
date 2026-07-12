package github

import (
	"context"
	"fmt"
	"strconv"
	"strings"
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

// LabelIDs looks up repository labels by name.
//
// Results have the same length and order as labels. Repeated names share one
// lookup. A label that does not exist has an empty ID in its result position.
// See https://docs.github.com/en/graphql/reference/repos#repository.
func (c *Gateway) LabelIDs(ctx context.Context, owner, repo string, labels []string) ([]ID, error) {
	if len(labels) == 0 {
		return nil, nil
	}
	// Each alias requests at most one shallow Label node. GitHub's documented
	// hard limit is 500,000 nodes per call, far above the labels supplied by one
	// forge operation, so splitting would add requests without protecting a
	// documented nearer boundary.
	// See https://docs.github.com/en/graphql/overview/rate-limits-and-query-limits-for-the-graphql-api#node-limit.

	// Each distinct name gets a stable indexed alias. Keep the index for every
	// input position so duplicates can share the selection without losing the
	// caller's result shape.
	uniqueLabels := make([]string, 0, len(labels))
	labelIndexByName := make(map[string]int, len(labels))
	labelIndexes := make([]int, len(labels))
	for i, label := range labels {
		labelIndex, ok := labelIndexByName[label]
		if !ok {
			labelIndex = len(uniqueLabels)
			labelIndexByName[label] = labelIndex
			uniqueLabels = append(uniqueLabels, label)
		}
		labelIndexes[i] = labelIndex
	}

	variables := make(map[string]any, len(uniqueLabels)+2)
	variables["name"] = repo
	variables["owner"] = owner

	// Build one aliased selection per distinct label name:
	//
	// query($label0:String!$label1:String!$name:String!$owner:String!) {
	//   repository(owner: $owner, name: $name) {
	//     label0: label(name: $label0) { id }
	//     label1: label(name: $label1) { id }
	//   }
	// }
	var variableDefinitions strings.Builder
	var selections strings.Builder
	for i, label := range uniqueLabels {
		alias := "label" + strconv.Itoa(i)
		fmt.Fprintf(&variableDefinitions, "$%s:String!", alias)
		if i > 0 {
			selections.WriteByte(',')
		}
		fmt.Fprintf(&selections, "%s:label(name: $%s){id}", alias, alias)
		variables[alias] = label
	}

	query := compactGraphQL(
		"query(" + variableDefinitions.String() + "$name:String!$owner:String!){" +
			"repository(owner: $owner, name: $name){" + selections.String() + "}}",
	)
	var result struct {
		Repository map[string]*struct {
			ID ID `json:"id"`
		} `json:"repository"`
	}
	if err := c.execute(ctx, query, variables, &result); err != nil {
		return nil, fmt.Errorf("query labels: %w", err)
	}

	ids := make([]ID, len(labels))
	for i, labelIndex := range labelIndexes {
		label := result.Repository["label"+strconv.Itoa(labelIndex)]
		if label != nil {
			ids[i] = label.ID
		}
	}
	return ids, nil
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
