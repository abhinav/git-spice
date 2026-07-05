package forgetest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/fixturetest"
	"go.abhg.dev/gs/internal/forge"
)

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
		s.assertHashMatch(t, commitHash, foundChange.HeadHash.String(),
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
		s.assertHashMatch(t, commitHash, foundChange.HeadHash.String(),
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
