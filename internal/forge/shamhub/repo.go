package shamhub

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"slices"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/silog"
)

// shamRepo is the internal representation of a repository.
type shamRepo struct {
	Owner string
	Name  string

	// If this is a fork, ForkOf points to the parent repository.
	ForkOf *repoID
}

// Repository represents a repository on ShamHub.
type Repository struct {
	Owner    string      `json:"owner"`
	Name     string      `json:"name"`
	FullName string      `json:"full_name"`
	Fork     bool        `json:"fork"`
	Parent   *Repository `json:"parent,omitempty"`
}

// NewRepository creates a new Git repository
// with the given owner and repo name
// and returns the URL to the repository.
func (sh *ShamHub) NewRepository(owner, repo string) (string, error) {
	return sh.newRepository(owner, repo, nil /* forkOf */)
}

// ForkRepository forks an existing repository
// under a different owner.
func (sh *ShamHub) ForkRepository(owner, repo, forkOwner string) (string, error) {
	return sh.newRepository(forkOwner, repo, &repoID{Owner: owner, Name: repo})
}

type repoID struct{ Owner, Name string }

// newRepository creates a new Git repository, optionally as a fork.
func (sh *ShamHub) newRepository(owner, repo string, forkOf *repoID) (string, error) {
	// Only one newRepository at a time.
	sh.mu.Lock()
	defer sh.mu.Unlock()

	// Check if repository already exists
	alreadyExists := slices.ContainsFunc(sh.repos, func(r shamRepo) bool {
		return r.Owner == owner && r.Name == repo
	})
	if alreadyExists {
		return "", fmt.Errorf("repository %s/%s already exists", owner, repo)
	}

	// If this is a fork, verify parent exists
	if forkOf != nil {
		ok := slices.ContainsFunc(sh.repos, func(r shamRepo) bool {
			return r.Owner == forkOf.Owner && r.Name == forkOf.Name
		})
		if !ok {
			return "", fmt.Errorf("parent repository %s not found", forkOf)
		}
	}

	repoDir := sh.repoDir(owner, repo)
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		return "", fmt.Errorf("create repository: %w", err)
	}

	logw, flush := silog.Writer(sh.log, silog.LevelDebug)
	defer flush()

	var cmd *exec.Cmd
	if forkOf != nil {
		// For forks, clone from the parent
		parentDir := sh.repoDir(forkOf.Owner, forkOf.Name)
		cmd = exec.Command(sh.gitExe, "clone", "--bare", parentDir, repoDir)
	} else {
		// For new repositories, initialize from scratch
		cmd = exec.Command(sh.gitExe, "init", "--bare", "--initial-branch=main", repoDir)
	}
	cmd.Stdout = logw
	cmd.Stderr = logw
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("create repository: %w", err)
	}

	// Configure the repository to accept pushes.
	cfgCmd := exec.Command(sh.gitExe, "config", "http.receivepack", "true")
	cfgCmd.Dir = repoDir
	cfgCmd.Stdout = logw
	cfgCmd.Stderr = logw
	if err := cfgCmd.Run(); err != nil {
		return "", fmt.Errorf("configure repository: %w", err)
	}

	// Add to our repository list
	sh.repos = append(sh.repos, shamRepo{
		Owner:  owner,
		Name:   repo,
		ForkOf: forkOf,
	})

	return sh.gitServer.URL + "/" + owner + "/" + repo + ".git", nil
}

// forgeRepository is a repository hosted on a ShamHub server.
// It implements [forge.Repository].
type forgeRepository struct {
	forge  *Forge
	owner  string
	repo   string
	apiURL *url.URL
	log    *silog.Logger
	client *jsonHTTPClient
}

var _ forge.Repository = (*forgeRepository)(nil)

func (r *forgeRepository) Forge() forge.Forge { return r.forge }
