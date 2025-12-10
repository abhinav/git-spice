package shamhub

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/fixturetest"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

var _fixtures = fixturetest.Config{Update: forgetest.Update}

func TestIntegration(t *testing.T) {
	t.Cleanup(func() {
		if t.Failed() && !forgetest.Update() {
			t.Logf("To update the test fixtures, run:")
			t.Logf("    go test -update -run '^%s$'", t.Name())
		}
	})

	// Store URLs in fixtures so they're consistent across test runs.
	apiURLFixture, setAPIURL := fixturetest.Stored[string](_fixtures, "apiURL")
	gitURLFixture, setGitURL := fixturetest.Stored[string](_fixtures, "gitURL")
	repoURLFixture, setRepoURL := fixturetest.Stored[string](_fixtures, "repoURL")
	tokenFixture, setToken := fixturetest.Stored[string](_fixtures, "token")

	var shamhub *ShamHub // non-nil only in update mode
	if forgetest.Update() {
		var err error
		shamhub, err = New(Config{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, shamhub.Close())
		})

		apiURL := shamhub.APIURL()
		gitURL := shamhub.GitURL()
		setAPIURL(apiURL)
		setGitURL(gitURL)

		repoURL, err := shamhub.NewRepository("abhinav", "test-repo")
		require.NoError(t, err)
		t.Logf("Created test repository at %s", repoURL)
		setRepoURL(repoURL)

		// Add initial commit to the repository so tests can create branches.
		func() {
			repoDir := t.TempDir()
			cmd := exec.Command("git", "clone", repoURL, repoDir)
			cmd.Stdout = t.Output()
			cmd.Stderr = t.Output()
			require.NoError(t, cmd.Run(), "failed to clone repository")

			work, err := git.OpenWorktree(t.Context(), repoDir, git.OpenOptions{
				Log: silogtest.New(t),
			})
			require.NoError(t, err, "failed to open worktree")

			// Create initial file and commit.
			require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# test-repo\n"), 0o644))

			// Stage the file.
			addCmd := exec.Command("git", "add", ".")
			addCmd.Dir = repoDir
			addCmd.Stdout = t.Output()
			addCmd.Stderr = t.Output()
			require.NoError(t, addCmd.Run(), "failed to stage file")

			require.NoError(t, work.Commit(t.Context(), git.CommitRequest{
				Message: "Initial commit",
				Author: &git.Signature{
					Name:  "Test User",
					Email: "test@example.com",
				},
				Committer: &git.Signature{
					Name:  "Test User",
					Email: "test@example.com",
				},
			}), "failed to commit")

			// Push to remote.
			require.NoError(t, work.Push(t.Context(), git.PushOptions{
				Remote:  "origin",
				Refspec: "main",
			}), "failed to push")
		}()

		// Register users for testing.
		require.NoError(t, shamhub.RegisterUser("test-user"))
		require.NoError(t, shamhub.RegisterUser("reviewer-user"))
		require.NoError(t, shamhub.RegisterUser("assignee-user"))

		// Issue token for test-user.
		token, err := shamhub.IssueToken("test-user")
		require.NoError(t, err)
		setToken(token)

	}

	apiURL := apiURLFixture.Get(t)
	gitURL := gitURLFixture.Get(t)
	repoURL := repoURLFixture.Get(t)
	token := tokenFixture.Get(t)

	shamForge := &Forge{
		Options: Options{
			URL:    gitURL,
			APIURL: apiURL,
		},
		Log: silogtest.New(t),
	}

	forgetest.RunIntegration(t, forgetest.IntegrationConfig{
		RemoteURL: repoURL,
		Forge:     shamForge,
		OpenRepository: func(t *testing.T, httpClient *http.Client) forge.Repository {
			repoID, err := shamForge.ParseRemoteURL(repoURL)
			require.NoError(t, err)

			repo, err := newRepository(
				shamForge,
				&AuthenticationToken{tok: token},
				repoID.(*RepositoryID),
				httpClient,
			)
			require.NoError(t, err)
			return repo
		},
		MergeChange: func(t *testing.T, repo forge.Repository, changeID forge.ChangeID) {
			if forgetest.Update() {
				r := repo.(*forgeRepository)
				require.NoError(t, shamhub.MergeChange(MergeChangeRequest{
					Owner:  r.owner,
					Repo:   r.repo,
					Number: int(changeID.(ChangeID)),
				}))
			}
		},
		CloseChange: func(t *testing.T, repo forge.Repository, changeID forge.ChangeID) {
			if forgetest.Update() {
				r := repo.(*forgeRepository)
				require.NoError(t, shamhub.RejectChange(RejectChangeRequest{
					Owner:  r.owner,
					Repo:   r.repo,
					Number: int(changeID.(ChangeID)),
				}))
			}
		},
		SetCommentsPageSize: SetListChangeCommentsPageSize,
		Reviewers:           []string{"reviewer-user"},
		Assignees:           []string{"assignee-user"},
	})
}
