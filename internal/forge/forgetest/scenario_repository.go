package forgetest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/fixturetest"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git/giturl"
)

func (s *integrationSuite) TestSubmitChangeFromPushRepository(t *testing.T) {
	branchFixture := fixturetest.New(s.Fixtures, "fork-branch", func() string {
		return randomString(8)
	})
	commitHashFixture, setCommitHash := fixturetest.Stored[string](
		s.Fixtures,
		"forkCommitHash",
	)

	branchName := branchFixture.Get(t)
	t.Logf("Creating fork branch: %s", branchName)
	if Update() {
		testRepo := newTestRepository(t, s.RemoteURL)

		testRepo.CreateBranch(branchName)
		testRepo.CheckoutBranch(branchName)
		testRepo.WriteFile(branchName+".txt", randomString(32))
		hash := testRepo.AddAllAndCommit("commit from fork test")
		testRepo.AddRemote("pushrepo", s.PushRemoteURL)
		testRepo.PushTo("pushrepo", branchName)
		setCommitHash(hash.String())

		t.Cleanup(func() {
			testRepo.DeleteRemoteBranchFrom("pushrepo", branchName)
		})
	}
	commitHash := commitHashFixture.Get(t)

	remoteURL, err := giturl.Parse(s.PushRemoteURL)
	require.NoError(t, err, "parse push repository URL")

	pushRepository, err := s.Forge.ParseRepositoryPath(remoteURL.Path)
	require.NoError(t, err, "parse push repository URL")

	repo := s.OpenRepository(t)
	change, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject:        "Testing fork " + branchName,
		Body:           "Test fork change request",
		Base:           "main",
		Head:           branchName,
		PushRepository: pushRepository,
	})
	require.NoError(t, err, "error creating fork change request")

	t.Run("FindChangeByID", func(t *testing.T) {
		foundChange, err := repo.FindChangeByID(t.Context(), change.ID)
		require.NoError(t, err, "error finding change by ID")
		s.assertHashMatch(t, commitHash, foundChange.HeadHash.String(),
			"head hash should match fork commit")
	})

	t.Run("FindChangesByBranch", func(t *testing.T) {
		changes, err := repo.FindChangesByBranch(t.Context(), branchName,
			forge.FindChangesOptions{
				PushRepository: pushRepository,
			})
		require.NoError(t, err, "error finding change by fork branch")
		require.Len(t, changes, 1, "expected exactly one change")

		foundChange := changes[0]
		assert.Equal(t, change.ID, foundChange.ID, "ID should match")
		s.assertHashMatch(t, commitHash, foundChange.HeadHash.String(),
			"head hash should match fork commit")
	})

	t.Run("FindChangesByBranchDefaultsToTargetRepository", func(t *testing.T) {
		changes, err := repo.FindChangesByBranch(t.Context(), branchName,
			forge.FindChangesOptions{})
		require.NoError(t, err, "error finding change by branch")
		assert.Empty(t, changes,
			"fork change should not match target repository default")
	})
}

func (s *integrationSuite) TestListChangeTemplates(t *testing.T) {
	templatePaths := s.Forge.ChangeTemplatePaths()
	require.NotEmpty(t, templatePaths, "forge must have template paths")

	t.Run("NoTemplates", func(t *testing.T) {
		if Update() {
			testRepo := newTestRepository(t, s.RemoteURL)

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

	var templateDir string
	for _, path := range templatePaths {
		if !strings.HasSuffix(path, ".md") {
			templateDir = path
			break
		}
	}

	t.Run("TemplatesPresent", func(t *testing.T) {
		var emptyTemplateName, nonEmptyTemplateName string
		if templateDir != "" {
			emptyTemplateName = fixturetest.New(s.Fixtures, "empty-template", func() string {
				return randomString(8) + ".md"
			}).Get(t)
			nonEmptyTemplateName = fixturetest.New(s.Fixtures, "non-empty-template", func() string {
				return randomString(8) + ".md"
			}).Get(t)
			t.Logf("Creating templates: %s (empty), %s (non-empty)",
				emptyTemplateName, nonEmptyTemplateName)
		}

		if Update() {
			testRepo := newTestRepository(t, s.RemoteURL)

			if templateDir != "" {
				testRepo.WriteFile(filepath.Join(templateDir, emptyTemplateName))
				t.Logf("Created empty template at: %s",
					filepath.Join(templateDir, emptyTemplateName))

				testRepo.WriteFile(
					filepath.Join(templateDir, nonEmptyTemplateName),
					"This is a test template")
				t.Logf("Created non-empty template at: %s",
					filepath.Join(templateDir, nonEmptyTemplateName))
			} else {
				testRepo.WriteFile(templatePaths[0], "This is a test template")
				t.Logf("Created template at: %s", templatePaths[0])
			}

			testRepo.AddAllAndCommit("Add templates")
			testRepo.Push("main")
		}

		ctx := t.Context()
		repo := s.OpenRepository(t)
		templates, err := repo.ListChangeTemplates(ctx)
		require.NoError(t, err)

		// Find our test templates in the results.
		var foundEmpty, foundNonEmpty, foundSingleFile bool
		for _, template := range templates {
			// Template names may not have extensions depending on the forge.
			templateName := strings.TrimSuffix(template.Filename, ".md") + ".md"

			switch templateName {
			case filepath.Base(templatePaths[0]):
				foundSingleFile = true
				assert.Equal(t,
					strings.TrimSpace("This is a test template"),
					strings.TrimSpace(template.Body),
					"template should have correct body")

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

		if templateDir == "" {
			assert.True(t, foundSingleFile, "template not found in results")
		} else {
			assert.True(t, foundEmpty, "empty template not found in results")
			assert.True(t, foundNonEmpty, "non-empty template not found in results")
		}
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
