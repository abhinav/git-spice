// Package shamhub implements a fake GitHub-like Forge.
//
// It stores Git repositories in a temporary directory,
// and provides a REST-like API for interacting with them.
package shamhub

import (
	"fmt"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"go.abhg.dev/gs/internal/silog"
)

// ShamHub is a fake GitHub-like Forge.
// The [ShamHub] type provides control of the forge,
// with direct access to Git repositories and change proposals.
//
// It provides two HTTP endpoints:
// one for the API server implementing the Forge API,
// and one for the Git server implementing the Git HTTP protocol.
// Note that the HTTP API provided by ShamHub is not the same as the GitHub API.
type ShamHub struct {
	log *silog.Logger

	gitRoot string // destination for Git repos
	gitExe  string // path to git binary

	apiServer *httptest.Server // API server
	gitServer *httptest.Server // Git HTTP remote

	mu       sync.RWMutex
	changes  []shamChange  // all changes
	users    []shamUser    // all users
	comments []shamComment // all comments

	tokens map[string]string // token -> username
}

// Config configures a ShamHub server.
type Config struct {
	// Git is the path to the git binary.
	// If not set, we'll look for it in the PATH.
	Git string

	Log *silog.Logger
}

// New creates a new ShamHub server and returns an ShamHub to control it.
// The server should be closed with the Close method when done.
func New(cfg Config) (*ShamHub, error) {
	if cfg.Log == nil {
		cfg.Log = silog.Nop()
	}

	if cfg.Git == "" {
		gitExe, err := exec.LookPath("git")
		if err != nil {
			return nil, fmt.Errorf("find git binary: %w", err)
		}

		cfg.Git = gitExe
	}

	gitRoot, err := os.MkdirTemp("", "shamhub-git")
	if err != nil {
		return nil, err
	}

	sh := ShamHub{
		log:     cfg.Log.With("module", "shamhub"),
		gitRoot: gitRoot,
		gitExe:  cfg.Git,
		tokens:  make(map[string]string),
	}
	sh.apiServer = httptest.NewServer(sh.apiHandler())
	sh.gitServer = httptest.NewServer(&cgi.Handler{
		// git-http-backend is a CGI script
		// that can be used to serve Git repositories over HTTP.
		Path: cfg.Git,
		Args: []string{"http-backend"},
		Env: []string{
			"GIT_HTTP_EXPORT_ALL=1",
			"GIT_PROJECT_ROOT=" + sh.gitRoot,
		},
	})

	return &sh, nil
}

// Close closes the ShamHub server.
func (sh *ShamHub) Close() error {
	sh.apiServer.Close()
	sh.gitServer.Close()

	if err := os.RemoveAll(sh.gitRoot); err != nil {
		return fmt.Errorf("remove git root: %w", err)
	}

	return nil
}

// GitRoot returns the path to the root directory of the Git repositories.
func (sh *ShamHub) GitRoot() string {
	return sh.gitRoot
}

// APIURL returns the URL to which API requests should be sent.
// Configure the shamhub.Forge to use this as the API URL.
func (sh *ShamHub) APIURL() string {
	return sh.apiServer.URL
}

// GitURL returns the URL for the Git HTTP server.
// Append <owner>/<repo>.git to these to get the Git remote.
// Configure the shamhub.Forge to use this as the Base URL.
func (sh *ShamHub) GitURL() string {
	return sh.gitServer.URL
}

// RepoURL returns the URL for the Git repository with the given owner and repo name.
func (sh *ShamHub) RepoURL(owner, repo string) string {
	repo = strings.TrimSuffix(repo, ".git")
	return sh.gitServer.URL + "/" + owner + "/" + repo + ".git"
}

func (sh *ShamHub) repoDir(owner, repo string) string {
	repo = strings.TrimSuffix(repo, ".git")
	return filepath.Join(sh.gitRoot, owner, repo+".git")
}

func (sh *ShamHub) changeURL(owner, repo string, change int) string {
	return fmt.Sprintf("%s/%s/%s/change/%d", sh.GitURL(), owner, repo, change)
}
