package shamhub

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/silog"
)

// NewRepository creates a new Git repository
// with the given owner and repo name
// and returns the URL to the repository.
func (sh *ShamHub) NewRepository(owner, repo string) (string, error) {
	// Only one NewRepository at a time.
	sh.mu.Lock()
	defer sh.mu.Unlock()

	repoDir := sh.repoDir(owner, repo)
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		return "", fmt.Errorf("create repository: %w", err)
	}

	logw, flush := silog.Writer(sh.log, silog.LevelDebug)
	defer flush()

	initCmd := exec.Command(sh.gitExe, "init", "--bare", "--initial-branch=main")
	initCmd.Dir = repoDir
	initCmd.Stdout = logw
	initCmd.Stderr = logw
	if err := initCmd.Run(); err != nil {
		return "", fmt.Errorf("initialize repository: %w", err)
	}

	// Configure the repository to accept pushes.
	cfgCmd := exec.Command(sh.gitExe, "config", "http.receivepack", "true")
	cfgCmd.Dir = repoDir
	cfgCmd.Stdout = logw
	cfgCmd.Stderr = logw
	if err := cfgCmd.Run(); err != nil {
		return "", fmt.Errorf("configure repository: %w", err)
	}

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
