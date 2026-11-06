package azuredevops

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/azuredevops"
	"go.abhg.dev/gs/internal/silog"
)

//go:generate go tool mockgen -destination=mocks_test.go -package=azuredevops -write_package_comment=false -typed -mock_names=azureDevOpsGateway=MockAzureDevOpsGateway . azureDevOpsGateway

type azureDevOpsGateway interface {
	AddComment(context.Context, string, string, int, string) (int, int, error)
	AddLabel(context.Context, string, string, int, string) error
	AddReviewer(context.Context, string, string, int, string) error
	AddReviewerByName(context.Context, string, string, int, string) error
	CommentExists(context.Context, string, string, int, int, int) (bool, error)
	CurrentUserID(context.Context) (string, error)
	DeleteComment(context.Context, string, string, int, int, int) error
	FindPullRequests(context.Context, *azuredevops.FindPullRequestsInput) ([]*azuredevops.PullRequest, error)
	Item(context.Context, string, string, string) (*azuredevops.Item, error)
	Items(context.Context, string, string, string) ([]azuredevops.Item, error)
	Labels(context.Context, string, string, int) ([]string, error)
	PullRequest(context.Context, string, string, int) (*azuredevops.PullRequest, error)
	RefExists(context.Context, string, string, string) (bool, error)
	Repository(context.Context, string, string) (*azuredevops.Repository, error)
	ReviewerID(context.Context, string) (string, error)
	Reviewers(context.Context, string, string, int) ([]string, error)
	Threads(context.Context, string, string, int) ([]azuredevops.Thread, error)
	UpdateComment(context.Context, string, string, int, int, int, string) error
	UpdatePullRequest(context.Context, *azuredevops.UpdatePullRequestInput) error
	CreatePullRequest(context.Context, *azuredevops.CreatePullRequestInput) (*azuredevops.PullRequest, error)
}

var _ azureDevOpsGateway = (*azuredevops.Gateway)(nil)

// Repository is an Azure DevOps repository.
type Repository struct {
	forge   *Forge
	repoID  *RepositoryID
	log     *silog.Logger
	gateway azureDevOpsGateway

	// Cached repository info from API.
	repoInfo *azuredevops.Repository

	// reviewerIDs maps reviewer names to Azure DevOps identity IDs.
	reviewerIDs map[string]string
}

var _ forge.Repository = (*Repository)(nil)

func newRepository(
	ctx context.Context,
	f *Forge,
	repoID *RepositoryID,
	log *silog.Logger,
	gateway azureDevOpsGateway,
) (*Repository, error) {
	log = log.With(
		"org", repoID.organization,
		"project", repoID.project,
		"repo", repoID.repository,
	)

	// Get the repository info to ensure it exists and get the UUID.
	repoInfo, err := gateway.Repository(ctx, repoID.project, repoID.repository)
	if err != nil {
		return nil, fmt.Errorf("get repository: %w", err)
	}

	return &Repository{
		forge:    f,
		repoID:   repoID,
		log:      log,
		gateway:  gateway,
		repoInfo: repoInfo,
	}, nil
}

// Forge returns the forge this repository belongs to.
func (r *Repository) Forge() forge.Forge { return r.forge }

// repositoryID returns the repository ID as a string for API calls.
func (r *Repository) repositoryID() string {
	if r.repoInfo != nil && r.repoInfo.ID != "" {
		return r.repoInfo.ID
	}
	return r.repoID.repository
}

// project returns the project name for API calls.
func (r *Repository) project() string {
	return r.repoID.project
}

func (r *Repository) getRepository(
	ctx context.Context,
	id forge.RepositoryID,
) (*azuredevops.Repository, error) {
	rid := mustRepositoryID(id)
	if rid.organization != r.repoID.organization {
		return nil, fmt.Errorf(
			"repository %q belongs to organization %q, not %q",
			rid.repository, rid.organization, r.repoID.organization,
		)
	}

	repo, err := r.gateway.Repository(ctx, rid.project, rid.repository)
	if err != nil {
		return nil, fmt.Errorf("get repository: %w", err)
	}
	return repo, nil
}
