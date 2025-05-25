package gitlab

import (
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

	repoID int

	// Information about the current user:
	userID   int
	userRole gitlab.AccessLevelValue
}

var _ forge.Repository = (*Repository)(nil)

func newRepository(
	ctx context.Context,
	forge *Forge,
	owner, repo string,
	log *silog.Logger,
	client *gitlabClient,
	repoID *int, // if nil, repository ID will be looked up
) (*Repository, error) {
	var projectIdentifier string
	if repoID != nil {
		projectIdentifier = strconv.Itoa(*repoID)
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
