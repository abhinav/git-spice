package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
)

func (r *Repository) addLabelsToPullRequest(ctx context.Context, labels []string, prGraphQLID githubv4.ID) error {
	if len(labels) == 0 {
		return nil
	}
	labelIDs, err := r.getOrCreateLabelIDs(ctx, labels)
	if err != nil {
		return fmt.Errorf("get label IDs: %w", err)
	}

	var addLabelsM struct {
		AddLabelsToLabelable struct {
			Clientmutationid githubv4.String `graphql:"clientMutationId"`
		} `graphql:"addLabelsToLabelable(input: $input)"`
	}

	labelsInput := githubv4.AddLabelsToLabelableInput{
		LabelableID: prGraphQLID,
		LabelIDs:    labelIDs,
	}

	if err := r.client.Mutate(ctx, &addLabelsM, labelsInput, nil); err != nil {
		return fmt.Errorf("add labels to labelable: %w", err)
	}
	return nil
}

func (r *Repository) getOrCreateLabelIDs(ctx context.Context, labelNames []string) ([]githubv4.ID, error) {
	if len(labelNames) == 0 {
		return nil, nil
	}

	var query struct {
		Repository struct {
			Labels struct {
				Nodes []struct {
					ID   githubv4.ID     `graphql:"id"`
					Name githubv4.String `graphql:"name"`
				} `graphql:"nodes"`
			} `graphql:"labels(first: 100)"` // I think 100 is a reasonable limit for labels
		} `graphql:"repository(owner: $owner, name: $name)"`
	}

	variables := map[string]interface{}{
		"owner": githubv4.String(r.owner),
		"name":  githubv4.String(r.repo),
	}

	if err := r.client.Query(ctx, &query, variables); err != nil {
		return nil, fmt.Errorf("query labels: %w", err)
	}

	labelMap := make(map[string]githubv4.ID)
	for _, label := range query.Repository.Labels.Nodes {
		labelMap[string(label.Name)] = label.ID
	}

	var labelIDs []githubv4.ID
	for _, name := range labelNames {
		id, exists := labelMap[name]
		if !exists {
			var err error
			id, err = r.createLabel(ctx, name)
			if err != nil {
				return nil, fmt.Errorf("create label %q: %w", name, err)
			}
		}
		labelIDs = append(labelIDs, id)
	}

	return labelIDs, nil
}

func (r *Repository) createLabel(ctx context.Context, name string) (githubv4.ID, error) {
	var m struct {
		CreateLabel struct {
			Label struct {
				ID githubv4.ID `graphql:"id"`
			} `graphql:"label"`
		} `graphql:"createLabel(input: $input)"`
	}

	color := "EDEDED"
	input := githubv4.CreateLabelInput{
		RepositoryID: r.repoID,
		Name:         githubv4.String(name),
		Color:        githubv4.String(color),
	}

	if err := r.client.Mutate(ctx, &m, input, nil); err != nil {
		return "", fmt.Errorf("create label mutation: %w", err)
	}

	return m.CreateLabel.Label.ID, nil
}
