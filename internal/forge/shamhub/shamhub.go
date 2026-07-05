// Package shamhub implements a fake GitHub-like Forge.
//
// It stores Git repositories in a temporary directory,
// and provides a REST-like API for interacting with them.
package shamhub

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/xec"
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

	apiServer   *httptest.Server // API server
	gitServer   *httptest.Server // Git HTTP remote
	keepGitRoot bool
	adminToken  string

	mu       sync.RWMutex
	changes  []shamChange  // all changes
	users    []shamUser    // all users
	comments []shamComment // all comments
	repos    []shamRepo    // all repositories

	tokens             map[string]string // token -> username
	defaultMergeMethod MergeMethod       // used when API merge requests omit a method
	// changeTemplateErrorDelay makes the change-template endpoint return a
	// delayed error, allowing terminal tests to exercise background failures.
	changeTemplateErrorDelay time.Duration
}

// Config configures a ShamHub server.
type Config struct {
	// Git is the path to the git binary.
	// If not set, we'll look for it in the PATH.
	Git string

	// APIAddr is the address for the API server to listen on.
	// If unset, the server listens on a loopback address with any port.
	APIAddr string

	// GitAddr is the address for the Git HTTP server to listen on.
	// If unset, the server listens on a loopback address with any port.
	GitAddr string

	// GitRoot is the directory in which bare Git repositories are stored.
	// If unset, New creates a temporary directory.
	GitRoot string

	// KeepGitRoot keeps GitRoot on disk after Close.
	KeepGitRoot bool

	// AdminToken authorizes ShamHub administration endpoints.
	// If unset, New generates a random token.
	AdminToken string

	Log *silog.Logger
}

// New creates a new ShamHub server and returns a ShamHub to control it.
// The server should be closed with the Close method when done.
func New(cfg Config) (*ShamHub, error) {
	if cfg.Log == nil {
		cfg.Log = silog.Nop()
	}

	if cfg.Git == "" {
		gitExe, err := xec.LookPath("git")
		if err != nil {
			return nil, fmt.Errorf("find git binary: %w", err)
		}

		cfg.Git = gitExe
	}

	gitRoot := cfg.GitRoot
	if gitRoot == "" {
		var err error
		gitRoot, err = os.MkdirTemp("", "shamhub-git")
		if err != nil {
			return nil, fmt.Errorf("create git root: %w", err)
		}
	} else if err := os.MkdirAll(gitRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create git root: %w", err)
	}

	adminToken := cfg.AdminToken
	if adminToken == "" {
		adminToken = rand.Text()
	}

	sh := ShamHub{
		log:                cfg.Log.With("module", "shamhub"),
		gitRoot:            gitRoot,
		gitExe:             cfg.Git,
		keepGitRoot:        cfg.KeepGitRoot,
		adminToken:         adminToken,
		tokens:             make(map[string]string),
		defaultMergeMethod: MergeMethodMerge,
	}
	var err error
	sh.apiServer, err = newServer(cfg.APIAddr, sh.apiHandler())
	if err != nil {
		return nil, fmt.Errorf("start API server: %w", err)
	}
	sh.gitServer, err = newServer(cfg.GitAddr, &cgi.Handler{
		// git-http-backend is a CGI script
		// that can be used to serve Git repositories over HTTP.
		Path: cfg.Git,
		Args: []string{"http-backend"},
		Env: []string{
			"GIT_HTTP_EXPORT_ALL=1",
			"GIT_PROJECT_ROOT=" + sh.gitRoot,
		},
	})
	if err != nil {
		sh.apiServer.Close()
		if !sh.keepGitRoot {
			_ = os.RemoveAll(sh.gitRoot)
		}
		return nil, fmt.Errorf("start Git server: %w", err)
	}

	return &sh, nil
}

// Close closes the ShamHub server.
func (sh *ShamHub) Close() error {
	sh.apiServer.Close()
	sh.gitServer.Close()

	if sh.keepGitRoot {
		return nil
	}
	if err := os.RemoveAll(sh.gitRoot); err != nil {
		return fmt.Errorf("remove git root: %w", err)
	}

	return nil
}

// GitRoot returns the path to the root directory of the Git repositories.
func (sh *ShamHub) GitRoot() string {
	return sh.gitRoot
}

// AdminToken returns the token required for administration endpoints.
func (sh *ShamHub) AdminToken() string {
	return sh.adminToken
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

// gitCmd builds a git command scoped to the bare repository for owner/repo.
// Use this instead of WithDir(repoDir(...)): git 2.50 no longer discovers a
// bare repo from the current working directory alone, so an explicit
// --git-dir is required for `git config`, `git show-ref`, `git rev-parse`,
// and similar commands.
func (sh *ShamHub) gitCmd(ctx context.Context, owner, repo string, args ...string) *xec.Cmd {
	return xec.Command(ctx, sh.log, sh.gitExe,
		append([]string{"--git-dir", sh.repoDir(owner, repo)}, args...)...)
}

func (sh *ShamHub) changeURL(owner, repo string, change int) string {
	return fmt.Sprintf("%s/%s/%s/change/%d", sh.GitURL(), owner, repo, change)
}

func newServer(addr string, handler http.Handler) (*httptest.Server, error) {
	if addr == "" {
		return httptest.NewServer(handler), nil
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen on %q: %w", addr, err)
	}

	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	return server, nil
}
