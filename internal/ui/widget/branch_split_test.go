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

// Runs tests inside testdata/script/branch_split.
// The following files are expected:
//
//   - commits: JSON describing input commit summaries
//   - want: expected selected commits (list of git hashes as JSON)
//   - head (optional): name of the HEAD commit
//   - desc (optional): description for the prompt
func TestBranchSplit_Script(t *testing.T) {
	stub.Func(&_timeNow, time.Date(2024, 12, 11, 10, 9, 8, 7, time.UTC))

	uitest.RunScripts(t,
		func(t testing.TB, ts *testscript.TestScript, view ui.InteractiveView) {
			var input []CommitSummary
			require.NoError(t,
				json.Unmarshal([]byte(ts.ReadFile("commits")), &input),
				"read 'commits' file")

			var want []git.Hash
			require.NoError(t,
				json.Unmarshal([]byte(ts.ReadFile("want")), &want),
				"read 'want' file")

			var head string
			if _, err := os.Stat(ts.MkAbs("head")); err == nil {
				head = strings.TrimSpace(ts.ReadFile("head"))
			}

			var desc string
			if _, err := os.Stat(ts.MkAbs("desc")); err == nil {
				desc = strings.TrimSpace(ts.ReadFile("desc"))
			}

			commits := make([]CommitSummary, len(input))
			for i, c := range input {
				commits[i] = CommitSummary(c)
			}

			widget := NewBranchSplit().
				WithTitle("Select a commit").
				WithCommits(commits...).
				WithDescription(desc).
				WithHEAD(head)

			assert.NoError(t, ui.Run(view, widget))

			var got []git.Hash
			for _, idx := range widget.Selected() {
				got = append(got, input[idx].ShortHash)
			}

			assert.Equal(t, want, got)
		},
		&uitest.RunScriptsOptions{
			Update: *UpdateFixtures,
		},
		"testdata/script/branch_split",
	)
}
