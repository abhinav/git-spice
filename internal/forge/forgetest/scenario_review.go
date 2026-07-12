package forgetest

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/fixturetest"
	"go.abhg.dev/gs/internal/forge"
)

func (s *integrationSuite) TestSubmitCombinedMetadata(t *testing.T) {
	require.NotEmpty(t, s.Reviewers, "test requires at least one reviewer")
	require.NotEmpty(t, s.Assignees, "test requires at least one assignee")

	branchFixture := fixturetest.New(s.Fixtures, "branch-combined-metadata", func() string {
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
	_, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject:   "Testing " + branchName,
		Body:      "Test PR with combined metadata",
		Base:      "main",
		Head:      branchName,
		Reviewers: s.Reviewers[:1],
		Assignees: s.Assignees[:1],
	})
	require.NoError(t, err, "error creating PR")

	foundChanges, err := repo.FindChangesByBranch(t.Context(), branchName, forge.FindChangesOptions{})
	require.NoError(t, err)
	require.Len(t, foundChanges, 1, "expected exactly one change")
	assert.Equal(t, s.Reviewers[:1], foundChanges[0].Reviewers)
	assert.Equal(t, s.Assignees[:1], foundChanges[0].Assignees)
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
	if !s.skipCommentPagination {
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
	}

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

// TestCommentCountsByChange tests the CommentCountsByChange method.
// This test creates a PR and verifies that comment counts can be retrieved.
// Note: Creating resolvable review threads requires forge-specific operations,
// so this test verifies the method works but may return zero counts.
func (s *integrationSuite) TestCommentCountsByChange(t *testing.T) {
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
		Body:    "Test PR for comment counts",
		Base:    "main",
		Head:    branchName,
	})
	require.NoError(t, err, "error creating PR")

	// Call CommentCountsByChange with the new change.
	counts, err := repo.CommentCountsByChange(t.Context(), []forge.ChangeID{change.ID})
	require.NoError(t, err, "error getting comment counts")
	require.Len(t, counts, 1, "expected one result")

	// Verify the counts structure is valid.
	// We can't guarantee there are comments, but the counts should be non-negative.
	result := counts[0]
	require.NotNil(t, result, "comment counts should not be nil")
	assert.GreaterOrEqual(t, result.Total, 0, "total should be non-negative")
	assert.GreaterOrEqual(t, result.Resolved, 0, "resolved should be non-negative")
	assert.GreaterOrEqual(t, result.Unresolved, 0, "unresolved should be non-negative")
	assert.Equal(t, result.Total, result.Resolved+result.Unresolved,
		"total should equal resolved + unresolved")
}

// testRepository manages a local Git repository clone for testing.
// Only available in update mode.
