package gitlab

import (
	"fmt"

	"github.com/charmbracelet/log"
	"github.com/xanzy/go-gitlab"
	"go.abhg.dev/gs/internal/forge"
)

// Repository is a GitLab repository.
type Repository struct {
	owner, repo string
	log         *log.Logger
	forge       *Forge

	repoID int

	// Information about the current user:
	userID   int
	userRole gitlab.AccessLevelValue

	// API access:
	client *gitlab.Client
	notes  notesService
}

var _ forge.Repository = (*Repository)(nil)

func newRepository(
	forge *Forge,
	owner, repo string,
	log *log.Logger,
	client *gitlab.Client,
	repoID *int,
) (*Repository, error) {
	var projectIdentifier string
	if repoID != nil {
		projectIdentifier = fmt.Sprintf("%d", *repoID)
	} else {
		projectIdentifier = owner + "/" + repo
	}

	project, _, err := client.Projects.GetProject(projectIdentifier, nil)
	if err != nil {
		return nil, fmt.Errorf("get repository ID: %w", err)
	}

	user, _, err := client.Users.CurrentUser()
	if err != nil {
		return nil, fmt.Errorf("get current user: %w", err)
	}

	var accessLevel gitlab.AccessLevelValue
	if project.Permissions.ProjectAccess != nil {
		accessLevel = project.Permissions.ProjectAccess.AccessLevel
	} else {
		accessLevel = project.Permissions.GroupAccess.AccessLevel
	}

	return &Repository{
		owner:    owner,
		repo:     repo,
		forge:    forge,
		log:      log,
		client:   client,
		userID:   user.ID,
		userRole: accessLevel,
		repoID:   project.ID,
		notes:    client.Notes,
	}, nil
}

// Forge returns the forge this repository belongs to.
func (r *Repository) Forge() forge.Forge { return r.forge }
