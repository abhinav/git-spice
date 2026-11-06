package azuredevops

import (
	"context"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/core"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
)

// AddLabel adds label to a pull request.
func (g *Gateway) AddLabel(
	ctx context.Context,
	project string,
	repository string,
	pullRequest int,
	label string,
) error {
	_, err := g.gitClient.CreatePullRequestLabel(
		ctx,
		git.CreatePullRequestLabelArgs{
			Project:       &project,
			RepositoryId:  &repository,
			PullRequestId: &pullRequest,
			Label: &core.WebApiCreateTagRequestData{
				Name: &label,
			},
		},
	)
	return normalizeError(err)
}

// Labels returns the label names attached to a pull request.
func (g *Gateway) Labels(
	ctx context.Context,
	project string,
	repository string,
	pullRequest int,
) ([]string, error) {
	labels, err := g.gitClient.GetPullRequestLabels(
		ctx,
		git.GetPullRequestLabelsArgs{
			Project:       &project,
			RepositoryId:  &repository,
			PullRequestId: &pullRequest,
		},
	)
	if err != nil {
		return nil, normalizeError(err)
	}
	return labelsFromSDK(labels), nil
}
