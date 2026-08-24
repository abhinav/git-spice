package azuredevops

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/fixturetest"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
	"gopkg.in/yaml.v3"
)

var _azureFixtures = fixturetest.Config{Update: forgetest.Update}

const (
	_azureFixtureBaseURL     = "https://dev.azure.example.com"
	_azureFixtureOrg         = "azure-org"
	_azureFixtureProject     = "azure-project"
	_azureFixtureRepo        = "azure-project"
	_azureFixtureReviewer    = "reviewer@example.com"
	_azureFixtureReviewerID  = "00000000-0000-4000-8000-000000000999"
	_azureFixtureDisplayName = "Git Spice Reviewer"
)

func azureFixtureValue(
	t *testing.T,
	name string,
	live func() string,
	canonical string,
) string {
	t.Helper()

	if !forgetest.Update() {
		return fixturetest.New(_azureFixtures, name, func() string {
			return canonical
		}).Get(t)
	}

	fixture, set := fixturetest.Stored[string](_azureFixtures, name)
	set(canonical)
	_ = fixture.Get(t)
	return live()
}

func TestIntegration(t *testing.T) {
	if !forgetest.Update() && !azureIntegrationFixturesExist() {
		t.Skip("Azure DevOps fixtures have not been recorded yet")
	}

	t.Cleanup(func() {
		if t.Failed() && !forgetest.Update() {
			t.Logf("To update the test fixtures, run:")
			t.Logf("    go test -update -run '^%s$' ./internal/forge/azuredevops", t.Name())
		}
	})

	baseURL := azureFixtureValue(t, "base-url", func() string {
		return cmp.Or(os.Getenv("AZURE_DEVOPS_URL"), DefaultURL)
	}, _azureFixtureBaseURL)
	organization := azureFixtureValue(t, "organization", func() string {
		return requireEnv(t, "AZURE_DEVOPS_ORGANIZATION")
	}, _azureFixtureOrg)
	project := azureFixtureValue(t, "project", func() string {
		return requireEnv(t, "AZURE_DEVOPS_PROJECT")
	}, _azureFixtureProject)
	repository := azureFixtureValue(t, "repository", func() string {
		return requireEnv(t, "AZURE_DEVOPS_REPOSITORY")
	}, _azureFixtureRepo)
	reviewer := azureFixtureValue(t, "reviewer", func() string {
		return requireEnv(t, "AZURE_DEVOPS_REVIEWER")
	}, _azureFixtureReviewer)
	authToken := integrationAuthenticationToken(t)

	var currentUser azureCurrentUser
	if forgetest.Update() {
		currentUser = currentAuthenticatedUser(t, baseURL, organization, authToken)
	}
	reviewerID := azureFixtureValue(t, "reviewer-id", func() string {
		if id := os.Getenv("AZURE_DEVOPS_REVIEWER_ID"); id != "" {
			return id
		}
		return currentUser.ID
	}, _azureFixtureReviewerID)
	remoteURL := azureFixtureValue(t, "remote-url", func() string {
		if remoteURL := os.Getenv("AZURE_DEVOPS_REMOTE_URL"); remoteURL != "" {
			return remoteURL
		}
		return fmt.Sprintf(
			"%s/%s/%s/_git/%s",
			baseURL, organization, project, repository,
		)
	}, fmt.Sprintf(
		"%s/%s/%s/_git/%s",
		_azureFixtureBaseURL,
		_azureFixtureOrg,
		_azureFixtureProject,
		_azureFixtureRepo,
	))

	azureForge := Forge{
		Options: Options{URL: baseURL},
		Log:     silogtest.New(t),
	}
	repoID := &RepositoryID{
		url:          baseURL,
		organization: organization,
		project:      project,
		repository:   repository,
	}

	setIntegrationGitEnv(t, remoteURL, authToken)
	scrubber := newAzureFixtureScrubber(azureFixtureScrubConfig{
		BaseURL:      baseURL,
		Organization: organization,
		Project:      project,
		Repository:   repository,
		Reviewer:     reviewer,
		ReviewerID:   reviewerID,
		ReviewerDisplayName: cmp.Or(
			os.Getenv("AZURE_DEVOPS_REVIEWER_DISPLAY_NAME"),
			currentUser.DisplayName,
		),
		RemoteURL: remoteURL,
	})
	if forgetest.Update() {
		t.Cleanup(func() {
			if !t.Failed() {
				scrubAzureFixtures(t, scrubber)
			}
		})
	}

	forgetest.RunIntegration(t, forgetest.IntegrationConfig{
		RemoteURL:      remoteURL,
		Forge:          &azureForge,
		SkipAssignees:  true,
		// Azure DevOps does not support PR assignees,
		// so the combined reviewer+assignee scenario cannot be exercised.
		SkipCombinedMetadata: true,
		// TODO: Record fixtures for the shared mergeability scenario
		// and remove this once Azure DevOps credentials are available.
		SkipMergeability:      true,
		Reviewers:             []string{reviewer},
		SetCommentsPageSize:   func(testing.TB, int) {},
		BaseBranchMayBeAbsent: false,
		OpenRepository: func(t *testing.T, httpClient *http.Client) forge.Repository {
			client, err := newAzureDevOpsClientWithHTTPClient(
				t.Context(),
				baseURL+"/"+organization,
				authToken,
				httpClient,
			)
			require.NoError(t, err)

			repo, err := newRepository(
				t.Context(),
				&azureForge,
				repoID,
				silogtest.New(t),
				client,
			)
			require.NoError(t, err)
			repo.reviewerIDs = map[string]string{reviewer: reviewerID}
			return repo
		},
		MergeChange: func(t *testing.T, repo forge.Repository, change forge.ChangeID) {
			require.NoError(t, mergeChange(t, repo.(*Repository), change.(*PR)))
		},
		CloseChange: func(t *testing.T, repo forge.Repository, change forge.ChangeID) {
			require.NoError(t, closeChange(t, repo.(*Repository), change.(*PR)))
		},
	})
}

func mergeChange(t *testing.T, repo *Repository, pr *PR) error {
	t.Helper()

	pullRequest, err := repo.client.gitClient.GetPullRequest(
		t.Context(),
		git.GetPullRequestArgs{
			Project:       strPtr(repo.project()),
			RepositoryId:  strPtr(repo.repositoryID()),
			PullRequestId: &pr.Number,
		},
	)
	if err != nil {
		return fmt.Errorf("get pull request: %w", err)
	}

	deleteSourceBranch := false
	mergeStrategy := git.GitPullRequestMergeStrategyValues.NoFastForward
	status := git.PullRequestStatusValues.Completed
	_, err = repo.client.gitClient.UpdatePullRequest(
		t.Context(),
		git.UpdatePullRequestArgs{
			Project:       strPtr(repo.project()),
			RepositoryId:  strPtr(repo.repositoryID()),
			PullRequestId: &pr.Number,
			GitPullRequestToUpdate: &git.GitPullRequest{
				Status:                &status,
				LastMergeSourceCommit: pullRequest.LastMergeSourceCommit,
				CompletionOptions: &git.GitPullRequestCompletionOptions{
					DeleteSourceBranch: &deleteSourceBranch,
					MergeStrategy:      &mergeStrategy,
				},
			},
		},
	)
	if err != nil {
		return fmt.Errorf("update pull request: %w", err)
	}

	for range 30 {
		pullRequest, err := repo.client.gitClient.GetPullRequest(
			t.Context(),
			git.GetPullRequestArgs{
				Project:       strPtr(repo.project()),
				RepositoryId:  strPtr(repo.repositoryID()),
				PullRequestId: &pr.Number,
			},
		)
		if err != nil {
			return fmt.Errorf("get pull request: %w", err)
		}
		if pullRequest.Status != nil &&
			*pullRequest.Status == git.PullRequestStatusValues.Completed {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("pull request %d was not completed", pr.Number)
}

func closeChange(t *testing.T, repo *Repository, pr *PR) error {
	t.Helper()

	status := git.PullRequestStatusValues.Abandoned
	_, err := repo.client.gitClient.UpdatePullRequest(
		t.Context(),
		git.UpdatePullRequestArgs{
			Project:       strPtr(repo.project()),
			RepositoryId:  strPtr(repo.repositoryID()),
			PullRequestId: &pr.Number,
			GitPullRequestToUpdate: &git.GitPullRequest{
				Status: &status,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("update pull request: %w", err)
	}
	return nil
}

func azureIntegrationFixturesExist() bool {
	_, err := os.Stat("testdata/fixtures/TestIntegration/SubmitEditChange.yaml")
	return err == nil
}

type azureFixtureScrubConfig struct {
	BaseURL             string
	Organization        string
	Project             string
	Repository          string
	Reviewer            string
	ReviewerID          string
	ReviewerDisplayName string
	RemoteURL           string
}

type azureFixtureScrubber struct {
	replacer         *strings.Replacer
	uuidRE           *regexp.Regexp
	vsspsHostRE      *regexp.Regexp
	descriptorRE     *regexp.Regexp
	uuidReplacements map[string]string
	nextUUID         int
}

func newAzureFixtureScrubber(cfg azureFixtureScrubConfig) *azureFixtureScrubber {
	pairs := azureFixtureReplacementPairs(cfg)
	return &azureFixtureScrubber{
		replacer: strings.NewReplacer(pairs...),
		uuidRE: regexp.MustCompile(
			`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`,
		),
		vsspsHostRE: regexp.MustCompile(
			`https://[^/"\\]+\.vssps\.visualstudio\.com`,
		),
		descriptorRE: regexp.MustCompile(
			`\b(?:aad|imp|msa|svc)\.[A-Za-z0-9_-]+`,
		),
		uuidReplacements: map[string]string{
			strings.ToLower(cfg.ReviewerID): _azureFixtureReviewerID,
		},
	}
}

func azureFixtureReplacementPairs(cfg azureFixtureScrubConfig) []string {
	var pairs []string
	addReplacement := func(realValue, placeholder string) {
		if realValue == "" || realValue == placeholder {
			return
		}
		pairs = append(pairs, realValue, placeholder)
		pairs = append(pairs,
			url.QueryEscape(realValue),
			url.QueryEscape(placeholder),
		)
	}

	addReplacement(cfg.BaseURL, _azureFixtureBaseURL)
	if u, err := url.Parse(cfg.BaseURL); err == nil && u.Host != "" {
		placeholderURL, _ := url.Parse(_azureFixtureBaseURL)
		addReplacement(u.Host, placeholderURL.Host)
	}
	addReplacement(cfg.Organization, _azureFixtureOrg)
	addReplacement(cfg.Project, _azureFixtureProject)
	addReplacement(cfg.Repository, _azureFixtureRepo)
	addReplacement(cfg.Reviewer, _azureFixtureReviewer)
	addReplacement(cfg.ReviewerDisplayName, _azureFixtureDisplayName)
	addReplacement(cfg.RemoteURL, fmt.Sprintf(
		"%s/%s/%s/_git/%s",
		_azureFixtureBaseURL,
		_azureFixtureOrg,
		_azureFixtureProject,
		_azureFixtureRepo,
	))
	return pairs
}

func (s *azureFixtureScrubber) Scrub(i *cassette.Interaction) error {
	i.Request.Host = s.scrubString(i.Request.Host)
	i.Request.URL = s.scrubString(i.Request.URL)
	i.Request.Body = s.scrubString(i.Request.Body)
	i.Request.ContentLength = int64(len(i.Request.Body))
	s.scrubForm(i.Request.Form)
	s.scrubHeaders(i.Request.Headers)

	if i.Request.Method == http.MethodOptions {
		i.Response.Body = s.scrubStringWithoutUUIDs(i.Response.Body)
	} else {
		i.Response.Body = s.scrubString(i.Response.Body)
	}
	if i.Response.ContentLength >= 0 {
		i.Response.ContentLength = int64(len(i.Response.Body))
	}
	s.scrubHeaders(i.Response.Headers)
	return nil
}

func (s *azureFixtureScrubber) scrubString(value string) string {
	value = s.scrubStringWithoutUUIDs(value)
	return s.uuidRE.ReplaceAllStringFunc(value, s.uuidReplacement)
}

func (s *azureFixtureScrubber) scrubStringWithoutUUIDs(value string) string {
	value = s.replacer.Replace(value)
	value = s.vsspsHostRE.ReplaceAllString(value, "https://vssps.azure.example.com")
	return s.descriptorRE.ReplaceAllString(value, "msa.REVIEWER_DESCRIPTOR")
}

func (s *azureFixtureScrubber) scrubForm(form url.Values) {
	for key, values := range form {
		delete(form, key)
		newKey := s.scrubString(key)
		for _, value := range values {
			form.Add(newKey, s.scrubString(value))
		}
	}
}

func (s *azureFixtureScrubber) scrubHeaders(headers http.Header) {
	for key, values := range headers {
		for i, value := range values {
			values[i] = s.scrubString(value)
		}
		headers[key] = values
	}
}

func (s *azureFixtureScrubber) uuidReplacement(value string) string {
	key := strings.ToLower(value)
	if strings.HasPrefix(key, "00000000-0000-4000-8000-") {
		return key
	}
	if replacement, ok := s.uuidReplacements[key]; ok {
		return replacement
	}

	s.nextUUID++
	replacement := fmt.Sprintf(
		"00000000-0000-4000-8000-%012d",
		s.nextUUID,
	)
	s.uuidReplacements[key] = replacement
	return replacement
}

func scrubAzureFixtures(t *testing.T, scrubber *azureFixtureScrubber) {
	t.Helper()

	root := filepath.Join("testdata", "fixtures")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".yaml" {
			return err
		}

		cassetteName := strings.TrimSuffix(path, ".yaml")
		cassetteFile, err := cassette.Load(cassetteName)
		if err != nil {
			return err
		}
		for _, interaction := range cassetteFile.Interactions {
			if err := scrubber.Scrub(interaction); err != nil {
				return err
			}
		}
		cassetteFile.MarshalFunc = yaml.Marshal
		return cassetteFile.Save()
	})
	require.NoError(t, err)
}

func integrationAuthenticationToken(t *testing.T) *AuthenticationToken {
	t.Helper()

	if pat := os.Getenv("AZURE_DEVOPS_PAT"); pat != "" {
		return &AuthenticationToken{
			AccessToken: pat,
			AuthType:    AuthTypePAT,
		}
	}

	if forgetest.Update() {
		t.Fatal("AZURE_DEVOPS_PAT must be set when recording Azure DevOps fixtures")
	}

	return &AuthenticationToken{
		AccessToken: "token",
		AuthType:    AuthTypePAT,
	}
}

func setIntegrationGitEnv(t *testing.T, remoteURL string, token *AuthenticationToken) {
	t.Helper()

	if !forgetest.Update() || token.AuthType != AuthTypePAT {
		return
	}

	header := "AUTHORIZATION: " +
		"Basic " + base64.StdEncoding.EncodeToString([]byte(":"+token.AccessToken))
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv(
		"GIT_CONFIG_KEY_0",
		"http."+strings.TrimRight(remoteURL, "/")+".extraheader",
	)
	t.Setenv("GIT_CONFIG_VALUE_0", header)
}

type azureCurrentUser struct {
	ID          string
	DisplayName string
}

func currentAuthenticatedUser(
	t *testing.T,
	baseURL string,
	organization string,
	token *AuthenticationToken,
) azureCurrentUser {
	t.Helper()

	req, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		strings.TrimRight(baseURL, "/")+"/"+organization+
			"/_apis/connectionData?api-version=7.1-preview.1",
		nil,
	)
	require.NoError(t, err)
	req.Header.Set(
		"Authorization",
		"Basic "+base64.StdEncoding.EncodeToString([]byte(":"+token.AccessToken)),
	)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, resp.Body.Close())
	}()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		AuthenticatedUser struct {
			ID                  string `json:"id"`
			ProviderDisplayName string `json:"providerDisplayName"`
		} `json:"authenticatedUser"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.NotEmpty(t, body.AuthenticatedUser.ID)
	return azureCurrentUser{
		ID:          body.AuthenticatedUser.ID,
		DisplayName: body.AuthenticatedUser.ProviderDisplayName,
	}
}

func requireEnv(t testing.TB, key string) string {
	t.Helper()

	value := os.Getenv(key)
	if value == "" {
		t.Fatalf("%s must be set when recording Azure DevOps fixtures", key)
	}
	return value
}
