package gitlab

import (
	"cmp"
	"context"
	"fmt"
	"strconv"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/silog"
)

// Repository is a GitLab repository.
type Repository struct {
	client *gitlabClient

	owner, repo string
	log         *silog.Logger
	forge       *Forge

	repoID int64

	// Information about the current user:
	userID   int64
	userRole gitlab.AccessLevelValue

	removeSourceBranchOnMerge bool
}

var _ forge.Repository = (*Repository)(nil)

type repositoryOptions struct {
	RepositoryID *int64 // if nil, repository ID will be looked up

	RemoveSourceBranchOnMerge bool
}

func newRepository(
	ctx context.Context,
	forge *Forge,
	owner, repo string,
	log *silog.Logger,
	client *gitlabClient,
	opts *repositoryOptions,
) (*Repository, error) {
	opts = cmp.Or(opts, &repositoryOptions{})
	repoID := opts.RepositoryID

	var projectIdentifier string
	if repoID != nil {
		projectIdentifier = strconv.FormatInt(*repoID, 10)
	} else {
		projectIdentifier = owner + "/" + repo
	}

	project, _, err := client.Projects.GetProject(projectIdentifier, nil,
		gitlab.WithContext(ctx),
	)
	if err != nil {
		return nil, fmt.Errorf("get repository ID: %w", err)
	}

	user, _, err := client.Users.CurrentUser(gitlab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("get current user: %w", err)
	}

	var accessLevel gitlab.AccessLevelValue
	if project.Permissions.ProjectAccess != nil {
		accessLevel = project.Permissions.ProjectAccess.AccessLevel
	} else if project.Permissions.GroupAccess != nil {
		accessLevel = project.Permissions.GroupAccess.AccessLevel
	}
	log.Debugf("Repository access level: %v", accessValueName(accessLevel))

	return &Repository{
		client:   client,
		owner:    owner,
		repo:     repo,
		forge:    forge,
		log:      log,
		userID:   user.ID,
		userRole: accessLevel,
		repoID:   project.ID,

		removeSourceBranchOnMerge: opts.RemoveSourceBranchOnMerge,
	}, nil
}

// Forge returns the forge this repository belongs to.
func (r *Repository) Forge() forge.Forge { return r.forge }

var _accessLevelNames = map[gitlab.AccessLevelValue]string{
	gitlab.NoPermissions:            "none",
	gitlab.MinimalAccessPermissions: "minimal",
	gitlab.GuestPermissions:         "guest",
	gitlab.PlannerPermissions:       "planner",
	gitlab.ReporterPermissions:      "reporter",
	gitlab.DeveloperPermissions:     "developer",
	gitlab.MaintainerPermissions:    "maintainer",
	gitlab.OwnerPermissions:         "owner",
	gitlab.AdminPermissions:         "admin",
}

func accessValueName(value gitlab.AccessLevelValue) string {
	if name, ok := _accessLevelNames[value]; ok {
		return name
	}
	return strconv.Itoa(int(value))
}

// resolveReviewerIDs converts usernames to GitLab user IDs.
func (r *Repository) resolveReviewerIDs(ctx context.Context, usernames []string) ([]int64, error) {
	if len(usernames) == 0 {
		return nil, nil
	}

	reviewerIDs := make([]int64, 0, len(usernames))
	for _, username := range usernames {
		users, _, err := r.client.Users.ListUsers(&gitlab.ListUsersOptions{
			Username: &username,
		}, gitlab.WithContext(ctx))
		if err != nil {
			return nil, fmt.Errorf("lookup user %q: %w", username, err)
		}

		if len(users) == 0 {
			return nil, fmt.Errorf("user not found: %q", username)
		}

		reviewerIDs = append(reviewerIDs, users[0].ID)
		r.log.Debug("Resolved reviewer ID", "username", username, "id", users[0].ID)
	}

	return reviewerIDs, nil
}
