// Package forgetest implements utilities for testing Forge implementations.
package forgetest

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/fixturetest"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/httptest"
)

type (
	// MergeChangeFunc merges a change with provider-specific test setup.
	//
	// Leave IntegrationConfig.MergeChange unset to use
	// forge.Repository.MergeChange directly.
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

	// SetChangeCheckFunc sets a synthetic check for a change.
	SetChangeCheckFunc func(
		t *testing.T,
		httpClient *http.Client,
		repo forge.Repository,
		changeID forge.ChangeID,
		headHash git.Hash,
		check forge.ChangeCheck,
	)
)

// IntegrationConfig configures a forge integration test run.
type IntegrationConfig struct {
	// RemoteURL is the Git remote URL to clone in update mode.
	// Example: "https://github.com/abhinav/test-repo"
	RemoteURL string // required

	// PushRemoteURL is the Git remote URL for a fork repository
	// used to test cross-repository change requests.
	//
	// If empty, fork integration tests are skipped.
	PushRemoteURL string // optional

	// Forge is the forge being tested.
	Forge forge.Forge // required

	// OpenRepository creates a forge.Repository for testing.
	// It receives an HTTP client to wrap as needed for the forge implementation.
	OpenRepository func(*testing.T, *http.Client) forge.Repository // required

	// MergeChange merges a change with provider-specific test setup.
	//
	// If nil, the integration suite uses forge.Repository.MergeChange.
	MergeChange MergeChangeFunc // optional

	// CloseChange closes a change without merging.
	CloseChange CloseChangeFunc // required

	// SetChangeCheck sets a synthetic check for a change.
	SetChangeCheck SetChangeCheckFunc // optional

	// SkipMergeability skips shared mergeability integration tests.
	//
	// The tests are enabled by default.
	// Set to true only for forges that do not support mergeability.
	SkipMergeability bool // optional

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

	// SkipLabels skips label-related tests.
	// Set to true for forges that don't support labels (e.g., Bitbucket).
	SkipLabels bool // optional

	// SkipAssignees skips assignee-related tests.
	// Set to true for forges that don't support assignees (e.g., Bitbucket).
	SkipAssignees bool // optional

	// SkipTemplates skips template-related tests.
	// Set to true for forges that don't support PR templates.
	SkipTemplates bool // optional

	// SkipDraft skips draft-related tests.
	// Set to true for forges with limited draft support.
	SkipDraft bool // optional

	// ShortHeadHash indicates the forge returns truncated commit hashes.
	// When true, hash comparisons use prefix matching.
	ShortHeadHash bool // optional

	// SkipReviewers skips reviewer-related tests.
	// Set to true for forges where user lookup by username doesn't work.
	SkipReviewers bool // optional

	// SkipMerge skips merge-related tests in ChangeStatuses.
	// Set to true for forges that require approvals before merge.
	SkipMerge bool // optional

	// SkipCommentPagination skips the ListAllComments pagination test.
	// Set to true for forges where comment listing with small page sizes fails.
	SkipCommentPagination bool // optional

	// Sanitizers are applied to recorded HTTP fixtures.
	// Use ConfigSanitizers to create sanitizers from test configuration.
	Sanitizers []httptest.Sanitizer // optional

	// SkipCommentCounts skips the CommentCountsByChange test.
	// Set to true for forges that don't support comment resolution tracking.
	SkipCommentCounts bool // optional
}

// RunIntegration runs integration tests with the given configuration.
func RunIntegration(t *testing.T, config IntegrationConfig) {
	mergeChange := config.MergeChange
	if mergeChange == nil {
		mergeChange = func(
			t *testing.T,
			repo forge.Repository,
			changeID forge.ChangeID,
		) {
			require.NoError(t, repo.MergeChange(
				t.Context(), changeID, forge.MergeChangeOptions{},
			))
		}
	}

	suite := &integrationSuite{
		Forge: config.Forge,
		Fixtures: fixturetest.Config{
			Update: Update,
		},
		RemoteURL:             config.RemoteURL,
		PushRemoteURL:         config.PushRemoteURL,
		openRepository:        config.OpenRepository,
		MergeChange:           mergeChange,
		CloseChange:           config.CloseChange,
		SetChangeCheck:        config.SetChangeCheck,
		Reviewers:             config.Reviewers,
		Assignees:             config.Assignees,
		SetCommentsPageSize:   config.SetCommentsPageSize,
		Sanitizers:            config.Sanitizers,
		shortHeadHash:         config.ShortHeadHash,
		skipReviewers:         config.SkipReviewers,
		skipMerge:             config.SkipMerge,
		skipCommentPagination: config.SkipCommentPagination,
		skipCommentCounts:     config.SkipCommentCounts,
	}

	t.Run("SubmitEditChange", func(t *testing.T) {
		t.Parallel()

		suite.TestSubmitEditChange(t)
	})

	t.Run("SubmitEditBase", func(t *testing.T) {
		t.Parallel()

		suite.TestSubmitChangeBase(t)
	})

	if !config.SkipDraft {
		t.Run("SubmitEditDraft", func(t *testing.T) {
			t.Parallel()

			suite.TestSubmitChangeDraft(t)
		})
	}

	if !config.SkipMerge {
		t.Run("ChangesStates", func(t *testing.T) {
			t.Parallel()

			suite.TestChangeStates(t)
		})
	}

	if config.SetChangeCheck != nil {
		// Keep the pre-rename subtest name so existing VCR fixture paths
		// remain valid until the fixtures are re-recorded.
		t.Run("ChangeChecksState", func(t *testing.T) {
			t.Parallel()

			suite.TestChangeChecks(t)
		})
	}

	if !config.SkipMergeability {
		t.Run("ChangeMergeability", func(t *testing.T) {
			t.Parallel()

			suite.TestChangeMergeability(t, !config.SkipDraft)
		})
	}

	t.Run("FindChangesByBranchDoesNotExist", func(t *testing.T) {
		t.Parallel()

		suite.TestFindChangesByBranchDoesNotExist(t)
	})

	if config.PushRemoteURL != "" {
		t.Run("SubmitChangeFromPushRepository", func(t *testing.T) {
			t.Parallel()

			suite.TestSubmitChangeFromPushRepository(t)
		})
	}

	// NOTE: ListChangeTemplates cannot run in parallel
	// because it modifies the main branch.
	if !config.SkipTemplates {
		t.Run("ListChangeTemplates", func(t *testing.T) {
			suite.TestListChangeTemplates(t)
		})
	}

	if !config.SkipLabels {
		t.Run("SubmitEditLabels", func(t *testing.T) {
			t.Parallel()

			suite.TestSubmitEditLabels(t)
		})
	}

	if !config.BaseBranchMayBeAbsent {
		t.Run("SubmitBaseDoesNotExist", func(t *testing.T) {
			t.Parallel()

			suite.TestSubmitBaseDoesNotExist(t)
		})
	}

	if !config.SkipReviewers {
		t.Run("SubmitEditReviewers", func(t *testing.T) {
			t.Parallel()

			suite.TestSubmitEditReviewers(t)
		})
	}

	if !config.SkipAssignees {
		t.Run("SubmitEditAssignees", func(t *testing.T) {
			t.Parallel()

			suite.TestSubmitEditAssignees(t)
		})
	}

	t.Run("ChangeComments", func(t *testing.T) {
		t.Parallel()

		suite.TestChangeComments(t)
	})

	if !config.SkipCommentCounts {
		t.Run("CommentCountsByChange", func(t *testing.T) {
			t.Parallel()

			suite.TestCommentCountsByChange(t)
		})
	}
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

	// PushRemoteURL is the Git remote URL for a fork repository.
	PushRemoteURL string

	// MergeChange merges a change.
	MergeChange MergeChangeFunc

	// CloseChange closes a change without merging.
	CloseChange CloseChangeFunc

	// SetChangeCheck sets a synthetic check for a change.
	SetChangeCheck SetChangeCheckFunc

	// Reviewers is a list of usernames that can be added as reviewers to changes.
	Reviewers []string

	// Assignees is a list of usernames that can be assigned to changes.
	Assignees []string

	// SetCommentsPageSize sets the page size for listing comments.
	SetCommentsPageSize func(testing.TB, int)

	// Sanitizers are applied to recorded HTTP fixtures.
	Sanitizers []httptest.Sanitizer

	// shortHeadHash indicates the forge returns truncated commit hashes.
	shortHeadHash bool

	// skipReviewers skips reviewer-related tests.
	skipReviewers bool

	// skipMerge skips merge-related tests.
	skipMerge bool

	// skipCommentPagination skips pagination test.
	skipCommentPagination bool

	// skipCommentCounts skips comment counts test.
	skipCommentCounts bool

	openRepository func(*testing.T, *http.Client) forge.Repository
}

// HTTPClient creates an HTTP client for use in the given test.
// In Update mode, it records HTTP interactions to fixtures.
// In non-update mode, it replays from existing fixtures.
func (s *integrationSuite) HTTPClient(t *testing.T) *http.Client {
	rec := NewHTTPRecorder(t, t.Name(), s.Sanitizers)
	return rec.GetDefaultClient()
}

// OpenRepository creates a forge.Repository for testing.
// It receives an HTTP client to wrap as needed for the forge implementation.
func (s *integrationSuite) OpenRepository(t *testing.T) forge.Repository {
	httpClient := s.HTTPClient(t)
	return s.openRepository(t, httpClient)
}

// assertHashMatch asserts that two hashes match.
// If shortHeadHash is set, it uses prefix matching (API returns short hash).
func (s *integrationSuite) assertHashMatch(t *testing.T, expected, actual, msg string) {
	t.Helper()
	if s.shortHeadHash {
		assert.True(t, strings.HasPrefix(expected, actual),
			"%s: expected %q to be prefix of %q", msg, actual, expected)
	} else {
		assert.Equal(t, expected, actual, msg)
	}
}
