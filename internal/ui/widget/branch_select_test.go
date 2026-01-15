package widget

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/uitest"
)

// Runs tests inside testdata/script/branch_select.
// The following files are expected:
//
//   - want: name of the branch expected to be selected at the end
//   - branches: branches available in the list. See below for format.
//   - desc (optional): prompt description
//   - home (optional): user's home directory
//   - worktree (optional): current worktree
//
// The branches file is a JSON-encoded file with the format:
//
//	[
//		{branch: string, base: string?, disabled: bool?},
//		...
//	]
func TestBranchTreeSelect_Script(t *testing.T) {
	uitest.RunScripts(t,
		func(t testing.TB, ts *testscript.TestScript, view ui.InteractiveView) {
			wantBranch := strings.TrimSpace(ts.ReadFile("want"))

			var input []BranchTreeItem
			require.NoError(t,
				json.Unmarshal([]byte(ts.ReadFile("branches")), &input),
				"read 'branches' file")

			desc := readOptionalFile(ts, "desc")

			if home := readOptionalFile(ts, "home"); home != "" {
				ts.Setenv("HOME", home)
			}

			worktree := readOptionalFile(ts, "worktree")

			var gotBranch string
			widget := NewBranchTreeSelect().
				WithTitle("Select a branch").
				WithItems(input...).
				WithDescription(desc).
				WithValue(&gotBranch).
				WithCurrentWorktree(worktree)

			assert.NoError(t, ui.Run(view, widget))
			assert.Equal(t, wantBranch, gotBranch)
		},
		&uitest.RunScriptsOptions{
			Update: *UpdateFixtures,
		},
		"testdata/script/branch_tree_select",
	)
}

func readOptionalFile(ts *testscript.TestScript, path string) string {
	if _, err := os.Stat(ts.MkAbs(path)); err == nil {
		return strings.TrimSpace(ts.ReadFile(path))
	}
	return ""
}
