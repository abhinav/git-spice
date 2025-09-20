package git_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/text"
)

func TestWorktree_CheckoutFiles(t *testing.T) {
	t.Parallel()

	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		git init
		git add init.txt
		git commit -m 'Initial commit'

		git checkout -b feature
		git add feature.txt
		git commit -m 'Add feature'
		cp modified-init.txt init.txt

		git checkout main
		cp main-init.txt init.txt

		-- init.txt --
		Initial content

		-- feature.txt --
		Feature content

		-- modified-init.txt --
		Modified init

		-- main-init.txt --
		Main init

	`)))
	require.NoError(t, err)

	wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	t.Run("CheckoutFromIndex", func(t *testing.T) {
		require.NoError(t, wt.CheckoutFiles(t.Context(), &git.CheckoutFilesRequest{
			Pathspecs: []string{"init.txt"},
		}))

		content, err := os.ReadFile(filepath.Join(fixture.Dir(), "init.txt"))
		require.NoError(t, err)
		assert.Equal(t, "Initial content", strings.TrimSpace(string(content)))
	})

	t.Run("CheckoutFromBranch", func(t *testing.T) {
		require.NoError(t, wt.CheckoutFiles(t.Context(), &git.CheckoutFilesRequest{
			TreeIsh:   "feature",
			Pathspecs: []string{"feature.txt"},
		}))

		content, err := os.ReadFile(filepath.Join(fixture.Dir(), "feature.txt"))
		require.NoError(t, err)
		assert.Equal(t, "Feature content", strings.TrimSpace(string(content)))
	})
}
