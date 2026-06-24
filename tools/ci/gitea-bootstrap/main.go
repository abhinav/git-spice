// Command gitea-bootstrap prepares the canonical Docker Gitea topology
// for fixture recording.
//
// It expects tools/record-gitea-fixtures.sh to have started Gitea
// with INSTALL_LOCK=true and sqlite3,
// and to have created the owner account as an administrator
// with the credentials below.
// It is not used for GITEA_RECORD_MODE=existing;
// existing-instance recording reads topology from
// internal/forge/forgetest/testconfig.yaml and assumes the configured users,
// repository, fork repository, and collaborator permissions already exist.
//
// The command creates the rest of the actor graph needed by forgetest:
// a reviewer account,
// a primary repository,
// a fork repository,
// and collaborator permissions for reviewer and assignee flows.
//
// The repository and user names must stay aligned with the `gitea` section
// in internal/forge/forgetest/testconfig.yaml
// and with forgetest.CanonicalGiteaConfig.
// Integration tests use those canonical names in replay mode,
// while this command prints only dynamic token values
// that cannot be checked in.
//
// The owner token and fork-owner token are deliberately separate.
// forgetest exercises pull requests whose source branch is pushed to a fork,
// so Git operations against the fork must authenticate as the fork owner
// instead of relying on the primary repository owner.
//
// On success it writes shell assignments for use by
// tools/record-gitea-fixtures.sh.
//
//	GITEA_URL=<container-api-url>
//	GITEA_TOKEN=<owner-access-token>
//	GITEA_FORK_TOKEN=<fork-owner-access-token>
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	// _giteaURL is the URL exposed by tools/record-gitea-fixtures.sh.
	_giteaURL = "http://127.0.0.1:3000"

	// _ownerUser owns the primary repository used by integration tests.
	//
	// The recorder script creates this account with administrator privileges
	// before invoking this command because Gitea's CLI is the reliable way
	// to create the first admin user on a fresh INSTALL_LOCK=true instance.
	_ownerUser  = "test-owner"
	_ownerPass  = "owner123!"
	_ownerEmail = "owner@test.example"

	// _reviewerUser is also the fork owner in Gitea fixtures.
	//
	// forgetest.CanonicalGiteaConfig intentionally uses the same account for
	// reviewer, assignee, and fork-owner roles so fixture sanitization has one
	// stable external username for non-owner actors.
	_reviewerUser  = "test-reviewer"
	_reviewerPass  = "reviewer123!"
	_reviewerEmail = "reviewer@test.example"

	_testRepo = "test-repo"
	_forkRepo = "test-fork-repo"

	_tokenName      = "gs-record-gitea"
	_repoPermission = "write"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "gitea-bootstrap: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if err := waitForGitea(); err != nil {
		return fmt.Errorf("wait for Gitea: %w", err)
	}

	token, err := createToken(_ownerUser, _ownerPass)
	if err != nil {
		return fmt.Errorf("create token: %w", err)
	}

	if err := createUser(
		token,
		_reviewerUser,
		_reviewerPass,
		_reviewerEmail,
	); err != nil {
		return fmt.Errorf("create reviewer user: %w", err)
	}

	forkToken, err := createToken(_reviewerUser, _reviewerPass)
	if err != nil {
		return fmt.Errorf("create fork owner token: %w", err)
	}

	if err := createRepo(token, _testRepo); err != nil {
		return fmt.Errorf("create test repository: %w", err)
	}
	if err := forkRepo(forkToken, _forkRepo); err != nil {
		return fmt.Errorf("create fork repository: %w", err)
	}

	if err := addCollaborator(
		token,
		_ownerUser,
		_testRepo,
		_reviewerUser,
	); err != nil {
		return fmt.Errorf("add reviewer collaborator: %w", err)
	}

	fmt.Printf("GITEA_URL=%s\n", _giteaURL)
	fmt.Printf("GITEA_TOKEN=%s\n", token)
	fmt.Printf("GITEA_FORK_TOKEN=%s\n", forkToken)
	return nil
}

func waitForGitea() error {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(_giteaURL + "/api/v1/version")
		if err == nil && resp.StatusCode == http.StatusOK {
			return resp.Body.Close()
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		time.Sleep(2 * time.Second)
	}
	return errors.New("timed out waiting for Gitea to start")
}

func createToken(username string, password string) (string, error) {
	body := map[string]any{
		"name": _tokenName,
		"scopes": []string{
			"write:admin",
			"write:issue",
			"write:repository",
			"write:user",
		},
	}
	resp, err := apiPostWithBasicAuth(
		_giteaURL+"/api/v1/users/"+username+"/tokens",
		username,
		password,
		body,
	)
	if err != nil {
		return "", err
	}
	token, ok := resp["sha1"].(string)
	if !ok {
		return "", errors.New("token not found in response")
	}
	return token, nil
}

func createUser(
	adminToken string,
	username string,
	password string,
	email string,
) error {
	body := map[string]any{
		"email":                email,
		"must_change_password": false,
		"password":             password,
		"username":             username,
	}
	_, err := apiPost(_giteaURL+"/api/v1/admin/users", adminToken, body)
	return err
}

func createRepo(token string, name string) error {
	body := map[string]any{
		"auto_init":      true,
		"default_branch": "main",
		"description":    "git-spice integration test repository",
		"name":           name,
		"private":        false,
	}
	_, err := apiPost(_giteaURL+"/api/v1/user/repos", token, body)
	return err
}

func forkRepo(token string, name string) error {
	body := map[string]any{"name": name}
	if _, err := apiPost(
		fmt.Sprintf(
			"%s/api/v1/repos/%s/%s/forks",
			_giteaURL,
			_ownerUser,
			_testRepo,
		),
		token,
		body,
	); err != nil {
		return err
	}
	return waitForRepo(token, _reviewerUser, name)
}

func waitForRepo(token string, owner string, repo string) error {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := apiGet(
			fmt.Sprintf(
				"%s/api/v1/repos/%s/%s",
				_giteaURL,
				owner,
				repo,
			),
			token,
		); err == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out waiting for repository %q/%q", owner, repo)
}

func addCollaborator(
	token string,
	owner string,
	repo string,
	collaborator string,
) error {
	body := map[string]any{"permission": _repoPermission}
	_, err := apiRequest(
		http.MethodPut,
		fmt.Sprintf(
			"%s/api/v1/repos/%s/%s/collaborators/%s",
			_giteaURL,
			owner,
			repo,
			collaborator,
		),
		token,
		body,
	)
	return err
}

func apiPost(
	url string,
	token string,
	body any,
) (map[string]any, error) {
	return apiRequest(http.MethodPost, url, token, body)
}

func apiGet(url string, token string) (map[string]any, error) {
	return apiRequest(http.MethodGet, url, token, nil)
}

func apiRequest(
	method string,
	url string,
	token string,
	body any,
) (map[string]any, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, respBody)
	}

	var result map[string]any
	_ = json.Unmarshal(respBody, &result)
	return result, nil
}

func apiPostWithBasicAuth(
	url string,
	username string,
	password string,
	body any,
) (map[string]any, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(username, password)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, respBody)
	}

	var result map[string]any
	_ = json.Unmarshal(respBody, &result)
	return result, nil
}
