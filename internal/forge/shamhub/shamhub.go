// Package shamhub implements a fake GitHub-like Forge.
//
// It stores Git repositories in a temporary directory,
// and provides a REST-like API for interacting with them.
package shamhub

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
	"go.abhg.dev/gs/internal/ioutil"
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
	log *log.Logger

	gitRoot string // destination for Git repos
	gitExe  string // path to git binary

	apiServer *httptest.Server // API server
	gitServer *httptest.Server // Git HTTP remote

	mu      sync.RWMutex
	changes []shamChange // all changes
}

// Config configures a ShamHub server.
type Config struct {
	// Git is the path to the git binary.
	// If not set, we'll look for it in the PATH.
	Git string

	Log *log.Logger
}

// New creates a new ShamHub server and returns an ShamHub to control it.
// The server should be closed with the Close method when done.
func New(cfg Config) (*ShamHub, error) {
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

// ListChanges reports all changes known to the forge.
func (sh *ShamHub) ListChanges() ([]*Change, error) {
	sh.mu.RLock()
	defer sh.mu.RUnlock()

	changes := make([]*Change, len(sh.changes))
	for i, c := range sh.changes {
		change, err := sh.toChange(c)
		if err != nil {
			return nil, err
		}

		changes[i] = change
	}

	return changes, nil
}

// MergeChangeRequest is a request to merge an open change
// proposed against this forge.
type MergeChangeRequest struct {
	Owner, Repo string
	Number      int

	// Optional fields:
	Time                          time.Time
	CommitterName, CommitterEmail string
	// TODO: Use git.Signature here instead?

	// TODO: option to use squash commit
}

// MergeChange merges an open change against this forge.
func (sh *ShamHub) MergeChange(req MergeChangeRequest) error {
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

	var changeIdx int
	for idx, change := range sh.changes {
		if change.Owner == req.Owner && change.Repo == req.Repo && change.Number == req.Number {
			changeIdx = idx
			break
		}
	}

	if sh.changes[changeIdx].State != shamChangeOpen {
		return fmt.Errorf("change %d is not open", req.Number)
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

		cmd := exec.Command(sh.gitExe, "merge-tree", "--write-tree", sh.changes[changeIdx].Base, sh.changes[changeIdx].Head)
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

		msg := fmt.Sprintf("Merge change #%d", req.Number)
		cmd := exec.Command(sh.gitExe,
			"commit-tree",
			"-p", sh.changes[changeIdx].Base,
			"-p", sh.changes[changeIdx].Head,
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

		ref := fmt.Sprintf("refs/heads/%s", sh.changes[changeIdx].Base)
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

	sh.changes[changeIdx].State = shamChangeMerged
	return nil
}

func (sh *ShamHub) apiHandler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /{owner}/{repo}/changes", sh.handleSubmitChange)
	mux.HandleFunc("GET /{owner}/{repo}/changes/by-branch/{branch}", sh.handleFindChangesByBranch)
	mux.HandleFunc("GET /{owner}/{repo}/change/{number}", sh.handleGetChange)
	mux.HandleFunc("PATCH /{owner}/{repo}/change/{number}", sh.handleEditChange)
	mux.HandleFunc("GET /{owner}/{repo}/change/{number}/merged", sh.handleIsMerged)
	mux.HandleFunc("GET /{owner}/{repo}/change-template", sh.handleChangeTemplate)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		sh.log.Errorf("Unexpected request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "not found", http.StatusNotFound)
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sh.log.Infof("ShamHub: %s %s", r.Method, r.URL.String())
		mux.ServeHTTP(w, r)
	})
}

type submitChangeRequest struct {
	Subject string `json:"subject,omitempty"`
	Body    string `json:"body,omitempty"`
	Base    string `json:"base,omitempty"`
	Head    string `json:"head,omitempty"`
	Draft   bool   `json:"draft,omitempty"`
}

type submitChangeResponse struct {
	Number int    `json:"number,omitempty"`
	URL    string `json:"url,omitempty"`
}

func (sh *ShamHub) handleSubmitChange(w http.ResponseWriter, r *http.Request) {
	owner, repo := r.PathValue("owner"), r.PathValue("repo")
	if owner == "" || repo == "" {
		http.Error(w, "owner and repo are required", http.StatusBadRequest)
		return
	}

	var data submitChangeRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&data); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sh.mu.Lock()
	change := shamChange{
		// We'll just use a global counter for the change number for now.
		// We can scope it by owner/repo if needed.
		Number:  len(sh.changes) + 1,
		Owner:   owner,
		Repo:    repo,
		Draft:   data.Draft,
		Subject: data.Subject,
		Body:    data.Body,
		Base:    data.Base,
		Head:    data.Head,
	}
	sh.changes = append(sh.changes, change)
	sh.mu.Unlock()

	res := submitChangeResponse{
		Number: change.Number,
		URL:    sh.changeURL(owner, repo, change.Number),
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type editChangeRequest struct {
	Base  *string `json:"base,omitempty"`
	Draft *bool   `json:"draft,omitempty"`
}

type editChangeResponse struct{}

func (sh *ShamHub) handleEditChange(w http.ResponseWriter, r *http.Request) {
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

	var data editChangeRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()

	changeIdx := -1
	for idx, change := range sh.changes {
		if change.Owner == owner && change.Repo == repo && change.Number == num {
			changeIdx = idx
			break
		}
	}
	if changeIdx == -1 {
		http.Error(w, "change not found", http.StatusNotFound)
		return
	}

	if b := data.Base; b != nil {
		sh.changes[changeIdx].Base = *b
	}
	if d := data.Draft; d != nil {
		sh.changes[changeIdx].Draft = *d
	}

	res := editChangeResponse{} // empty for now

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (sh *ShamHub) handleFindChangesByBranch(w http.ResponseWriter, r *http.Request) {
	owner, repo, branch := r.PathValue("owner"), r.PathValue("repo"), r.PathValue("branch")
	if owner == "" || repo == "" || branch == "" {
		http.Error(w, "owner, repo, and branch are required", http.StatusBadRequest)
		return
	}

	limit := 10
	if l := r.FormValue("limit"); l != "" {
		var err error
		limit, err = strconv.Atoi(l)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	filters := []func(shamChange) bool{
		func(c shamChange) bool { return c.Owner == owner },
		func(c shamChange) bool { return c.Repo == repo },
		func(c shamChange) bool { return c.Head == branch },
	}

	if state := r.FormValue("state"); state != "" && state != "all" {
		var s shamChangeState
		switch state {
		case "open":
			s = shamChangeOpen
		case "closed":
			s = shamChangeClosed
		case "merged":
			s = shamChangeMerged
		}

		filters = append(filters, func(c shamChange) bool { return c.State == s })
	}

	var got []shamChange
	sh.mu.RLock()
nextChange:
	for _, c := range sh.changes {
		if len(got) >= limit {
			break
		}

		for _, f := range filters {
			if !f(c) {
				continue nextChange
			}
		}

		got = append(got, c)
	}
	sh.mu.RUnlock()

	changes := make([]*Change, len(got))
	for i, c := range got {
		change, err := sh.toChange(c)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		changes[i] = change
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(changes); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (sh *ShamHub) handleGetChange(w http.ResponseWriter, r *http.Request) {
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
	var (
		got   shamChange
		found bool
	)
	for _, c := range sh.changes {
		if c.Owner == owner && c.Repo == repo && c.Number == num {
			got = c
			found = true
			break
		}
	}
	sh.mu.RUnlock()

	if !found {
		http.Error(w, "change not found", http.StatusNotFound)
		return
	}

	change, err := sh.toChange(got)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(change); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type isMergedResponse struct {
	Merged bool `json:"merged"`
}

func (sh *ShamHub) handleIsMerged(w http.ResponseWriter, r *http.Request) {
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
	var (
		merged bool
		found  bool
	)
	for _, c := range sh.changes {
		if c.Owner == owner && c.Repo == repo && c.Number == num {
			merged = c.State == shamChangeMerged
			found = true
			break
		}
	}
	sh.mu.RUnlock()

	if !found {
		http.Error(w, "change not found", http.StatusNotFound)
		return
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(isMergedResponse{Merged: merged}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type changeTemplateResponse []*changeTemplate

type changeTemplate struct {
	Filename string `json:"filename,omitempty"`
	Body     string `json:"body,omitempty"`
}

func (sh *ShamHub) handleChangeTemplate(w http.ResponseWriter, r *http.Request) {
	owner, repo := r.PathValue("owner"), r.PathValue("repo")
	if owner == "" || repo == "" {
		http.Error(w, "owner, and repo are required", http.StatusBadRequest)
		return
	}

	// If the repository has a .shamhub/CHANGE_TEMPLATE.md file,
	// that's the template to use.
	logw, flush := ioutil.LogWriter(sh.log, log.DebugLevel)
	defer flush()

	var res changeTemplateResponse
	for _, path := range _changeTemplatePaths {
		cmd := exec.Command(sh.gitExe, "cat-file", "-p", "HEAD:"+path)
		cmd.Dir = sh.repoDir(owner, repo)
		cmd.Stderr = logw

		if out, err := cmd.Output(); err == nil {
			res = append(res, &changeTemplate{
				Filename: path,
				Body:     strings.TrimSpace(string(out)) + "\n",
			})
		}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// shamChangeState records the state of a Change.
type shamChangeState int

const (
	// shamChangeOpen specifies that a change is open
	// and may be merged.
	shamChangeOpen shamChangeState = iota

	// shamChangeClosed indicates that a change has been closed
	// without being merged.
	shamChangeClosed

	// shamChangeMerged indicates that a change has been merged.
	shamChangeMerged
)

// shamChange is the internal representation of a [Change].
type shamChange struct {
	Owner string
	Repo  string

	Number int
	Draft  bool
	State  shamChangeState

	Subject string
	Body    string

	Base string
	Head string
}

// Change is a change proposal against a repository.
type Change struct {
	Number int    `json:"number"`
	URL    string `json:"html_url"`

	Draft  bool   `json:"draft,omitempty"`
	State  string `json:"state"`
	Merged bool   `json:"merged,omitempty"`

	Subject string `json:"title"`
	Body    string `json:"body"`

	Base *ChangeBranch `json:"base"`
	Head *ChangeBranch `json:"head"`
}

func (sh *ShamHub) toChange(c shamChange) (*Change, error) {
	base, err := sh.toChangeBranch(c.Owner, c.Repo, c.Base)
	if err != nil {
		return nil, fmt.Errorf("base branch: %w", err)
	}

	head, err := sh.toChangeBranch(c.Owner, c.Repo, c.Head)
	if err != nil {
		return nil, fmt.Errorf("head branch: %w", err)
	}

	change := &Change{
		Number:  c.Number,
		URL:     sh.changeURL(c.Owner, c.Repo, c.Number),
		Draft:   c.Draft,
		Subject: c.Subject,
		Body:    c.Body,
		Base:    base,
		Head:    head,
	}
	switch c.State {
	case shamChangeOpen:
		change.State = "open"
	case shamChangeClosed:
		change.State = "closed"
	case shamChangeMerged:
		change.State = "closed"
		change.Merged = true
	default:
		return nil, fmt.Errorf("unknown change state: %d", c.State)
	}

	return change, nil
}

// ChangeBranch is a branch in a change proposal.
type ChangeBranch struct {
	Name string `json:"ref"`
	Hash string `json:"sha"`
}

func (sh *ShamHub) toChangeBranch(owner, repo, ref string) (*ChangeBranch, error) {
	logw, flush := ioutil.LogWriter(sh.log, log.DebugLevel)
	defer flush()

	cmd := exec.Command(sh.gitExe, "rev-parse", ref)
	cmd.Dir = sh.repoDir(owner, repo)
	cmd.Stderr = logw
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("get SHA for %v/%v:%v: %w", owner, repo, ref, err)
	}

	return &ChangeBranch{
		Name: ref,
		Hash: strings.TrimSpace(string(out)),
	}, nil
}
