// Command forgejo-bootstrap prepares a local Docker Forgejo instance
// for fixture recording.
//
// It expects tools/record-forgejo-fixtures.sh to have started Forgejo
// with INSTALL_LOCK=true and sqlite3,
// and to have created the test owner account as an administrator
// with the credentials below.
//
// The command creates the rest of the actor graph needed by forgetest:
// a reviewer,
// an assignee,
// a fork owner,
// a primary repository,
// a fork repository,
// and collaborator permissions for reviewer and assignee flows.
//
// The repository and user names must stay aligned with the forgejo section
// in internal/forge/forgetest/testconfig.yaml.
// That file supplies repository topology to the integration tests,
// while this command prints only the dynamic token values that cannot be
// checked in.
//
// The owner token and fork-owner token are deliberately separate.
// forgetest exercises pull requests whose source branch is pushed to a fork,
// so Git operations against the fork must authenticate as the fork owner
// instead of relying on the primary repository owner.
//
// On success it writes shell assignments for use by
// tools/record-forgejo-fixtures.sh.
package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	// _ownerUser owns the primary repository used by integration tests.
	//
	// The recorder script creates this account with administrator privileges
	// before invoking this command because only Forgejo's CLI can create the
	// first admin user reliably on a fresh INSTALL_LOCK=true instance.
	_ownerUser = "test-owner"
	_ownerPass = "owner123!"

	// _reviewerUser is added as a collaborator
	// so reviewer-related forgetest scenarios can assign review requests.
	_reviewerUser  = "test-reviewer"
	_reviewerPass  = "reviewer123!"
	_reviewerEmail = "reviewer@test.example"

	// _assigneeUser is added as a collaborator
	// so assignment-related forgetest scenarios can edit pull request
	// assignees without depending on the repository owner account.
	_assigneeUser  = "test-assignee"
	_assigneePass  = "assignee123!"
	_assigneeEmail = "assignee@test.example"

	// _forkOwnerUser owns the fork repository used by push-remote tests.
	//
	// The fork owner receives a separate token because those tests push
	// branches to the fork before opening pull requests against _ownerUser.
	_forkOwnerUser  = "test-fork-owner"
	_forkOwnerPass  = "forkowner123!"
	_forkOwnerEmail = "forkowner@test.example"

	// _testRepo is used for both the primary repository
	// and the fork repository.
	_testRepo = "test-repo"

	// _tokenName names the API tokens created during each fixture run.
	_tokenName = "gs-record-forgejo"

	// _repoPermission gives reviewer and assignee accounts enough access
	// to participate in pull request workflows without making them admins.
	_repoPermission = "write"
)

// _forgejoURL is the URL exposed by tools/record-forgejo-fixtures.sh.
//
// The recorder script sets FORGEJO_URL from its chosen host port before it
// invokes this command.
// The default matches the checked-in Forgejo test config.
var _forgejoURL = cmp.Or(os.Getenv("FORGEJO_URL"), "http://127.0.0.1:3000")

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "forgejo-bootstrap: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if err := waitForForgejo(); err != nil {
		return fmt.Errorf("wait for Forgejo: %w", err)
	}

	// The owner token drives repository setup and most API interactions
	// during fixture recording.
	token, err := createToken(_ownerUser, _ownerPass)
	if err != nil {
		return fmt.Errorf("create token: %w", err)
	}

	// These users model the separate human actors that forgetest needs
	// for reviewer,
	// assignee,
	// and cross-repository pull request workflows.
	if err := createUser(
		token,
		_reviewerUser,
		_reviewerPass,
		_reviewerEmail,
	); err != nil {
		return fmt.Errorf("create reviewer user: %w", err)
	}
	if err := createUser(
		token,
		_assigneeUser,
		_assigneePass,
		_assigneeEmail,
	); err != nil {
		return fmt.Errorf("create assignee user: %w", err)
	}
	if err := createUser(
		token,
		_forkOwnerUser,
		_forkOwnerPass,
		_forkOwnerEmail,
	); err != nil {
		return fmt.Errorf("create fork owner user: %w", err)
	}
	forkToken, err := createToken(_forkOwnerUser, _forkOwnerPass)
	if err != nil {
		return fmt.Errorf("create fork owner token: %w", err)
	}

	// The repository starts with an initial commit
	// so integration tests can create branches and pull requests
	// without having to bootstrap an empty default branch first.
	if err := createRepo(token, _testRepo); err != nil {
		return fmt.Errorf("create test repository: %w", err)
	}
	if err := forkRepo(forkToken, _testRepo); err != nil {
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
	if err := addCollaborator(
		token,
		_ownerUser,
		_testRepo,
		_assigneeUser,
	); err != nil {
		return fmt.Errorf("add assignee collaborator: %w", err)
	}

	// The recorder script evals this output.
	// Keep names shell-safe and print only dynamic values
	// that cannot live in testconfig.yaml.
	fmt.Printf("FORGEJO_URL=%s\n", _forgejoURL)
	fmt.Printf("FORGEJO_TOKEN=%s\n", token)
	fmt.Printf("FORGEJO_FORK_TOKEN=%s\n", forkToken)
	return nil
}

func waitForForgejo() error {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(_forgejoURL + "/api/v1/version")
		if err == nil && resp.StatusCode == http.StatusOK {
			return resp.Body.Close()
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		time.Sleep(2 * time.Second)
	}
	return errors.New("timed out waiting for Forgejo to start")
}

func createToken(username, password string) (string, error) {
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
		_forgejoURL+"/api/v1/users/"+username+"/tokens",
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
	_, err := apiPost(_forgejoURL+"/api/v1/admin/users", adminToken, body)
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
	_, err := apiPost(_forgejoURL+"/api/v1/user/repos", token, body)
	return err
}

func forkRepo(token string, name string) error {
	body := map[string]any{"name": name}
	if _, err := apiPost(
		fmt.Sprintf(
			"%s/api/v1/repos/%s/%s/forks",
			_forgejoURL,
			_ownerUser,
			_testRepo,
		),
		token,
		body,
	); err != nil {
		return err
	}
	// Fork creation returns before the fork is immediately available
	// through every API path used by the tests.
	return waitForRepo(token, _forkOwnerUser, name)
}

func waitForRepo(token string, owner string, repo string) error {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := apiGet(
			fmt.Sprintf(
				"%s/api/v1/repos/%s/%s",
				_forgejoURL,
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
			_forgejoURL,
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
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "token "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	return decodeResponse(resp)
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

	return decodeResponse(resp)
}

func decodeResponse(resp *http.Response) (map[string]any, error) {
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, respBody)
	}

	var result map[string]any
	if len(respBody) == 0 {
		return result, nil
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}
	return result, nil
}
