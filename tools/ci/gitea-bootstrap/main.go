// Command gitea-bootstrap sets up a fresh Gitea instance for integration tests.
//
// It creates an admin user, a test repository, a fork repository,
// and additional test user accounts via Gitea's REST API.
// On success it writes the following lines to stdout for use by CI:
//
//	token=<access-token>
//	owner=<owner-username>
//	repo=<repo-name>
//	fork_owner=<fork-owner-username>
//	fork_repo=<fork-repo-name>
//	reviewer=<reviewer-username>
//	assignee=<assignee-username>
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
	_giteaURL      = "http://localhost:3000"
	_adminUser     = "testadmin"
	_adminPass     = "testadmin123!"
	_adminEmail    = "admin@test.example"
	_reviewerUser  = "test-reviewer"
	_reviewerPass  = "reviewer123!"
	_reviewerEmail = "reviewer@test.example"
	_testRepo      = "test-repo"
	_testForkRepo  = "test-fork-repo"
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

	if err := createAdminUser(); err != nil {
		return fmt.Errorf("create admin user: %w", err)
	}

	tok, err := createToken(_adminUser, _adminPass)
	if err != nil {
		return fmt.Errorf("create token: %w", err)
	}

	if err := createRepo(tok, _adminUser, _testRepo, false); err != nil {
		return fmt.Errorf("create test repo: %w", err)
	}

	if err := createUser(tok, _reviewerUser, _reviewerPass, _reviewerEmail); err != nil {
		return fmt.Errorf("create reviewer user: %w", err)
	}

	if err := createRepo(tok, _reviewerUser, _testForkRepo, false); err != nil {
		return fmt.Errorf("create fork repo: %w", err)
	}

	// Write outputs for the CI job.
	fmt.Printf("token=%s\n", tok)
	fmt.Printf("owner=%s\n", _adminUser)
	fmt.Printf("repo=%s\n", _testRepo)
	fmt.Printf("fork_owner=%s\n", _reviewerUser)
	fmt.Printf("fork_repo=%s\n", _testForkRepo)
	fmt.Printf("reviewer=%s\n", _reviewerUser)
	fmt.Printf("assignee=%s\n", _reviewerUser)
	return nil
}

func waitForGitea() error {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(_giteaURL + "/api/v1/version")
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			return nil
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		time.Sleep(2 * time.Second)
	}
	return errors.New("timed out waiting for Gitea to start")
}

func createAdminUser() error {
	body := map[string]any{
		"username":             _adminUser,
		"password":             _adminPass,
		"email":                _adminEmail,
		"must_change_password": false,
	}
	_, err := apiPost(_giteaURL+"/api/v1/admin/users", "", body)
	return err
}

func createToken(username, password string) (string, error) {
	body := map[string]any{
		"name":   "gs-integration-test",
		"scopes": []string{"repository"},
	}
	resp, err := apiPostWithBasicAuth(
		_giteaURL+"/api/v1/users/"+username+"/tokens",
		username, password, body,
	)
	if err != nil {
		return "", err
	}
	tok, ok := resp["sha1"].(string)
	if !ok {
		return "", errors.New("token not found in response")
	}
	return tok, nil
}

func createUser(adminToken, username, password, email string) error {
	body := map[string]any{
		"username":             username,
		"password":             password,
		"email":                email,
		"must_change_password": false,
	}
	_, err := apiPost(_giteaURL+"/api/v1/admin/users", adminToken, body)
	return err
}

func createRepo(adminToken, owner, name string, _ bool) error {
	body := map[string]any{
		"name":        name,
		"description": "git-spice integration test repository",
		"private":     false,
		"auto_init":   true,
	}
	_, err := apiPost(
		fmt.Sprintf("%s/api/v1/admin/users/%s/repos", _giteaURL, owner),
		adminToken, body,
	)
	return err
}

func apiPost(url, token string, body any) (map[string]any, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
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

func apiPostWithBasicAuth(url, username, password string, body any) (map[string]any, error) {
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
