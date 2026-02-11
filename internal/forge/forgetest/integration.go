// Package forgetest implements utilities for testing Forge implementations.
package forgetest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/fixturetest"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/httptest"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/xec"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

// Update returns true if running in fixture update mode.
func Update() bool {
	return flag.Lookup("update").Value.(flag.Getter).Get().(bool)
}

func init() {
	if flag.Lookup("update") == nil {
		flag.Bool("update", false, "update test fixtures")
	}
}

// Token retrieves authentication credentials for the given forge URL.
// In update mode, it tries multiple sources in order:
//  1. Environment variable (explicit override)
//  2. Stored OAuth credentials from secret stash (from 'gs auth login')
//  3. GCM (git-credential-manager)
//
// In replay mode, it returns a dummy token.
//
// The envVar parameter should be the name of the environment variable
// to check (e.g., "GITHUB_TOKEN").
func Token(t *testing.T, forgeURL, envVar string) string {
	if !Update() {
		return "token"
	}

	// Try environment variable first for explicit override.
	if token := os.Getenv(envVar); token != "" {
		t.Logf("Using %s from environment", envVar)
		return token
	}

	// Try stored OAuth credentials from stash.
	if token := loadStashToken(t, forgeURL); token != "" {
		t.Logf("Using stored OAuth token from stash for %s", forgeURL)
		return token
	}

	// Try GCM.
	cred, err := forge.LoadGCMCredential(forgeURL)
	if err == nil {
		t.Logf("Using token from git-credential-manager for %s", forgeURL)
		return cred.Password
	}

	t.Fatalf("No credentials available for %s: set %s, run 'gs auth login', or configure git-credential-manager",
		forgeURL, envVar)
	return ""
}

// loadStashToken attempts to load OAuth credentials from the secret stash.
// Returns empty string if no credentials are found or on error.
func loadStashToken(t *testing.T, forgeURL string) string {
	stash := new(secret.Keyring)
	tokstr, err := stash.LoadSecret(forgeURL, "token")
	if err != nil {
		return ""
	}

	// Parse the JSON token structure to extract access_token.
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal([]byte(tokstr), &tok); err != nil {
		t.Logf("Failed to parse stored token: %v", err)
		return ""
	}

	return tok.AccessToken
}

// NewHTTPRecorder creates a new HTTP recorder for the given test and name.
func NewHTTPRecorder(t *testing.T, name string) *recorder.Recorder {
	return httptest.NewTransportRecorder(t, name, httptest.TransportRecorderOptions{
		Update: Update,
		Matcher: func(r *http.Request, i cassette.Request) bool {
			// If there's no body, just match the method and URL.
			if r.Body == nil || r.Body == http.NoBody {
				return r.Method == i.Method && r.URL.String() == i.URL
			}

			reqBody, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			assert.NoError(t, r.Body.Close())

			r.Body = io.NopCloser(bytes.NewBuffer(reqBody))
			return r.Method == i.Method &&
				r.URL.String() == i.URL &&
				string(reqBody) == i.Body
		},
	})
}

type (
	// MergeChangeFunc merges a change.
	// This functionality is not available on the forge interface.
	MergeChangeFunc func(
		t *testing.T,
		repo forge.Repository,
		changeID forge.ChangeID,
	)

	// CloseChangeFunc closes a change without merging.
	// This functionality is not available on the forge interface.
	CloseChangeFunc func(
		t *testing.T,
		repo forge.Repository,
		changeID forge.ChangeID,
	)
)

// IntegrationConfig configures a forge integration test run.
type IntegrationConfig struct {
	// RemoteURL is the Git remote URL to clone in update mode.
	// Example: "https://github.com/abhinav/test-repo"
	RemoteURL string // required

	// Forge is the forge being tested.
	Forge forge.Forge // required

	// OpenRepository creates a forge.Repository for testing.
	// It receives an HTTP client to wrap as needed for the forge implementation.
	OpenRepository func(*testing.T, *http.Client) forge.Repository // required

	// MergeChange merges a change.
	MergeChange MergeChangeFunc // required

	// CloseChange closes a change without merging.
	CloseChange CloseChangeFunc // required

	// Reviewers is a list of usernames that can be added as reviewers to changes.
	Reviewers []string // required

	// Assignees is a list of usernames that can be assigned to changes.
	Assignees []string // required

	// SetCommentsPageSize sets the page size for listing comments.
	// This is used to test pagination.
	SetCommentsPageSize func(testing.TB, int) // required

	// BaseBranchMayBeAbsent indicates whether the forge allows
	// base branches to be absent when submitting changes.
	// (GitLab does this. It's not clear why.)
	BaseBranchMayBeAbsent bool // optional
}

// RunIntegration runs integration tests with the given configuration.
func RunIntegration(t *testing.T, config IntegrationConfig) {
	suite := &integrationSuite{
		Forge: config.Forge,
		Fixtures: fixturetest.Config{
			Update: Update,
		},
		RemoteURL:           config.RemoteURL,
		openRepository:      config.OpenRepository,
		MergeChange:         config.MergeChange,
		CloseChange:         config.CloseChange,
		Reviewers:           config.Reviewers,
		Assignees:           config.Assignees,
		SetCommentsPageSize: config.SetCommentsPageSize,
	}

	t.Run("SubmitEditChange", func(t *testing.T) {
		t.Parallel()

		suite.TestSubmitEditChange(t)
	})

	t.Run("SubmitEditBase", func(t *testing.T) {
		t.Parallel()

		suite.TestSubmitChangeBase(t)
	})

	t.Run("SubmitEditDraft", func(t *testing.T) {
		t.Parallel()

		suite.TestSubmitChangeDraft(t)
	})

	t.Run("ChangesStates", func(t *testing.T) {
		t.Parallel()

		suite.TestChangeStates(t)
	})

	t.Run("FindChangesByBranchDoesNotExist", func(t *testing.T) {
		t.Parallel()

		suite.TestFindChangesByBranchDoesNotExist(t)
	})

	// NOTE: ListChangeTemplates cannot run in parallel
	// because it modifies the main branch.
	t.Run("ListChangeTemplates", func(t *testing.T) {
		suite.TestListChangeTemplates(t)
	})

	t.Run("SubmitEditLabels", func(t *testing.T) {
		t.Parallel()

		suite.TestSubmitEditLabels(t)
	})

	if !config.BaseBranchMayBeAbsent {
		t.Run("SubmitBaseDoesNotExist", func(t *testing.T) {
			t.Parallel()

			suite.TestSubmitBaseDoesNotExist(t)
		})
	}

	t.Run("SubmitEditReviewers", func(t *testing.T) {
		t.Parallel()

		suite.TestSubmitEditReviewers(t)
	})

	t.Run("SubmitEditAssignees", func(t *testing.T) {
		t.Parallel()

		suite.TestSubmitEditAssignees(t)
	})

	t.Run("ChangeComments", func(t *testing.T) {
		t.Parallel()

		suite.TestChangeComments(t)
	})
}

type integrationSuite struct {
	Forge forge.Forge

	// Fixtures manages test fixtures.
	// These are persisted across test runs when in update mode.
	// They can be sourced from functions or set manually.
	Fixtures fixturetest.Config

	// RemoteURL is the Git remote URL to clone in update mode.
	//
	// Example: "https://github.com/abhinav/test-repo"
	RemoteURL string

	// MergeChange merges a change.
	MergeChange MergeChangeFunc

	// CloseChange closes a change without merging.
	CloseChange CloseChangeFunc

	// Reviewers is a list of usernames that can be added as reviewers to changes.
	Reviewers []string

	// Assignees is a list of usernames that can be assigned to changes.
	Assignees []string

	// SetCommentsPageSize sets the page size for listing comments.
	SetCommentsPageSize func(testing.TB, int)

	openRepository func(*testing.T, *http.Client) forge.Repository
}

// HTTPClient creates an HTTP client for use in the given test.
// In Update mode, it records HTTP interactions to fixtures.
// In non-update mode, it replays from existing fixtures.
func (s *integrationSuite) HTTPClient(t *testing.T) *http.Client {
	rec := NewHTTPRecorder(t, t.Name())
	return rec.GetDefaultClient()
}

// OpenRepository creates a forge.Repository for testing.
// It receives an HTTP client to wrap as needed for the forge implementation.
func (s *integrationSuite) OpenRepository(t *testing.T) forge.Repository {
	httpClient := s.HTTPClient(t)
	return s.openRepository(t, httpClient)
}

func (s *integrationSuite) TestSubmitEditChange(t *testing.T) {
	// Name of the branch we're working with.
	branchFixture := fixturetest.New(s.Fixtures, "branch", func() string {
		return randomString(8)
	})
	// Commit hash of the commit we pushed to the branch.
	commitHashFixture, setCommitHash := fixturetest.Stored[string](s.Fixtures, "firstCommitHash")

	branchName := branchFixture.Get(t)
	t.Logf("Creating branch: %s", branchName)
	if Update() {
		testRepo := newTestRepository(t, s.RemoteURL)

		// Create branch with random content
		testRepo.CreateBranch(branchName)
		testRepo.CheckoutBranch(branchName)
		testRepo.WriteFile(branchName+".txt", randomString(32))
		hash := testRepo.AddAllAndCommit("commit from test")
		testRepo.Push(branchName)
		setCommitHash(hash.String())

		t.Cleanup(func() {
			testRepo.DeleteRemoteBranch(branchName)
		})
	}
	commitHash := commitHashFixture.Get(t)
	t.Logf("Got commit hash: %s", commitHash)

	repo := s.OpenRepository(t)

	change, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Testing " + branchName,
		Body:    "Test PR",
		Base:    "main",
		Head:    branchName,
	})
	require.NoError(t, err, "error creating PR")
	changeID := change.ID

	// After submitting the change, we can find it by ID.
	t.Run("FindChangeByID", func(t *testing.T) {
		foundChange, err := repo.FindChangeByID(t.Context(), changeID)
		require.NoError(t, err, "error finding change by ID")
		assert.Equal(t, commitHash, foundChange.HeadHash.String(),
			"head hash should match first commit")
		assert.Equal(t, "Testing "+branchName, foundChange.Subject, "subject should match")
		assert.Equal(t, "main", foundChange.BaseName, "base name should match")
		assert.Equal(t, forge.ChangeOpen, foundChange.State, "state should be open")
		assert.Equal(t, change.URL, foundChange.URL, "URL should match")
	})

	// We can also find the change by branch.
	t.Run("FindChangesByBranch", func(t *testing.T) {
		changes, err := repo.FindChangesByBranch(t.Context(), branchName, forge.FindChangesOptions{})
		require.NoError(t, err, "error finding changes by branch")
		require.Len(t, changes, 1, "expected exactly one change")

		foundChange := changes[0]
		assert.Equal(t, changeID, foundChange.ID, "ID should match")
		assert.Equal(t, commitHash, foundChange.HeadHash.String(),
			"head hash should match first commit")
		assert.Equal(t, "Testing "+branchName, foundChange.Subject, "subject should match")
		assert.Equal(t, "main", foundChange.BaseName, "base name should match")
		assert.Equal(t, forge.ChangeOpen, foundChange.State, "state should be open")
		assert.Equal(t, change.URL, foundChange.URL, "URL should match")
	})
}

// Changes can be submitted with a non-main base,
// and then edited to change the base to main.
func (s *integrationSuite) TestSubmitChangeBase(t *testing.T) {
	// Fixture for branch and base names
	branchFixture := fixturetest.New(s.Fixtures, "branch", func() string {
		return randomString(8)
	})
	baseFixture := fixturetest.New(s.Fixtures, "base", func() string {
		return randomString(8)
	})

	branchName := branchFixture.Get(t)
	baseName := baseFixture.Get(t)
	t.Logf("Creating branch: %s with base: %s", branchName, baseName)

	if Update() {
		testRepo := newTestRepository(t, s.RemoteURL)

		// Push the base branch at current main position
		testRepo.Push("main:" + baseName)
		t.Cleanup(func() {
			testRepo.DeleteRemoteBranch(baseName)
		})

		// Create and push the feature branch
		testRepo.CreateBranch(branchName)
		testRepo.CheckoutBranch(branchName)
		testRepo.WriteFile(branchName+".txt", randomString(32))
		testRepo.AddAllAndCommit("commit from test")
		testRepo.Push(branchName)

		t.Cleanup(func() {
			testRepo.DeleteRemoteBranch(branchName)
		})
	}

	repo := s.OpenRepository(t)

	// Submit change with non-main base
	change, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Testing " + branchName,
		Body:    "Test PR with custom base",
		Base:    baseName,
		Head:    branchName,
	})
	require.NoError(t, err, "error creating PR")
	changeID := change.ID

	// Verify base is set correctly.
	foundChange, err := repo.FindChangeByID(t.Context(), changeID)
	require.NoError(t, err, "error finding change by ID")
	assert.Equal(t, baseName, foundChange.BaseName, "base should be custom base")

	// Edit change to set base to main.
	err = repo.EditChange(t.Context(), changeID, forge.EditChangeOptions{
		Base: "main",
	})
	require.NoError(t, err, "error changing PR base to main")

	// Verify base changed to main.
	foundChange, err = repo.FindChangeByID(t.Context(), changeID)
	require.NoError(t, err, "error finding change after base change")
	assert.Equal(t, "main", foundChange.BaseName, "base should be main")
}

// Changes can be submitted as drafts, and edited to toggle draft status.
func (s *integrationSuite) TestSubmitChangeDraft(t *testing.T) {
	branchFixture := fixturetest.New(s.Fixtures, "branch", func() string {
		return randomString(8)
	})
	branchName := branchFixture.Get(t)
	t.Logf("Creating branch: %s", branchName)

	if Update() {
		testRepo := newTestRepository(t, s.RemoteURL)

		testRepo.CreateBranch(branchName)
		testRepo.CheckoutBranch(branchName)
		testRepo.WriteFile(branchName+".txt", randomString(32))
		testRepo.AddAllAndCommit("commit from test")
		testRepo.Push(branchName)

		t.Cleanup(func() {
			testRepo.DeleteRemoteBranch(branchName)
		})
	}

	repo := s.OpenRepository(t)

	// Submit as draft.
	change, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Testing " + branchName,
		Body:    "Test draft PR",
		Base:    "main",
		Head:    branchName,
		Draft:   true,
	})
	require.NoError(t, err, "error creating draft PR")
	changeID := change.ID

	// Verify it's a draft.
	foundChange, err := repo.FindChangeByID(t.Context(), changeID)
	require.NoError(t, err, "error finding change by ID")
	assert.True(t, foundChange.Draft, "change should be draft")

	// Update to non-draft.
	var draft bool
	err = repo.EditChange(t.Context(), changeID, forge.EditChangeOptions{
		Draft: &draft,
	})
	require.NoError(t, err, "error marking change as ready")

	// Verify it's no longer a draft
	foundChange, err = repo.FindChangeByID(t.Context(), changeID)
	require.NoError(t, err, "error finding change after marking ready")
	assert.False(t, foundChange.Draft, "change should not be draft")

	// Update back to draft.
	draft = true
	err = repo.EditChange(t.Context(), changeID, forge.EditChangeOptions{
		Draft: &draft,
	})
	require.NoError(t, err, "error marking change as draft again")

	// Verify it's a draft again.
	foundChange, err = repo.FindChangeByID(t.Context(), changeID)
	require.NoError(t, err, "error finding change after marking draft again")
	assert.True(t, foundChange.Draft, "change should be draft again")
}

func (s *integrationSuite) TestChangeStates(t *testing.T) {
	// We'll create 3 PRs and put them each in a different state.
	openBranchFixture := fixturetest.New(s.Fixtures, "openBranch", func() string {
		return randomString(8)
	})
	mergedBranchFixture := fixturetest.New(s.Fixtures, "mergedBranch", func() string {
		return randomString(8)
	})
	closedBranchFixture := fixturetest.New(s.Fixtures, "closedBranch", func() string {
		return randomString(8)
	})

	openBranch := openBranchFixture.Get(t)
	mergedBranch := mergedBranchFixture.Get(t)
	closedBranch := closedBranchFixture.Get(t)

	t.Logf("Creating branches: %s, %s, %s",
		openBranch, mergedBranch, closedBranch)

	if Update() {
		testRepo := newTestRepository(t, s.RemoteURL)

		// Create and push all three branches.
		for _, branch := range []string{openBranch, mergedBranch, closedBranch} {
			testRepo.CheckoutBranch("main")
			testRepo.CreateBranch(branch)
			testRepo.CheckoutBranch(branch)
			testRepo.WriteFile(branch+".txt", randomString(32))
			testRepo.AddAllAndCommit("commit for " + branch)
			testRepo.Push(branch)
		}

		t.Cleanup(func() {
			for _, branch := range []string{openBranch, mergedBranch, closedBranch} {
				testRepo.DeleteRemoteBranch(branch)
			}
		})
	}

	repo := s.OpenRepository(t)

	// Submit all three changes.
	// We'll put them in different states later.
	openChange, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Open " + openBranch,
		Body:    "Open change",
		Base:    "main",
		Head:    openBranch,
	})
	require.NoError(t, err, "error creating open change")

	mergedChange, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Merged " + mergedBranch,
		Body:    "Merged change",
		Base:    "main",
		Head:    mergedBranch,
	})
	require.NoError(t, err, "error creating merged change")

	closedChange, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Closed " + closedBranch,
		Body:    "Closed change",
		Base:    "main",
		Head:    closedBranch,
	})
	require.NoError(t, err)

	s.MergeChange(t, repo, mergedChange.ID)
	s.CloseChange(t, repo, closedChange.ID)

	// Verify states.
	states, err := repo.ChangesStates(t.Context(), []forge.ChangeID{
		openChange.ID,
		mergedChange.ID,
		closedChange.ID,
	})
	require.NoError(t, err, "error fetching change states")
	assert.Equal(t, []forge.ChangeState{
		forge.ChangeOpen,
		forge.ChangeMerged,
		forge.ChangeClosed,
	}, states, "change states should match expected")
}

// FindChangesByBranch returns no error, and an empty slice
// when the branch does not exist.
func (s *integrationSuite) TestFindChangesByBranchDoesNotExist(t *testing.T) {
	repo := s.OpenRepository(t)

	changes, err := repo.FindChangesByBranch(t.Context(), "does-not-exist", forge.FindChangesOptions{})
	require.NoError(t, err, "should not error for non-existent branch")
	assert.Empty(t, changes, "should return empty slice for non-existent branch")
}

func (s *integrationSuite) TestListChangeTemplates(t *testing.T) {
	// Get the template paths from the forge.
	// We'll use the first non-.md path as the directory for templates.
	templatePaths := s.Forge.ChangeTemplatePaths()
	require.NotEmpty(t, templatePaths, "forge must have template paths")

	var templateDir string
	for _, path := range templatePaths {
		if !strings.HasSuffix(path, ".md") {
			templateDir = path
			break
		}
	}
	t.Logf("Will write templates to directory: %s", templateDir)
	require.NotEmpty(t, templateDir, "could not find template directory")

	// Repository has no templates.
	t.Run("NoTemplates", func(t *testing.T) {
		if Update() {
			testRepo := newTestRepository(t, s.RemoteURL)

			// Nuke all CR templates in the repo.
			t.Logf("Removing all templates from main")
			var deleted bool
			for _, path := range templatePaths {
				fullPath := filepath.Join(testRepo.root, path)
				if _, err := os.Stat(fullPath); err != nil {
					if os.IsNotExist(err) {
						continue
					}
					require.NoError(t, err, "could not stat template path: %s", path)
				}

				deleted = true
				require.NoError(t, os.RemoveAll(fullPath),
					"could not remove template path: %s", path)
			}

			if deleted {
				testRepo.AddAllAndCommit("Remove all templates")
				testRepo.Push("main")
			}
		}

		ctx := t.Context()
		repo := s.OpenRepository(t)
		templates, err := repo.ListChangeTemplates(ctx)
		require.NoError(t, err)
		assert.Empty(t, templates, "should have no templates")
	})

	// Repository has templates.
	t.Run("TemplatesPresent", func(t *testing.T) {
		// Generate template names.
		emptyTemplateFixture := fixturetest.New(s.Fixtures, "empty-template", func() string {
			return randomString(8) + ".md"
		})
		nonEmptyTemplateFixture := fixturetest.New(s.Fixtures, "non-empty-template", func() string {
			return randomString(8) + ".md"
		})

		emptyTemplateName := emptyTemplateFixture.Get(t)
		nonEmptyTemplateName := nonEmptyTemplateFixture.Get(t)
		t.Logf("Creating templates: %s (empty), %s (non-empty)",
			emptyTemplateName, nonEmptyTemplateName)

		if Update() {
			testRepo := newTestRepository(t, s.RemoteURL)

			testRepo.WriteFile(filepath.Join(templateDir, emptyTemplateName))
			t.Logf("Created empty template at: %s",
				filepath.Join(templateDir, emptyTemplateName))

			testRepo.WriteFile(
				filepath.Join(templateDir, nonEmptyTemplateName),
				"This is a test template")
			t.Logf("Created non-empty template at: %s",
				filepath.Join(templateDir, nonEmptyTemplateName))

			testRepo.AddAllAndCommit("Add templates")
			testRepo.Push("main")
		}

		ctx := t.Context()
		repo := s.OpenRepository(t)
		templates, err := repo.ListChangeTemplates(ctx)
		require.NoError(t, err)

		// Find our test templates in the results.
		var foundEmpty, foundNonEmpty bool
		for _, template := range templates {
			// Template names may not have extensions depending on the forge.
			templateName := strings.TrimSuffix(template.Filename, ".md") + ".md"

			switch templateName {
			case emptyTemplateName:
				foundEmpty = true
				// https://github.com/abhinav/git-spice/issues/931
				assert.Empty(t, strings.TrimSpace(template.Body), "empty template should have empty body")

			case nonEmptyTemplateName:
				foundNonEmpty = true
				assert.Equal(t,
					strings.TrimSpace("This is a test template"),
					strings.TrimSpace(template.Body),
					"non-empty template should have correct body")

			default:
				t.Logf("unexpected template: %s", templateName)
			}
		}

		assert.True(t, foundEmpty, "empty template not found in results")
		assert.True(t, foundNonEmpty, "non-empty template not found in results")
	})
}

func (s *integrationSuite) TestSubmitEditLabels(t *testing.T) {
	label1Fixture := fixturetest.New(s.Fixtures, "label1", func() string {
		return randomString(8)
	})
	label2Fixture := fixturetest.New(s.Fixtures, "label2", func() string {
		return randomString(8)
	})
	label3Fixture := fixturetest.New(s.Fixtures, "label3", func() string {
		return randomString(8)
	})

	label1 := label1Fixture.Get(t)
	label2 := label2Fixture.Get(t)
	label3 := label3Fixture.Get(t)

	branchFixture := fixturetest.New(s.Fixtures, "branch", func() string {
		return randomString(8)
	})
	branchName := branchFixture.Get(t)
	t.Logf("Creating branch: %s", branchName)

	if Update() {
		testRepo := newTestRepository(t, s.RemoteURL)

		testRepo.CreateBranch(branchName)
		testRepo.CheckoutBranch(branchName)
		testRepo.WriteFile(branchName+".txt", randomString(32))
		testRepo.AddAllAndCommit("commit from test")
		testRepo.Push(branchName)

		t.Cleanup(func() {
			testRepo.DeleteRemoteBranch(branchName)
		})
	}

	repo := s.OpenRepository(t)

	change, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Testing " + branchName,
		Body:    "Test PR with labels",
		Base:    "main",
		Head:    branchName,
		Labels:  []string{label1},
	})
	require.NoError(t, err, "error creating PR")
	changeID := change.ID

	// Verify initial label.
	foundChange, err := repo.FindChangeByID(t.Context(), changeID)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{label1}, foundChange.Labels,
		"change should have label1")

	// Add label that doesn't exist yet.
	t.Run("AddLabel", func(t *testing.T) {
		require.NoError(t,
			repo.EditChange(t.Context(), changeID, forge.EditChangeOptions{
				AddLabels: []string{label2},
			}), "could not add label2")
	})

	// Add a label that already exists.
	t.Run("AddDuplicateLabel", func(t *testing.T) {
		require.NoError(t,
			repo.EditChange(t.Context(), changeID, forge.EditChangeOptions{
				AddLabels: []string{label2, label3},
			}), "could not add label2 and label3")

		// Verify labels via FindChangesByBranch.
		foundChanges, err := repo.FindChangesByBranch(t.Context(), branchName, forge.FindChangesOptions{})
		require.NoError(t, err)
		require.Len(t, foundChanges, 1, "expected exactly one change")
		assert.ElementsMatch(t,
			[]string{label1, label2, label3},
			foundChanges[0].Labels,
			"change should have all three labels")
	})
}

func (s *integrationSuite) TestSubmitBaseDoesNotExist(t *testing.T) {
	branchFixture := fixturetest.New(s.Fixtures, "branch", func() string {
		return randomString(8)
	})
	baseBranchFixture := fixturetest.New(s.Fixtures, "base-branch", func() string {
		return randomString(8)
	})

	branchName := branchFixture.Get(t)
	baseBranchName := baseBranchFixture.Get(t)
	t.Logf("Creating branch %s with base branch %s", branchName, baseBranchName)

	if Update() {
		testRepo := newTestRepository(t, s.RemoteURL)

		testRepo.CreateBranch(branchName)
		testRepo.CheckoutBranch(branchName)
		testRepo.WriteFile(branchName+".txt", randomString(32))
		testRepo.AddAllAndCommit("commit from test")
		testRepo.Push(branchName)

		t.Cleanup(func() {
			testRepo.DeleteRemoteBranch(branchName)
		})
	}

	repo := s.OpenRepository(t)

	_, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Testing " + branchName,
		Body:    "Test PR with non-existent base",
		Base:    baseBranchName,
		Head:    branchName,
	})
	require.Error(t, err, "error expected when base branch does not exist")
	assert.ErrorIs(t, err, forge.ErrUnsubmittedBase,
		"error should be ErrUnsubmittedBase")
}

func (s *integrationSuite) TestSubmitEditReviewers(t *testing.T) {
	require.NotEmpty(t, s.Reviewers, "test requires at least one reviewer")

	t.Run("SubmitWithReviewer", func(t *testing.T) {
		t.Parallel()

		branchFixture := fixturetest.New(s.Fixtures, "branch-with-reviewer", func() string {
			return randomString(8)
		})
		branchName := branchFixture.Get(t)
		t.Logf("Creating branch: %s", branchName)

		if Update() {
			testRepo := newTestRepository(t, s.RemoteURL)

			testRepo.CreateBranch(branchName)
			testRepo.CheckoutBranch(branchName)
			testRepo.WriteFile(branchName+".txt", randomString(32))
			testRepo.AddAllAndCommit("commit from test")
			testRepo.Push(branchName)

			t.Cleanup(func() {
				testRepo.DeleteRemoteBranch(branchName)
			})
		}

		repo := s.OpenRepository(t)

		// Submit a change with a reviewer.
		_, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
			Subject:   "Testing " + branchName,
			Body:      "Test PR with reviewer",
			Base:      "main",
			Head:      branchName,
			Reviewers: s.Reviewers,
		})
		require.NoError(t, err, "error creating PR")

		foundChanges, err := repo.FindChangesByBranch(t.Context(), branchName, forge.FindChangesOptions{})
		require.NoError(t, err)
		require.Len(t, foundChanges, 1, "expected exactly one change")
		assert.Equal(t, s.Reviewers, foundChanges[0].Reviewers,
			"change should have reviewer")
	})

	t.Run("AddReviewer", func(t *testing.T) {
		t.Parallel()

		branchFixture := fixturetest.New(s.Fixtures, "branch-no-reviewer", func() string {
			return randomString(8)
		})
		branchName := branchFixture.Get(t)
		t.Logf("Creating branch: %s", branchName)

		if Update() {
			testRepo := newTestRepository(t, s.RemoteURL)

			testRepo.CreateBranch(branchName)
			testRepo.CheckoutBranch(branchName)
			testRepo.WriteFile(branchName+".txt", randomString(32))
			testRepo.AddAllAndCommit("commit from test")
			testRepo.Push(branchName)

			t.Cleanup(func() {
				testRepo.DeleteRemoteBranch(branchName)
			})
		}

		repo := s.OpenRepository(t)

		// Submit a change with no reviewers.
		change, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
			Subject: "Testing " + branchName,
			Body:    "Test PR without reviewers",
			Base:    "main",
			Head:    branchName,
		})
		require.NoError(t, err, "error creating PR")
		changeID := change.ID

		foundChange, err := repo.FindChangeByID(t.Context(), changeID)
		require.NoError(t, err)
		assert.Empty(t, foundChange.Reviewers, "change should have no reviewers")

		// Add reviewers with EditChange.
		require.NoError(t,
			repo.EditChange(t.Context(), changeID, forge.EditChangeOptions{
				AddReviewers: s.Reviewers,
			}), "could not add reviewer")

		foundChange, err = repo.FindChangeByID(t.Context(), changeID)
		require.NoError(t, err)
		assert.Equal(t, s.Reviewers, foundChange.Reviewers,
			"change should have reviewer")
	})

	// If there are multiple available reviewers,
	// test adding them one at a time.
	if len(s.Reviewers) > 1 {
		t.Run("AddReviewersOneByOne", func(t *testing.T) {
			t.Parallel()

			branchFixture := fixturetest.New(s.Fixtures, "branch-no-reviewer-one-by-one", func() string {
				return randomString(8)
			})
			branchName := branchFixture.Get(t)
			t.Logf("Creating branch: %s", branchName)

			if Update() {
				testRepo := newTestRepository(t, s.RemoteURL)

				testRepo.CreateBranch(branchName)
				testRepo.CheckoutBranch(branchName)
				testRepo.WriteFile(branchName+".txt", randomString(32))
				testRepo.AddAllAndCommit("commit from test")
				testRepo.Push(branchName)

				t.Cleanup(func() {
					testRepo.DeleteRemoteBranch(branchName)
				})
			}

			repo := s.OpenRepository(t)

			// Submit a change with no reviewers.
			change, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
				Subject: "Testing " + branchName,
				Body:    "Test PR without reviewers",
				Base:    "main",
				Head:    branchName,
			})
			require.NoError(t, err, "error creating PR")
			changeID := change.ID

			foundChange, err := repo.FindChangeByID(t.Context(), changeID)
			require.NoError(t, err)
			assert.Empty(t, foundChange.Reviewers, "change should have no reviewers")

			// Add reviewers one by one.
			for _, reviewer := range s.Reviewers {
				require.NoError(t,
					repo.EditChange(t.Context(), changeID, forge.EditChangeOptions{
						AddReviewers: []string{reviewer},
					}), "could not add reviewer: %s", reviewer)
			}

			// Verify all reviewers added.
			foundChange, err = repo.FindChangeByID(t.Context(), changeID)
			require.NoError(t, err)
			assert.Equal(t, s.Reviewers, foundChange.Reviewers,
				"change should have all reviewers")
		})
	}
}

func (s *integrationSuite) TestSubmitEditAssignees(t *testing.T) {
	require.NotEmpty(t, s.Assignees, "test requires at least one assignee")

	t.Run("SubmitWithAssignee", func(t *testing.T) {
		t.Parallel()

		branchFixture := fixturetest.New(s.Fixtures, "branch-with-assignee", func() string {
			return randomString(8)
		})
		branchName := branchFixture.Get(t)
		t.Logf("Creating branch: %s", branchName)

		if Update() {
			testRepo := newTestRepository(t, s.RemoteURL)

			testRepo.CreateBranch(branchName)
			testRepo.CheckoutBranch(branchName)
			testRepo.WriteFile(branchName+".txt", randomString(32))
			testRepo.AddAllAndCommit("commit from test")
			testRepo.Push(branchName)

			t.Cleanup(func() {
				testRepo.DeleteRemoteBranch(branchName)
			})
		}

		repo := s.OpenRepository(t)

		// Submit a change with one assignee.
		change, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
			Subject:   "Testing " + branchName,
			Body:      "Test PR with assignee",
			Base:      "main",
			Head:      branchName,
			Assignees: s.Assignees,
		})
		require.NoError(t, err, "error creating PR")
		changeID := change.ID

		// Verify assignee via FindChangeByID.
		foundChange, err := repo.FindChangeByID(t.Context(), changeID)
		require.NoError(t, err)
		assert.ElementsMatch(t, s.Assignees, foundChange.Assignees,
			"change should have assignee")

		// Verify assignee via FindChangesByBranch.
		foundChanges, err := repo.FindChangesByBranch(t.Context(), branchName, forge.FindChangesOptions{})
		require.NoError(t, err)
		require.Len(t, foundChanges, 1, "expected exactly one change")
		assert.ElementsMatch(t, s.Assignees, foundChanges[0].Assignees,
			"change should have assignee")
	})

	t.Run("AddAssignee", func(t *testing.T) {
		t.Parallel()

		branchFixture := fixturetest.New(s.Fixtures, "branch-no-assignee", func() string {
			return randomString(8)
		})
		branchName := branchFixture.Get(t)
		t.Logf("Creating branch: %s", branchName)

		if Update() {
			testRepo := newTestRepository(t, s.RemoteURL)

			testRepo.CreateBranch(branchName)
			testRepo.CheckoutBranch(branchName)
			testRepo.WriteFile(branchName+".txt", randomString(32))
			testRepo.AddAllAndCommit("commit from test")
			testRepo.Push(branchName)

			t.Cleanup(func() {
				testRepo.DeleteRemoteBranch(branchName)
			})
		}

		repo := s.OpenRepository(t)

		// Submit a change with no assignees.
		change, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
			Subject: "Testing " + branchName,
			Body:    "Test PR without assignees",
			Base:    "main",
			Head:    branchName,
		})
		require.NoError(t, err, "error creating PR")
		changeID := change.ID

		foundChange, err := repo.FindChangeByID(t.Context(), changeID)
		require.NoError(t, err)
		assert.Empty(t, foundChange.Assignees, "change should have no assignees")

		require.NoError(t,
			repo.EditChange(t.Context(), changeID, forge.EditChangeOptions{
				AddAssignees: s.Assignees,
			}), "could not add assignee")

		// Verify assignee via FindChangeByID.
		foundChange, err = repo.FindChangeByID(t.Context(), changeID)
		require.NoError(t, err)
		assert.ElementsMatch(t, s.Assignees, foundChange.Assignees,
			"change should have assignee")
	})

	// If there are multiple available assignees,
	// test adding them one at a time.
	if len(s.Assignees) > 1 {
		t.Run("AddAssigneesOneByOne", func(t *testing.T) {
			t.Parallel()

			branchFixture := fixturetest.New(s.Fixtures, "branch-no-assignee-one-by-one", func() string {
				return randomString(8)
			})

			branchName := branchFixture.Get(t)
			t.Logf("Creating branch: %s", branchName)

			if Update() {
				testRepo := newTestRepository(t, s.RemoteURL)

				testRepo.CreateBranch(branchName)
				testRepo.CheckoutBranch(branchName)
				testRepo.WriteFile(branchName+".txt", randomString(32))
				testRepo.AddAllAndCommit("commit from test")
				testRepo.Push(branchName)

				t.Cleanup(func() {
					testRepo.DeleteRemoteBranch(branchName)
				})
			}

			repo := s.OpenRepository(t)

			// Submit a change with no assignees.
			change, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
				Subject: "Testing " + branchName,
				Body:    "Test PR without assignees",
				Base:    "main",
				Head:    branchName,
			})
			require.NoError(t, err, "error creating PR")

			changeID := change.ID
			for _, assignee := range s.Assignees {
				require.NoError(t,
					repo.EditChange(t.Context(), changeID, forge.EditChangeOptions{
						AddAssignees: []string{assignee},
					}), "could not add assignee: %s", assignee)
			}

			foundChange, err := repo.FindChangeByID(t.Context(), changeID)
			require.NoError(t, err)
			assert.ElementsMatch(t, s.Assignees, foundChange.Assignees,
				"change should have all assignees")
		})
	}
}

func (s *integrationSuite) TestChangeComments(t *testing.T) {
	const TotalComments = 10

	branchFixture := fixturetest.New(s.Fixtures, "branch", func() string {
		return randomString(8)
	})
	branchName := branchFixture.Get(t)
	t.Logf("Creating branch: %s", branchName)

	if Update() {
		testRepo := newTestRepository(t, s.RemoteURL)

		testRepo.CreateBranch(branchName)
		testRepo.CheckoutBranch(branchName)
		testRepo.WriteFile(branchName+".txt", randomString(32))
		testRepo.AddAllAndCommit("commit from test")
		testRepo.Push(branchName)

		t.Cleanup(func() {
			testRepo.DeleteRemoteBranch(branchName)
		})
	}

	repo := s.OpenRepository(t)

	change, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Testing " + branchName,
		Body:    "Test PR for comments",
		Base:    "main",
		Head:    branchName,
	})
	require.NoError(t, err, "error creating PR")
	changeID := change.ID

	// Generate and post 10 comments.
	commentsFixture := fixturetest.New(s.Fixtures, "comments", func() []string {
		comments := make([]string, TotalComments)
		for i := range comments {
			comments[i] = randomString(32)
		}
		return comments
	})
	comments := commentsFixture.Get(t)

	var commentIDs []forge.ChangeCommentID
	for _, comment := range comments {
		commentID, err := repo.PostChangeComment(t.Context(), changeID, comment)
		require.NoError(t, err, "could not post comment")
		t.Logf("Posted comment: %s", commentID)
		commentIDs = append(commentIDs, commentID)
	}

	// Update one of the comments.
	t.Run("UpdateComment", func(t *testing.T) {
		updatedBodyFixture := fixturetest.New(s.Fixtures, "updated-comment", func() string {
			return randomString(32)
		})
		updatedBody := updatedBodyFixture.Get(t)

		// Update the first comment.
		require.NoError(t,
			repo.UpdateChangeComment(t.Context(), commentIDs[0], updatedBody),
			"could not update comment")

		// Update the slice to reflect the change.
		comments[0] = updatedBody
	})

	// Updating a deleted comment should return ErrNotFound.
	t.Run("UpdateDeletedComment", func(t *testing.T) {
		// Delete the second comment.
		err := repo.DeleteChangeComment(t.Context(), commentIDs[1])
		require.NoError(t, err, "could not delete comment")

		// Attempt to update the deleted comment.
		err = repo.UpdateChangeComment(t.Context(), commentIDs[1], "should fail")
		require.Error(t, err)
		assert.ErrorIs(t, err, forge.ErrNotFound,
			"expected ErrNotFound for deleted comment")

		// Remove from comments slice to keep ListAllComments happy.
		comments = append(comments[:1], comments[2:]...)
		commentIDs = append(commentIDs[:1], commentIDs[2:]...)
	})

	// List all comments with pagination.
	t.Run("ListAllComments", func(t *testing.T) {
		// Set a small page size to test pagination.
		s.SetCommentsPageSize(t, 3)

		var gotBodies []string
		for comment, err := range repo.ListChangeComments(t.Context(), changeID, nil /* opts */) {
			require.NoError(t, err)
			gotBodies = append(gotBodies, comment.Body)
		}

		assert.Len(t, gotBodies, len(comments))
		assert.ElementsMatch(t, comments, gotBodies)
	})

	// List comments with filtering.
	t.Run("ListFilteredComments", func(t *testing.T) {
		// Filter for the first comment (which was updated).
		listOpts := &forge.ListChangeCommentsOptions{
			BodyMatchesAll: []*regexp.Regexp{
				regexp.MustCompile(regexp.QuoteMeta(comments[0])),
			},
		}

		var gotBodies []string
		for comment, err := range repo.ListChangeComments(t.Context(), changeID, listOpts) {
			require.NoError(t, err)
			gotBodies = append(gotBodies, comment.Body)
		}

		assert.Equal(t, []string{comments[0]}, gotBodies)
	})
}

// testRepository manages a local Git repository clone for testing.
// Only available in update mode.
type testRepository struct {
	repo *git.Repository
	work *git.Worktree
	root string
	t    *testing.T
}

func newTestRepository(t *testing.T, remoteURL string) *testRepository {
	require.True(t, Update(), "testRepository only available in update mode")

	repoDir := t.TempDir()
	output := t.Output()
	cmd := xec.Command(t.Context(), silogtest.New(t), "git", "clone", remoteURL, repoDir).
		WithStdout(output).
		WithStderr(output)
	require.NoError(t, cmd.Run(), "failed to clone repository")

	ctx := t.Context()
	work, err := git.OpenWorktree(ctx, repoDir, git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err, "failed to open git worktree")

	return &testRepository{
		repo: work.Repository(),
		work: work,
		root: repoDir,
		t:    t,
	}
}

func (r *testRepository) ctx() context.Context {
	ctx := r.t.Context()
	// If the context was canceled, ignore its cancellation.
	if errors.Is(ctx.Err(), context.Canceled) {
		ctx = context.WithoutCancel(ctx)
	}
	return ctx
}

// WriteFile writes a file to the repository with the given lines.
func (r *testRepository) WriteFile(path string, lines ...string) {
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	require.NoError(r.t, os.MkdirAll(
		filepath.Dir(filepath.Join(r.root, path)),
		0o755,
	), "could not create directories for file: %s", path)
	require.NoError(r.t, os.WriteFile(
		filepath.Join(r.root, path),
		[]byte(content),
		0o644,
	), "could not write file: %s", path)
}

// AddAllAndCommit stages all changes and creates a commit.
func (r *testRepository) AddAllAndCommit(message string) git.Hash {
	output := r.t.Output()
	cmd := xec.Command(r.t.Context(), silogtest.New(r.t), "git", "add", ".").
		WithDir(r.root).
		WithStdout(output).
		WithStderr(output)
	require.NoError(r.t, cmd.Run(), "git add failed")

	ctx := r.ctx()
	sig := git.Signature{
		Name:  "gs-test[bot]",
		Email: "bot@example.com",
	}
	require.NoError(r.t, r.work.Commit(ctx, git.CommitRequest{
		Message:   message,
		Author:    &sig,
		Committer: &sig,
	}), "could not commit changes")

	hash, err := r.repo.PeelToCommit(ctx, "HEAD")
	require.NoError(r.t, err, "could not get commit hash")
	return hash
}

// CreateBranch creates a new branch.
func (r *testRepository) CreateBranch(name string) {
	ctx := r.ctx()
	require.NoError(r.t, r.repo.CreateBranch(ctx, git.CreateBranchRequest{
		Name: name,
	}), "could not create branch: %s", name)
}

// CheckoutBranch checks out an existing branch.
func (r *testRepository) CheckoutBranch(name string) {
	ctx := r.ctx()
	require.NoError(r.t, r.work.CheckoutBranch(ctx, name),
		"could not checkout branch: %s", name)
}

// Push pushes the given refspec to origin.
func (r *testRepository) Push(refspec string) {
	ctx := r.ctx()
	require.NoError(r.t, r.work.Push(ctx, git.PushOptions{
		Remote:  "origin",
		Refspec: git.Refspec(refspec),
	}), "error pushing refspec: %s", refspec)
}

// DeleteRemoteBranch deletes a remote branch.
func (r *testRepository) DeleteRemoteBranch(name string) {
	ctx := r.ctx()
	r.t.Logf("Deleting remote branch: %s", name)
	assert.NoError(r.t, r.work.Push(ctx, git.PushOptions{
		Remote:  "origin",
		Refspec: git.Refspec(":" + name),
	}), "error deleting branch")
}

// Repository returns the underlying git.Repository.
func (r *testRepository) Repository() *git.Repository {
	return r.repo
}

// Worktree returns the underlying git.Worktree.
func (r *testRepository) Worktree() *git.Worktree {
	return r.work
}

// randomString generates a random alphanumeric string of length n.
func randomString(n int) string {
	const alnum = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		var buf [1]byte
		_, _ = rand.Read(buf[:])
		idx := int(buf[0]) % len(alnum)
		b[i] = alnum[idx]
	}
	return string(b)
}
