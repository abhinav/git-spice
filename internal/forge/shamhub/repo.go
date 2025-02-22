package shamhub

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/logutil"
	"go.abhg.dev/gs/internal/must"
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

	logw, flush := logutil.Writer(sh.log, log.DebugLevel)
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

// OpenURL opens a repository hosted on the forge with the given remote URL.
func (f *Forge) OpenURL(_ context.Context, token forge.AuthenticationToken, remoteURL string) (forge.Repository, error) {
	must.NotBeBlankf(f.URL, "URL is required")
	must.NotBeBlankf(f.APIURL, "API URL is required")

	tok := token.(*AuthenticationToken).tok
	client := f.jsonHTTPClient()
	client.headers = map[string]string{
		"Authentication-Token": tok,
	}

	tail, ok := strings.CutPrefix(remoteURL, f.URL)
	if !ok {
		return nil, forge.ErrUnsupportedURL
	}

	tail = strings.TrimSuffix(strings.TrimPrefix(tail, "/"), ".git")
	owner, repo, ok := strings.Cut(tail, "/")
	if !ok {
		return nil, fmt.Errorf("%w: no '/' found in %q", forge.ErrUnsupportedURL, tail)
	}

	apiURL, err := url.Parse(f.APIURL)
	if err != nil {
		return nil, fmt.Errorf("parse API URL: %w", err)
	}

	return &forgeRepository{
		forge:  f,
		owner:  owner,
		repo:   repo,
		apiURL: apiURL,
		log:    f.Log,
		client: client,
	}, nil
}

// forgeRepository is a repository hosted on a ShamHub server.
// It implements [forge.Repository].
type forgeRepository struct {
	forge  *Forge
	owner  string
	repo   string
	apiURL *url.URL
	log    *log.Logger
	client *jsonHTTPClient
}

var _ forge.Repository = (*forgeRepository)(nil)

func (f *forgeRepository) Forge() forge.Forge { return f.forge }
