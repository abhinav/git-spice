// Package ghtest provides tools to test code that interacts with GitHub.
package ghtest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/google/go-github/v61/github"
	"go.abhg.dev/git-spice/internal/ioutil"
)

// ShamHub is a fake GitHub server.
//
// It can be used to test code that interacts with GitHub
// without actually making requests to the GitHub API.
// It provides a Git HTTP remote, and a GitHub API server.
//
// For the Git HTTP remote functionality, it relies on the
// 'git http-backend' command included with Git.
type ShamHub struct {
	log *log.Logger

	gitRoot string // destination for Git repos
	gitExe  string // path to git binary

	apiServer *httptest.Server // GitHub API server
	gitServer *httptest.Server // Git HTTP remote

	mu    sync.RWMutex
	pulls []shamPR // all pull requests
}

// ShamHubConfig configures a ShamHub server.
type ShamHubConfig struct {
	// Git is the path to the git binary.
	// If not set, we'll look for it in the PATH.
	Git string

	Log *log.Logger
}

// NewShamHub creates a new ShamHub server.
// The server should be closed with the Close method when done.
func NewShamHub(cfg ShamHubConfig) (*ShamHub, error) {
	if cfg.Log == nil {
		cfg.Log = log.New(io.Discard)
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

// Close closes the ShamHub.
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
// Configure the GitHub client to use this URL.
func (sh *ShamHub) APIURL() string {
	return sh.apiServer.URL
}

// GitURL returns the URL for the Git HTTP server.
// Append <owner>/<repo>.git to these to get the Git remote.
func (sh *ShamHub) GitURL() string {
	return sh.gitServer.URL
}

// RepoURL returns the URL for the Git repository with the given owner and repo name.
func (sh *ShamHub) RepoURL(owner, repo string) string {
	repo = strings.TrimSuffix(repo, ".git")
	return sh.gitServer.URL + "/" + owner + "/" + repo + ".git"
}

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

	logw, flush := ioutil.LogWriter(sh.log, log.DebugLevel)
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

// ListPulls returns a list of all pull requests
// that have been created on the ShamHub and their current state.
func (sh *ShamHub) ListPulls() ([]*github.PullRequest, error) {
	sh.mu.RLock()
	defer sh.mu.RUnlock()

	var ghPRs []*github.PullRequest
	for _, pr := range sh.pulls {
		ghpr, err := sh.toGitHubPR(&pr)
		if err != nil {
			return nil, err
		}
		ghPRs = append(ghPRs, ghpr)
	}

	return ghPRs, nil
}

// MergePullRequest is a request to merge a PR.
type MergePullRequest struct {
	Owner, Repo string
	Number      int

	// Optional fields:
	Time                          time.Time
	CommitterName, CommitterEmail string
	// TODO: Use git.Signature here instead?

	// TODO: option to use squash commit
}

// MergePull merges the pull request with the given owner, repo, and number.
func (sh *ShamHub) MergePull(req MergePullRequest) error {
	if req.Owner == "" || req.Repo == "" || req.Number == 0 {
		return fmt.Errorf("owner, repo, and number are required")
	}

	if req.CommitterName == "" {
		req.CommitterName = "ShamHub"
	}
	if req.CommitterEmail == "" {
		req.CommitterEmail = "shamhub@example.com"
	}

	commitEnv := []string{
		"GIT_COMMITTER_NAME=" + req.CommitterName,
		"GIT_COMMITTER_EMAIL=" + req.CommitterEmail,
		"GIT_AUTHOR_NAME=" + req.CommitterName,
		"GIT_AUTHOR_EMAIL=" + req.CommitterEmail,
	}
	if !req.Time.IsZero() {
		commitEnv = append(commitEnv,
			"GIT_COMMITTER_DATE="+req.Time.Format(time.RFC3339),
			"GIT_AUTHOR_DATE="+req.Time.Format(time.RFC3339),
		)
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()

	var prIdx int
	for idx, pr := range sh.pulls {
		if pr.Owner == req.Owner && pr.Repo == req.Repo && pr.Number == req.Number {
			prIdx = idx
			break
		}
	}

	if sh.pulls[prIdx].State != shamPROpen {
		return fmt.Errorf("pull request %d is not open", req.Number)
	}

	// To do this without a worktree, we need to:
	//
	//	TREE=$(git merge-tree --write-tree base head)
	//
	// If the above fails, there's a conflict, so reject the merge.
	// Otherwise, create a commit with the TREE and the commit message
	// using git commit-tree, and update the ref to point to the new commit.
	tree, err := func() (string, error) {
		logw, flush := ioutil.LogWriter(sh.log, log.DebugLevel)
		defer flush()

		cmd := exec.Command(sh.gitExe, "merge-tree", "--write-tree", sh.pulls[prIdx].Base, sh.pulls[prIdx].Head)
		cmd.Dir = sh.repoDir(req.Owner, req.Repo)
		cmd.Stderr = logw
		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("merge-tree: %w", err)
		}

		return strings.TrimSpace(string(out)), nil
	}()
	if err != nil {
		return err
	}

	commit, err := func() (string, error) {
		logw, flush := ioutil.LogWriter(sh.log, log.DebugLevel)
		defer flush()

		msg := fmt.Sprintf("Merge pull request #%d", req.Number)
		cmd := exec.Command(sh.gitExe,
			"commit-tree",
			"-p", sh.pulls[prIdx].Base,
			"-p", sh.pulls[prIdx].Head,
			"-m", msg,
			tree,
		)
		cmd.Dir = sh.repoDir(req.Owner, req.Repo)
		cmd.Stderr = logw
		cmd.Env = append(os.Environ(), commitEnv...)
		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("commit-tree: %w", err)
		}

		return strings.TrimSpace(string(out)), nil
	}()
	if err != nil {
		return err
	}

	// Update the ref to point to the new commit.
	err = func() error {
		logw, flush := ioutil.LogWriter(sh.log, log.DebugLevel)
		defer flush()

		ref := fmt.Sprintf("refs/heads/%s", sh.pulls[prIdx].Base)
		cmd := exec.Command(sh.gitExe, "update-ref", ref, commit)
		cmd.Dir = sh.repoDir(req.Owner, req.Repo)
		cmd.Stderr = logw
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("update-ref: %w", err)
		}

		return nil
	}()
	if err != nil {
		return err
	}

	sh.pulls[prIdx].State = shamPRMerged
	return nil
}

func (sh *ShamHub) apiHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/{owner}/{repo}/pulls", sh.listPullRequests)
	mux.HandleFunc("POST /repos/{owner}/{repo}/pulls", sh.createPullRequest)
	mux.HandleFunc("PATCH /repos/{owner}/{repo}/pulls/{number}", sh.updatePullRequest)
	mux.HandleFunc("GET /repos/{owner}/{repo}/pulls/{number}/merge", sh.prIsMerged)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiPath, ok := strings.CutPrefix(r.URL.Path, "/api/v3")
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		r.URL.Path = apiPath

		sh.log.Infof("ShamHub: %s %s", r.Method, r.URL)
		mux.ServeHTTP(w, r)
	})
}

func (sh *ShamHub) prIsMerged(w http.ResponseWriter, r *http.Request) {
	owner, repo, numStr := r.PathValue("owner"), r.PathValue("repo"), r.PathValue("number")
	if owner == "" || repo == "" || numStr == "" {
		http.Error(w, "owner, repo, and number are required", http.StatusBadRequest)
		return
	}

	num, err := strconv.Atoi(numStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sh.mu.RLock()
	var merged bool
	for _, pr := range sh.pulls {
		if pr.Owner == owner && pr.Repo == repo && pr.Number == num {
			merged = pr.State == shamPRMerged
			break
		}
	}
	sh.mu.RUnlock()

	// If the PR has been merged, response is 204.
	// Otherwise, response is 404.
	if merged {
		w.WriteHeader(http.StatusNoContent)
	} else {
		http.Error(w, "pull request not merged or not found", http.StatusNotFound)
	}
}

func (sh *ShamHub) listPullRequests(w http.ResponseWriter, r *http.Request) {
	type prMatcher func(*shamPR) bool

	owner, repo := r.PathValue("owner"), r.PathValue("repo")
	if owner == "" || repo == "" {
		http.Error(w, "owner and repo are required", http.StatusBadRequest)
		return
	}

	matchers := []prMatcher{
		func(pr *shamPR) bool { return pr.Owner == owner },
		func(pr *shamPR) bool { return pr.Repo == repo },
	}
	if head := r.FormValue("head"); head != "" {
		// head is in the form "owner:branch".
		head = strings.TrimPrefix(head, owner+":")
		matchers = append(matchers,
			func(pr *shamPR) bool { return pr.Head == head })
	}

	switch state := r.FormValue("state"); state {
	case "open":
		matchers = append(matchers, func(pr *shamPR) bool {
			return pr.State == shamPROpen
		})
	case "closed":
		matchers = append(matchers, func(pr *shamPR) bool {
			return pr.State != shamPROpen
		})
	}

	got := make([]shamPR, 0, len(sh.pulls))
	sh.mu.RLock()
outer:
	for _, pr := range sh.pulls {
		for _, match := range matchers {
			if !match(&pr) {
				continue outer
			}
		}

		got = append(got, pr)
	}
	sh.mu.RUnlock()

	var prs []*github.PullRequest
	for _, pr := range got {
		ghpr, err := sh.toGitHubPR(&pr)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		prs = append(prs, ghpr)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(prs); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (sh *ShamHub) createPullRequest(w http.ResponseWriter, r *http.Request) {
	owner, repo := r.PathValue("owner"), r.PathValue("repo")
	if owner == "" || repo == "" {
		http.Error(w, "owner and repo are required", http.StatusBadRequest)
		return
	}

	var data struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Base  string `json:"base"`
		Head  string `json:"head"`
		Draft bool   `json:"draft"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&data); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sh.mu.Lock()
	shamPR := shamPR{
		// We'll just use a global counter for the PR number for now.
		// We can scope it by owner/repo if needed.
		Number: len(sh.pulls) + 1,
		Owner:  owner,
		Repo:   repo,
		Draft:  data.Draft,
		Title:  data.Title,
		Body:   data.Body,
		Base:   data.Base,
		Head:   data.Head,
	}
	sh.pulls = append(sh.pulls, shamPR)
	sh.mu.Unlock()

	ghpr, err := sh.toGitHubPR(&shamPR)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(ghpr); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (sh *ShamHub) updatePullRequest(w http.ResponseWriter, r *http.Request) {
	owner, repo, numStr := r.PathValue("owner"), r.PathValue("repo"), r.PathValue("number")
	if owner == "" || repo == "" || numStr == "" {
		http.Error(w, "owner, repo, and number are required", http.StatusBadRequest)
		return
	}

	num, err := strconv.Atoi(numStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var data struct {
		Base  *string `json:"base"`
		Draft *bool   `json:"draft"`
		State *string `json:"state"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()

	prIdx := -1
	for idx, pr := range sh.pulls {
		if pr.Owner == owner && pr.Repo == repo && pr.Number == num {
			prIdx = idx
			break
		}
	}
	if prIdx == -1 {
		http.Error(w, "pull request not found", http.StatusNotFound)
		return
	}

	if b := data.Base; b != nil {
		sh.pulls[prIdx].Base = *b
	}
	if d := data.Draft; d != nil {
		sh.pulls[prIdx].Draft = *d
	}
	if s := data.State; s != nil {
		switch *s {
		case "open":
			sh.pulls[prIdx].State = shamPROpen
		case "closed":
			sh.pulls[prIdx].State = shamPRClosed
		}
	}

	ghpr, err := sh.toGitHubPR(&sh.pulls[prIdx])
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(ghpr); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (sh *ShamHub) repoDir(owner, repo string) string {
	repo = strings.TrimSuffix(repo, ".git")
	return filepath.Join(sh.gitRoot, owner, repo+".git")
}

type shamPRState int

const (
	shamPROpen shamPRState = iota
	shamPRClosed
	shamPRMerged
)

func (s shamPRState) toGitHubState() string {
	switch s {
	case shamPROpen:
		return "open"
	case shamPRClosed, shamPRMerged:
		return "closed"
	default:
		return ""
	}
}

type shamPR struct {
	Owner string
	Repo  string

	Number int
	Draft  bool
	State  shamPRState

	Title string
	Body  string

	Base string
	Head string
}

func (sh *ShamHub) toGitHubPR(pr *shamPR) (*github.PullRequest, error) {
	url := fmt.Sprintf("%s/%s/%s/pull/%d", sh.GitURL(), pr.Owner, pr.Repo, pr.Number)

	base, err := sh.toGitHubPRBranch(pr.Owner, pr.Repo, pr.Base)
	if err != nil {
		return nil, fmt.Errorf("convert base branch: %w", err)
	}

	head, err := sh.toGitHubPRBranch(pr.Owner, pr.Repo, pr.Head)
	if err != nil {
		return nil, fmt.Errorf("convert head branch: %w", err)
	}

	ghpr := &github.PullRequest{
		Number:  &pr.Number,
		State:   github.String(pr.State.toGitHubState()),
		Title:   &pr.Title,
		Body:    &pr.Body,
		Base:    base,
		Head:    head,
		HTMLURL: &url,
	}
	// Don't set optional fields if they're empty
	// to reduce noise in JSON output.
	if pr.Draft {
		ghpr.Draft = github.Bool(true)
	}
	if pr.State == shamPRMerged {
		ghpr.Merged = github.Bool(true)
	}
	return ghpr, nil
}

func (sh *ShamHub) toGitHubPRBranch(owner, repo, ref string) (*github.PullRequestBranch, error) {
	logw, flush := ioutil.LogWriter(sh.log, log.DebugLevel)
	defer flush()

	headCmd := exec.Command(sh.gitExe, "rev-parse", ref)
	headCmd.Dir = sh.repoDir(owner, repo)
	headCmd.Stderr = logw
	head, err := headCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("get SHA for %v/%v:%v: %w", owner, repo, ref, err)
	}

	return &github.PullRequestBranch{
		Ref: &ref,
		SHA: github.String(strings.TrimSpace(string(head))),
	}, nil
}
