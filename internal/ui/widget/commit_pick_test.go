package widget

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/testscript"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/uitest"
	"go.abhg.dev/testing/stub"
)

// Expected files:
//
//   - branches: JSON list of input branches and their commits
//   - want: commit hash to expect as selected
//   - desc (optional): widget description
//   - give (optional): initial value of the hash
func TestCommitPick(t *testing.T) {
	stub.Func(&_timeNow, time.Date(2024, 12, 11, 10, 9, 8, 7, time.UTC))

	uitest.RunScripts(t, func(t testing.TB, ts *testscript.TestScript, view ui.InteractiveView) {
		var input []CommitPickBranch
		require.NoError(t,
			json.Unmarshal([]byte(ts.ReadFile("branches")), &input),
			"read 'branches' file")

		want := git.Hash(strings.TrimSpace(ts.ReadFile("want")))

		var desc string
		if _, err := os.Stat(ts.MkAbs("desc")); err == nil {
			desc = strings.TrimSpace(ts.ReadFile("desc"))
		}

		var give git.Hash
		if _, err := os.Stat(ts.MkAbs("give")); err == nil {
			give = git.Hash(strings.TrimSpace(ts.ReadFile("give")))
		}

		got := give
		widget := NewCommitPick().
			WithTitle("Select a commit").
			WithBranches(input...).
			WithDescription(desc).
			WithValue(&got)

		require.NoError(t, ui.Run(view, widget))
		assert.Equal(t, want, got)
	}, &uitest.RunScriptsOptions{Update: *UpdateFixtures}, "testdata/script/commit_pick")
}

func TestCommitPickErrors(t *testing.T) {
	t.Run("NoBranches", func(t *testing.T) {
		view := uitest.NewEmulatorView(nil)
		defer func() { _ = view.Close() }()

		err := ui.Run(view, NewCommitPick())
		require.Error(t, err)
		assert.ErrorContains(t, err, "no branches provided")
	})

	t.Run("NoCommits", func(t *testing.T) {
		view := uitest.NewEmulatorView(nil)
		defer func() { _ = view.Close() }()

		err := ui.Run(view, NewCommitPick().WithBranches(
			CommitPickBranch{Branch: "foo"},
			CommitPickBranch{Branch: "bar"},
		))
		require.Error(t, err)
		assert.ErrorContains(t, err, "no commits found")
	})
}
