package forgetest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/fixturetest"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
)

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

	// Verify statuses.
	statuses, err := repo.ChangeStatuses(t.Context(), []forge.ChangeID{
		openChange.ID,
		mergedChange.ID,
		closedChange.ID,
	})
	require.NoError(t, err, "error fetching change states")
	assert.Equal(t, forge.ChangeOpen, statuses[0].State)
	assert.Equal(t, forge.ChangeMerged, statuses[1].State)
	assert.Equal(t, forge.ChangeClosed, statuses[2].State)
	assert.NotEmpty(t, statuses[0].HeadHash)
	assert.NotEmpty(t, statuses[1].HeadHash)
	assert.NotEmpty(t, statuses[2].HeadHash)
}

// TestChangeChecks verifies that forges report checks
// for newly submitted changes.
func (s *integrationSuite) TestChangeChecks(t *testing.T) {
	tests := []struct {
		name string
		want forge.ChangeCheck
	}{
		{
			name: "Pending",
			want: forge.ChangeCheck{
				Name:  "git-spice integration pending",
				State: forge.ChangeCheckPending,
			},
		},
		{
			name: "Passed",
			want: forge.ChangeCheck{
				Name:  "git-spice integration passed",
				State: forge.ChangeCheckPassed,
			},
		},
		{
			name: "Failed",
			want: forge.ChangeCheck{
				Name:  "git-spice integration failed",
				State: forge.ChangeCheckFailed,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			branchFixture := fixturetest.New(s.Fixtures, "branch", func() string {
				return randomString(8)
			})
			headHashFixture, setHeadHash := fixturetest.Stored[string](
				s.Fixtures,
				"headHash",
			)

			branch := branchFixture.Get(t)
			if Update() {
				testRepo := newTestRepository(t, s.RemoteURL)
				testRepo.CheckoutBranch("main")
				testRepo.CreateBranch(branch)
				testRepo.CheckoutBranch(branch)
				testRepo.WriteFile(branch+".txt", randomString(32))
				hash := testRepo.AddAllAndCommit("commit for checks " + tt.name)
				testRepo.Push(branch)
				setHeadHash(hash.String())

				t.Cleanup(func() {
					testRepo.DeleteRemoteBranch(branch)
				})
			}

			httpClient := s.HTTPClient(t)
			repo := s.openRepository(t, httpClient)
			headHash := git.Hash(headHashFixture.Get(t))

			change, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
				Subject: "Checks " + branch,
				Body:    "Checks state test",
				Base:    "main",
				Head:    branch,
			})
			require.NoError(t, err, "error creating change")

			s.SetChangeCheck(
				t,
				httpClient,
				repo,
				change.ID,
				headHash,
				tt.want,
			)

			got, err := repo.ChangeChecks(t.Context(), change.ID)
			require.NoError(t, err, "error fetching checks")
			assert.Equal(t, []forge.ChangeCheck{tt.want}, got)
		})
	}
}

// TestChangeMergeability verifies that forges report mergeability
// independently from the CI/check status surface.
func (s *integrationSuite) TestChangeMergeability(
	t *testing.T,
	includeDraft bool,
) {
	t.Run("Ready", func(t *testing.T) {
		t.Parallel()

		s.testChangeMergeabilityReady(t)
	})

	t.Run("Conflicts", func(t *testing.T) {
		t.Parallel()

		s.testChangeMergeabilityConflicts(t)
	})

	if includeDraft {
		t.Run("Draft", func(t *testing.T) {
			t.Parallel()

			s.testChangeMergeabilityDraft(t)
		})
	}
}

func (s *integrationSuite) testChangeMergeabilityReady(t *testing.T) {
	baseName := fixturetest.New(s.Fixtures, "base", func() string {
		return "ready-base-" + randomString(8)
	}).Get(t)
	headName := fixturetest.New(s.Fixtures, "head", func() string {
		return "ready-head-" + randomString(8)
	}).Get(t)

	if Update() {
		testRepo := newTestRepository(t, s.RemoteURL)
		testRepo.CheckoutBranch("main")
		testRepo.Push("main:" + baseName)
		t.Cleanup(func() {
			testRepo.DeleteRemoteBranch(baseName)
		})

		testRepo.CreateBranch(headName)
		testRepo.CheckoutBranch(headName)
		testRepo.WriteFile(headName+".txt", randomString(32))
		testRepo.AddAllAndCommit("commit for mergeability ready")
		testRepo.Push(headName)
		t.Cleanup(func() {
			testRepo.DeleteRemoteBranch(headName)
		})
	}

	repo := s.OpenRepository(t)

	change, err := repo.SubmitChange(
		t.Context(),
		forge.SubmitChangeRequest{
			Subject: "Mergeability " + headName,
			Body:    "Mergeability scenario test",
			Base:    baseName,
			Head:    headName,
		},
	)
	require.NoError(t, err, "error creating change")
	if Update() {
		t.Cleanup(func() {
			s.CloseChange(t, repo, change.ID)
		})
	}

	var got forge.ChangeMergeability
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var err error
		got, err = repo.ChangeMergeability(t.Context(), change.ID)
		require.NoError(c, err, "error fetching mergeability")
		assert.Equal(c, forge.ChangeMergeability{
			State: forge.ChangeMergeabilityReady,
		}, got)
	}, 30*time.Second, 500*time.Millisecond)
}

func (s *integrationSuite) testChangeMergeabilityConflicts(t *testing.T) {
	baseName := fixturetest.New(s.Fixtures, "base", func() string {
		return "conflict-base-" + randomString(8)
	}).Get(t)
	headName := fixturetest.New(s.Fixtures, "head", func() string {
		return "conflict-head-" + randomString(8)
	}).Get(t)

	if Update() {
		testRepo := newTestRepository(t, s.RemoteURL)
		testRepo.CheckoutBranch("main")
		testRepo.Push("main:" + baseName)
		t.Cleanup(func() {
			testRepo.DeleteRemoteBranch(baseName)
		})

		testRepo.CreateBranch(headName)
		testRepo.CheckoutBranch(headName)
		testRepo.WriteFile("mergeability-conflict.txt", "head "+randomString(32))
		testRepo.AddAllAndCommit("commit conflicting head")
		testRepo.Push(headName)
		t.Cleanup(func() {
			testRepo.DeleteRemoteBranch(headName)
		})

		testRepo.CheckoutBranch("main")
		testRepo.WriteFile("mergeability-conflict.txt", "base "+randomString(32))
		testRepo.AddAllAndCommit("commit conflicting base")
		testRepo.Push("HEAD:" + baseName)
	}

	repo := s.OpenRepository(t)

	change, err := repo.SubmitChange(
		t.Context(),
		forge.SubmitChangeRequest{
			Subject: "Mergeability " + headName,
			Body:    "Mergeability scenario test",
			Base:    baseName,
			Head:    headName,
		},
	)
	require.NoError(t, err, "error creating change")
	if Update() {
		t.Cleanup(func() {
			s.CloseChange(t, repo, change.ID)
		})
	}

	var got forge.ChangeMergeability
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var err error
		got, err = repo.ChangeMergeability(t.Context(), change.ID)
		require.NoError(c, err, "error fetching mergeability")
		assert.Equal(c, forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonConflicts,
		}, got)
	}, 30*time.Second, 500*time.Millisecond)
}

func (s *integrationSuite) testChangeMergeabilityDraft(t *testing.T) {
	baseName := fixturetest.New(s.Fixtures, "base", func() string {
		return "draft-base-" + randomString(8)
	}).Get(t)
	headName := fixturetest.New(s.Fixtures, "head", func() string {
		return "draft-head-" + randomString(8)
	}).Get(t)

	if Update() {
		testRepo := newTestRepository(t, s.RemoteURL)
		testRepo.CheckoutBranch("main")
		testRepo.Push("main:" + baseName)
		t.Cleanup(func() {
			testRepo.DeleteRemoteBranch(baseName)
		})

		testRepo.CreateBranch(headName)
		testRepo.CheckoutBranch(headName)
		testRepo.WriteFile(headName+".txt", randomString(32))
		testRepo.AddAllAndCommit("commit for mergeability draft")
		testRepo.Push(headName)
		t.Cleanup(func() {
			testRepo.DeleteRemoteBranch(headName)
		})
	}

	repo := s.OpenRepository(t)

	change, err := repo.SubmitChange(
		t.Context(),
		forge.SubmitChangeRequest{
			Subject: "Mergeability " + headName,
			Body:    "Mergeability scenario test",
			Base:    baseName,
			Head:    headName,
			Draft:   true,
		},
	)
	require.NoError(t, err, "error creating change")
	if Update() {
		t.Cleanup(func() {
			s.CloseChange(t, repo, change.ID)
		})
	}

	var got forge.ChangeMergeability
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var err error
		got, err = repo.ChangeMergeability(t.Context(), change.ID)
		require.NoError(c, err, "error fetching mergeability")
		assert.Equal(c, forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonDraft,
		}, got)
	}, 30*time.Second, 500*time.Millisecond)
}

// FindChangesByBranch returns no error, and an empty slice
// when the branch does not exist.
func (s *integrationSuite) TestFindChangesByBranchDoesNotExist(t *testing.T) {
	repo := s.OpenRepository(t)

	changes, err := repo.FindChangesByBranch(t.Context(), "does-not-exist", forge.FindChangesOptions{})
	require.NoError(t, err, "should not error for non-existent branch")
	assert.Empty(t, changes, "should return empty slice for non-existent branch")
}

// TestSubmitChangeFromPushRepository verifies that a forge can create
// and discover a change whose head branch belongs to a fork repository.
